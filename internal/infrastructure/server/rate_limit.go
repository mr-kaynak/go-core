package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	identityDomain "github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// rateLimitWindow is the fixed window all rate-limit classes share.
const rateLimitWindow = time.Minute

// sseStreamPath is the long-lived SSE endpoint exempt from rate limiting and
// timeouts (also referenced by the middleware skips in server.go).
const sseStreamPath = "/api/v1/notifications/stream"

// endpointClass identifies which limit and keying strategy applies to a request.
type endpointClass int

const (
	// classDefault applies the general per-identity limit (falls back to IP for
	// anonymous requests).
	classDefault endpointClass = iota
	// classAuth applies the strict authentication limit, always keyed by IP
	// regardless of any identity presented, to blunt credential stuffing and
	// account enumeration from a single source.
	classAuth
)

// authPaths are the unauthenticated authentication endpoints subject to the
// strict auth-class limit. These mirror the public auth routes registered by
// the identity module; keep in sync with auth.PublicPaths (auth subset).
var authPaths = map[string]struct{}{
	"/api/v1/auth/register":               {},
	"/api/v1/auth/login":                  {},
	"/api/v1/auth/2fa/validate":           {},
	"/api/v1/auth/refresh":                {},
	"/api/v1/auth/resend-verification":    {},
	"/api/v1/auth/request-password-reset": {},
	"/api/v1/auth/reset-password":         {},
}

// classify returns the endpoint class for a request path.
func classify(path string) endpointClass {
	if _, ok := authPaths[path]; ok {
		return classAuth
	}
	return classDefault
}

// rateLimitVerifier resolves presented credentials to a VERIFIED identity.
// Only verified identities may select their own rate-limit bucket: an
// unverified token or API key is trivially rotated, and letting it pick a key
// would hand an attacker an unbounded number of fresh buckets from one IP.
type rateLimitVerifier interface {
	// VerifyBearer returns the subject of a valid access token.
	VerifyBearer(ctx context.Context, token string) (subject string, ok bool)
	// VerifyAPIKey returns the id of a valid (unrevoked, unexpired) API key.
	VerifyAPIKey(ctx context.Context, rawKey string) (keyID string, ok bool)
}

// rateLimitIdentity is the production rateLimitVerifier. The limiter is
// registered before the identity module is wired, so it is created empty and
// bound later via bind(); until then every credential is unverified and keys
// by IP. Reads and the single bind are guarded so the order is safe even if
// a request slips in during startup.
type rateLimitIdentity struct {
	mu     sync.RWMutex
	tokens *identityService.TokenService
	keys   identityRepo.APIKeyRepository
}

func (r *rateLimitIdentity) bind(tokens *identityService.TokenService, keys identityRepo.APIKeyRepository) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens = tokens
	r.keys = keys
}

func (r *rateLimitIdentity) VerifyBearer(ctx context.Context, token string) (string, bool) {
	r.mu.RLock()
	tokens := r.tokens
	r.mu.RUnlock()
	if tokens == nil {
		return "", false
	}
	claims, err := tokens.ValidateAccessToken(ctx, token)
	if err != nil {
		return "", false
	}
	return claims.UserID.String(), true
}

// VerifyAPIKey looks the key up by hash without side effects (no last-used
// write, no owner check) — the auth middleware does the full validation; here
// we only need to know the key is real so its bucket cannot be forged.
func (r *rateLimitIdentity) VerifyAPIKey(ctx context.Context, rawKey string) (string, bool) {
	r.mu.RLock()
	keys := r.keys
	r.mu.RUnlock()
	if keys == nil {
		return "", false
	}
	key, err := keys.GetByHash(ctx, identityDomain.HashAPIKey(rawKey))
	if err != nil || key == nil || !key.IsValid() {
		return "", false
	}
	return key.ID.String(), true
}

