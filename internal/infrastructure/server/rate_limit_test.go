package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
)

// makeUnsignedJWT builds a syntactically valid JWT (header.payload.signature)
// carrying a "sub" claim but a bogus signature. The limiter must NOT trust it:
// an attacker can mint unlimited such tokens to open fresh buckets.
func makeUnsignedJWT(sub string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, _ := json.Marshal(map[string]string{"sub": sub})
	payload := base64.RawURLEncoding.EncodeToString(body)
	return header + "." + payload + ".ZmFrZXNpZw"
}

// verifierStub resolves only the credentials it was told about; everything
// else is unverified, exactly like a forged token or a random API key.
type verifierStub struct {
	bearers map[string]string // raw token -> subject
	keys    map[string]string // raw api key -> key id
}

func (v verifierStub) VerifyBearer(_ context.Context, token string) (string, bool) {
	sub, ok := v.bearers[token]
	return sub, ok
}

func (v verifierStub) VerifyAPIKey(_ context.Context, rawKey string) (string, bool) {
	id, ok := v.keys[rawKey]
	return id, ok
}

func testRateLimitCfg() *config.Config {
	return &config.Config{RateLimit: config.RateLimitConfig{PerMinute: 60, AuthPerMinute: 10, Burst: 10}}
}

// captureKey runs rateLimitKey inside a real Fiber request context so that
// c.IP(), c.Path() and c.Get() behave exactly as in production.
func captureKey(t *testing.T, path string, headers map[string]string, v rateLimitVerifier) (key string, limit int) {
	t.Helper()
	app := fiber.New()
	cfg := testRateLimitCfg()

	app.All("/*", func(c fiber.Ctx) error {
		key, limit = rateLimitKey(c, classify(c.Path()), cfg, v)
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	return key, limit
}

func TestClassifyAuthEndpoints(t *testing.T) {
	authEndpoints := []string{
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/auth/2fa/validate",
		"/api/v1/auth/request-password-reset",
		"/api/v1/auth/reset-password",
		"/api/v1/auth/resend-verification",
		"/api/v1/auth/refresh",
	}
	for _, p := range authEndpoints {
		if classify(p) != classAuth {
			t.Errorf("expected %s to be classAuth", p)
		}
	}

	defaultEndpoints := []string{
		"/api/v1/blog/posts",
		"/api/v1/users/me",
		"/api/v1/auth/logout", // authenticated, not in the strict set
		"/api/v1/notifications",
	}
	for _, p := range defaultEndpoints {
		if classify(p) != classDefault {
			t.Errorf("expected %s to be classDefault", p)
		}
	}
}

// TestKeyPerIdentityIsolation verifies two different VERIFIED users hitting the
// same default endpoint (same source IP in-test) get distinct rate-limit keys.
func TestKeyPerIdentityIsolation(t *testing.T) {
	v := verifierStub{bearers: map[string]string{"tok-A": "user-A", "tok-B": "user-B"}}
	keyA, limitA := captureKey(t, "/api/v1/blog/posts", map[string]string{"Authorization": "Bearer tok-A"}, v)
	keyB, limitB := captureKey(t, "/api/v1/blog/posts", map[string]string{"Authorization": "Bearer tok-B"}, v)

	if keyA != "user:user-A" || keyB != "user:user-B" {
		t.Errorf("unexpected keys: A=%q B=%q", keyA, keyB)
	}
	if limitA != 60 || limitB != 60 {
		t.Errorf("expected default limit 60, got A=%d B=%d", limitA, limitB)
	}
}

// TestKeyUnverifiedBearerFallsBackToIP: a token that does not verify must not
// select its own bucket — otherwise minting tokens with fresh "sub" claims
// gives an attacker an unbounded number of buckets from one IP.
func TestKeyUnverifiedBearerFallsBackToIP(t *testing.T) {
	v := verifierStub{bearers: map[string]string{}}
	anon, _ := captureKey(t, "/api/v1/blog/posts", nil, v)
	forgedA, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"Authorization": "Bearer " + makeUnsignedJWT("a")}, v)
	forgedB, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"Authorization": "Bearer " + makeUnsignedJWT("b")}, v)

	if !strings.HasPrefix(anon, "ip:") {
		t.Fatalf("expected ip-scoped anonymous key, got %q", anon)
	}
	if forgedA != anon || forgedB != anon {
		t.Errorf("unverified bearers must share the IP bucket: anon=%q A=%q B=%q", anon, forgedA, forgedB)
	}
}

