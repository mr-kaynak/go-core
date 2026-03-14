package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	rabbitmqPkg "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

// mockRedisBridge implements SSERedisBridge for testing cross-instance broadcasting.
type mockRedisBridge struct {
	mu         sync.Mutex
	published  []*domain.SSEEvent
	handler    func(event *domain.SSEEvent)
	subscribed bool
	subErr     error
	pubErr     error
	stopped    bool
}

func (m *mockRedisBridge) Publish(ctx context.Context, event *domain.SSEEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pubErr != nil {
		return m.pubErr
	}
	m.published = append(m.published, event)
	return nil
}

func (m *mockRedisBridge) OnEvent(handler func(event *domain.SSEEvent)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

func (m *mockRedisBridge) Subscribe(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribed = true
	return m.subErr
}

func (m *mockRedisBridge) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
}

func (m *mockRedisBridge) getPublished() []*domain.SSEEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*domain.SSEEvent, len(m.published))
	copy(cp, m.published)
	return cp
}

func TestSSEServiceRedisBridgePublishOnBroadcastToUser(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	bridge := &mockRedisBridge{}
	svc.SetRedisBridge(bridge)
	svc.config.EnableRedis = true

	userID := uuid.New()
	client := streaming.NewClient(context.Background(), userID)
	_ = svc.RegisterClient(client)

	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeNotification,
		Data:      "hello",
		Timestamp: time.Now(),
		UserID:    &userID,
	}

	err := svc.BroadcastToUser(context.Background(), userID, event)
	if err != nil {
		t.Fatalf("broadcast to user failed: %v", err)
	}

	// Give async processing time
	time.Sleep(50 * time.Millisecond)

	published := bridge.getPublished()
	if len(published) != 1 {
		t.Fatalf("expected 1 event published to redis bridge, got %d", len(published))
	}
}

func TestSSEServiceRedisBridgePublishOnBroadcastToAll(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	bridge := &mockRedisBridge{}
	svc.SetRedisBridge(bridge)
	svc.config.EnableRedis = true

	client := streaming.NewClient(context.Background(), uuid.New())
	_ = svc.RegisterClient(client)

	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeSystemMessage,
		Data:      "broadcast",
		Timestamp: time.Now(),
	}

	err := svc.BroadcastToAll(context.Background(), event)
	if err != nil {
		t.Fatalf("broadcast to all failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	published := bridge.getPublished()
	if len(published) != 1 {
		t.Fatalf("expected 1 event published to redis, got %d", len(published))
	}
}

func TestSSEServiceRedisBridgeNotPublishedWhenDisabled(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	bridge := &mockRedisBridge{}
	svc.SetRedisBridge(bridge)
	svc.config.EnableRedis = false // Redis disabled

	client := streaming.NewClient(context.Background(), uuid.New())
	_ = svc.RegisterClient(client)

	event := &domain.SSEEvent{
		ID: uuid.New().String(), Type: domain.SSEEventTypeSystemMessage,
		Data: "test", Timestamp: time.Now(),
	}
	_ = svc.BroadcastToAll(context.Background(), event)

	time.Sleep(50 * time.Millisecond)

	published := bridge.getPublished()
	if len(published) != 0 {
		t.Fatalf("expected no redis publish when disabled, got %d", len(published))
	}
}

func TestSSEServiceGetClientState(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	// Set heartbeat timeout for state tests (the test SSEConfig has zero value
	// which makes every client appear idle since time.Since(lastPing) > 0 is always true)
	svc.config.Heartbeat.Timeout = 5 * time.Minute

	t.Run("ready_client", func(t *testing.T) {
		client := streaming.NewClient(context.Background(), uuid.New())
		_ = svc.RegisterClient(client)

		state := svc.getClientState(client)
		if state != streaming.ConnectionStateReady {
			t.Fatalf("expected ready state, got %s", state)
		}
	})

	t.Run("closed_client", func(t *testing.T) {
		client := streaming.NewClient(context.Background(), uuid.New())
		_ = svc.RegisterClient(client)
		_ = client.Close()

		state := svc.getClientState(client)
		if state != streaming.ConnectionStateClosed {
			t.Fatalf("expected closed state, got %s", state)
		}
	})

	t.Run("unready_client", func(t *testing.T) {
		client := streaming.NewClient(context.Background(), uuid.New())
		_ = svc.RegisterClient(client)
		client.SetReady(false)

		state := svc.getClientState(client)
		if state != streaming.ConnectionStateConnecting {
			t.Fatalf("expected connecting state for unready client, got %s", state)
		}
	})

	t.Run("idle_client", func(t *testing.T) {
		client := streaming.NewClient(context.Background(), uuid.New())
		_ = svc.RegisterClient(client)
		// Set last ping to far in the past to trigger idle
		client.LastPing = time.Now().Add(-10 * time.Minute)

		state := svc.getClientState(client)
		if state != streaming.ConnectionStateError {
			t.Fatalf("expected error state for idle client, got %s", state)
		}
	})
}

func TestSSEServiceGetUserConnections(t *testing.T) {
	svc := newSSEServiceForTest()
	svc.config.Heartbeat.Timeout = 5 * time.Minute
	defer func() { _ = svc.Stop(context.Background()) }()

	userID := uuid.New()
	c1 := streaming.NewClient(context.Background(), userID)
	c1.IPAddress = "10.0.0.1"
	c1.UserAgent = "TestAgent"
	_ = svc.RegisterClient(c1)

	c2 := streaming.NewClient(context.Background(), userID)
	_ = svc.RegisterClient(c2)

	conns := svc.GetUserConnections(userID)
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(conns))
	}

	foundIP := false
	for _, conn := range conns {
		if conn.IPAddress == "10.0.0.1" {
			foundIP = true
		}
		if conn.State != streaming.ConnectionStateReady {
			t.Fatalf("expected ready state, got %s", conn.State)
		}
	}
	if !foundIP {
		t.Fatal("expected to find connection with IP 10.0.0.1")
	}
}

