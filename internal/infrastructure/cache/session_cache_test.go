package cache

import (
	"context"
	"testing"
	"time"
)

func TestSessionCacheSetGetPermissions(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	sc := NewSessionCache(rc, 5*time.Second)
	ctx := context.Background()

	roles := []string{"admin", "user"}
	permissions := []string{"posts.read", "posts.write", "users.manage"}

	if err := sc.SetPermissions(ctx, "user-123", roles, permissions); err != nil {
		t.Fatalf("SetPermissions failed: %v", err)
	}

	cp, err := sc.GetPermissions(ctx, "user-123")
	if err != nil {
		t.Fatalf("GetPermissions failed: %v", err)
	}
	if cp == nil {
		t.Fatalf("expected non-nil CachedPermissions")
	}
	if cp.UserID != "user-123" {
		t.Fatalf("expected user_id user-123, got %s", cp.UserID)
	}
	if len(cp.Roles) != 2 || cp.Roles[0] != "admin" {
		t.Fatalf("expected 2 roles starting with admin, got %v", cp.Roles)
	}
	if len(cp.Permissions) != 3 {
		t.Fatalf("expected 3 permissions, got %d", len(cp.Permissions))
	}
	if cp.CachedAt == 0 {
		t.Fatalf("expected non-zero CachedAt")
	}
}

func TestSessionCacheGetPermissionsMiss(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	sc := NewSessionCache(rc, time.Second)
	ctx := context.Background()

	cp, err := sc.GetPermissions(ctx, "nonexistent-user")
	if err != nil {
		t.Fatalf("expected nil error on miss, got %v", err)
	}
	if cp != nil {
		t.Fatalf("expected nil CachedPermissions on miss")
	}
}

func TestSessionCacheInvalidateUser(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	sc := NewSessionCache(rc, 5*time.Second)
	ctx := context.Background()

	_ = sc.SetPermissions(ctx, "user-456", []string{"user"}, []string{"read"})

	if err := sc.InvalidateUser(ctx, "user-456"); err != nil {
		t.Fatalf("InvalidateUser failed: %v", err)
	}

	cp, err := sc.GetPermissions(ctx, "user-456")
	if err != nil {
		t.Fatalf("expected nil error after invalidate, got %v", err)
	}
	if cp != nil {
		t.Fatalf("expected nil after invalidation")
	}
}

func TestSessionCacheTTLExpiry(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	sc := NewSessionCache(rc, 80*time.Millisecond)
	ctx := context.Background()

	_ = sc.SetPermissions(ctx, "ttl-user", []string{"user"}, []string{"read"})

	cp, _ := sc.GetPermissions(ctx, "ttl-user")
	if cp == nil {
		t.Fatalf("expected data before TTL expiry")
	}

	time.Sleep(120 * time.Millisecond)

	cp, err := sc.GetPermissions(ctx, "ttl-user")
	if err != nil {
		t.Fatalf("expected nil error after expiry, got %v", err)
	}
	if cp != nil {
		t.Fatalf("expected nil after TTL expiry")
	}
}

func TestSessionCacheOverwritePermissions(t *testing.T) {
	rc, _ := newRedisClientWithFakeBackend(t)
	sc := NewSessionCache(rc, 5*time.Second)
	ctx := context.Background()

	_ = sc.SetPermissions(ctx, "user-overwrite", []string{"user"}, []string{"read"})

	// Overwrite with new roles/permissions
	_ = sc.SetPermissions(ctx, "user-overwrite", []string{"admin", "superadmin"}, []string{"read", "write", "delete"})

	cp, err := sc.GetPermissions(ctx, "user-overwrite")
	if err != nil {
		t.Fatalf("GetPermissions failed: %v", err)
	}
	if cp == nil {
		t.Fatalf("expected non-nil CachedPermissions after overwrite")
	}
	if len(cp.Roles) != 2 || cp.Roles[0] != "admin" {
		t.Fatalf("expected overwritten roles, got %v", cp.Roles)
	}
	if len(cp.Permissions) != 3 {
		t.Fatalf("expected 3 overwritten permissions, got %d", len(cp.Permissions))
	}
}
