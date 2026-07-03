package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/circuitbreaker"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

const (
	pingTimeout        = 5 * time.Second
	healthCheckTimeout = 3 * time.Second

	// halfOpenProbeRequests bounds the number of concurrent probe requests
	// allowed while the circuit is half-open. Keeping this at 1 ensures a
	// recovering Redis is probed by a single request at a time rather than
	// being flooded by every waiting goroutine.
	halfOpenProbeRequests = 1
)

// ErrCircuitOpen is returned by cache operations when the Redis circuit
// breaker is open. Callers that need fail-closed behavior (e.g. the token
// blacklist) rely on this being a non-nil error.
var ErrCircuitOpen = fmt.Errorf("redis circuit breaker is open")

// RedisClient wraps the go-redis client with a circuit breaker and logging.
type RedisClient struct {
	client  *redis.Client
	cfg     *config.Config
	logger  *logger.Logger
	breaker *circuitbreaker.CircuitBreaker
}

// NewRedisClient creates a new Redis client from config, pings to verify connectivity.
func NewRedisClient(cfg *config.Config) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:            cfg.GetRedisAddr(),
		Password:        cfg.Redis.Password,
		DB:              cfg.Redis.DB,
		PoolSize:        cfg.Redis.PoolSize,
		MinIdleConns:    cfg.Redis.MinIdleConns,
		ReadTimeout:     cfg.Redis.ReadTimeout,
		WriteTimeout:    cfg.Redis.WriteTimeout,
		ConnMaxIdleTime: cfg.Redis.ConnMaxIdleTime,
		ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
	})

	rc := &RedisClient{
		client: client,
		cfg:    cfg,
		logger: logger.Get().WithField("component", "redis"),
	}
	rc.breaker = newRedisBreaker(cfg.Redis.CBThreshold, cfg.Redis.CBResetTimeout, rc.logger)

	// Enable OpenTelemetry tracing on Redis client
	if err := redisotel.InstrumentTracing(client); err != nil {
		rc.logger.Warn("Failed to instrument Redis tracing", "error", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	rc.logger.Info("Redis client connected", "addr", cfg.GetRedisAddr())
	return rc, nil
}

// Client returns the underlying redis.Client for advanced operations (e.g. pub/sub).
func (r *RedisClient) Client() *redis.Client {
	return r.client
}

// --- Circuit Breaker ---

// newRedisBreaker builds a circuit breaker from the Redis CB config values.
// The Redis breaker trips on consecutive failures (MaxFailures) only — the
// rate-based FailureThreshold is disabled to preserve the historical
// consecutive-count semantics. Half-open admits a bounded number of concurrent
// probe requests so a recovering Redis is not flooded.
func newRedisBreaker(threshold int, resetTimeout time.Duration, log *logger.Logger) *circuitbreaker.CircuitBreaker {
	cfg := circuitbreaker.Config{
		MaxFailures:         threshold,
		FailureThreshold:    0, // disable rate-based tripping; use consecutive-count only
		Timeout:             0, // rely on go-redis's own read/write timeouts
		ResetTimeout:        resetTimeout,
		HalfOpenMaxRequests: halfOpenProbeRequests,
		ObservationWindow:   resetTimeout,
		OnStateChange: func(from, to circuitbreaker.State) {
			switch to {
			case circuitbreaker.StateOpen:
				log.Warn("Redis circuit breaker opened", "from", from.String())
			case circuitbreaker.StateClosed:
				log.Info("Redis circuit breaker closed", "from", from.String())
			case circuitbreaker.StateHalfOpen:
				log.Info("Redis circuit breaker half-open", "from", from.String())
			}
		},
	}
	return circuitbreaker.New(cfg)
}

// isCircuitOpen reports whether the breaker currently rejects requests. It is a
// read-only check that does not consume a half-open probe slot, so it is safe
// to call from availability probes. It returns false once the reset timeout has
// elapsed and the circuit is ready for a half-open probe, mirroring the
// previous inline behavior.
func (r *RedisClient) isCircuitOpen() bool {
	return r.breaker.IsOpen()
}

func (r *RedisClient) exec(fn func() error) error {
	if !r.breaker.Allow() {
		return ErrCircuitOpen
	}
	err := fn()
	if err != nil {
		// redis.Nil is a normal "key not found" result, not a transport
		// failure, so it must not count against the breaker.
		if errors.Is(err, redis.Nil) {
			r.breaker.RecordSuccess()
		} else {
			r.breaker.RecordFailure()
		}
		return err
	}
	r.breaker.RecordSuccess()
	return nil
}

// --- Core Operations ---

// Get retrieves a value by key.
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	var val string
	err := r.exec(func() error {
		var e error
		val, e = r.client.Get(ctx, key).Result()
		return e
	})
	if m := metrics.GetMetrics(); m != nil {
		if err == nil {
			m.RecordCacheHit()
		} else if errors.Is(err, redis.Nil) {
			m.RecordCacheMiss()
		}
	}
	return val, err
}

// Set stores a key-value pair with expiration.
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.exec(func() error {
		return r.client.Set(ctx, key, value, expiration).Err()
	})
}

// Del removes one or more keys.
func (r *RedisClient) Del(ctx context.Context, keys ...string) error {
	return r.exec(func() error {
		return r.client.Del(ctx, keys...).Err()
	})
}

// Exists checks if a key exists.
func (r *RedisClient) Exists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := r.exec(func() error {
		n, e := r.client.Exists(ctx, key).Result()
		exists = n > 0
		return e
	})
	return exists, err
}

// SetNX sets a key only if it does not exist (SET ... NX).
func (r *RedisClient) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	var ok bool
	err := r.exec(func() error {
		var e error
		ok, e = r.client.SetNX(ctx, key, value, expiration).Result()
		return e
	})
	return ok, err
}

// Incr atomically increments a key.
func (r *RedisClient) Incr(ctx context.Context, key string) (int64, error) {
	var val int64
	err := r.exec(func() error {
		var e error
		val, e = r.client.Incr(ctx, key).Result()
		return e
	})
	return val, err
}

// Expire sets an expiration on a key.
func (r *RedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return r.exec(func() error {
		return r.client.Expire(ctx, key, expiration).Err()
	})
}

// TTL returns the remaining time to live of a key.
func (r *RedisClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	var ttl time.Duration
	err := r.exec(func() error {
		var e error
		ttl, e = r.client.TTL(ctx, key).Result()
		return e
	})
	return ttl, err
}

// --- Health / Lifecycle ---

// HealthCheck pings Redis and returns an error if unreachable.
func (r *RedisClient) HealthCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

// Close closes the Redis client connection.
func (r *RedisClient) Close() error {
	r.logger.Info("Closing Redis client")
	return r.client.Close()
}

// IsAvailable returns true if the circuit breaker is closed (or half-open)
// AND the Redis server is reachable. It combines a circuit breaker check with
// a lightweight PING probe so callers get a reliable availability signal.
func (r *RedisClient) IsAvailable() bool {
	if r.isCircuitOpen() {
		return false
	}
	return r.HealthCheck() == nil
}