func TestSSEServiceGetAllConnections(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	for i := 0; i < 5; i++ {
		c := streaming.NewClient(context.Background(), uuid.New())
		_ = svc.RegisterClient(c)
	}

	// Without limit
	conns := svc.GetAllConnections(0)
	if len(conns) != 5 {
		t.Fatalf("expected 5 connections without limit, got %d", len(conns))
	}

	// With limit
	conns = svc.GetAllConnections(3)
	if len(conns) != 3 {
		t.Fatalf("expected 3 connections with limit, got %d", len(conns))
	}
}

func TestSSEServiceGetStats(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	stats := svc.GetStats()
	if stats["server_id"] != "test-sse" {
		t.Fatalf("expected server_id=test-sse, got %v", stats["server_id"])
	}
	if stats["started"] != true {
		t.Fatal("expected started=true")
	}
	cmStats, ok := stats["connection_manager"].(map[string]interface{})
	if !ok {
		t.Fatal("expected connection_manager stats")
	}
	if cmStats["total_connections"] != 0 {
		t.Fatalf("expected 0 connections, got %v", cmStats["total_connections"])
	}
}

func TestSSEServiceGetServerID(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	if svc.GetServerID() != "test-sse" {
		t.Fatalf("expected server ID test-sse, got %s", svc.GetServerID())
	}
}

