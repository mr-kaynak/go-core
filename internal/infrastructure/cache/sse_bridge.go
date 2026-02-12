package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/redis/go-redis/v9"
)

// redisBridgeMessage is the wire format published to Redis.
type redisBridgeMessage struct {
	ServerID string          `json:"server_id"`
	Event    *domain.SSEEvent `json:"event"`
}

// SSEBridge bridges local SSE broadcasts to a Redis pub/sub channel so
// multiple server instances can share SSE events.
type SSEBridge struct {
	pubsub   *PubSub
	channel  string
	serverID string
	logger   *logger.Logger

	sub     *redis.PubSub
	handler func(event *domain.SSEEvent)
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewSSEBridge creates a new SSE bridge.
func NewSSEBridge(rc *RedisClient, channel, serverID string) *SSEBridge {
	return &SSEBridge{
		pubsub:   NewPubSub(rc),
		channel:  channel,
		serverID: serverID,
		logger:   logger.Get().WithField("component", "sse_bridge"),
	}
}

// Publish publishes an SSE event to the Redis channel.
func (b *SSEBridge) Publish(ctx context.Context, event *domain.SSEEvent) error {
	msg := redisBridgeMessage{
		ServerID: b.serverID,
		Event:    event,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("sse bridge marshal: %w", err)
	}
	return b.pubsub.Publish(ctx, b.channel, data)
}

// OnEvent sets the handler that will be called for events from other servers.
func (b *SSEBridge) OnEvent(handler func(event *domain.SSEEvent)) {
	b.handler = handler
}

// Subscribe starts listening for events on the Redis channel.
// Events originating from this server (same serverID) are ignored.
func (b *SSEBridge) Subscribe(ctx context.Context) error {
	subCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	b.sub = b.pubsub.Subscribe(subCtx, b.channel)

	// Wait for subscription confirmation
	if _, err := b.sub.Receive(subCtx); err != nil {
		cancel()
		return fmt.Errorf("sse bridge subscribe: %w", err)
	}

	b.wg.Add(1)
	go b.listen(subCtx)

	b.logger.Info("SSE bridge subscribed", "channel", b.channel, "server_id", b.serverID)
	return nil
}

func (b *SSEBridge) listen(ctx context.Context) {
	defer b.wg.Done()
	ch := b.sub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			b.handleMessage(msg)
		}
	}
}

func (b *SSEBridge) handleMessage(msg *redis.Message) {
	var m redisBridgeMessage
	if err := json.Unmarshal([]byte(msg.Payload), &m); err != nil {
		b.logger.Warn("SSE bridge: failed to unmarshal message", "error", err)
		return
	}

	// Skip messages from this server
	if m.ServerID == b.serverID {
		return
	}

	if b.handler != nil && m.Event != nil {
		b.handler(m.Event)
	}
}

// Stop closes the subscription and waits for the listener goroutine.
func (b *SSEBridge) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	if b.sub != nil {
		b.sub.Close()
	}
	b.wg.Wait()
	b.logger.Info("SSE bridge stopped")
}
