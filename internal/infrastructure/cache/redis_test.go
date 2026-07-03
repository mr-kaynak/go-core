package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/circuitbreaker"
	"github.com/redis/go-redis/v9"
)

func newRedisClientWithFakeBackend(t *testing.T) (*RedisClient, *fakeRedisBackend) {
	t.Helper()
	backend := newFakeRedisBackend()

	log := logger.Get().WithField("component", "redis-test")
	rc := &RedisClient{
		client: redis.NewClient(&redis.Options{
			Addr:         "fake-redis:6379",
			Dialer:       backend.Dialer,
			PoolSize:     2,
			ReadTimeout:  time.Second,
			WriteTimeout: time.Second,
		}),
		logger:  log,
		breaker: newRedisBreaker(5, 30*time.Second, log),
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
	rc.breaker = newRedisBreaker(2, 50*time.Millisecond, rc.logger)

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

	if err := rc.exec(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open-circuit error, got %v", err)
	}

	time.Sleep(70 * time.Millisecond)

	if rc.isCircuitOpen() {
		t.Fatalf("expected circuit to become half-open/closed after reset timeout")
	}

	// A single success in half-open (HalfOpenMaxRequests=1) closes the circuit.
	if err := rc.exec(func() error { return nil }); err != nil {
		t.Fatalf("expected successful execution after reset, got %v", err)
	}
	if stats := rc.breaker.GetStats(); stats.ConsecutiveFailures != 0 {
		t.Fatalf("expected consecutive failures to reset after success, got %d", stats.ConsecutiveFailures)
	}
}

func TestRedisClientNilResultDoesNotTripBreaker(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rc.breaker = newRedisBreaker(2, time.Minute, rc.logger)

	// redis.Nil (key not found) is a normal result, not a transport failure,
	// so any number of them must never open the circuit.
	for i := 0; i < 10; i++ {
		_, err := rc.Get(context.Background(), "does-not-exist")
		if !errors.Is(err, redis.Nil) {
			t.Fatalf("expected redis.Nil, got %v", err)
		}
	}

	if rc.isCircuitOpen() {
		t.Fatalf("circuit must not open on redis.Nil results")
	}
	if stats := rc.breaker.GetStats(); stats.ConsecutiveFailures != 0 {
		t.Fatalf("expected zero consecutive failures on redis.Nil, got %d", stats.ConsecutiveFailures)
	}
}

func TestRedisClientHalfOpenAdmitsBoundedProbes(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rc.breaker = newRedisBreaker(2, 30*time.Millisecond, rc.logger)

	failing := func() error { return errors.New("boom") }
	_ = rc.exec(failing)
	_ = rc.exec(failing)
	if rc.breaker.GetState() != circuitbreaker.StateOpen {
		t.Fatalf("expected circuit open after threshold")
	}

	// Wait past the reset timeout so the breaker is eligible for half-open.
	time.Sleep(50 * time.Millisecond)

	// Fire many concurrent probes; the breaker bounds half-open admissions to
	// halfOpenProbeRequests (1). A blocking probe holds the single slot, so all
	// other concurrent probes must be rejected with ErrCircuitOpen.
	const probes = 20
	release := make(chan struct{})
	var admitted, rejected int64
	var wg sync.WaitGroup
	wg.Add(probes)
	for i := 0; i < probes; i++ {
		go func() {
			defer wg.Done()
			err := rc.exec(func() error {
				<-release // hold the probe slot until released
				return nil
			})
			if errors.Is(err, ErrCircuitOpen) {
				atomic.AddInt64(&rejected, 1)
			} else if err == nil {
				atomic.AddInt64(&admitted, 1)
			}
		}()
	}

	// Give the goroutines time to contend for the single probe slot, then let
	// the admitted probe complete.
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()

	if admitted != halfOpenProbeRequests {
		t.Fatalf("expected exactly %d admitted probe(s), got %d", halfOpenProbeRequests, admitted)
	}
	if rejected != probes-halfOpenProbeRequests {
		t.Fatalf("expected %d rejected probes, got %d", probes-halfOpenProbeRequests, rejected)
	}
}

func TestPubSubSubscribeRecordsFailureWhenRedisDown(t *testing.T) {
	rc, backend := newRedisClientWithFakeBackend(t)
	rc.breaker = newRedisBreaker(1, time.Minute, rc.logger)
	ps := NewPubSub(rc)

	backend.Close()

	sub, err := ps.Subscribe(context.Background(), "some-channel")
	if sub != nil {
		_ = sub.Close()
	}
	if err == nil {
		t.Fatalf("expected Subscribe to error when redis is down")
	}

	// A single Subscribe failure must count against the breaker and, with a
	// threshold of 1, open the circuit.
	if !rc.isCircuitOpen() {
		t.Fatalf("expected Subscribe failure to open the circuit breaker")
	}
}

func TestPubSubSubscribeRejectedWhenCircuitOpen(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	rc.breaker = newRedisBreaker(2, time.Minute, rc.logger)
	ps := NewPubSub(rc)

	failing := func() error { return errors.New("boom") }
	_ = rc.exec(failing)
	_ = rc.exec(failing)
	if !rc.isCircuitOpen() {
		t.Fatalf("expected circuit open before Subscribe")
	}

	sub, err := ps.Subscribe(context.Background(), "some-channel")
	if sub != nil {
		_ = sub.Close()
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen when subscribing with open circuit, got %v", err)
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
	rc.breaker = newRedisBreaker(2, time.Minute, rc.logger)

	if !rc.IsAvailable() {
		t.Fatalf("expected client to be available while backend is running")
	}

	// Trip the breaker: two consecutive failures open the circuit for the full
	// (1 minute) reset timeout, so it should report unavailable.
	failing := func() error { return errors.New("boom") }
	_ = rc.exec(failing)
	_ = rc.exec(failing)
	if rc.IsAvailable() {
		t.Fatalf("expected unavailable when circuit is open")
	}

	// Reset the breaker to closed and shut down the backend: availability must
	// now fall through to the live health check and report unavailable.
	rc.breaker.Reset()
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

func TestRedisClientGetReturnsRawClient(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	client := rc.Client()
	if client == nil {
		t.Fatalf("expected non-nil underlying redis.Client")
	}
}

func TestRedisClientHealthCheck(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	if err := rc.HealthCheck(); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
}

func TestRedisClientHealthCheckDown(t *testing.T) {
	rc, backend := newRedisClientWithFakeBackend(t)
	backend.Close()
	if err := rc.HealthCheck(); err == nil {
		t.Fatalf("expected error after backend shutdown")
	}
}

func TestRedisClientGetNonExistentKey(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	_, err := rc.Get(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatalf("expected redis.Nil error for non-existent key")
	}
}

func TestNewPubSubCreation(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	ps := NewPubSub(rc)
	if ps == nil {
		t.Fatalf("expected non-nil PubSub")
	}
	if ps.rc != rc {
		t.Fatalf("expected PubSub to reference the given RedisClient")
	}
}

func TestRedisClientSetNXOnNewKey(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	ctx := context.Background()

	ok, err := rc.SetNX(ctx, "unique-key", "value", 5*time.Second)
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected SetNX to succeed on new key")
	}
}

func TestRedisClientDelNonExistentKey(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	ctx := context.Background()

	// Del on non-existent key should not error
	if err := rc.Del(ctx, "no-such-key"); err != nil {
		t.Fatalf("Del on non-existent key should not fail, got %v", err)
	}
}

func TestRedisClientExistsNonExistentKey(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	ctx := context.Background()

	exists, err := rc.Exists(ctx, "no-such-key")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Fatalf("expected key to not exist")
	}
}
