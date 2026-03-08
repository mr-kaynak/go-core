package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

func newConnectionManagerForTest() *ConnectionManager {
	return NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        1000,
		MaxConnectionsPerUser: 100,
		IdleTimeout:           100 * time.Millisecond,
		CleanupInterval:       50 * time.Millisecond,
		EnableMetrics:         true,
	})
}

func TestConnectionManagerAddRemoveAndUserConnectionCount(t *testing.T) {
	cm := newConnectionManagerForTest()
	defer func() { _ = cm.Shutdown(context.Background()) }()

	userID := uuid.New()
	c1 := streaming.NewClient(context.Background(), userID)
	c2 := streaming.NewClient(context.Background(), userID)

	if err := cm.Register(c1); err != nil {
		t.Fatalf("register client1 failed: %v", err)
	}
	if err := cm.Register(c2); err != nil {
		t.Fatalf("register client2 failed: %v", err)
	}

	if got := len(cm.GetUserClients(userID)); got != 2 {
		t.Fatalf("expected 2 user connections, got %d", got)
	}

	if err := cm.Unregister(c1.ID); err != nil {
		t.Fatalf("unregister client1 failed: %v", err)
	}
	if got := len(cm.GetUserClients(userID)); got != 1 {
		t.Fatalf("expected 1 user connection after remove, got %d", got)
	}
}

func TestConnectionManagerConcurrentAccess(t *testing.T) {
	cm := newConnectionManagerForTest()
	defer func() { _ = cm.Shutdown(context.Background()) }()

	var wg sync.WaitGroup
	userID := uuid.New()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := streaming.NewClient(context.Background(), userID)
			_ = cm.Register(client)
			_, _ = cm.GetClient(client.ID)
			_ = cm.Unregister(client.ID)
		}()
	}
	wg.Wait()
}

func TestConnectionManagerBroadcastToAll(t *testing.T) {
	cm := newConnectionManagerForTest()
	defer func() { _ = cm.Shutdown(context.Background()) }()

	userID := uuid.New()
	c1 := streaming.NewClient(context.Background(), userID)
	c2 := streaming.NewClient(context.Background(), userID)
	_ = cm.Register(c1)
	_ = cm.Register(c2)

	ev := &domain.SSEEvent{ID: uuid.NewString(), Type: domain.SSEEventTypeSystemMessage, Data: "x", Timestamp: time.Now()}
	okCount, failCount := cm.BroadcastToAll(ev)
	if okCount != 2 || failCount != 0 {
		t.Fatalf("expected broadcast success to 2 clients, got success=%d fail=%d", okCount, failCount)
	}
}
