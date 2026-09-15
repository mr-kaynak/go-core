package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
)

// fakeStore is an in-memory rateLimitStore: fixed-window counters plus a
// verification cache. Like Redis, it fails when the context is already done.
type fakeStore struct {
	mu     sync.Mutex
	counts map[string]int
	cache  map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{counts: map[string]int{}, cache: map[string]string{}}
}

func (f *fakeStore) AllowN(ctx context.Context, key string, maxTokens int, _ time.Duration) (cache.Decision, error) {
	if err := ctx.Err(); err != nil {
		return cache.Decision{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[key]++
	n := f.counts[key]
	rem := maxTokens - n
	if rem < 0 {
		rem = 0
	}
	return cache.Decision{Allowed: n <= maxTokens, Limit: maxTokens, Remaining: rem}, nil
}

func (f *fakeStore) GetVerified(ctx context.Context, hash string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.cache[hash]
	return v, ok, nil
}

func (f *fakeStore) SetVerified(ctx context.Context, hash, identity string, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache[hash] = identity
	return nil
}

func (f *fakeStore) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[key]
}

// countingVerifier records lookups, whether each carried a deadline, and can
// be made slow to simulate a stalled backend.
type countingVerifier struct {
	mu           sync.Mutex
	calls        int
	deadlines    int
	delay        time.Duration
	validKeys    map[string]string
	validBearers map[string]string
}

func (v *countingVerifier) record(ctx context.Context) {
	v.mu.Lock()
	v.calls++
	if _, ok := ctx.Deadline(); ok {
		v.deadlines++
	}
	v.mu.Unlock()
	if v.delay > 0 {
		time.Sleep(v.delay)
	}
}

func (v *countingVerifier) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

func (v *countingVerifier) VerifyBearer(ctx context.Context, token string) (string, bool) {
	v.record(ctx)
	sub, ok := v.validBearers[token]
	return sub, ok
}

func (v *countingVerifier) VerifyAPIKey(ctx context.Context, raw string) (string, bool) {
	v.record(ctx)
	id, ok := v.validKeys[raw]
	return id, ok
}

func limitedApp(cfg *config.Config, st rateLimitStore, v rateLimitVerifier) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	app.Use(newRateLimitHandler(cfg, st, v))
	app.All("/*", func(c fiber.Ctx) error { return c.SendStatus(http.StatusOK) })
	return app
}

func hit(t *testing.T, app *fiber.App, headers map[string]string) int {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/blog/posts", nil)
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func cfgWith(perMinute int) *config.Config {
	return &config.Config{RateLimit: config.RateLimitConfig{PerMinute: perMinute, AuthPerMinute: 10}}
}

// TestReplayedInvalidCredentialIsLookedUpOnce: the same bad API key sent over
// and over costs ONE backend lookup; the negative result is cached.
func TestReplayedInvalidCredentialIsLookedUpOnce(t *testing.T) {
	v := &countingVerifier{}
	app := limitedApp(cfgWith(60), newFakeStore(), v)
	for i := 0; i < 6; i++ {
		hit(t, app, map[string]string{"X-API-Key": "bad-key"})
	}
	if v.callCount() != 1 {
		t.Fatalf("expected a single lookup for a replayed invalid key, got %d", v.callCount())
	}
}

// TestRotatingInvalidCredentialsAreBudgetedPerIP: distinct bad keys defeat the
// cache, so uncached verifications are admitted from a per-IP budget equal to
// the request limit; beyond it no lookup happens at all.
func TestRotatingInvalidCredentialsAreBudgetedPerIP(t *testing.T) {
	v := &countingVerifier{}
	app := limitedApp(cfgWith(3), newFakeStore(), v)
	keys := []string{"k1", "k2", "k3", "k4", "k5", "k6"}
	for _, k := range keys {
		hit(t, app, map[string]string{"X-API-Key": k})
	}
	if v.callCount() != 3 {
		t.Fatalf("expected lookups capped at the per-IP verification budget (3), got %d", v.callCount())
	}
}

// TestRotatingInvalidCredentialsConcurrent: the budget must hold under a
// concurrent burst — capacity is reserved atomically BEFORE the lookup.
func TestRotatingInvalidCredentialsConcurrent(t *testing.T) {
	v := &countingVerifier{delay: 5 * time.Millisecond}
	app := limitedApp(cfgWith(3), newFakeStore(), v)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/blog/posts", nil)
			req.Header.Set("X-API-Key", "rot-"+string(rune('a'+i)))
			resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
			if err == nil {
				_ = resp.Body.Close()
			}
		}(i)
	}
	wg.Wait()
	if v.callCount() > 3 {
		t.Fatalf("concurrent burst exceeded verification budget: %d lookups", v.callCount())
	}
}

