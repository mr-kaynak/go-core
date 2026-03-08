package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

func newBroadcasterTestSetup(t *testing.T) (*ConnectionManager, *EventBroadcaster) {
	t.Helper()
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        1000,
		MaxConnectionsPerUser: 100,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers:     2,
		QueueSize:      100,
		MaxRetries:     1,
		RetryDelay:     10 * time.Millisecond,
		ProcessTimeout: 1 * time.Second,
		EnableBatching: false,
		BatchSize:      10,
		BatchInterval:  10 * time.Millisecond,
	})
	cm.Start()
	eb.Start()
	return cm, eb
}

func TestEventBroadcasterBroadcastsToMultipleClients(t *testing.T) {
	cm, eb := newBroadcasterTestSetup(t)
	defer func() {
		_ = eb.Shutdown(context.Background())
		_ = cm.Shutdown(context.Background())
	}()

	userID := uuid.New()
	c1 := streaming.NewClient(context.Background(), userID)
	c2 := streaming.NewClient(context.Background(), uuid.New())
	_ = cm.Register(c1)
	_ = cm.Register(c2)

	err := eb.BroadcastToAll(context.Background(), &domain.SSEEvent{
		ID:        uuid.NewString(),
		Type:      domain.SSEEventTypeSystemMessage,
		Data:      "hello",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	stats := eb.GetStats()
	if stats.SuccessfulSends < 2 {
		t.Fatalf("expected at least 2 successful sends, got %d", stats.SuccessfulSends)
	}
}

func TestEventBroadcasterHandlesBufferFullSendError(t *testing.T) {
	cm, eb := newBroadcasterTestSetup(t)
	defer func() {
		_ = eb.Shutdown(context.Background())
		_ = cm.Shutdown(context.Background())
	}()

	client := streaming.NewClientWithOptions(context.Background(), uuid.New(), streaming.ClientOptions{
		BufferSize:     1,
		SendTimeout:    10 * time.Millisecond,
		MaxMessageSize: 1024,
		EnableMetrics:  true,
	})
	_ = cm.Register(client)

	// Fill client buffer so subsequent sends fail with ErrBufferFull.
	_ = client.TrySend(&domain.SSEEvent{ID: "seed", Type: domain.SSEEventTypeSystemMessage, Data: "seed", Timestamp: time.Now()})

	err := eb.BroadcastToAll(context.Background(), &domain.SSEEvent{
		ID:        uuid.NewString(),
		Type:      domain.SSEEventTypeSystemMessage,
		Data:      "overflow",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	stats := eb.GetStats()
	if stats.FailedSends == 0 {
		t.Fatalf("expected failed sends due to full client buffer")
	}
}