func TestSSEServiceNotRunningRejectsOperations(t *testing.T) {
	svc := newSSEServiceForTest()
	svc.mu.Lock()
	svc.started = false
	svc.mu.Unlock()

	client := streaming.NewClient(context.Background(), uuid.New())
	err := svc.RegisterClient(client)
	if err == nil {
		t.Fatal("expected error when registering on stopped service")
	}

	err = svc.BroadcastToUser(context.Background(), uuid.New(), &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification, Data: "test", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error on broadcast when not running")
	}

	err = svc.BroadcastToUsers(context.Background(), []uuid.UUID{uuid.New()}, &domain.SSEEvent{
		ID: "2", Type: domain.SSEEventTypeNotification, Data: "test", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error on broadcast to users when not running")
	}

	err = svc.BroadcastToAll(context.Background(), &domain.SSEEvent{
		ID: "3", Type: domain.SSEEventTypeNotification, Data: "test", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error on broadcast to all when not running")
	}

	err = svc.BroadcastToChannel(context.Background(), "test", &domain.SSEEvent{
		ID: "4", Type: domain.SSEEventTypeNotification, Data: "test", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error on broadcast to channel when not running")
	}
}

func TestSSEServiceSendNotificationEventWhenNotRunning(t *testing.T) {
	svc := newSSEServiceForTest()
	svc.mu.Lock()
	svc.started = false
	svc.mu.Unlock()

	// Should silently return nil
	err := svc.SendNotificationEvent(&domain.Notification{
		ID:     uuid.New(),
		UserID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("expected nil error when service not running, got %v", err)
	}
}

func TestSSEServiceIsHealthy(t *testing.T) {
	svc := newSSEServiceForTest()

	// Not healthy when heartbeat has not sent anything
	healthy := svc.IsHealthy()
	if healthy {
		t.Fatal("expected not healthy before heartbeat starts")
	}

	// Start heartbeat to make it healthy
	_ = svc.heartbeat.Start()
	time.Sleep(50 * time.Millisecond)

	healthy = svc.IsHealthy()
	if !healthy {
		t.Fatal("expected healthy after heartbeat starts")
	}

	_ = svc.Stop(context.Background())
}

func TestSSEServiceProcessAcknowledgment(t *testing.T) {
	svc := newSSEServiceForTest()
	defer func() { _ = svc.Stop(context.Background()) }()

	// Should not panic
	svc.ProcessAcknowledgment(uuid.New(), "event-123")
}

func TestConnectionManagerIdleCleanup(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           50 * time.Millisecond,
		CleanupInterval:       30 * time.Millisecond,
	})
	cm.Start()
	defer func() { _ = cm.Shutdown(context.Background()) }()

	client := streaming.NewClient(context.Background(), uuid.New())
	// Set last ping far in the past
	client.LastPing = time.Now().Add(-200 * time.Millisecond)
	_ = cm.Register(client)

	// Wait for cleanup to run
	time.Sleep(100 * time.Millisecond)

	stats := cm.GetStats()
	if stats.TotalConnections != 0 {
		t.Fatalf("expected idle connection to be cleaned up, got %d connections", stats.TotalConnections)
	}
}

func TestConnectionManagerEvictsOldestOnPerUserLimit(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 2,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	userID := uuid.New()
	c1 := streaming.NewClient(context.Background(), userID)
	c2 := streaming.NewClient(context.Background(), userID)
	c3 := streaming.NewClient(context.Background(), userID)

	_ = cm.Register(c1)
	_ = cm.Register(c2)

	// Third registration should evict c1
	_ = cm.Register(c3)

	// c1 should be evicted
	_, err := cm.GetClient(c1.ID)
	if err == nil {
		t.Fatal("expected c1 to be evicted")
	}

	// c2 and c3 should still be present
	_, err = cm.GetClient(c2.ID)
	if err != nil {
		t.Fatalf("expected c2 to still exist: %v", err)
	}
	_, err = cm.GetClient(c3.ID)
	if err != nil {
		t.Fatalf("expected c3 to still exist: %v", err)
	}
}

func TestConnectionManagerMaxConnectionsLimit(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        2,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	c1 := streaming.NewClient(context.Background(), uuid.New())
	c2 := streaming.NewClient(context.Background(), uuid.New())
	c3 := streaming.NewClient(context.Background(), uuid.New())

	_ = cm.Register(c1)
	_ = cm.Register(c2)

	err := cm.Register(c3)
	if err == nil {
		t.Fatal("expected error when max connections reached")
	}
}

func TestConnectionManagerIsUserConnected(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	userID := uuid.New()
	if cm.IsUserConnected(userID) {
		t.Fatal("expected user to not be connected")
	}

	client := streaming.NewClient(context.Background(), userID)
	_ = cm.Register(client)

	if !cm.IsUserConnected(userID) {
		t.Fatal("expected user to be connected")
	}
}

func TestConnectionManagerGetConnectedUsers(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	u1 := uuid.New()
	u2 := uuid.New()
	_ = cm.Register(streaming.NewClient(context.Background(), u1))
	_ = cm.Register(streaming.NewClient(context.Background(), u2))

	users := cm.GetConnectedUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 connected users, got %d", len(users))
	}
}

func TestWorkerPoolExhaustion(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)
	// Use a tiny semaphore to test pool exhaustion
	svc.sem = make(chan struct{}, 1)

	// Fill the pool
	svc.sem <- struct{}{}

	// Submit should drop the task when pool is full
	called := false
	svc.submit("test-task", func() { called = true })

	time.Sleep(50 * time.Millisecond)
	if called {
		t.Fatal("expected task to be dropped when pool is full")
	}

	// Release the semaphore
	<-svc.sem
}

