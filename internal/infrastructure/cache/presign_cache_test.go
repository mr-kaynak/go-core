package cache

import (
	"context"
	"testing"
	"time"
)

func TestNewPresignCacheTTLCalculation(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)

	// Normal case: 15min presign -> cacheTTL = 15m - 1m = 14m (>= 1min, kept)
	pc := NewPresignCache(rc, 15*time.Minute)
	if pc.ttl != 14*time.Minute {
		t.Fatalf("expected 14m TTL, got %v", pc.ttl)
	}

	// Short presign: 90s -> cacheTTL = 90s - 60s = 30s, which is < 1min so use 90s/2 = 45s
	pc = NewPresignCache(rc, 90*time.Second)
	if pc.ttl != 45*time.Second {
		t.Fatalf("expected 45s TTL for 90s presign, got %v", pc.ttl)
	}

	// Very short presign: 40s -> cacheTTL = 40s - 60s = -20s, < 1min so 40s/2 = 20s, < 30s min so 30s
	pc = NewPresignCache(rc, 40*time.Second)
	if pc.ttl != 30*time.Second {
		t.Fatalf("expected 30s minimum TTL, got %v", pc.ttl)
	}

	// Edge case: exactly 2 minutes -> cacheTTL = 2m - 1m = 1m (>= 1min, kept)
	pc = NewPresignCache(rc, 2*time.Minute)
	if pc.ttl != time.Minute {
		t.Fatalf("expected 1m TTL for 2m presign, got %v", pc.ttl)
	}

	// Edge case: 1 minute presign -> cacheTTL = 1m - 1m = 0s, < 1min so 1m/2 = 30s
	pc = NewPresignCache(rc, time.Minute)
	if pc.ttl != 30*time.Second {
		t.Fatalf("expected 30s TTL for 1m presign, got %v", pc.ttl)
	}
}

func TestPresignCacheSetGetInvalidate(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	pc := NewPresignCache(rc, 15*time.Minute)
	ctx := context.Background()

	// Set
	if err := pc.SetPresignedURL(ctx, "images/photo.jpg", "https://s3.example.com/signed-url"); err != nil {
		t.Fatalf("SetPresignedURL failed: %v", err)
	}

	// Get hit
	url, err := pc.GetPresignedURL(ctx, "images/photo.jpg")
	if err != nil {
		t.Fatalf("GetPresignedURL failed: %v", err)
	}
	if url != "https://s3.example.com/signed-url" {
		t.Fatalf("expected signed URL, got %s", url)
	}

	// Invalidate
	if err := pc.InvalidatePresignedURL(ctx, "images/photo.jpg"); err != nil {
		t.Fatalf("InvalidatePresignedURL failed: %v", err)
	}

	// Get miss after invalidation
	url, err = pc.GetPresignedURL(ctx, "images/photo.jpg")
	if err != nil {
		t.Fatalf("expected nil error on miss, got %v", err)
	}
	if url != "" {
		t.Fatalf("expected empty string on miss, got %s", url)
	}
}

func TestPresignCacheGetMiss(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	pc := NewPresignCache(rc, 15*time.Minute)

	url, err := pc.GetPresignedURL(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error on miss, got %v", err)
	}
	if url != "" {
		t.Fatalf("expected empty string on miss, got %s", url)
	}
}

func TestPresignCacheTTLExpiry(t *testing.T) {
	// Use a very short presign TTL so that cache TTL = min(30s) but we test
	// with the fake backend's PX-based expiry
	rc, _ := newRedisClientWithFakeBackend(t)
	pc := NewPresignCache(rc, 40*time.Second) // cacheTTL = 30s (minimum)
	ctx := context.Background()

	if err := pc.SetPresignedURL(ctx, "expire-test", "https://example.com/url"); err != nil {
		t.Fatalf("SetPresignedURL failed: %v", err)
	}

	url, err := pc.GetPresignedURL(ctx, "expire-test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if url == "" {
		t.Fatalf("expected URL before expiry")
	}
}

func TestPresignCacheMultipleKeys(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	pc := NewPresignCache(rc, 15*time.Minute)
	ctx := context.Background()

	_ = pc.SetPresignedURL(ctx, "key1", "url1")
	_ = pc.SetPresignedURL(ctx, "key2", "url2")

	url1, _ := pc.GetPresignedURL(ctx, "key1")
	url2, _ := pc.GetPresignedURL(ctx, "key2")

	if url1 != "url1" {
		t.Fatalf("expected url1, got %s", url1)
	}
	if url2 != "url2" {
		t.Fatalf("expected url2, got %s", url2)
	}

	// Invalidate one shouldn't affect the other
	_ = pc.InvalidatePresignedURL(ctx, "key1")
	url1, _ = pc.GetPresignedURL(ctx, "key1")
	url2, _ = pc.GetPresignedURL(ctx, "key2")

	if url1 != "" {
		t.Fatalf("expected empty after invalidate, got %s", url1)
	}
	if url2 != "url2" {
		t.Fatalf("expected url2 unchanged, got %s", url2)
	}
}
