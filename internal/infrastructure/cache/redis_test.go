package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/redis/go-redis/v9"
)

func newRedisClientWithFakeBackend(t *testing.T) (*RedisClient, *fakeRedisBackend) {
	t.Helper()
	backend := newFakeRedisBackend()

	rc := &RedisClient{
		client: redis.NewClient(&redis.Options{
			Addr:         "fake-redis:6379",
			Dialer:       backend.Dialer,
			PoolSize:     2,
			ReadTimeout:  time.Second,
			WriteTimeout: time.Second,
		}),
		logger:           logger.Get().WithField("component", "redis-test"),
		failureThreshold: defaultFailureThreshold,
		resetTimeout:     defaultResetTimeout,
	}

	t.Cleanup(func() {
		_ = rc.client.Close()
		backend.Close()
	})

	return rc, backend
}

func TestNewRedisClientConfigValidation(t *testing.T) {
	cfg := &config.Config{
		Redis: config.RedisConfig{
			Host:     "127.0.0.1",
			Port:     1,
			DB:       0,
			PoolSize: 1,
		},
	}

	client, err := NewRedisClient(cfg)
	if err == nil {
		_ = client.Close()
		t.Fatalf("expected NewRedisClient to fail with unreachable redis")
	}
}

func TestRedisClientCircuitBreakerStateTransitions(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rc.failureThreshold = 2
	rc.resetTimeout = 50 * time.Millisecond

	failing := func() error { return errors.New("boom") }

	if err := rc.exec(failing); err == nil {
		t.Fatalf("expected first failure")
	}
	if err := rc.exec(failing); err == nil {
		t.Fatalf("expected second failure")
	}
	if !rc.isCircuitOpen() {
		t.Fatalf("expected circuit to be open after threshold")
	}

	if err := rc.exec(func() error { return nil }); err == nil || err.Error() != "redis circuit breaker is open" {
		t.Fatalf("expected open-circuit error, got %v", err)
	}

	time.Sleep(70 * time.Millisecond)

	if rc.isCircuitOpen() {
		t.Fatalf("expected circuit to become half-open/closed after reset timeout")
	}

	if err := rc.exec(func() error { return nil }); err != nil {
		t.Fatalf("expected successful execution after reset, got %v", err)
	}
	if rc.failures != 0 {
		t.Fatalf("expected failures to reset after success, got %d", rc.failures)
	}
}

func TestRedisClientCloseGraceful(t *testing.T) {
	rc, backend := newRedisClientWithFakeBackend(t)

	if err := rc.Close(); err != nil {
		t.Fatalf("expected graceful close, got %v", err)
	}

	backend.Close()
	if err := rc.Close(); err == nil {
		t.Fatalf("expected repeated close to return client closed error")
	}
}

func TestRedisClientIsAvailableReflectsHealthAndCircuit(t *testing.T) {
	rc, backend := newRedisClientWithFakeBackend(t)

	if !rc.IsAvailable() {
		t.Fatalf("expected client to be available while backend is running")
	}

	rc.mu.Lock()
	rc.circuitOpen = true
	rc.lastFailure = time.Now()
	rc.mu.Unlock()
	if rc.IsAvailable() {
		t.Fatalf("expected unavailable when circuit is open")
	}

	rc.mu.Lock()
	rc.circuitOpen = false
	rc.mu.Unlock()
	backend.Close()

	if rc.IsAvailable() {
		t.Fatalf("expected unavailable after backend shutdown")
	}
}

func TestRedisClientCoreOpsAgainstFakeRedis(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)

	ctx := context.Background()
	if err := rc.Set(ctx, "k1", "v1", time.Second); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	got, err := rc.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got != "v1" {
		t.Fatalf("expected value v1, got %s", got)
	}

	exists, err := rc.Exists(ctx, "k1")
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected key to exist")
	}

	nx, err := rc.SetNX(ctx, "k1", "other", time.Second)
	if err != nil {
		t.Fatalf("setnx failed: %v", err)
	}
	if nx {
		t.Fatalf("expected setnx to fail for existing key")
	}

	n, err := rc.Incr(ctx, "counter")
	if err != nil {
		t.Fatalf("incr failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected counter to be 1, got %d", n)
	}

	if err := rc.Expire(ctx, "counter", 2*time.Second); err != nil {
		t.Fatalf("expire failed: %v", err)
	}
	ttl, err := rc.TTL(ctx, "counter")
	if err != nil {
		t.Fatalf("ttl failed: %v", err)
	}
	if ttl < 0 {
		t.Fatalf("expected non-negative ttl, got %v", ttl)
	}

	if err := rc.Del(ctx, "k1", "counter"); err != nil {
		t.Fatalf("del failed: %v", err)
	}
}