// TestValidCredentialOverBudgetStopsCostingLookups: a valid key whose own
// bucket is exhausted keeps getting 429 without a backend lookup per request.
func TestValidCredentialOverBudgetStopsCostingLookups(t *testing.T) {
	v := &countingVerifier{validKeys: map[string]string{"good": "id-1"}}
	st := newFakeStore()
	app := limitedApp(cfgWith(2), st, v)

	var statuses []int
	for i := 0; i < 6; i++ {
		statuses = append(statuses, hit(t, app, map[string]string{"X-API-Key": "good"}))
	}
	want := []int{200, 200, 429, 429, 429, 429}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", statuses, want)
		}
	}
	if v.callCount() != 1 {
		t.Fatalf("expected one lookup for a repeated valid key, got %d", v.callCount())
	}
	if st.count("apikey:id-1") != 6 {
		t.Fatalf("expected all 6 hits on the verified identity bucket, got %d", st.count("apikey:id-1"))
	}
}

// TestVerificationBudgetExhaustionIsRejectedNotRekeyed: once the per-IP
// verification budget is spent, an uncached credential must be REJECTED, not
// silently re-keyed to the IP bucket. Otherwise a user whose own bucket is
// exhausted can burn the budget with garbage keys (riding a cached valid
// bearer so the IP bucket stays fresh) and then present a second valid token
// to collect a whole new allowance from the untouched IP bucket.
func TestVerificationBudgetExhaustionIsRejectedNotRekeyed(t *testing.T) {
	v := &countingVerifier{validBearers: map[string]string{"tokA": "u1", "tokB": "u1"}}
	st := newFakeStore()
	app := limitedApp(cfgWith(3), st, v)

	// 1: tokA verified and cached; user bucket 1/3, verify budget 1/3.
	if s := hit(t, app, map[string]string{"Authorization": "Bearer tokA"}); s != 200 {
		t.Fatalf("step 1: got %d", s)
	}
	// 2-3: garbage API keys (each an uncached verification: budget 2/3, 3/3)
	// alongside the cached bearer, so requests charge user:u1 (2/3, 3/3).
	for _, k := range []string{"bad1", "bad2"} {
		if s := hit(t, app, map[string]string{"X-API-Key": k, "Authorization": "Bearer tokA"}); s != 200 {
			t.Fatalf("step %s: got %d", k, s)
		}
	}
	// 4: user bucket exhausted -> 429 as expected.
	if s := hit(t, app, map[string]string{"Authorization": "Bearer tokA"}); s != 429 {
		t.Fatalf("step 4: expected 429 for exhausted user bucket, got %d", s)
	}
	// 5: a second VALID but uncached token for the same user. The verification
	// budget is spent, so it cannot be verified; it must be rejected rather than
	// handed the fresh IP bucket.
	if s := hit(t, app, map[string]string{"Authorization": "Bearer tokB"}); s != 429 {
		t.Fatalf("step 5: expected 429 when verification budget is exhausted, got %d", s)
	}
	if st.count("ip:0.0.0.0") != 0 {
		t.Fatalf("budget exhaustion must not charge the IP bucket, got %d", st.count("ip:0.0.0.0"))
	}
}

// TestVerificationRunsUnderDeadline: the lookup must be bounded.
func TestVerificationRunsUnderDeadline(t *testing.T) {
	v := &countingVerifier{}
	app := limitedApp(cfgWith(60), newFakeStore(), v)
	hit(t, app, map[string]string{"Authorization": "Bearer whatever"})
	if v.callCount() != 1 || v.deadlines != 1 {
		t.Fatalf("expected one verification carrying a deadline, calls=%d deadlines=%d", v.calls, v.deadlines)
	}
}

// TestSlowVerificationStillEnforcesLimit: when the lookup hits its timeout,
// the counter operations after it must run under a FRESH deadline, so an
// exhausted bucket still yields 429 instead of failing open.
func TestSlowVerificationStillEnforcesLimit(t *testing.T) {
	prev := verifyTimeout
	verifyTimeout = 10 * time.Millisecond
	t.Cleanup(func() { verifyTimeout = prev })

	v := &countingVerifier{delay: 30 * time.Millisecond}
	st := newFakeStore()
	app := limitedApp(cfgWith(1), st, v)

	// Exhaust the IP bucket first (anonymous request lands on ip:).
	if s := hit(t, app, nil); s != 200 {
		t.Fatalf("first anonymous request should pass, got %d", s)
	}
	// Slow, unverifiable credential: verification times out; the request must
	// still be judged against the (now exhausted) IP bucket.
	if s := hit(t, app, map[string]string{"X-API-Key": "slow"}); s != 429 {
		t.Fatalf("expected 429 after verification timeout with exhausted IP bucket, got %d", s)
	}
}

// TestNilStoreFallbackDisabled verifies the middleware constructor returns nil
// when Redis is unavailable, so the caller falls back to the in-memory limiter.
func TestNilStoreFallbackDisabled(t *testing.T) {
	if mw := newRateLimitMiddleware(cfgWith(60), nil, nil); mw != nil {
		t.Fatal("expected nil middleware when Redis is unavailable")
	}
}