// rateLimitKey derives the Redis rate-limit key and human-readable limit for a
// request. For the auth class the key is always ip-scoped. For the default
// class the key prefers a VERIFIED caller identity (API key id, then JWT
// subject) and falls back to the client IP for anonymous or unverified
// traffic. A nil verifier treats every credential as unverified.
func rateLimitKey(c fiber.Ctx, class endpointClass, cfg *config.Config, v rateLimitVerifier) (key string, limit int) {
	ip := c.IP()

	if class == classAuth {
		return "auth:" + ip, cfg.RateLimit.AuthPerMinute
	}

	if v != nil {
		if apiKey := c.Get("X-API-Key"); apiKey != "" {
			if id, ok := v.VerifyAPIKey(c.Context(), apiKey); ok {
				return "apikey:" + id, cfg.RateLimit.PerMinute
			}
		}
		if token := bearerToken(c.Get("Authorization")); token != "" {
			if sub, ok := v.VerifyBearer(c.Context(), token); ok {
				return "user:" + sub, cfg.RateLimit.PerMinute
			}
		}
	}

	return "ip:" + ip, cfg.RateLimit.PerMinute
}

// bearerToken extracts the token from a "Bearer <token>" header, or "" if the
// header is absent or uses another scheme.
func bearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// rateLimitStore is the storage the limiter needs: an atomic fixed-window
// increment plus a small cache of verification outcomes. *cache.RateLimiter
// satisfies it; tests use an in-memory fake.
type rateLimitStore interface {
	AllowN(ctx context.Context, key string, maxTokens int, expiration time.Duration) (cache.Decision, error)
	GetVerified(ctx context.Context, credentialHash string) (identity string, found bool, err error)
	SetVerified(ctx context.Context, credentialHash, identity string, ttl time.Duration) error
}

// verifyTimeout bounds a single credential verification (JWT parse + blacklist
// check, or API-key lookup) so a slow backend cannot hold requests open. A
// variable so tests can shorten it.
var verifyTimeout = 2 * time.Second

// storeTimeout bounds each individual Redis operation. Every operation gets a
// FRESH deadline: a verification that ran to its own timeout must not leave
// the subsequent counter update with an already-expired context (which would
// fail open).
const storeTimeout = 2 * time.Second

// verifyCacheTTL is how long a verification outcome is remembered. Bucket
// selection is not an authorization decision, so a stale positive for up to
// one window after logout/revocation is harmless; the auth middleware still
// rejects the request.
const verifyCacheTTL = rateLimitWindow

// unverifiedMarker is cached for credentials that failed verification.
const unverifiedMarker = "!"

func withStoreTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, storeTimeout)
}

// newRateLimitMiddleware builds the identity-aware, class-based rate limiter.
// When rc is non-nil it uses the shared Redis-backed fixed-window limiter so the
// budget is enforced across all instances. When rc is nil (Redis unavailable at
// startup) it returns nil and the caller falls back to the in-memory Fiber
// limiter to preserve the prior per-instance degraded behavior.
func newRateLimitMiddleware(cfg *config.Config, rc *cache.RedisClient, v rateLimitVerifier) fiber.Handler {
	if rc == nil {
		return nil
	}
	return newRateLimitHandler(cfg, cache.NewRateLimiter(rc), v)
}

// newRateLimitHandler is the storage-agnostic core of the limiter.
//
// Credential verification costs a backend round-trip, so it is guarded twice
// before any lookup happens (see budgetedVerifier): the outcome for a given
// credential is cached for one window, and uncached verifications are admitted
// from a per-IP budget that is reserved atomically BEFORE the lookup. Replaying
// one credential therefore costs one lookup per window, and rotating
// credentials costs at most PerMinute lookups per IP per window — whether or
// not the request itself ends up allowed.
func newRateLimitHandler(cfg *config.Config, st rateLimitStore, v rateLimitVerifier) fiber.Handler {
	return func(c fiber.Ctx) error {
		// Skip long-lived SSE streaming connections, matching prior behavior.
		if c.Path() == sseStreamPath {
			return c.Next()
		}

		class := classify(c.Path())
		var verifier rateLimitVerifier
		var budgeted *budgetedVerifier
		if v != nil && class == classDefault {
			budgeted = &budgetedVerifier{
				store:  st,
				inner:  v,
				parent: c.Context(),
				budget: "verify:" + c.IP(),
				limit:  cfg.RateLimit.PerMinute,
			}
			verifier = budgeted
		}
		key, limit := rateLimitKey(c, class, cfg, verifier)

		// Admission denial is itself a rate-limit verdict. Falling back to the
		// IP bucket here would hand a caller whose identity bucket is exhausted a
		// fresh allowance (burn the budget with garbage keys, then present an
		// uncached valid token), so the request is rejected outright.
		if budgeted != nil && budgeted.denied {
			retryAfter := budgeted.retryAfter
			if retryAfter <= 0 {
				retryAfter = rateLimitWindow
			}
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(retryAfter.Seconds())))
			return errors.NewRateLimitExceeded(cfg.RateLimit.PerMinute)
		}

		ctx, cancel := withStoreTimeout(c.Context())
		defer cancel()
		decision, err := st.AllowN(ctx, key, limit, rateLimitWindow)
		if err != nil {
			// Fail open: a Redis error must not take down request handling. The
			// circuit breaker inside RedisClient already logs the failure.
			logger.Get().Warn("Rate limiter unavailable; allowing request", "error", err)
			return c.Next()
		}

		setRateLimitHeaders(c, decision, rateLimitWindow)

		if !decision.Allowed {
			retryAfter := decision.ResetAfter
			if retryAfter <= 0 {
				retryAfter = rateLimitWindow
			}
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(retryAfter.Seconds())))
			return errors.NewRateLimitExceeded(limit)
		}

		return c.Next()
	}
}

