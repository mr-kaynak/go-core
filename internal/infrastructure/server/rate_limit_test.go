package server

import (
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
// with the given subject. The signature is a dummy value — subjectFromBearer
// must not verify it, so any placeholder works.
func makeUnsignedJWT(sub string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, _ := json.Marshal(map[string]string{"sub": sub})
	payload := base64.RawURLEncoding.EncodeToString(body)
	return header + "." + payload + ".ZmFrZXNpZw"
}

func testRateLimitCfg() *config.Config {
	return &config.Config{RateLimit: config.RateLimitConfig{PerMinute: 60, AuthPerMinute: 10, Burst: 10}}
}

// captureKey runs rateLimitKey inside a real Fiber request context so that
// c.IP(), c.Path() and c.Get() behave exactly as in production.
func captureKey(t *testing.T, path string, headers map[string]string) (key string, limit int) {
	t.Helper()
	app := fiber.New()
	cfg := testRateLimitCfg()

	app.All("/*", func(c fiber.Ctx) error {
		key, limit = rateLimitKey(c, classify(c.Path()), cfg)
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, path, nil)
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

func TestSubjectFromBearer(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid bearer", "Bearer " + makeUnsignedJWT("user-123"), "user-123"},
		{"lowercase scheme", "bearer " + makeUnsignedJWT("user-456"), "user-456"},
		{"empty header", "", ""},
		{"no bearer prefix", makeUnsignedJWT("user-789"), ""},
		{"wrong scheme", "Basic abc", ""},
		{"malformed token (two segments)", "Bearer a.b", ""},
		{"garbage payload", "Bearer aaa.!!!.ccc", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subjectFromBearer(tt.header); got != tt.want {
				t.Errorf("subjectFromBearer(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// TestKeyPerIdentityIsolation verifies two different authenticated users hitting
// the same default endpoint (and thus the same source IP in-test) get distinct
// rate-limit keys — the core requirement that identity, not IP, is the bucket.
func TestKeyPerIdentityIsolation(t *testing.T) {
	keyA, limitA := captureKey(t, "/api/v1/blog/posts", map[string]string{
		"Authorization": "Bearer " + makeUnsignedJWT("user-A"),
	})
	keyB, limitB := captureKey(t, "/api/v1/blog/posts", map[string]string{
		"Authorization": "Bearer " + makeUnsignedJWT("user-B"),
	})

	if keyA == keyB {
		t.Fatalf("expected distinct keys for two users, both got %q", keyA)
	}
	if keyA != "user:user-A" || keyB != "user:user-B" {
		t.Errorf("unexpected keys: A=%q B=%q", keyA, keyB)
	}
	if limitA != 60 || limitB != 60 {
		t.Errorf("expected default limit 60, got A=%d B=%d", limitA, limitB)
	}
}

// TestKeyAnonymousFallsBackToIP verifies requests without any identity are keyed
// by IP under the default class.
func TestKeyAnonymousFallsBackToIP(t *testing.T) {
	key, limit := captureKey(t, "/api/v1/blog/posts", nil)
	if !strings.HasPrefix(key, "ip:") {
		t.Errorf("expected ip-scoped key for anonymous request, got %q", key)
	}
	if limit != 60 {
		t.Errorf("expected default limit 60, got %d", limit)
	}
}

// TestKeyAPIKeyIdentity verifies API-key requests are keyed by the (hashed) key,
// isolating distinct API keys sharing one IP.
func TestKeyAPIKeyIdentity(t *testing.T) {
	keyA, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "secret-key-A"})
	keyB, _ := captureKey(t, "/api/v1/blog/posts", map[string]string{"X-API-Key": "secret-key-B"})

	if keyA == keyB {
		t.Fatalf("expected distinct keys for two API keys, both got %q", keyA)
	}
	for _, k := range []string{keyA, keyB} {
		if !strings.HasPrefix(k, "apikey:") {
			t.Errorf("expected apikey-scoped key, got %q", k)
		}
	}
	// Raw secret must never appear in the key.
	if strings.Contains(keyA, "secret-key-A") || strings.Contains(keyB, "secret-key-B") {
		t.Errorf("raw API key leaked into rate-limit key: A=%q B=%q", keyA, keyB)
	}
}

// TestKeyAuthClassAlwaysIP verifies the auth class is keyed by IP regardless of
// any identity presented — a token or API key must NOT let an attacker escape
// the strict per-IP auth budget.
func TestKeyAuthClassAlwaysIP(t *testing.T) {
	// Even with a bearer token, the auth endpoint keys by IP.
	keyTokened, limit := captureKey(t, "/api/v1/auth/login", map[string]string{
		"Authorization": "Bearer " + makeUnsignedJWT("attacker"),
	})
	keyAnon, _ := captureKey(t, "/api/v1/auth/login", nil)

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

// TestNilRedisFallbackDisabled verifies the middleware constructor returns nil
// when Redis is unavailable, so the caller falls back to the in-memory limiter.
func TestNilRedisFallbackDisabled(t *testing.T) {
	if mw := newRateLimitMiddleware(testRateLimitCfg(), nil); mw != nil {
		t.Fatal("expected nil middleware when Redis is unavailable")
	}
}
