package streaming

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()
	if opts.BufferSize != 100 {
		t.Fatalf("expected BufferSize=100, got %d", opts.BufferSize)
	}
	if opts.SendTimeout != 5*time.Second {
		t.Fatalf("expected SendTimeout=5s, got %v", opts.SendTimeout)
	}
	if opts.MaxMessageSize != 1024*1024 {
		t.Fatalf("expected MaxMessageSize=1MB, got %d", opts.MaxMessageSize)
	}
	if !opts.EnableMetrics {
		t.Fatal("expected EnableMetrics=true")
	}
}

func TestNewClient(t *testing.T) {
	userID := uuid.New()
	client := NewClient(context.Background(), userID)
	defer client.Close()

	if client.UserID != userID {
		t.Fatalf("expected UserID=%v, got %v", userID, client.UserID)
	}
	if client.ID == uuid.Nil {
		t.Fatal("expected non-nil client ID")
	}
	if !client.IsReady() {
		t.Fatal("expected client to be ready")
	}
	if client.IsClosed() {
		t.Fatal("expected client to not be closed")
	}
	if cap(client.Channel) != 100 {
		t.Fatalf("expected default buffer size=100, got %d", cap(client.Channel))
	}
}

func TestNewClientWithOptions(t *testing.T) {
	userID := uuid.New()
	opts := ClientOptions{
		BufferSize:     50,
		SendTimeout:    2 * time.Second,
		MaxMessageSize: 512 * 1024,
		EnableMetrics:  false,
	}
	client := NewClientWithOptions(context.Background(), userID, opts)
	defer client.Close()

	if cap(client.Channel) != 50 {
		t.Fatalf("expected buffer size=50, got %d", cap(client.Channel))
	}
	if client.options.SendTimeout != 2*time.Second {
		t.Fatalf("expected SendTimeout=2s, got %v", client.options.SendTimeout)
	}
}

func TestClientSendSuccess(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeNotification,
		Data:      "test",
		Timestamp: time.Now(),
	}

	err := client.Send(event)
	if err != nil {
		t.Fatalf("expected send success, got %v", err)
	}

	stats := client.GetStats()
	if stats.MessagesSent != 1 {
		t.Fatalf("expected 1 message sent, got %d", stats.MessagesSent)
	}
	if stats.BytesTransferred == 0 {
		t.Fatal("expected non-zero bytes transferred")
	}
	if stats.BufferSize != 1 {
		t.Fatalf("expected 1 message in buffer, got %d", stats.BufferSize)
	}
}

func TestClientSendToClosedClient(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	_ = client.Close()

	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeNotification,
		Data:      "test",
		Timestamp: time.Now(),
	}

	err := client.Send(event)
	if err != ErrClientClosed {
		t.Fatalf("expected ErrClientClosed, got %v", err)
	}

	stats := client.GetStats()
	if stats.MessagesDropped != 1 {
		t.Fatalf("expected 1 message dropped, got %d", stats.MessagesDropped)
	}
}

func TestClientSendTimeout(t *testing.T) {
	opts := ClientOptions{
		BufferSize:     1,
		SendTimeout:    20 * time.Millisecond,
		MaxMessageSize: 1024,
		EnableMetrics:  true,
	}
	client := NewClientWithOptions(context.Background(), uuid.New(), opts)
	defer client.Close()

	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeNotification,
		Data:      "fill",
		Timestamp: time.Now(),
	}

	// Fill the buffer
	err := client.Send(event)
	if err != nil {
		t.Fatalf("first send should succeed: %v", err)
	}

	// Second send should timeout since buffer is full
	event2 := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeNotification,
		Data:      "overflow",
		Timestamp: time.Now(),
	}
	err = client.Send(event2)
	if err != ErrSendTimeout {
		t.Fatalf("expected ErrSendTimeout, got %v", err)
	}

	stats := client.GetStats()
	if stats.MessagesDropped != 1 {
		t.Fatalf("expected 1 message dropped due to timeout, got %d", stats.MessagesDropped)
	}
}

func TestClientSendContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	opts := ClientOptions{
		BufferSize:     1,
		SendTimeout:    5 * time.Second,
		MaxMessageSize: 1024,
		EnableMetrics:  true,
	}
	client := NewClientWithOptions(ctx, uuid.New(), opts)
	defer client.Close()

	// Fill the buffer
	_ = client.Send(&domain.SSEEvent{
		ID: uuid.New().String(), Type: domain.SSEEventTypeHeartbeat,
		Data: "fill", Timestamp: time.Now(),
	})

	// Cancel the context before the next send
	cancel()

	err := client.Send(&domain.SSEEvent{
		ID: uuid.New().String(), Type: domain.SSEEventTypeHeartbeat,
		Data: "should fail", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestClientTrySend(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeNotification,
		Data:      "test",
		Timestamp: time.Now(),
	}

	ok := client.TrySend(event)
	if !ok {
		t.Fatal("expected TrySend to succeed")
	}

	stats := client.GetStats()
	if stats.MessagesSent != 1 {
		t.Fatalf("expected 1 message sent, got %d", stats.MessagesSent)
	}
}

func TestClientTrySendBufferFull(t *testing.T) {
	opts := ClientOptions{
		BufferSize:     1,
		SendTimeout:    time.Second,
		MaxMessageSize: 1024,
		EnableMetrics:  true,
	}
	client := NewClientWithOptions(context.Background(), uuid.New(), opts)
	defer client.Close()

	// Fill the buffer
	_ = client.TrySend(&domain.SSEEvent{
		ID: "a", Type: domain.SSEEventTypeHeartbeat, Data: "fill", Timestamp: time.Now(),
	})

	// Should fail non-blocking
	ok := client.TrySend(&domain.SSEEvent{
		ID: "b", Type: domain.SSEEventTypeHeartbeat, Data: "overflow", Timestamp: time.Now(),
	})
	if ok {
		t.Fatal("expected TrySend to return false when buffer is full")
	}

	stats := client.GetStats()
	if stats.MessagesDropped != 1 {
		t.Fatalf("expected 1 dropped, got %d", stats.MessagesDropped)
	}
}

func TestClientTrySendClosedClient(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	_ = client.Close()

	ok := client.TrySend(&domain.SSEEvent{
		ID: "x", Type: domain.SSEEventTypeHeartbeat, Data: "test", Timestamp: time.Now(),
	})
	if ok {
		t.Fatal("expected TrySend to return false for closed client")
	}
}

func TestClientCloseIdempotent(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	err := client.Close()
	if err != nil {
		t.Fatalf("first close should succeed: %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}
	if !client.IsClosed() {
		t.Fatal("expected client to be closed")
	}
	if client.IsReady() {
		t.Fatal("expected closed client to not be ready")
	}
}

func TestClientSubscribeUnsubscribe(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	client.Subscribe("alerts")
	client.Subscribe("metrics")

	if !client.IsSubscribed("alerts") {
		t.Fatal("expected client to be subscribed to 'alerts'")
	}
	if !client.IsSubscribed("metrics") {
		t.Fatal("expected client to be subscribed to 'metrics'")
	}
	if client.IsSubscribed("unknown") {
		t.Fatal("expected client to not be subscribed to 'unknown'")
	}

	subs := client.GetSubscriptions()
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}

	client.Unsubscribe("alerts")
	if client.IsSubscribed("alerts") {
		t.Fatal("expected client to be unsubscribed from 'alerts'")
	}

	subs = client.GetSubscriptions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription after unsubscribe, got %d", len(subs))
	}
}

func TestClientSetReadyState(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	if !client.IsReady() {
		t.Fatal("expected client to be ready initially")
	}

	client.SetReady(false)
	if client.IsReady() {
		t.Fatal("expected client to not be ready after SetReady(false)")
	}

	client.SetReady(true)
	if !client.IsReady() {
		t.Fatal("expected client to be ready after SetReady(true)")
	}
}

func TestClientIsIdleAndPingTracking(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	// Client should not be idle right after creation
	if client.IsIdle(100 * time.Millisecond) {
		t.Fatal("expected client to not be idle immediately")
	}

	// Manipulate last ping to the past
	client.mu.Lock()
	client.LastPing = time.Now().Add(-200 * time.Millisecond)
	client.mu.Unlock()

	if !client.IsIdle(100 * time.Millisecond) {
		t.Fatal("expected client to be idle after 200ms with 100ms threshold")
	}

	// UpdatePing should reset idle state
	client.UpdatePing()
	if client.IsIdle(100 * time.Millisecond) {
		t.Fatal("expected client to not be idle after UpdatePing")
	}

	// Verify GetLastPing returns updated time
	lastPing := client.GetLastPing()
	if time.Since(lastPing) > 50*time.Millisecond {
		t.Fatal("expected GetLastPing to return recent time after UpdatePing")
	}
}

func TestClientGetLastMessage(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	lastMsg := client.GetLastMessage()
	if time.Since(lastMsg) > 50*time.Millisecond {
		t.Fatal("expected LastMessage to be set on creation")
	}
}

func TestClientGetStats(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	client.Subscribe("alerts")
	client.Subscribe("updates")

	event := &domain.SSEEvent{
		ID: uuid.New().String(), Type: domain.SSEEventTypeNotification,
		Data: "msg", Timestamp: time.Now(),
	}
	_ = client.Send(event)

	stats := client.GetStats()
	if stats.ClientID != client.ID {
		t.Fatalf("expected client ID match")
	}
	if stats.UserID != client.UserID {
		t.Fatalf("expected user ID match")
	}
	if stats.MessagesSent != 1 {
		t.Fatalf("expected 1 message sent, got %d", stats.MessagesSent)
	}
	if stats.BytesTransferred == 0 {
		t.Fatal("expected non-zero bytes")
	}
	if stats.BufferSize != 1 {
		t.Fatalf("expected buffer size=1, got %d", stats.BufferSize)
	}
	if stats.BufferCapacity != 100 {
		t.Fatalf("expected buffer capacity=100, got %d", stats.BufferCapacity)
	}
	if stats.Subscriptions != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", stats.Subscriptions)
	}
	if !stats.IsReady {
		t.Fatal("expected IsReady=true")
	}
}

