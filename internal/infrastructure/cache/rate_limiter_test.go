package cache

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterAllowWithinLimitAndDenyWhenExceeded(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		allowed, err := rl.Allow(ctx, "user:42", 3, time.Second)
		if err != nil {
			t.Fatalf("allow failed at iteration %d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("expected request %d to be allowed", i)
		}
	}

	allowed, err := rl.Allow(ctx, "user:42", 3, time.Second)
	if err != nil {
		t.Fatalf("allow failed for exceeded request: %v", err)
	}
	if allowed {
		t.Fatalf("expected request above limit to be denied")
	}
}

func TestRateLimiterWindowResetAllowsAgain(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	ctx := context.Background()

	allowed, err := rl.Allow(ctx, "window-user", 1, time.Second)
	if err != nil {
		t.Fatalf("first allow failed: %v", err)
	}
	if !allowed {
		t.Fatalf("expected first request to be allowed")
	}

	allowed, err = rl.Allow(ctx, "window-user", 1, time.Second)
	if err != nil {
		t.Fatalf("second allow failed: %v", err)
	}
	if allowed {
		t.Fatalf("expected second request to be denied in same window")
	}

	time.Sleep(1200 * time.Millisecond)

	allowed, err = rl.Allow(ctx, "window-user", 1, time.Second)
	if err != nil {
		t.Fatalf("allow after reset failed: %v", err)
	}
	if !allowed {
		t.Fatalf("expected request to be allowed after window reset")
	}
}
