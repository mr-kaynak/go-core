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
//
// The returned *redis.PubSub is always non-nil so existing callers can defer
// Close on it unconditionally. Subscription is routed through the circuit
// breaker: when the breaker is open the call is rejected up front, and any
// failure establishing the subscription is recorded against the breaker so a
// broken connection counts toward opening the circuit. err is non-nil when the
// subscription could not be established.
func (p *PubSub) Subscribe(ctx context.Context, channels ...string) (*redis.PubSub, error) {
	if !p.rc.breaker.Allow() {
		return p.rc.client.Subscribe(ctx, channels...), ErrCircuitOpen
	}

	sub := p.rc.client.Subscribe(ctx, channels...)

	// go-redis establishes the subscription lazily; force it now so a broken
	// connection surfaces as an error we can record against the breaker.
	if _, err := sub.Receive(ctx); err != nil {
		p.rc.breaker.RecordFailure()
		return sub, err
	}
	p.rc.breaker.RecordSuccess()
	return sub, nil
}