func TestClientGetStatsAfterClose(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	_ = client.Close()

	stats := client.GetStats()
	if stats.IsReady {
		t.Fatal("expected IsReady=false after close")
	}
}

func TestClientShouldReceiveEvent_UserIDFiltering(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	client := NewClient(context.Background(), userID)
	defer client.Close()

	// Event targeted to this user
	event := &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification,
		Data: "test", UserID: &userID, Timestamp: time.Now(),
	}
	if !client.ShouldReceiveEvent(event) {
		t.Fatal("expected client to receive event targeted to its user")
	}

	// Event targeted to another user
	event2 := &domain.SSEEvent{
		ID: "2", Type: domain.SSEEventTypeNotification,
		Data: "test", UserID: &otherUserID, Timestamp: time.Now(),
	}
	if client.ShouldReceiveEvent(event2) {
		t.Fatal("expected client to not receive event targeted to another user")
	}

	// Broadcast event (nil UserID)
	event3 := &domain.SSEEvent{
		ID: "3", Type: domain.SSEEventTypeNotification,
		Data: "test", Timestamp: time.Now(),
	}
	if !client.ShouldReceiveEvent(event3) {
		t.Fatal("expected client to receive broadcast event")
	}
}

func TestClientShouldReceiveEvent_EventTypeFiltering(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	client.SetEventTypes([]domain.SSEEventType{domain.SSEEventTypeNotification})

	allowed := &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification,
		Data: "test", Timestamp: time.Now(),
	}
	if !client.ShouldReceiveEvent(allowed) {
		t.Fatal("expected client to receive event of allowed type")
	}

	blocked := &domain.SSEEvent{
		ID: "2", Type: domain.SSEEventTypeHeartbeat,
		Data: "test", Timestamp: time.Now(),
	}
	if client.ShouldReceiveEvent(blocked) {
		t.Fatal("expected client to not receive event of blocked type")
	}
}

func TestClientShouldReceiveEvent_PriorityFiltering(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	client.SetPriorities([]domain.NotificationPriority{domain.NotificationPriorityHigh, domain.NotificationPriorityUrgent})

	// Notification event with high priority
	highPriority := &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification,
		Data:      domain.SSENotificationData{Priority: domain.NotificationPriorityHigh},
		Timestamp: time.Now(),
	}
	if !client.ShouldReceiveEvent(highPriority) {
		t.Fatal("expected client to receive high priority notification")
	}

	// Notification event with low priority
	lowPriority := &domain.SSEEvent{
		ID: "2", Type: domain.SSEEventTypeNotification,
		Data:      domain.SSENotificationData{Priority: domain.NotificationPriorityLow},
		Timestamp: time.Now(),
	}
	if client.ShouldReceiveEvent(lowPriority) {
		t.Fatal("expected client to filter out low priority notification")
	}

	// Non-notification event should pass through regardless of priority filter
	systemMsg := &domain.SSEEvent{
		ID: "3", Type: domain.SSEEventTypeSystemMessage,
		Data: "message", Timestamp: time.Now(),
	}
	if !client.ShouldReceiveEvent(systemMsg) {
		t.Fatal("expected non-notification event to pass priority filter")
	}
}