func TestNotificationServiceShutdownTimeout(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	// Start a blocking task
	svc.wg.Add(1)
	go func() {
		defer svc.wg.Done()
		time.Sleep(5 * time.Second) // long running task
	}()

	// Shutdown with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := svc.Shutdown(ctx)
	if err == nil {
		t.Fatal("expected timeout error from shutdown")
	}
}

func TestSSEServiceStartWithRedisBridgeSubscribeFailure(t *testing.T) {
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
	})
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: time.Minute,
		Timeout:  2 * time.Minute,
	})
	ctx, cancel := context.WithCancel(context.Background())

	svc := &SSEService{
		connManager: cm,
		broadcaster: eb,
		heartbeat:   hm,
		config: SSEConfig{
			Enabled:             true,
			EnableRedis:         true,
			MetricsPushInterval: time.Minute,
		},
		serverID: "test",
		logger:   logger.Get().WithField("service", "sse-test"),
		ctx:      ctx,
		cancel:   cancel,
	}

	bridge := &mockRedisBridge{
		subErr: context.DeadlineExceeded,
	}
	svc.SetRedisBridge(bridge)

	// Start should succeed even if Redis subscribe fails (non-fatal)
	err := svc.Start()
	if err != nil {
		t.Fatalf("expected start to succeed despite redis failure, got %v", err)
	}

	_ = svc.Stop(context.Background())
}

func TestSSEServiceStopWithRedisBridge(t *testing.T) {
	svc := newSSEServiceForTest()
	bridge := &mockRedisBridge{}
	svc.SetRedisBridge(bridge)
	svc.config.EnableRedis = true

	// Start the service
	_ = svc.heartbeat.Start()

	// Stop should call bridge.Stop()
	_ = svc.Stop(context.Background())

	bridge.mu.Lock()
	stopped := bridge.stopped
	bridge.mu.Unlock()

	if !stopped {
		t.Fatal("expected redis bridge to be stopped")
	}
}

func TestHeartbeatManagerHandlePong(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers: 1, QueueSize: 50,
		ProcessTimeout: time.Second,
		EnableBatching: false,
	})
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: time.Minute, Timeout: 2 * time.Minute,
	})

	userID := uuid.New()
	client := streaming.NewClient(context.Background(), userID)
	client.SetReady(false) // Simulate unhealthy client
	_ = cm.Register(client)

	// Handle pong should recover the client
	hm.HandlePong(client.ID)

	if !client.IsReady() {
		t.Fatal("expected client to be recovered after pong")
	}
}

func TestHeartbeatManagerHandlePongUnknownClient(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections:        100,
		MaxConnectionsPerUser: 10,
		IdleTimeout:           time.Minute,
		CleanupInterval:       time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers: 1, QueueSize: 50,
		ProcessTimeout: time.Second,
		EnableBatching: false,
	})
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: time.Minute, Timeout: 2 * time.Minute,
	})

	// Should not panic for unknown client
	hm.HandlePong(uuid.New())
}

