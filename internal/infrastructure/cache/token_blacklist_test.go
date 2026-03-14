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

func TestTokenBlacklistNonExistentToken(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	blacklist := NewTokenBlacklist(rc)
	ctx := context.Background()

	blacklisted, err := blacklist.IsBlacklisted(ctx, "never-blacklisted")
	if err != nil {
		t.Fatalf("expected no error for non-existent token, got %v", err)
	}
	if blacklisted {
		t.Fatalf("expected non-blacklisted token to return false")
	}
}

func TestTokenBlacklistClearUserBlacklist(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	blacklist := NewTokenBlacklist(rc)
	ctx := context.Background()

	_ = blacklist.BlacklistUser(ctx, "user-clear", 5*time.Second)

	blacklisted, _ := blacklist.IsUserBlacklisted(ctx, "user-clear")
	if !blacklisted {
		t.Fatalf("expected user to be blacklisted before clear")
	}

	if err := blacklist.ClearUserBlacklist(ctx, "user-clear"); err != nil {
		t.Fatalf("ClearUserBlacklist failed: %v", err)
	}

	blacklisted, _ = blacklist.IsUserBlacklisted(ctx, "user-clear")
	if blacklisted {
		t.Fatalf("expected user to NOT be blacklisted after clear")
	}
}

func TestTokenBlacklistFailClosedOnRedisDownForToken(t *testing.T) {
	rc, backend := newRedisClientWithFakeBackend(t)
	blacklist := NewTokenBlacklist(rc)
	ctx := context.Background()

	// Shutdown Redis backend
	backend.Close()

	// Individual token blacklist check should fail-closed (return true + error)
	blacklisted, err := blacklist.IsBlacklisted(ctx, "some-token")
	if err == nil {
		t.Fatalf("expected error during redis outage")
	}
	if !blacklisted {
		t.Fatalf("expected true (fail-closed) during redis outage for individual token")
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