// TestKeyAnonymousFallsBackToIP verifies requests without any identity are keyed
// by IP under the default class.
func TestKeyAnonymousFallsBackToIP(t *testing.T) {
	key, limit := captureKey(t, "/api/v1/blog/posts", nil, verifierStub{})
	if !strings.HasPrefix(key, "ip:") {
		t.Errorf("expected ip-scoped key for anonymous request, got %q", key)
	}
	if limit != 60 {
		t.Errorf("expected default limit 60, got %d", limit)
	}
}

// TestKeyAPIKeyIdentity verifies VERIFIED API-key requests are keyed by the
// key's id, isolating distinct API keys sharing one IP, without leaking the
// raw secret into the key.
func TestKeyAPIKeyIdentity(t *testing.T) {
	v := verifierStub{keys: map[string]string{"secret-key-A": "id-A", "secret-key-B": "id-B"}}
	keyA, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "secret-key-A"}, v)
	keyB, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "secret-key-B"}, v)

	if keyA != "apikey:id-A" || keyB != "apikey:id-B" {
		t.Errorf("unexpected keys: A=%q B=%q", keyA, keyB)
	}
	if strings.Contains(keyA, "secret-key-A") || strings.Contains(keyB, "secret-key-B") {
		t.Errorf("raw API key leaked into rate-limit key: A=%q B=%q", keyA, keyB)
	}
}

// TestKeyUnverifiedAPIKeyFallsBackToIP: random X-API-Key values must not each
// get a fresh bucket.
func TestKeyUnverifiedAPIKeyFallsBackToIP(t *testing.T) {
	v := verifierStub{keys: map[string]string{}}
	anon, _ := captureKey(t, "/api/v1/blog/posts", nil, v)
	garbageA, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "nope-1"}, v)
	garbageB, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "nope-2"}, v)

	if garbageA != anon || garbageB != anon {
		t.Errorf("unverified API keys must share the IP bucket: anon=%q A=%q B=%q", anon, garbageA, garbageB)
	}
}

// TestKeyNoVerifierFallsBackToIP: before identity services are wired (or if
// they never are) the limiter must treat every credential as unverified.
func TestKeyNoVerifierFallsBackToIP(t *testing.T) {
	anon, _ := captureKey(t, "/api/v1/blog/posts", nil, nil)
	bearer, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"Authorization": "Bearer tok"}, nil)
	apiKey, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "k"}, nil)
	if bearer != anon || apiKey != anon {
		t.Errorf("nil verifier must key by IP: anon=%q bearer=%q apikey=%q", anon, bearer, apiKey)
	}
}

// TestKeyAuthClassAlwaysIP verifies the auth class is keyed by IP regardless of
// any identity presented — even a VERIFIED token must not let an attacker
// escape the strict per-IP auth budget.
func TestKeyAuthClassAlwaysIP(t *testing.T) {
	v := verifierStub{bearers: map[string]string{"tok": "attacker"}}
	keyTokened, limit := captureKey(t, "/api/v1/auth/login", map[string]string{"Authorization": "Bearer tok"}, v)
	keyAnon, _ := captureKey(t, "/api/v1/auth/login", nil, v)

	if keyTokened != keyAnon {
		t.Errorf("auth class must key by IP regardless of identity: tokened=%q anon=%q", keyTokened, keyAnon)
	}
	if !strings.HasPrefix(keyTokened, "auth:") {
		t.Errorf("expected auth-scoped key, got %q", keyTokened)
	}
	if limit != 10 {
		t.Errorf("expected strict auth limit 10, got %d", limit)
	}
}