func TestClientShouldReceiveEvent_TenantFiltering(t *testing.T) {
	userID := uuid.New()
	tenantA := uuid.New()
	tenantB := uuid.New()

	client := NewClient(context.Background(), userID)
	defer client.Close()
	client.TenantID = &tenantA

	// Same tenant
	event := &domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification,
		Data: "test", TenantID: &tenantA, Timestamp: time.Now(),
	}
	if !client.ShouldReceiveEvent(event) {
		t.Fatal("expected client to receive event from same tenant")
	}

	// Different tenant
	event2 := &domain.SSEEvent{
		ID: "2", Type: domain.SSEEventTypeNotification,
		Data: "test", TenantID: &tenantB, Timestamp: time.Now(),
	}
	if client.ShouldReceiveEvent(event2) {
		t.Fatal("expected client to not receive event from different tenant")
	}
}

func TestClientSetEventTypesAndPriorities(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	types := []domain.SSEEventType{domain.SSEEventTypeNotification, domain.SSEEventTypeHeartbeat}
	client.SetEventTypes(types)
	if len(client.EventTypes) != 2 {
		t.Fatalf("expected 2 event types, got %d", len(client.EventTypes))
	}

	priorities := []domain.NotificationPriority{domain.NotificationPriorityHigh}
	client.SetPriorities(priorities)
	if len(client.Priorities) != 1 {
		t.Fatalf("expected 1 priority, got %d", len(client.Priorities))
	}
}

func TestClientDrain(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	for i := 0; i < 3; i++ {
		_ = client.Send(&domain.SSEEvent{
			ID: uuid.New().String(), Type: domain.SSEEventTypeNotification,
			Data: "msg", Timestamp: time.Now(),
		})
	}

	events := client.Drain()
	if len(events) != 3 {
		t.Fatalf("expected 3 drained events, got %d", len(events))
	}

	// Buffer should be empty now
	events = client.Drain()
	if len(events) != 0 {
		t.Fatalf("expected empty drain after draining, got %d", len(events))
	}
}

func TestClientFlush(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	// Flush empty channel should succeed immediately
	err := client.Flush(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("expected flush of empty channel to succeed, got %v", err)
	}

	// Send a message and drain it in a goroutine, flush should complete
	_ = client.Send(&domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification, Data: "msg", Timestamp: time.Now(),
	})
	go func() {
		time.Sleep(10 * time.Millisecond)
		<-client.Channel
	}()

	err = client.Flush(500 * time.Millisecond)
	if err != nil {
		t.Fatalf("expected flush to succeed, got %v", err)
	}
}

func TestClientFlushTimeout(t *testing.T) {
	opts := ClientOptions{
		BufferSize:     5,
		SendTimeout:    time.Second,
		MaxMessageSize: 1024,
		EnableMetrics:  true,
	}
	client := NewClientWithOptions(context.Background(), uuid.New(), opts)
	defer client.Close()

	// Fill the buffer but don't drain
	_ = client.Send(&domain.SSEEvent{
		ID: "1", Type: domain.SSEEventTypeNotification, Data: "msg", Timestamp: time.Now(),
	})

	err := client.Flush(30 * time.Millisecond)
	if err != ErrSendTimeout {
		t.Fatalf("expected ErrSendTimeout, got %v", err)
	}
}

func TestClientString(t *testing.T) {
	client := NewClient(context.Background(), uuid.New())
	defer client.Close()

	s := client.String()
	if s != client.ID.String() {
		t.Fatalf("expected String() to return client ID, got %q", s)
	}
}

func TestClientConcurrentSendAndClose(t *testing.T) {
	client := NewClientWithOptions(context.Background(), uuid.New(), ClientOptions{
		BufferSize:     1000,
		SendTimeout:    100 * time.Millisecond,
		MaxMessageSize: 1024,
		EnableMetrics:  true,
	})

	var wg sync.WaitGroup
	// Concurrently send events
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.Send(&domain.SSEEvent{
				ID: uuid.New().String(), Type: domain.SSEEventTypeNotification,
				Data: "msg", Timestamp: time.Now(),
			})
		}()
	}

	// Close while sends are in progress
	time.Sleep(5 * time.Millisecond)
	_ = client.Close()

	wg.Wait()

	stats := client.GetStats()
	total := stats.MessagesSent + stats.MessagesDropped
	if total == 0 {
		t.Fatal("expected some messages to have been processed")
	}
}

