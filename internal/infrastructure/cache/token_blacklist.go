package cache

import (
	"context"
	"fmt"
	"time"
)

const (
	blacklistPrefix     = "blacklist:"
	userBlacklistPrefix = "blacklist:user:"
)

// TokenBlacklist provides token revocation via Redis.
// When Redis is unavailable, IsBlacklisted returns false (graceful degradation).
type TokenBlacklist struct {
	rc     *RedisClient
	logger interface{ Warn(string, ...interface{}) }
}

// NewTokenBlacklist creates a new TokenBlacklist.
func NewTokenBlacklist(rc *RedisClient) *TokenBlacklist {
	return &TokenBlacklist{
		rc:     rc,
		logger: rc.logger,
	}
}

// Blacklist adds a token hash to the blacklist with the given expiry.
func (tb *TokenBlacklist) Blacklist(ctx context.Context, tokenHash string, expiry time.Duration) error {
	key := fmt.Sprintf("%s%s", blacklistPrefix, tokenHash)
	return tb.rc.Set(ctx, key, "1", expiry)
}

// IsBlacklisted checks whether a token hash has been blacklisted.
// Returns true if Redis is unavailable (fail-closed for security).
func (tb *TokenBlacklist) IsBlacklisted(ctx context.Context, tokenHash string) (bool, error) {
	key := fmt.Sprintf("%s%s", blacklistPrefix, tokenHash)
	exists, err := tb.rc.Exists(ctx, key)
	if err != nil {
		tb.logger.Warn("Redis unavailable for blacklist check, rejecting token (fail-closed)", "error", err)
		return true, err
	}
	return exists, nil
}

// BlacklistUser blacklists all tokens for a specific user.
func (tb *TokenBlacklist) BlacklistUser(ctx context.Context, userID string, expiry time.Duration) error {
	key := fmt.Sprintf("%s%s", userBlacklistPrefix, userID)
	return tb.rc.Set(ctx, key, "1", expiry)
}

// IsUserBlacklisted checks whether a user's tokens have been bulk-blacklisted.
// Returns true if Redis is unavailable (fail-closed for security).
func (tb *TokenBlacklist) IsUserBlacklisted(ctx context.Context, userID string) (bool, error) {
	key := fmt.Sprintf("%s%s", userBlacklistPrefix, userID)
	exists, err := tb.rc.Exists(ctx, key)
	if err != nil {
		tb.logger.Warn("Redis unavailable for user blacklist check, rejecting token (fail-closed)", "error", err)
		return true, err
	}
	return exists, nil
}

// ClearUserBlacklist removes the user-level blacklist entry.
// Called after a successful token refresh so newly issued tokens are accepted.
func (tb *TokenBlacklist) ClearUserBlacklist(ctx context.Context, userID string) error {
	key := fmt.Sprintf("%s%s", userBlacklistPrefix, userID)
	return tb.rc.Del(ctx, key)
}
