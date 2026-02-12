package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const sessionPrefix = "session:"

// CachedPermissions holds cached role/permission data for a user.
type CachedPermissions struct {
	UserID      string   `json:"user_id"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	CachedAt    int64    `json:"cached_at"`
}

// SessionCache provides Redis-backed session/permission caching.
type SessionCache struct {
	rc  *RedisClient
	ttl time.Duration
}

// NewSessionCache creates a new SessionCache. ttl controls how long cached entries live.
func NewSessionCache(rc *RedisClient, ttl time.Duration) *SessionCache {
	return &SessionCache{
		rc:  rc,
		ttl: ttl,
	}
}

// SetPermissions caches the user's roles and permissions.
func (sc *SessionCache) SetPermissions(ctx context.Context, userID string, roles, permissions []string) error {
	cp := CachedPermissions{
		UserID:      userID,
		Roles:       roles,
		Permissions: permissions,
		CachedAt:    time.Now().Unix(),
	}

	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("session cache marshal: %w", err)
	}

	key := fmt.Sprintf("%s%s", sessionPrefix, userID)
	return sc.rc.Set(ctx, key, data, sc.ttl)
}

// GetPermissions returns cached permissions for a user.
// Returns nil, nil when the key does not exist.
func (sc *SessionCache) GetPermissions(ctx context.Context, userID string) (*CachedPermissions, error) {
	key := fmt.Sprintf("%s%s", sessionPrefix, userID)
	val, err := sc.rc.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var cp CachedPermissions
	if err := json.Unmarshal([]byte(val), &cp); err != nil {
		return nil, fmt.Errorf("session cache unmarshal: %w", err)
	}
	return &cp, nil
}

// InvalidateUser removes cached session data for a user.
func (sc *SessionCache) InvalidateUser(ctx context.Context, userID string) error {
	key := fmt.Sprintf("%s%s", sessionPrefix, userID)
	return sc.rc.Del(ctx, key)
}
