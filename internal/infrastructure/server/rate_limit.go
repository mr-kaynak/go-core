package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
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

// rateLimitKey derives the Redis rate-limit key and human-readable limit for a
// request. For the auth class the key is always ip-scoped. For the default
// class the key prefers a caller identity (API key, then JWT subject) and falls
// back to the client IP for anonymous traffic.
//
// Identity extraction here is for KEYING ONLY. The JWT subject is read without
// signature verification, so it must never be used for authorization — it only
// selects a rate-limit bucket. A forged token can at most move an attacker into
// a different (still limited) bucket, never grant access.
func rateLimitKey(c fiber.Ctx, class endpointClass, cfg *config.Config) (key string, limit int) {
	ip := c.IP()

	if class == classAuth {
		return "auth:" + ip, cfg.RateLimit.AuthPerMinute
	}

	if apiKey := c.Get("X-API-Key"); apiKey != "" {
		return "apikey:" + hashIdentifier(apiKey), cfg.RateLimit.PerMinute
	}

	if sub := subjectFromBearer(c.Get("Authorization")); sub != "" {
		return "user:" + sub, cfg.RateLimit.PerMinute
	}

	return "ip:" + ip, cfg.RateLimit.PerMinute
}

// hashIdentifier returns a short, stable, non-reversible fragment of an opaque
// credential so raw API keys never land in Redis key names or logs. Collision
// resistance is not security-critical here — a hash collision merely shares a
// rate-limit bucket — so a fast non-cryptographic hash is sufficient.
func hashIdentifier(v string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(v))
	return strconv.FormatUint(h.Sum64(), 36)
}

// subjectFromBearer extracts the "sub" claim from a Bearer JWT WITHOUT verifying
// its signature. Returns "" if the header is absent or malformed. This is safe
// only because the result is used purely as a rate-limit bucket selector.
func subjectFromBearer(authHeader string) string {
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	segments := strings.Split(strings.TrimSpace(parts[1]), ".")
	if len(segments) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}

// newRateLimitMiddleware builds the identity-aware, class-based rate limiter.
// When rc is non-nil it uses the shared Redis-backed fixed-window limiter so the
// budget is enforced across all instances. When rc is nil (Redis unavailable at
// startup) it returns nil and the caller falls back to the in-memory Fiber
// limiter to preserve the prior per-instance degraded behavior.
func newRateLimitMiddleware(cfg *config.Config, rc *cache.RedisClient) fiber.Handler {
	if rc == nil {
		return nil
	}
	rl := cache.NewRateLimiter(rc)

	return func(c fiber.Ctx) error {
		// Skip long-lived SSE streaming connections, matching prior behavior.
		if c.Path() == sseStreamPath {
			return c.Next()
		}

		class := classify(c.Path())
		key, limit := rateLimitKey(c, class, cfg)

		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()

		decision, err := rl.AllowN(ctx, key, limit, rateLimitWindow)
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
