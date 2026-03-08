package cache

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// PubSub wraps Redis pub/sub operations.
type PubSub struct {
	rc *RedisClient
}

// NewPubSub creates a new PubSub instance.
func NewPubSub(rc *RedisClient) *PubSub {
	return &PubSub{rc: rc}
}

// Publish publishes a message to a Redis channel.
func (p *PubSub) Publish(ctx context.Context, channel string, message interface{}) error {
	return p.rc.exec(func() error {
		return p.rc.client.Publish(ctx, channel, message).Err()
	})
}

// Subscribe subscribes to one or more Redis channels.
func (p *PubSub) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return p.rc.client.Subscribe(ctx, channels...)
}