func TestEventFilterMatches(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	t.Run("empty_filter_matches_all", func(t *testing.T) {
		f := &EventFilter{}
		if !f.Matches("notification", "high", &userID, &tenantID, now) {
			t.Fatal("empty filter should match all events")
		}
	})

	t.Run("event_type_filter", func(t *testing.T) {
		f := &EventFilter{EventTypes: []string{"notification"}}
		if !f.Matches("notification", "", nil, nil, now) {
			t.Fatal("expected match for included event type")
		}
		if f.Matches("heartbeat", "", nil, nil, now) {
			t.Fatal("expected no match for excluded event type")
		}
	})

	t.Run("priority_filter", func(t *testing.T) {
		f := &EventFilter{Priorities: []string{"high", "urgent"}}
		if !f.Matches("notification", "high", nil, nil, now) {
			t.Fatal("expected match for high priority")
		}
		if f.Matches("notification", "low", nil, nil, now) {
			t.Fatal("expected no match for low priority")
		}
		// Empty priority string should pass
		if !f.Matches("notification", "", nil, nil, now) {
			t.Fatal("expected match when priority is empty")
		}
	})

	t.Run("user_id_filter", func(t *testing.T) {
		f := &EventFilter{UserIDs: []uuid.UUID{userID}}
		if !f.Matches("notification", "", &userID, nil, now) {
			t.Fatal("expected match for included user")
		}
		otherUser := uuid.New()
		if f.Matches("notification", "", &otherUser, nil, now) {
			t.Fatal("expected no match for other user")
		}
		// Nil userID should pass
		if !f.Matches("notification", "", nil, nil, now) {
			t.Fatal("expected match when userID is nil")
		}
	})

	t.Run("tenant_id_filter", func(t *testing.T) {
		f := &EventFilter{TenantIDs: []uuid.UUID{tenantID}}
		if !f.Matches("notification", "", nil, &tenantID, now) {
			t.Fatal("expected match for included tenant")
		}
		otherTenant := uuid.New()
		if f.Matches("notification", "", nil, &otherTenant, now) {
			t.Fatal("expected no match for other tenant")
		}
	})

	t.Run("timestamp_filter", func(t *testing.T) {
		minTS := now.Add(-1 * time.Hour)
		maxTS := now.Add(1 * time.Hour)
		f := &EventFilter{MinTimestamp: &minTS, MaxTimestamp: &maxTS}
		if !f.Matches("notification", "", nil, nil, now) {
			t.Fatal("expected match within timestamp range")
		}
		if f.Matches("notification", "", nil, nil, now.Add(-2*time.Hour)) {
			t.Fatal("expected no match before min timestamp")
		}
		if f.Matches("notification", "", nil, nil, now.Add(2*time.Hour)) {
			t.Fatal("expected no match after max timestamp")
		}
	})
}

func TestConnectionStateConstants(t *testing.T) {
	states := []ConnectionState{
		ConnectionStateConnecting,
		ConnectionStateConnected,
		ConnectionStateReady,
		ConnectionStateClosing,
		ConnectionStateClosed,
		ConnectionStateError,
	}
	seen := make(map[ConnectionState]bool)
	for _, s := range states {
		if seen[s] {
			t.Fatalf("duplicate connection state: %s", s)
		}
		seen[s] = true
	}
}

func TestChannelTypeConstants(t *testing.T) {
	types := []ChannelType{
		ChannelTypeUser,
		ChannelTypeGroup,
		ChannelTypeBroadcast,
		ChannelTypeSystem,
		ChannelTypeTenant,
		ChannelTypeRole,
	}
	seen := make(map[ChannelType]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Fatalf("duplicate channel type: %s", ct)
		}
		seen[ct] = true
	}
}

func TestMessageTypeConstants(t *testing.T) {
	types := []MessageType{
		MessageTypeSubscribe,
		MessageTypeUnsubscribe,
		MessageTypePing,
		MessageTypePong,
		MessageTypeAck,
		MessageTypeClose,
		MessageTypeAuth,
		MessageTypeConfig,
	}
	seen := make(map[MessageType]bool)
	for _, mt := range types {
		if seen[mt] {
			t.Fatalf("duplicate message type: %s", mt)
		}
		seen[mt] = true
	}
}
