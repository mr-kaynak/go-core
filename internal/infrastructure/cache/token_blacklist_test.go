package cache

import (
	"context"
	"errors"
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

func TestTokenBlacklistFailClosedWhenCircuitOpen(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	// Threshold of 1 so a single failure trips the breaker; long reset timeout
	// keeps it open for the duration of the test.
	rc.breaker = newRedisBreaker(1, time.Minute, rc.logger)
	blacklist := NewTokenBlacklist(rc)
	ctx := context.Background()

	// Trip the breaker with a transport failure so the circuit is open even
	// though the fake backend is still running.
	_ = rc.exec(func() error { return errors.New("boom") })
	if !rc.isCircuitOpen() {
		t.Fatalf("expected circuit to be open before blacklist check")
	}

	// With the circuit open, Exists returns ErrCircuitOpen, and the blacklist
	// must fail-closed: report the token as blacklisted.
	blacklisted, err := blacklist.IsBlacklisted(ctx, "some-token")
	if err == nil {
		t.Fatalf("expected error while circuit is open")
	}
	if !blacklisted {
		t.Fatalf("expected fail-closed (blacklisted=true) while circuit is open")
	}

	// The user-level check must fail-closed the same way.
	userBlacklisted, err := blacklist.IsUserBlacklisted(ctx, "some-user")
	if err == nil {
		t.Fatalf("expected error while circuit is open")
	}
	if !userBlacklisted {
		t.Fatalf("expected user fail-closed (blacklisted=true) while circuit is open")
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
