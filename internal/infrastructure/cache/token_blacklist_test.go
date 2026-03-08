package cache

import (
	"context"
	"testing"
	"time"
)

func TestTokenBlacklistAddQueryAndTTLExpiry(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	blacklist := NewTokenBlacklist(rc)
	ctx := context.Background()

	if err := blacklist.Blacklist(ctx, "token-hash", 80*time.Millisecond); err != nil {
		t.Fatalf("blacklist failed: %v", err)
	}

	isBlacklisted, err := blacklist.IsBlacklisted(ctx, "token-hash")
	if err != nil {
		t.Fatalf("isBlacklisted returned error: %v", err)
	}
	if !isBlacklisted {
		t.Fatalf("expected token to be blacklisted")
	}

	time.Sleep(120 * time.Millisecond)

	isBlacklisted, err = blacklist.IsBlacklisted(ctx, "token-hash")
	if err != nil {
		t.Fatalf("isBlacklisted returned error after ttl: %v", err)
	}
	if isBlacklisted {
		t.Fatalf("expected token to be removed after ttl expiration")
	}
}

func TestTokenBlacklistUserAddQueryAndGracefulDegradation(t *testing.T) {
	rc, backend := newRedisClientWithFakeBackend(t)
	blacklist := NewTokenBlacklist(rc)
	ctx := context.Background()

	if err := blacklist.BlacklistUser(ctx, "user-1", time.Second); err != nil {
		t.Fatalf("blacklist user failed: %v", err)
	}

	blacklisted, err := blacklist.IsUserBlacklisted(ctx, "user-1")
	if err != nil {
		t.Fatalf("isUserBlacklisted returned error: %v", err)
	}
	if !blacklisted {
		t.Fatalf("expected user to be blacklisted")
	}

	backend.Close()

	blacklisted, err = blacklist.IsUserBlacklisted(ctx, "user-1")
	if err == nil {
		t.Fatalf("expected error during redis outage, got nil")
	}
	if !blacklisted {
		t.Fatalf("expected true during redis outage (fail-closed)")
	}
}
