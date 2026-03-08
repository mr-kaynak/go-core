package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const presignPrefix = "presign:"

// PresignCache provides Redis-backed caching for S3/MinIO presigned URLs.
// TTL is set slightly below the presign expiry so cached URLs are always
// invalidated before the storage backend considers them expired.
type PresignCache struct {
	rc  *RedisClient
	ttl time.Duration
}

// NewPresignCache creates a new PresignCache.
// presignTTL is the actual presigned URL lifetime; the cache TTL is derived
// with a 1-minute safety margin.
func NewPresignCache(rc *RedisClient, presignTTL time.Duration) *PresignCache {
	const minCacheTTL = 30 * time.Second

	cacheTTL := presignTTL - time.Minute
	if cacheTTL < time.Minute {
		cacheTTL = presignTTL / 2
	}
	if cacheTTL < minCacheTTL {
		cacheTTL = minCacheTTL
	}

	return &PresignCache{
		rc:  rc,
		ttl: cacheTTL,
	}
}

// GetPresignedURL returns a cached presigned URL for the given storage key.
// Returns empty string and nil error on cache miss.
func (pc *PresignCache) GetPresignedURL(ctx context.Context, key string) (string, error) {
	cacheKey := fmt.Sprintf("%s%s", presignPrefix, key)
	val, err := pc.rc.Get(ctx, cacheKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

// SetPresignedURL caches a presigned URL for the given storage key.
func (pc *PresignCache) SetPresignedURL(ctx context.Context, key, url string) error {
	cacheKey := fmt.Sprintf("%s%s", presignPrefix, key)
	return pc.rc.Set(ctx, cacheKey, url, pc.ttl)
}

// InvalidatePresignedURL removes a cached presigned URL.
func (pc *PresignCache) InvalidatePresignedURL(ctx context.Context, key string) error {
	cacheKey := fmt.Sprintf("%s%s", presignPrefix, key)
	return pc.rc.Del(ctx, cacheKey)
}
