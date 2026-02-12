package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

func newSSEServiceForTest() *SSEService {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers:     2,
		QueueSize:      100,
		MaxRetries:     1,
		RetryDelay:     10 * time.Millisecond,
		ProcessTimeout: time.Second,
		EnableBatching: false,
		BatchSize:      10,
		BatchInterval:  10 * time.Millisecond,
	})
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: 20 * time.Millisecond,
		Timeout:  40 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	return &SSEService{
		connManager: cm,
		broadcaster: eb,
		heartbeat:   hm,
		config: SSEConfig{
			Enabled: true,
		},
		serverID: "test-sse",
		logger:   logger.Get().WithField("service", "sse-test"),
		started:  true,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func TestSSEServiceEventBroadcastAndSubscribeUnsubscribe(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	userID := uuid.New()
	client := streaming.NewClient(context.Background(), userID)
	if err := svc.RegisterClient(client); err != nil {
		t.Fatalf("register client failed: %v", err)
	}

	// Subscribe and broadcast to a channel.
	subCount := svc.SubscribeUserToChannels(userID, []string{"alerts"})
	if subCount != 1 {
		t.Fatalf("expected one subscription, got %d", subCount)
	}
	err := svc.BroadcastToChannel(context.Background(), "alerts", &domain.SSEEvent{
		ID:        uuid.NewString(),
		Type:      domain.SSEEventTypeSystemMessage,
		Data:      "hello",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("broadcast to channel failed: %v", err)
	}

	select {
	case <-client.Channel:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected event delivered to subscribed client")
	}

	unsubCount := svc.UnsubscribeUserFromChannels(userID, []string{"alerts"})
	if unsubCount != 1 {
		t.Fatalf("expected one unsubscription, got %d", unsubCount)
	}
}

func TestSSEServiceGracefulShutdown(t *testing.T) {
	svc := newSSEServiceForTest()
	client := streaming.NewClient(context.Background(), uuid.New())
	_ = svc.RegisterClient(client)

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	// Service should reject new registrations after stop.
	err := svc.RegisterClient(streaming.NewClient(context.Background(), uuid.New()))
	if err == nil {
		t.Fatalf("expected register to fail after stop")
	}
}