// budgetedVerifier wraps a rateLimitVerifier with a verification cache and a
// per-IP admission budget so that presenting credentials cannot generate
// unbounded backend work.
type budgetedVerifier struct {
	store  rateLimitStore
	inner  rateLimitVerifier
	parent context.Context
	budget string // per-IP admission bucket for uncached verifications
	limit  int

	// denied is set when an uncached credential could not be verified because
	// the admission budget is exhausted. The handler turns this into a 429;
	// it is distinct from "credential invalid", which simply keys by IP.
	denied     bool
	retryAfter time.Duration
}

func (b *budgetedVerifier) VerifyAPIKey(_ context.Context, rawKey string) (string, bool) {
	return b.lookup("k", rawKey, func(ctx context.Context) (string, bool) {
		return b.inner.VerifyAPIKey(ctx, rawKey)
	})
}

func (b *budgetedVerifier) VerifyBearer(_ context.Context, token string) (string, bool) {
	return b.lookup("b", token, func(ctx context.Context) (string, bool) {
		return b.inner.VerifyBearer(ctx, token)
	})
}

// lookup answers from the cache when it can; otherwise it reserves one unit
// of the admission budget, runs the verification under its own deadline, and
// caches the outcome (positive or negative). Every store operation gets a
// fresh deadline. Budget exhaustion marks the verifier denied (the handler
// rejects the request); store errors degrade to "unverified" — IP keying.
func (b *budgetedVerifier) lookup(kind, credential string, verify func(context.Context) (string, bool)) (string, bool) {
	hash := credentialHash(kind, credential)

	ctx, cancel := withStoreTimeout(b.parent)
	cached, found, err := b.store.GetVerified(ctx, hash)
	cancel()
	if err == nil && found {
		if cached == unverifiedMarker {
			return "", false
		}
		return cached, true
	}

	ctx, cancel = withStoreTimeout(b.parent)
	admit, err := b.store.AllowN(ctx, b.budget, b.limit, rateLimitWindow)
	cancel()
	if err != nil {
		// Store failure: degrade to IP keying, consistent with the limiter's
		// fail-open policy on Redis errors.
		return "", false
	}
	if !admit.Allowed {
		b.denied = true
		b.retryAfter = admit.ResetAfter
		return "", false
	}

	vctx, vcancel := context.WithTimeout(b.parent, verifyTimeout)
	identity, ok := verify(vctx)
	vcancel()

	stored := unverifiedMarker
	if ok {
		stored = identity
	}
	ctx, cancel = withStoreTimeout(b.parent)
	_ = b.store.SetVerified(ctx, hash, stored, verifyCacheTTL)
	cancel()

	return identity, ok
}

// credentialHash derives a cache key from a credential without storing the
// secret itself. kind separates API keys from bearer tokens.
func credentialHash(kind, credential string) string {
	sum := sha256.Sum256([]byte(kind + ":" + credential))
	return hex.EncodeToString(sum[:])
}

// setRateLimitHeaders emits the standard advisory headers on every response.
func setRateLimitHeaders(c fiber.Ctx, d cache.Decision, window time.Duration) {
	reset := d.ResetAfter
	if reset <= 0 {
		reset = window
	}
	c.Set("X-RateLimit-Limit", strconv.Itoa(d.Limit))
	c.Set("X-RateLimit-Remaining", strconv.Itoa(d.Remaining))
	c.Set("X-RateLimit-Reset", strconv.Itoa(int(reset.Seconds())))
}