func TestHeartbeatManagerStartIdempotent(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections: 100, MaxConnectionsPerUser: 10,
		IdleTimeout: time.Minute, CleanupInterval: time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers: 1, QueueSize: 50,
		ProcessTimeout: time.Second, EnableBatching: false,
	})
	cm.Start()
	eb.Start()
	hm := NewHeartbeatManager(cm, eb, HeartbeatConfig{
		Interval: 50 * time.Millisecond, Timeout: 100 * time.Millisecond,
	})
	defer func() {
		hm.Stop()
		_ = eb.Shutdown(context.Background())
		_ = cm.Shutdown(context.Background())
	}()

	err := hm.Start()
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Second start should be idempotent (no error, no second goroutine)
	err = hm.Start()
	if err != nil {
		t.Fatalf("second start failed: %v", err)
	}
}

func TestEventBroadcasterStoppedRejectsJobs(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections: 100, MaxConnectionsPerUser: 10,
		IdleTimeout: time.Minute, CleanupInterval: time.Minute,
	})
	eb := NewEventBroadcaster(cm, BroadcasterConfig{
		MaxWorkers: 1, QueueSize: 50,
		ProcessTimeout: time.Second, EnableBatching: false,
	})
	cm.Start()
	eb.Start()

	_ = eb.Shutdown(context.Background())

	err := eb.BroadcastToAll(context.Background(), &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeSystemMessage, Data: "test", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when broadcasting to stopped broadcaster")
	}

	_ = cm.Shutdown(context.Background())
}

func TestConnectionManagerBroadcastToUser(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections: 100, MaxConnectionsPerUser: 10,
		IdleTimeout: time.Minute, CleanupInterval: time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	userID := uuid.New()
	c1 := streaming.NewClient(context.Background(), userID)
	c2 := streaming.NewClient(context.Background(), userID)
	_ = cm.Register(c1)
	_ = cm.Register(c2)

	ev := &domain.SSEEvent{
		ID: uuid.New().String(), Type: domain.SSEEventTypeNotification,
		Data: "hello", Timestamp: time.Now(),
	}
	okCount, failCount := cm.BroadcastToUser(userID, ev)
	if okCount != 2 || failCount != 0 {
		t.Fatalf("expected 2 success 0 fail, got ok=%d fail=%d", okCount, failCount)
	}
}

func TestConnectionManagerBroadcastToUserNoClients(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections: 100, MaxConnectionsPerUser: 10,
		IdleTimeout: time.Minute, CleanupInterval: time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	okCount, failCount := cm.BroadcastToUser(uuid.New(), &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification, Data: "test", Timestamp: time.Now(),
	})
	if okCount != 0 || failCount != 0 {
		t.Fatalf("expected 0 ok 0 fail for no clients, got ok=%d fail=%d", okCount, failCount)
	}
}

func TestNotificationServiceGetNotification(t *testing.T) {
	repo := &notificationRepoStub{
		getNotificationFn: func(id uuid.UUID) (*domain.Notification, error) {
			return &domain.Notification{ID: id, Subject: "test"}, nil
		},
	}
	svc := newNotificationServiceForTest(repo)

	n, err := svc.GetNotification(uuid.New())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if n.Subject != "test" {
		t.Fatalf("expected subject 'test', got %q", n.Subject)
	}
}

func TestNotificationServiceGetNotificationNotFound(t *testing.T) {
	repo := &notificationRepoStub{
		getNotificationFn: func(id uuid.UUID) (*domain.Notification, error) {
			return nil, ErrClientNotFound
		},
	}
	svc := newNotificationServiceForTest(repo)

	_, err := svc.GetNotification(uuid.New())
	if err == nil {
		t.Fatal("expected error for not found notification")
	}
}

