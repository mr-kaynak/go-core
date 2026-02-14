package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

const (
	defaultFailureThreshold = 5
	defaultResetTimeout     = 30 * time.Second
	pingTimeout             = 5 * time.Second
	healthCheckTimeout      = 3 * time.Second
)

// RedisClient wraps the go-redis client with circuit breaker and logging.
type RedisClient struct {
	client *redis.Client
	cfg    *config.Config
	logger *logger.Logger

	// Circuit breaker state
	mu               sync.RWMutex
	failures         int
	lastFailure      time.Time
	circuitOpen      bool
	failureThreshold int
	resetTimeout     time.Duration
}

// NewRedisClient creates a new Redis client from config, pings to verify connectivity.
func NewRedisClient(cfg *config.Config) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.GetRedisAddr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})

	rc := &RedisClient{
		client:           client,
		cfg:              cfg,
		logger:           logger.Get().WithField("component", "redis"),
		failureThreshold: defaultFailureThreshold,
		resetTimeout:     defaultResetTimeout,
	}

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

func (r *RedisClient) isCircuitOpen() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.circuitOpen {
		return false
	}
	// Check if reset timeout has passed (half-open)
	if time.Since(r.lastFailure) > r.resetTimeout {
		return false
	}
	return true
}

func (r *RedisClient) recordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = 0
	r.circuitOpen = false
}

func (r *RedisClient) recordFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
	r.lastFailure = time.Now()
	if r.failures >= r.failureThreshold {
		r.circuitOpen = true
		r.logger.Warn("Redis circuit breaker opened", "failures", r.failures)
	}
}

func (r *RedisClient) exec(fn func() error) error {
	if r.isCircuitOpen() {
		return fmt.Errorf("redis circuit breaker is open")
	}
	err := fn()
	if err != nil {
		r.recordFailure()
		return err
	}
	r.recordSuccess()
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
	if err == nil {
		metrics.GetMetrics().RecordCacheHit()
	} else if errors.Is(err, redis.Nil) {
		metrics.GetMetrics().RecordCacheMiss()
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

// IsAvailable returns true if the circuit breaker is closed and Redis is reachable.
func (r *RedisClient) IsAvailable() bool {
	if r.isCircuitOpen() {
		return false
	}
	return r.HealthCheck() == nil
}
