package cache

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// Lua script for atomic sliding-window rate limiting.
// KEYS[1] = rate limit key
// ARGV[1] = expiration in seconds
// Returns the current count after increment.
var rateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return current
`)

// RateLimiter provides distributed rate limiting backed by Redis.
type RateLimiter struct {
	rc     *RedisClient
	prefix string
}

// NewRateLimiter creates a new Redis-backed rate limiter.
func NewRateLimiter(rc *RedisClient) *RateLimiter {
	return &RateLimiter{
		rc:     rc,
		prefix: "ratelimit:",
	}
}

// Allow checks if a request identified by key is allowed within the given max/expiration window.
func (rl *RateLimiter) Allow(ctx context.Context, key string, max int, expiration time.Duration) (bool, error) {
	fullKey := rl.prefix + key
	expSeconds := int(expiration.Seconds())
	if expSeconds < 1 {
		expSeconds = 1
	}

	result, err := rateLimitScript.Run(ctx, rl.rc.client, []string{fullKey}, expSeconds).Int64()
	if err != nil {
		return false, err
	}
	return result <= int64(max), nil
}

// --- Fiber Storage adapter ---

// redisStorage implements fiber.Storage backed by Redis.
type redisStorage struct {
	rc     *RedisClient
	prefix string
}

// FiberStorage returns a fiber.Storage implementation backed by Redis.
// This can be passed to Fiber's limiter middleware as the Storage option.
func (rl *RateLimiter) FiberStorage() fiber.Storage {
	return &redisStorage{
		rc:     rl.rc,
		prefix: rl.prefix,
	}
}

func (s *redisStorage) Get(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := s.rc.client.Get(ctx, s.prefix+key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return val, err
}

func (s *redisStorage) Set(key string, val []byte, exp time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return s.rc.client.Set(ctx, s.prefix+key, val, exp).Err()
}

func (s *redisStorage) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return s.rc.client.Del(ctx, s.prefix+key).Err()
}

func (s *redisStorage) Reset() error {
	// Not safe for production — only deletes keys with this prefix via SCAN.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	iter := s.rc.client.Scan(ctx, 0, s.prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		s.rc.client.Del(ctx, iter.Val())
	}
	return iter.Err()
}

func (s *redisStorage) Close() error {
	return nil // lifecycle managed by RedisClient
}
