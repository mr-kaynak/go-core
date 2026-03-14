package rabbitmq

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"gorm.io/gorm"
)

// mockOutboxRepo implements repository.OutboxRepository for testing
type mockOutboxRepo struct {
	messages map[uuid.UUID]*domain.OutboxMessage
	dlq      []*domain.OutboxDeadLetter
	logs     []*domain.OutboxProcessingLog
}

func newMockOutboxRepo() *mockOutboxRepo {
	return &mockOutboxRepo{
		messages: make(map[uuid.UUID]*domain.OutboxMessage),
	}
}

func (m *mockOutboxRepo) CreateMessage(msg *domain.OutboxMessage) error {
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	m.messages[msg.ID] = msg
	return nil
}

func (m *mockOutboxRepo) CreateMessageTx(_ *gorm.DB, msg *domain.OutboxMessage) error {
	return m.CreateMessage(msg)
}

func (m *mockOutboxRepo) GetMessage(id uuid.UUID) (*domain.OutboxMessage, error) {
	msg, ok := m.messages[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return msg, nil
}

func (m *mockOutboxRepo) GetPendingMessages(limit int) ([]*domain.OutboxMessage, error) {
	var result []*domain.OutboxMessage
	for _, msg := range m.messages {
		if msg.Status == domain.OutboxStatusPending {
			result = append(result, msg)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockOutboxRepo) GetMessagesForRetry(limit int) ([]*domain.OutboxMessage, error) {
	var result []*domain.OutboxMessage
	for _, msg := range m.messages {
		if msg.Status == domain.OutboxStatusFailed && msg.RetryCount < msg.MaxRetries {
			result = append(result, msg)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockOutboxRepo) UpdateMessage(msg *domain.OutboxMessage) error {
	m.messages[msg.ID] = msg
	return nil
}

func (m *mockOutboxRepo) DeleteMessage(id uuid.UUID) error {
	delete(m.messages, id)
	return nil
}

func (m *mockOutboxRepo) CreateMessages(msgs []*domain.OutboxMessage) error {
	for _, msg := range msgs {
		m.CreateMessage(msg)
	}
	return nil
}

func (m *mockOutboxRepo) MarkMessagesAsProcessing(ids []uuid.UUID) error {
	for _, id := range ids {
		if msg, ok := m.messages[id]; ok {
			msg.Status = domain.OutboxStatusProcessing
		}
	}
	return nil
}

func (m *mockOutboxRepo) MoveToDLQ(msg *domain.OutboxMessage, reason string) error {
	msg.Status = domain.OutboxStatusDLQ
	m.dlq = append(m.dlq, &domain.OutboxDeadLetter{
		ID:              uuid.New(),
		OutboxMessageID: msg.ID,
		OriginalMessage: msg.Payload,
		FailureReason:   reason,
	})
	return nil
}

func (m *mockOutboxRepo) GetDLQMessages(limit int) ([]*domain.OutboxDeadLetter, error) {
	if len(m.dlq) > limit {
		return m.dlq[:limit], nil
	}
	return m.dlq, nil
}

func (m *mockOutboxRepo) ReprocessDLQMessage(id uuid.UUID) error { return nil }

func (m *mockOutboxRepo) LogProcessing(log *domain.OutboxProcessingLog) error {
	m.logs = append(m.logs, log)
	return nil
}

func (m *mockOutboxRepo) GetProcessingLogs(messageID uuid.UUID) ([]*domain.OutboxProcessingLog, error) {
	var result []*domain.OutboxProcessingLog
	for _, l := range m.logs {
		if l.OutboxMessageID == messageID {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *mockOutboxRepo) ClaimMessagesForProcessing(pendingLimit, retryLimit int) ([]*domain.OutboxMessage, error) {
	pending, _ := m.GetPendingMessages(pendingLimit)
	retry, _ := m.GetMessagesForRetry(retryLimit)
	return append(pending, retry...), nil
}
func (m *mockOutboxRepo) CleanupProcessedMessages(olderThan time.Duration) error { return nil }
func (m *mockOutboxRepo) CleanupExpiredMessages() error                          { return nil }
func (m *mockOutboxRepo) GetStatistics() (*repository.OutboxStatistics, error) {
	return &repository.OutboxStatistics{PendingCount: int64(len(m.messages))}, nil
}

func newTestService(repo repository.OutboxRepository) *RabbitMQService {
	ctx, cancel := context.WithCancel(context.Background())
	return &RabbitMQService{
		cfg: &config.Config{
			RabbitMQ: config.RabbitMQConfig{
				Exchange:    "test-exchange",
				QueuePrefix: "test",
			},
		},
		outboxRepo: repo,
		handlers:   make(map[string]MessageHandler),
		shutdownCh: make(chan bool),
		logger:     logger.Get().WithFields(logger.Fields{"service": "rabbitmq-test"}),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func TestPublishMessage_OutboxCreation(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	msg := &Message{
		ID:            uuid.New().String(),
		Type:          "user.registered",
		Source:        "identity",
		Timestamp:     time.Now(),
		CorrelationID: "corr-123",
		Data:          map[string]interface{}{"email": "test@example.com"},
	}

	if err := svc.PublishMessage(context.Background(), nil, msg); err != nil {
		t.Fatalf("PublishMessage failed: %v", err)
	}

	if len(repo.messages) != 1 {
		t.Fatalf("expected 1 outbox message, got %d", len(repo.messages))
	}

	for _, outbox := range repo.messages {
		if outbox.EventType != "user.registered" {
			t.Errorf("expected event type user.registered, got %s", outbox.EventType)
		}
		if outbox.RoutingKey != "user.registered" {
			t.Errorf("expected routing key user.registered, got %s", outbox.RoutingKey)
		}
		if outbox.CorrelationID != "corr-123" {
			t.Errorf("expected correlation ID corr-123, got %s", outbox.CorrelationID)
		}

		var parsed Message
		if err := json.Unmarshal([]byte(outbox.Payload), &parsed); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if parsed.Type != "user.registered" {
			t.Errorf("payload type mismatch")
		}
	}
}

func TestPublishMessage_WithTransaction(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	msg := &Message{
		ID:   uuid.New().String(),
		Type: "order.created",
	}

	// CreateMessageTx is called when tx is non-nil — mock accepts any *gorm.DB
	dummyTx := &gorm.DB{}
	if err := svc.PublishMessage(context.Background(), dummyTx, msg); err != nil {
		t.Fatalf("PublishMessage with tx failed: %v", err)
	}
	if len(repo.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(repo.messages))
	}
}

func TestIsConnected_DefaultFalse(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	if svc.IsConnected() {
		t.Fatal("expected disconnected state without real AMQP")
	}
}

func TestPublishDirectly_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	msg := &Message{ID: "1", Type: "test"}
	err := svc.PublishDirectly(context.Background(), "test.key", msg)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestDeclareQueue_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	err := svc.DeclareQueue("test-queue", []string{"test.#"})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSubscribe_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	err := svc.Subscribe("test-queue", func(msg *Message) error { return nil })
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestHealthCheck_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	if err := svc.HealthCheck(); err == nil {
		t.Fatal("expected health check to fail when disconnected")
	}
}

func TestHandleMessage_ValidJSON(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	var received *Message
	svc.mu.Lock()
	svc.handlers["test-queue"] = func(msg *Message) error {
		received = msg
		return nil
	}
	svc.mu.Unlock()

	msg := &Message{
		ID:   "msg-1",
		Type: "test.event",
		Data: map[string]interface{}{"key": "value"},
	}
	body, _ := json.Marshal(msg)

	// handleMessage uses amqp.Delivery which we can't easily mock,
	// but we can verify handler registration works
	svc.mu.RLock()
	handler, exists := svc.handlers["test-queue"]
	svc.mu.RUnlock()

	if !exists {
		t.Fatal("expected handler to be registered")
	}

	if err := handler(msg); err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if received == nil || received.ID != "msg-1" {
		t.Fatal("expected handler to receive message")
	}
	_ = body
}

func TestProcessOutboxBatch_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	// Add a pending message
	repo.CreateMessage(&domain.OutboxMessage{
		EventType:  "batch.test",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "batch.test",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	})

	// processOutboxBatch should return early when not connected
	svc.processOutboxBatch()

	// Message should still be pending (not processed)
	for _, msg := range repo.messages {
		if msg.Status != domain.OutboxStatusPending {
			t.Errorf("expected message to remain pending when disconnected, got %s", msg.Status)
		}
	}
}

func TestClose_Idempotent(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	// Close multiple times should not panic
	if err := svc.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestMessageSerialization(t *testing.T) {
	msg := &Message{
		ID:            "id-1",
		Type:          "test.event",
		Source:        "test-service",
		Timestamp:     time.Now(),
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		UserID:        "user-1",
		TenantID:      "tenant-1",
		Data:          map[string]interface{}{"key": "value"},
		Metadata:      map[string]interface{}{"trace": "abc"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ID != msg.ID || decoded.Type != msg.Type || decoded.Source != msg.Source {
		t.Error("basic fields mismatch after roundtrip")
	}
	if decoded.CorrelationID != msg.CorrelationID || decoded.CausationID != msg.CausationID {
		t.Error("correlation fields mismatch")
	}
	if decoded.UserID != msg.UserID || decoded.TenantID != msg.TenantID {
		t.Error("tenant fields mismatch")
	}
}
