package cache

import (
	"context"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
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

func TestRateLimiterFiberStorageImplementsInterface(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	storage := rl.FiberStorage()

	// Verify it implements fiber.Storage
	var _ fiber.Storage = storage
	if storage == nil {
		t.Fatalf("expected non-nil fiber.Storage")
	}
}

func TestRateLimiterFiberStorageGetMiss(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	storage := rl.FiberStorage()

	// Get non-existent key should return nil, nil
	data, err := storage.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get miss should return nil error, got %v", err)
	}
	if data != nil {
		t.Fatalf("Get miss should return nil data, got %v", data)
	}
}

func TestRateLimiterFiberStorageSetAndGet(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	storage := rl.FiberStorage()

	// Set
	if err := storage.Set("test-key", []byte("test-value"), 5*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get hit
	data, err := storage.Get("test-key")
	if err != nil {
		t.Fatalf("Get hit failed: %v", err)
	}
	if string(data) != "test-value" {
		t.Fatalf("expected 'test-value', got '%s'", string(data))
	}
}

func TestRateLimiterFiberStorageDelete(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	storage := rl.FiberStorage()

	_ = storage.Set("del-key", []byte("val"), 5*time.Second)

	if err := storage.Delete("del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	data, err := storage.Get("del-key")
	if err != nil {
		t.Fatalf("Get after delete failed: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil after delete, got %v", data)
	}
}

func TestRateLimiterFiberStorageClose(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	storage := rl.FiberStorage()

	// Close should return nil (lifecycle managed by RedisClient)
	if err := storage.Close(); err != nil {
		t.Fatalf("Close should return nil, got %v", err)
	}
}

func TestRateLimiterFiberStorageReset(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	storage := rl.FiberStorage()

	_ = storage.Set("reset-key1", []byte("val1"), 5*time.Second)
	_ = storage.Set("reset-key2", []byte("val2"), 5*time.Second)

	if err := storage.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// After reset, keys with the ratelimit: prefix should be gone
	data, _ := storage.Get("reset-key1")
	if data != nil {
		t.Fatalf("expected nil after reset, got %v", data)
	}
	data, _ = storage.Get("reset-key2")
	if data != nil {
		t.Fatalf("expected nil after reset, got %v", data)
	}
}

func TestRateLimiterSubSecondExpiration(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rl := NewRateLimiter(rc)
	ctx := context.Background()

	// expiration < 1s should be clamped to 1s
	allowed, err := rl.Allow(ctx, "sub-second", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}
	if !allowed {
		t.Fatalf("expected first request allowed")
	}
}
