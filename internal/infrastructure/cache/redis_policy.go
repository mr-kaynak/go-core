package cache

import (
	"fmt"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
)

// Redis startup modes. The distinction matters because a nil client silently
// disables the token blacklist: "unreachable" must never be conflated with
// "intentionally disabled".
const (
	RedisModeRequired = "required"
	RedisModeOptional = "optional"
	RedisModeDisabled = "disabled"
)

// NewRedisClientWithPolicy applies the configured startup policy:
//   - disabled: no connection is attempted, returns (nil, nil)
//   - required (default): connection failure fails startup
//   - optional: connection failure logs a warning and returns (nil, nil)
func NewRedisClientWithPolicy(cfg *config.Config) (*RedisClient, error) {
	log := logger.Get().WithField("component", "redis")

	mode := cfg.Redis.Mode
	if mode == "" {
		mode = RedisModeRequired
	}

	switch mode {
	case RedisModeDisabled:
		log.Info("Redis disabled by configuration; token blacklist and cache features are off")
		return nil, nil
	case RedisModeRequired:
		client, err := NewRedisClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("redis is required (redis.mode=required) but unreachable: %w", err)
		}
		return client, nil
	case RedisModeOptional:
		client, err := NewRedisClient(cfg)
		if err != nil {
			log.Warn("Redis unreachable, continuing without it: token blacklist disabled — revoked tokens stay valid until JWT expiry", "error", err)
			return nil, nil
		}
		return client, nil
	default:
		return nil, fmt.Errorf("invalid redis.mode %q: must be one of required, optional, disabled", mode)
	}
}
