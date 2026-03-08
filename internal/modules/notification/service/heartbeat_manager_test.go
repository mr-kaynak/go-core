package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

func TestHeartbeatManagerGeneratesHeartbeats(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers:     1,
		QueueSize:      50,
		MaxRetries:     1,
		RetryDelay:     5 * time.Millisecond,
		ProcessTimeout: time.Second,
		EnableBatching: false,
	})
	cm.Start()
	eb.Start()
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: 20 * time.Millisecond,
		Timeout:  40 * time.Millisecond,
	})
	defer func() {
		_ = hm.Shutdown(context.Background())
		_ = eb.Shutdown(context.Background())
		_ = cm.Shutdown(context.Background())
	}()

	if err := hm.Start(); err != nil {
		t.Fatalf("start heartbeat manager failed: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	stats := hm.GetStats()
	if stats.HeartbeatsSent == 0 {
		t.Fatalf("expected heartbeat messages to be sent")
	}
}

func TestHeartbeatManagerMarksTimedOutClientsUnready(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers:     1,
		QueueSize:      50,
		MaxRetries:     1,
		RetryDelay:     5 * time.Millisecond,
		ProcessTimeout: time.Second,
		EnableBatching: false,
	})
	cm.Start()
	eb.Start()
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: 20 * time.Millisecond,
		Timeout:  30 * time.Millisecond,
	})
	defer func() {
		_ = hm.Shutdown(context.Background())
		_ = eb.Shutdown(context.Background())
		_ = cm.Shutdown(context.Background())
	}()

	client := streaming.NewClient(context.Background(), uuid.New())
	client.LastPing = time.Now().Add(-time.Minute)
	_ = cm.Register(client)

	if err := hm.Start(); err != nil {
		t.Fatalf("start heartbeat manager failed: %v", err)
	}
	time.Sleep(70 * time.Millisecond)

	if client.IsReady() {
		t.Fatalf("expected timed out client to be marked unready")
	}
}