func TestNotificationServiceCountUserNotifications(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	count, err := svc.CountUserNotifications(uuid.New())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestNotificationServiceGetUserPreferencesDefault(t *testing.T) {
	repo := &notificationRepoStub{
		getUserPrefsFn: func(userID uuid.UUID) (*domain.NotificationPreference, error) {
			return nil, nil // No existing preferences
		},
	}
	svc := newNotificationServiceForTest(repo)

	prefs, err := svc.GetUserPreferences(uuid.New())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if prefs == nil {
		t.Fatal("expected default preferences to be created")
	}
	if !prefs.EmailEnabled {
		t.Fatal("expected email enabled by default")
	}
	if !prefs.InAppEnabled {
		t.Fatal("expected in-app enabled by default")
	}
}

func TestNotificationServiceUpdateUserPreferences(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	userID := uuid.New()
	pref := &domain.NotificationPreference{
		EmailEnabled: true,
		InAppEnabled: false,
	}
	err := svc.UpdateUserPreferences(userID, pref)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if pref.UserID != userID {
		t.Fatal("expected UserID to be set")
	}
}

func TestNotificationServiceParsePriority(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	tests := []struct {
		input    string
		expected domain.NotificationPriority
	}{
		{"low", domain.NotificationPriorityLow},
		{"high", domain.NotificationPriorityHigh},
		{"urgent", domain.NotificationPriorityUrgent},
		{"normal", domain.NotificationPriorityNormal},
		{"unknown", domain.NotificationPriorityNormal},
		{"", domain.NotificationPriorityNormal},
	}

	for _, tc := range tests {
		result := svc.parsePriority(tc.input)
		if result != tc.expected {
			t.Fatalf("parsePriority(%q) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}

func TestNotificationServiceSendEmailPreferencesDisabled(t *testing.T) {
	repo := &notificationRepoStub{
		getUserPrefsFn: func(userID uuid.UUID) (*domain.NotificationPreference, error) {
			return &domain.NotificationPreference{EmailEnabled: false}, nil
		},
	}
	svc := newNotificationServiceForTest(repo)

	_, err := svc.SendEmail(&SendEmailRequest{
		UserID:   uuid.New(),
		To:       []string{"test@example.com"},
		Subject:  "Test",
		Template: "test",
	})
	if err == nil {
		t.Fatal("expected error when email is disabled")
	}
}

func TestNotificationServiceMarshalRecipients(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	// Empty recipients
	result := svc.marshalRecipients(nil)
	if string(result) != "[]" {
		t.Fatalf("expected empty array, got %s", result)
	}

	// With recipients
	result = svc.marshalRecipients([]string{"a@b.com", "c@d.com"})
	if string(result) == "[]" {
		t.Fatal("expected non-empty array")
	}
}

func TestNotificationServiceGetSSEService(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	// No SSE service set
	if svc.GetSSEService() != nil {
		t.Fatal("expected nil SSE service")
	}
}

func TestNotificationServiceSendEmailCreateError(t *testing.T) {
	repo := &notificationRepoStub{
		createNotificationFn: func(notification *domain.Notification) error {
			return fmt.Errorf("db error")
		},
	}
	svc := newNotificationServiceForTest(repo)

	_, err := svc.SendEmail(&SendEmailRequest{
		UserID:   uuid.New(),
		To:       []string{"test@example.com"},
		Subject:  "Test",
		Template: "test",
	})
	if err == nil {
		t.Fatal("expected error on create failure")
	}
}

func TestNotificationServiceSendEmailScheduled(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	scheduled := time.Now().Add(1 * time.Hour)
	n, err := svc.SendEmail(&SendEmailRequest{
		UserID:      uuid.New(),
		To:          []string{"test@example.com"},
		Subject:     "Test",
		Template:    "test",
		Priority:    "high",
		ScheduledAt: &scheduled,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if n == nil {
		t.Fatal("expected notification to be returned")
	}
	if n.Status != domain.NotificationStatusPending {
		t.Fatalf("expected pending status for scheduled, got %s", n.Status)
	}
}

func TestNotificationServiceSendEmailWithCCBCC(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	scheduled := time.Now().Add(1 * time.Hour)
	n, err := svc.SendEmail(&SendEmailRequest{
		UserID:      uuid.New(),
		To:          []string{"to@example.com"},
		CC:          []string{"cc@example.com"},
		BCC:         []string{"bcc@example.com"},
		Subject:     "Test",
		Template:    "test",
		Data:        map[string]interface{}{"key": "value"},
		ScheduledAt: &scheduled,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if n == nil {
		t.Fatal("expected notification")
	}
}

func TestNotificationServiceProcessNotificationUnknownType(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	n := &domain.Notification{
		ID:     uuid.New(),
		Type:   "unknown_type",
		Status: domain.NotificationStatusPending,
		UserID: uuid.New(),
	}
	svc.processNotification(context.Background(), n)
	if n.Status != domain.NotificationStatusFailed {
		t.Fatalf("expected failed for unknown type, got %s", n.Status)
	}
}

func TestNotificationServiceWebhookSentWithProvider(t *testing.T) {
	repo := &notificationRepoStub{}
	sent := false
	svc := newNotificationServiceForTest(repo)
	svc.SetWebhookProvider(&webhookProviderStub{
		sendFn: func(ctx context.Context, url string, payload interface{}) error {
			sent = true
			if url != "https://example.com/hook" {
				t.Fatalf("expected webhook URL, got %s", url)
			}
			return nil
		},
	})

	n := &domain.Notification{
		ID:       uuid.New(),
		Type:     domain.NotificationTypeWebhook,
		Status:   domain.NotificationStatusPending,
		UserID:   uuid.New(),
		Subject:  "subject",
		Content:  "body",
		Metadata: json.RawMessage(`{"webhook_url":"https://example.com/hook"}`),
	}
	svc.processNotification(context.Background(), n)
	if !sent {
		t.Fatal("expected webhook provider to be called")
	}
	if n.Status != domain.NotificationStatusSent {
		t.Fatalf("expected sent status, got %s", n.Status)
	}
}

func TestNotificationServiceSMSSentWithProvider(t *testing.T) {
	repo := &notificationRepoStub{}
	sent := false
	svc := newNotificationServiceForTest(repo)
	svc.SetSMSProvider(&smsProviderStub{
		sendFn: func(ctx context.Context, phoneNumber, message string) error {
			sent = true
			if phoneNumber != "+905001112233" {
				t.Fatalf("expected phone number, got %s", phoneNumber)
			}
			return nil
		},
	})

	n := &domain.Notification{
		ID:       uuid.New(),
		Type:     domain.NotificationTypeSMS,
		Status:   domain.NotificationStatusPending,
		UserID:   uuid.New(),
		Content:  "hello",
		Metadata: json.RawMessage(`{"phone":"+905001112233"}`),
	}
	svc.processNotification(context.Background(), n)
	if !sent {
		t.Fatal("expected SMS provider to be called")
	}
	if n.Status != domain.NotificationStatusSent {
		t.Fatalf("expected sent status, got %s", n.Status)
	}
}

func TestNotificationServiceConvertPriority(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	tests := []struct {
		input    domain.NotificationPriority
		expected string // just check it doesn't panic
	}{
		{domain.NotificationPriorityLow, "low"},
		{domain.NotificationPriorityNormal, "normal"},
		{domain.NotificationPriorityHigh, "high"},
		{domain.NotificationPriorityUrgent, "urgent"},
	}

	for _, tc := range tests {
		// Just verify it doesn't panic
		_ = svc.convertPriority(tc.input)
	}
}

func TestNotificationServiceSetMetrics(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	// getMetrics should return global singleton when no custom metrics set
	m := svc.getMetrics()
	if m == nil {
		t.Fatal("expected non-nil metrics from global singleton")
	}
}

func TestNotificationServiceHandleMessageMissingID(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	msg := &rabbitmqPkg.Message{
		Type: "notification.process",
		Data: map[string]interface{}{}, // missing notification_id
	}
	err := svc.handleNotificationMessage(msg)
	if err == nil {
		t.Fatal("expected error for missing notification_id")
	}
}

func TestNotificationServiceHandleMessageInvalidID(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	msg := &rabbitmqPkg.Message{
		Type: "notification.process",
		Data: map[string]interface{}{"notification_id": "not-a-uuid"},
	}
	err := svc.handleNotificationMessage(msg)
	if err == nil {
		t.Fatal("expected error for invalid notification_id")
	}
}

func TestNotificationServiceHandleMessageNotFound(t *testing.T) {
	repo := &notificationRepoStub{
		getNotificationFn: func(id uuid.UUID) (*domain.Notification, error) {
			return nil, nil // Not found
		},
	}
	svc := newNotificationServiceForTest(repo)

	msg := &rabbitmqPkg.Message{
		Type: "notification.process",
		Data: map[string]interface{}{"notification_id": uuid.New().String()},
	}
	err := svc.handleNotificationMessage(msg)
	if err != nil {
		t.Fatalf("expected nil error for not found notification, got %v", err)
	}
}

func TestNotificationServiceProcessPendingNotifications(t *testing.T) {
	repo := &notificationRepoStub{}
	svc := newNotificationServiceForTest(repo)

	err := svc.ProcessPendingNotifications()
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestSSEServiceBroadcastToChannel(t *testing.T) {
	svc := newSSEServiceForTest()
	svc.config.Heartbeat.Timeout = 5 * time.Minute
	defer func() { _ = svc.Stop(context.Background()) }()

	userID := uuid.New()
	client := streaming.NewClient(context.Background(), userID)
	_ = svc.RegisterClient(client)
	client.Subscribe("test-channel")

	err := svc.BroadcastToChannel(context.Background(), "test-channel", &domain.SSEEvent{
		ID: uuid.New().String(), Type: domain.SSEEventTypeSystemMessage,
		Data: "channel msg", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("broadcast to channel failed: %v", err)
	}

	// Verify client received the event
	select {
	case ev := <-client.Channel:
		if ev == nil {
			t.Fatal("expected non-nil event")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected event to be delivered to subscribed client")
	}
}

func TestSSEServiceSendNotificationEvent(t *testing.T) {
	svc := newSSEServiceForTest()
	svc.config.Heartbeat.Timeout = 5 * time.Minute
	defer func() { _ = svc.Stop(context.Background()) }()

	userID := uuid.New()
	client := streaming.NewClient(context.Background(), userID)
	_ = svc.RegisterClient(client)

	n := &domain.Notification{
		ID:       uuid.New(),
		UserID:   userID,
		Type:     domain.NotificationTypeInApp,
		Priority: domain.NotificationPriorityNormal,
		Subject:  "Test",
		Content:  "Hello",
	}

	err := svc.SendNotificationEvent(n)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// Wait for event
	time.Sleep(100 * time.Millisecond)
}

func TestConnectionManagerGetClientsByFilter(t *testing.T) {
	cm := NewConnectionManager(ConnectionManagerConfig{
		MaxConnections: 100, MaxConnectionsPerUser: 10,
		IdleTimeout: time.Minute, CleanupInterval: time.Minute,
	})
	defer func() { _ = cm.Shutdown(context.Background()) }()

	u1 := uuid.New()
	u2 := uuid.New()
	c1 := streaming.NewClient(context.Background(), u1)
	c1.Subscribe("alerts")
	c2 := streaming.NewClient(context.Background(), u2)

	_ = cm.Register(c1)
	_ = cm.Register(c2)

	filtered := cm.GetClientsByFilter(func(c *streaming.Client) bool {
		return c.IsSubscribed("alerts")
	})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered client, got %d", len(filtered))
	}
	if filtered[0].ID != c1.ID {
		t.Fatal("expected c1 to match filter")
	}
}
