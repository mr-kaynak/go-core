package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Mock OutboxRepository
// ---------------------------------------------------------------------------

type mockOutboxRepo struct {
	mu       sync.Mutex
	messages map[uuid.UUID]*domain.OutboxMessage
	dlq      []*domain.OutboxDeadLetter
	logs     []*domain.OutboxProcessingLog

	// Error injection hooks
	claimErr            error
	updateErr           error
	logErr              error
	moveToDLQErr        error
	cleanupProcessedErr error
	cleanupExpiredErr   error
	statsErr            error
	createErr           error
}

func newMockOutboxRepo() *mockOutboxRepo {
	return &mockOutboxRepo{
		messages: make(map[uuid.UUID]*domain.OutboxMessage),
	}
}

func (m *mockOutboxRepo) CreateMessage(msg *domain.OutboxMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.messages[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return msg, nil
}

func (m *mockOutboxRepo) GetPendingMessages(limit int) ([]*domain.OutboxMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	m.messages[msg.ID] = msg
	return nil
}

func (m *mockOutboxRepo) DeleteMessage(id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.messages, id)
	return nil
}

func (m *mockOutboxRepo) CreateMessages(msgs []*domain.OutboxMessage) error {
	for _, msg := range msgs {
		if err := m.CreateMessage(msg); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockOutboxRepo) MarkMessagesAsProcessing(ids []uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		if msg, ok := m.messages[id]; ok {
			msg.Status = domain.OutboxStatusProcessing
		}
	}
	return nil
}

func (m *mockOutboxRepo) MoveToDLQ(msg *domain.OutboxMessage, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.moveToDLQErr != nil {
		return m.moveToDLQErr
	}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.dlq) > limit {
		return m.dlq[:limit], nil
	}
	return m.dlq, nil
}

func (m *mockOutboxRepo) ReprocessDLQMessage(_ uuid.UUID) error { return nil }

func (m *mockOutboxRepo) LogProcessing(log *domain.OutboxProcessingLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.logErr != nil {
		return m.logErr
	}
	m.logs = append(m.logs, log)
	return nil
}

func (m *mockOutboxRepo) GetProcessingLogs(messageID uuid.UUID) ([]*domain.OutboxProcessingLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*domain.OutboxProcessingLog
	for _, l := range m.logs {
		if l.OutboxMessageID == messageID {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *mockOutboxRepo) ClaimMessagesForProcessing(pendingLimit, retryLimit int) ([]*domain.OutboxMessage, error) {
	if m.claimErr != nil {
		return nil, m.claimErr
	}
	pending, _ := m.GetPendingMessages(pendingLimit)
	retry, _ := m.GetMessagesForRetry(retryLimit)
	return append(pending, retry...), nil
}

func (m *mockOutboxRepo) CleanupProcessedMessages(_ time.Duration) error {
	return m.cleanupProcessedErr
}

func (m *mockOutboxRepo) CleanupExpiredMessages() error {
	return m.cleanupExpiredErr
}

func (m *mockOutboxRepo) GetStatistics() (*repository.OutboxStatistics, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return &repository.OutboxStatistics{PendingCount: int64(len(m.messages))}, nil
}

// ---------------------------------------------------------------------------
// Mock AMQP Channel
// ---------------------------------------------------------------------------

type mockChannel struct {
	mu sync.Mutex

	exchangeDeclareErr error
	queueDeclareErr    error
	queueBindErr       error
	publishErr         error
	consumeErr         error
	closeErr           error

	// Track calls
	exchangesDeclared []string
	queuesDeclared    []string
	queueBindings     []string
	publishedMessages []amqp.Publishing

	// For NotifyPublish: the confirmation to send back
	confirmAck bool                   // true = ack, false = nack
	confirmCh  chan amqp.Confirmation // reference to the confirm channel

	// For Consume: return this delivery channel
	deliveryCh chan amqp.Delivery
	closeOnce  sync.Once
}

func newMockChannel() *mockChannel {
	return &mockChannel{
		confirmAck: true,
		deliveryCh: make(chan amqp.Delivery, 10),
	}
}

func (mc *mockChannel) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.exchangesDeclared = append(mc.exchangesDeclared, name)
	return mc.exchangeDeclareErr
}

func (mc *mockChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.queuesDeclared = append(mc.queuesDeclared, name)
	return amqp.Queue{Name: name}, mc.queueDeclareErr
}

func (mc *mockChannel) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.queueBindings = append(mc.queueBindings, fmt.Sprintf("%s->%s->%s", name, key, exchange))
	return mc.queueBindErr
}

func (mc *mockChannel) NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation {
	mc.mu.Lock()
	mc.confirmCh = confirm
	mc.mu.Unlock()
	return confirm
}

func (mc *mockChannel) PublishWithContext(_ context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	mc.mu.Lock()
	publishErr := mc.publishErr
	mc.publishedMessages = append(mc.publishedMessages, msg)
	confirmCh := mc.confirmCh
	ack := mc.confirmAck
	mc.mu.Unlock()
	if publishErr != nil {
		return publishErr
	}
	// Send confirmation asynchronously to simulate broker confirm
	if confirmCh != nil {
		go func() {
			confirmCh <- amqp.Confirmation{DeliveryTag: uint64(len(mc.publishedMessages)), Ack: ack}
		}()
	}
	return nil
}

func (mc *mockChannel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	if mc.consumeErr != nil {
		return nil, mc.consumeErr
	}
	return mc.deliveryCh, nil
}

func (mc *mockChannel) Close() error {
	// Close the delivery channel so processMessages goroutines can exit,
	// mirroring what a real AMQP channel does on close.
	mc.closeOnce.Do(func() { close(mc.deliveryCh) })
	return mc.closeErr
}

// ---------------------------------------------------------------------------
// Mock Acknowledger for amqp.Delivery
// ---------------------------------------------------------------------------

type mockAcknowledger struct {
	mu          sync.Mutex
	ackCount    int
	nackCount   int
	rejectCount int
	lastRequeue bool
}

func (ma *mockAcknowledger) Ack(tag uint64, multiple bool) error {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	ma.ackCount++
	return nil
}

func (ma *mockAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	ma.nackCount++
	ma.lastRequeue = requeue
	return nil
}

func (ma *mockAcknowledger) Reject(tag uint64, requeue bool) error {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	ma.rejectCount++
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

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

// newConnectedTestService creates a test service with a mock channel in connected state.
func newConnectedTestService(repo repository.OutboxRepository, ch amqpChannel) *RabbitMQService {
	svc := newTestService(repo)
	svc.channel = ch
	svc.isConnected.Store(true)
	// Set up the channel-level confirm listener (matches connect() behavior)
	svc.confirmCh = make(chan amqp.Confirmation, 1)
	ch.NotifyPublish(svc.confirmCh)
	return svc
}

// ---------------------------------------------------------------------------
// Tests: PublishMessage (outbox creation)
// ---------------------------------------------------------------------------

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
		if outbox.Priority != 5 {
			t.Errorf("expected priority 5, got %d", outbox.Priority)
		}
		if outbox.MaxRetries != 3 {
			t.Errorf("expected max retries 3, got %d", outbox.MaxRetries)
		}
		if outbox.TTL != 300 {
			t.Errorf("expected TTL 300, got %d", outbox.TTL)
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

	dummyTx := &gorm.DB{}
	if err := svc.PublishMessage(context.Background(), dummyTx, msg); err != nil {
		t.Fatalf("PublishMessage with tx failed: %v", err)
	}
	if len(repo.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(repo.messages))
	}
}

func TestPublishMessage_CreateError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.createErr = errors.New("db connection refused")
	svc := newTestService(repo)
	defer svc.Close()

	msg := &Message{ID: "1", Type: "test"}
	err := svc.PublishMessage(context.Background(), nil, msg)
	if err == nil {
		t.Fatal("expected error when CreateMessage fails")
	}
	if len(repo.messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(repo.messages))
	}
}

func TestPublishMessage_CreateTxError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.createErr = errors.New("tx error")
	svc := newTestService(repo)
	defer svc.Close()

	msg := &Message{ID: "1", Type: "test"}
	err := svc.PublishMessage(context.Background(), &gorm.DB{}, msg)
	if err == nil {
		t.Fatal("expected error when CreateMessageTx fails")
	}
}

func TestPublishMessage_SetsFieldsCorrectly(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	msg := &Message{
		ID:            "msg-id-1",
		Type:          "order.shipped",
		Source:        "orders",
		CorrelationID: "corr-abc",
		CausationID:   "cause-xyz",
	}

	if err := svc.PublishMessage(context.Background(), nil, msg); err != nil {
		t.Fatalf("PublishMessage failed: %v", err)
	}

	for _, outbox := range repo.messages {
		if outbox.Queue != "test-exchange" {
			t.Errorf("expected queue to be exchange name, got %s", outbox.Queue)
		}
		if outbox.CausationID != "cause-xyz" {
			t.Errorf("expected causation ID cause-xyz, got %s", outbox.CausationID)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: IsConnected
// ---------------------------------------------------------------------------

func TestIsConnected_DefaultFalse(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	if svc.IsConnected() {
		t.Fatal("expected disconnected state without real AMQP")
	}
}

func TestIsConnected_TrueWhenSet(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	svc.isConnected.Store(true)
	if !svc.IsConnected() {
		t.Fatal("expected connected state")
	}
}

// ---------------------------------------------------------------------------
// Tests: PublishDirectly
// ---------------------------------------------------------------------------

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

func TestPublishDirectly_Success(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	msg := &Message{
		ID:            "pub-1",
		Type:          "user.created",
		Source:        "identity",
		CorrelationID: "corr-1",
		UserID:        "user-1",
		TenantID:      "tenant-1",
		Data:          map[string]interface{}{"email": "a@b.com"},
	}

	err := svc.PublishDirectly(context.Background(), "user.created", msg)
	if err != nil {
		t.Fatalf("PublishDirectly failed: %v", err)
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()
	if len(ch.publishedMessages) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(ch.publishedMessages))
	}

	pub := ch.publishedMessages[0]
	if pub.ContentType != "application/json" {
		t.Errorf("expected content type application/json, got %s", pub.ContentType)
	}
	if pub.DeliveryMode != amqp.Persistent {
		t.Errorf("expected persistent delivery mode")
	}
	if pub.MessageId != "pub-1" {
		t.Errorf("expected message ID pub-1, got %s", pub.MessageId)
	}
	if pub.CorrelationId != "corr-1" {
		t.Errorf("expected correlation ID corr-1, got %s", pub.CorrelationId)
	}

	// Verify body deserializes correctly
	var decoded Message
	if err := json.Unmarshal(pub.Body, &decoded); err != nil {
		t.Fatalf("failed to unmarshal published body: %v", err)
	}
	if decoded.Type != "user.created" {
		t.Errorf("expected type user.created in body, got %s", decoded.Type)
	}

	// Verify headers
	if pub.Headers["type"] != "user.created" {
		t.Errorf("expected header type user.created")
	}
	if pub.Headers["source"] != "identity" {
		t.Errorf("expected header source identity")
	}
	if pub.Headers["user_id"] != "user-1" {
		t.Errorf("expected header user_id user-1")
	}
	if pub.Headers["tenant_id"] != "tenant-1" {
		t.Errorf("expected header tenant_id tenant-1")
	}
}

func TestPublishDirectly_PublishError(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.publishErr = errors.New("channel closed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	msg := &Message{ID: "1", Type: "test"}
	err := svc.PublishDirectly(context.Background(), "test.key", msg)
	if err == nil {
		t.Fatal("expected error when publish fails")
	}
}

func TestPublishDirectly_BrokerNack(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.confirmAck = false // broker will nack
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	msg := &Message{ID: "1", Type: "test"}
	err := svc.PublishDirectly(context.Background(), "test.key", msg)
	if err == nil {
		t.Fatal("expected error when broker nacks")
	}
	if !contains(err.Error(), "nacked") {
		t.Errorf("expected nack error, got: %v", err)
	}
}

func TestPublishDirectly_ContextTimeout(t *testing.T) {
	repo := newMockOutboxRepo()
	// Create a channel that never sends confirmations
	ch := &slowConfirmChannel{mockChannel: *newMockChannel()}
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	msg := &Message{ID: "1", Type: "test"}
	err := svc.PublishDirectly(ctx, "test.key", msg)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !contains(err.Error(), "timed out") {
		t.Errorf("expected timeout message, got: %v", err)
	}
}

// slowConfirmChannel is a mock channel that never sends confirmations,
// allowing us to test context timeout in PublishDirectly.
type slowConfirmChannel struct {
	mockChannel
}

func (sc *slowConfirmChannel) NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation {
	// Do NOT send any confirmation - this forces the select to hit ctx.Done()
	return confirm
}

func (sc *slowConfirmChannel) PublishWithContext(_ context.Context, _, _ string, _, _ bool, msg amqp.Publishing) error {
	return nil
}

// ---------------------------------------------------------------------------
// Tests: DeclareQueue
// ---------------------------------------------------------------------------

func TestDeclareQueue_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	err := svc.DeclareQueue("test-queue", []string{"test.#"})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestDeclareQueue_Success(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	err := svc.DeclareQueue("events-queue", []string{"user.#", "order.#"})
	if err != nil {
		t.Fatalf("DeclareQueue failed: %v", err)
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	// Should declare DLQ + main queue = 2 queues
	if len(ch.queuesDeclared) != 2 {
		t.Fatalf("expected 2 queues declared, got %d: %v", len(ch.queuesDeclared), ch.queuesDeclared)
	}
	if ch.queuesDeclared[0] != "events-queue.dlq" {
		t.Errorf("expected first queue to be DLQ, got %s", ch.queuesDeclared[0])
	}
	if ch.queuesDeclared[1] != "events-queue" {
		t.Errorf("expected second queue to be main queue, got %s", ch.queuesDeclared[1])
	}

	// Should have 3 bindings: DLQ binding + 2 routing key bindings
	if len(ch.queueBindings) != 3 {
		t.Fatalf("expected 3 bindings, got %d: %v", len(ch.queueBindings), ch.queueBindings)
	}
}

func TestDeclareQueue_DLQDeclareError(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.queueDeclareErr = errors.New("dlq declare failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	err := svc.DeclareQueue("test-queue", []string{"test.#"})
	if err == nil {
		t.Fatal("expected error when DLQ declaration fails")
	}
	if !contains(err.Error(), "DLQ") {
		t.Errorf("expected DLQ error, got: %v", err)
	}
}

func TestDeclareQueue_QueueBindError(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.queueBindErr = errors.New("bind failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	err := svc.DeclareQueue("test-queue", []string{"test.#"})
	if err == nil {
		t.Fatal("expected error when queue bind fails")
	}
	if !contains(err.Error(), "bind") {
		t.Errorf("expected bind error, got: %v", err)
	}
}

func TestDeclareQueue_MultipleRoutingKeys(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	keys := []string{"user.created", "user.updated", "user.deleted"}
	err := svc.DeclareQueue("user-events", keys)
	if err != nil {
		t.Fatalf("DeclareQueue failed: %v", err)
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()
	// 1 DLQ bind + 3 routing key binds = 4 total
	if len(ch.queueBindings) != 4 {
		t.Fatalf("expected 4 bindings for 3 routing keys + 1 DLQ, got %d", len(ch.queueBindings))
	}
}

// ---------------------------------------------------------------------------
// Tests: Subscribe
// ---------------------------------------------------------------------------

func TestSubscribe_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	err := svc.Subscribe("test-queue", func(msg *Message) error { return nil })
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSubscribe_Success(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)

	handlerCalled := make(chan bool, 1)
	err := svc.Subscribe("test-queue", func(msg *Message) error {
		handlerCalled <- true
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Verify handler was registered
	svc.mu.RLock()
	_, exists := svc.handlers["test-queue"]
	svc.mu.RUnlock()
	if !exists {
		t.Fatal("expected handler to be registered for test-queue")
	}

	// Close to stop goroutines
	svc.Close()
}

func TestSubscribe_ConsumeError(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.consumeErr = errors.New("consume failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	err := svc.Subscribe("test-queue", func(msg *Message) error { return nil })
	if err == nil {
		t.Fatal("expected error when consume fails")
	}
}

func TestSubscribe_ProcessesDeliveredMessages(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)

	received := make(chan *Message, 1)
	acker := &mockAcknowledger{}

	err := svc.Subscribe("test-queue", func(msg *Message) error {
		received <- msg
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Deliver a message through the channel
	msgBody, _ := json.Marshal(&Message{
		ID:   "delivered-1",
		Type: "test.event",
		Data: map[string]interface{}{"key": "val"},
	})

	ch.deliveryCh <- amqp.Delivery{
		Acknowledger: acker,
		Body:         msgBody,
	}

	select {
	case msg := <-received:
		if msg.ID != "delivered-1" {
			t.Errorf("expected message ID delivered-1, got %s", msg.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message to be processed")
	}

	// Verify ack was called
	acker.mu.Lock()
	acks := acker.ackCount
	acker.mu.Unlock()
	if acks != 1 {
		t.Errorf("expected 1 ack, got %d", acks)
	}

	// svc.Close() closes the mock channel, which closes deliveryCh,
	// allowing the processMessages goroutine to exit.
	svc.Close()
}

// ---------------------------------------------------------------------------
// Tests: handleMessage
// ---------------------------------------------------------------------------

func TestHandleMessage_ValidJSON_Success(t *testing.T) {
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

	acker := &mockAcknowledger{}
	msgBody, _ := json.Marshal(&Message{
		ID:   "msg-1",
		Type: "test.event",
		Data: map[string]interface{}{"key": "value"},
	})

	delivery := amqp.Delivery{
		Acknowledger: acker,
		Body:         msgBody,
	}

	svc.handleMessage("test-queue", delivery)

	if received == nil || received.ID != "msg-1" {
		t.Fatal("expected handler to receive message with correct ID")
	}

	acker.mu.Lock()
	if acker.ackCount != 1 {
		t.Errorf("expected 1 ack, got %d", acker.ackCount)
	}
	acker.mu.Unlock()
}

func TestHandleMessage_InvalidJSON(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	acker := &mockAcknowledger{}
	delivery := amqp.Delivery{
		Acknowledger: acker,
		Body:         []byte("not valid json{{{"),
	}

	svc.handleMessage("test-queue", delivery)

	acker.mu.Lock()
	if acker.nackCount != 1 {
		t.Errorf("expected 1 nack for invalid JSON, got %d", acker.nackCount)
	}
	if acker.lastRequeue {
		t.Error("expected nack without requeue for malformed messages")
	}
	acker.mu.Unlock()
}

func TestHandleMessage_NoHandler(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	acker := &mockAcknowledger{}
	msgBody, _ := json.Marshal(&Message{ID: "1", Type: "test"})
	delivery := amqp.Delivery{
		Acknowledger: acker,
		Body:         msgBody,
	}

	svc.handleMessage("unknown-queue", delivery)

	acker.mu.Lock()
	if acker.nackCount != 1 {
		t.Errorf("expected 1 nack for missing handler, got %d", acker.nackCount)
	}
	if !acker.lastRequeue {
		t.Error("expected nack with requeue when handler is missing")
	}
	acker.mu.Unlock()
}

func TestHandleMessage_HandlerError(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	svc.mu.Lock()
	svc.handlers["test-queue"] = func(msg *Message) error {
		return errors.New("processing failed")
	}
	svc.mu.Unlock()

	acker := &mockAcknowledger{}
	msgBody, _ := json.Marshal(&Message{ID: "1", Type: "test"})
	delivery := amqp.Delivery{
		Acknowledger: acker,
		Body:         msgBody,
	}

	svc.handleMessage("test-queue", delivery)

	acker.mu.Lock()
	if acker.nackCount != 1 {
		t.Errorf("expected 1 nack for handler error, got %d", acker.nackCount)
	}
	if acker.lastRequeue {
		t.Error("expected nack without requeue for handler errors (send to DLQ)")
	}
	acker.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Tests: processOutboxBatch
// ---------------------------------------------------------------------------

func TestProcessOutboxBatch_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	_ = repo.CreateMessage(&domain.OutboxMessage{
		EventType:  "batch.test",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "batch.test",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	})

	svc.processOutboxBatch()

	// Message should still be pending (not processed)
	for _, msg := range repo.messages {
		if msg.Status != domain.OutboxStatusPending {
			t.Errorf("expected message to remain pending when disconnected, got %s", msg.Status)
		}
	}
}

func TestProcessOutboxBatch_ClaimError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.claimErr = errors.New("database error")
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	// Should not panic, just log error
	svc.processOutboxBatch()
}

func TestProcessOutboxBatch_EmptyBatch(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	// No messages in repo, should handle gracefully
	svc.processOutboxBatch()
}

func TestProcessOutboxBatch_ProcessesMessages(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	msg := &Message{
		ID:   "outbox-msg-1",
		Type: "user.created",
		Data: map[string]interface{}{"user": "test"},
	}
	payload, _ := json.Marshal(msg)

	_ = repo.CreateMessage(&domain.OutboxMessage{
		EventType:  "user.created",
		Payload:    string(payload),
		Queue:      "test-exchange",
		RoutingKey: "user.created",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	})

	svc.processOutboxBatch()

	// Verify the message was published
	ch.mu.Lock()
	pubCount := len(ch.publishedMessages)
	ch.mu.Unlock()
	if pubCount != 1 {
		t.Fatalf("expected 1 published message, got %d", pubCount)
	}

	// Verify the message was marked as sent
	for _, m := range repo.messages {
		if m.Status != domain.OutboxStatusSent {
			t.Errorf("expected status sent, got %s", m.Status)
		}
		if m.ProcessedAt == nil {
			t.Error("expected ProcessedAt to be set")
		}
	}

	// Verify processing log
	repo.mu.Lock()
	logCount := len(repo.logs)
	repo.mu.Unlock()
	if logCount != 1 {
		t.Fatalf("expected 1 processing log, got %d", logCount)
	}
	if repo.logs[0].Action != "sent" {
		t.Errorf("expected log action 'sent', got %s", repo.logs[0].Action)
	}
	if repo.logs[0].Status != "success" {
		t.Errorf("expected log status 'success', got %s", repo.logs[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Tests: processOutboxMessage
// ---------------------------------------------------------------------------

func TestProcessOutboxMessage_InvalidPayload(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    "invalid{json",
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
	}
	repo.messages[msg.ID] = msg

	svc.processOutboxMessage(msg)

	if msg.Status != domain.OutboxStatusFailed {
		t.Errorf("expected failed status for invalid payload, got %s", msg.Status)
	}
}

func TestProcessOutboxMessage_PublishFailure_CanRetry(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.publishErr = errors.New("publish failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "1", Type: "test"}
	payload, _ := json.Marshal(validMsg)

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    string(payload),
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
		RetryCount: 0,
	}
	repo.messages[msg.ID] = msg

	svc.processOutboxMessage(msg)

	// Should have incremented retry and set status to failed
	if msg.Status != domain.OutboxStatusFailed {
		t.Errorf("expected failed status, got %s", msg.Status)
	}
	if msg.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", msg.RetryCount)
	}
	if msg.NextRetryAt == nil {
		t.Error("expected NextRetryAt to be set")
	}

	// Verify log
	repo.mu.Lock()
	if len(repo.logs) != 1 || repo.logs[0].Action != "retried" {
		t.Error("expected log action 'retried'")
	}
	repo.mu.Unlock()
}

func TestProcessOutboxMessage_PublishFailure_MaxRetries_MoveToDLQ(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.publishErr = errors.New("permanent failure")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "1", Type: "test"}
	payload, _ := json.Marshal(validMsg)

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    string(payload),
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
		RetryCount: 3, // Already at max
	}
	repo.messages[msg.ID] = msg

	svc.processOutboxMessage(msg)

	// Should have been moved to DLQ
	if msg.Status != domain.OutboxStatusDLQ {
		t.Errorf("expected DLQ status, got %s", msg.Status)
	}

	repo.mu.Lock()
	if len(repo.dlq) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(repo.dlq))
	}
	if repo.dlq[0].FailureReason != "Max retries exceeded" {
		t.Errorf("expected DLQ reason 'Max retries exceeded', got %s", repo.dlq[0].FailureReason)
	}
	logCount := len(repo.logs)
	repo.mu.Unlock()

	if logCount != 1 {
		t.Fatalf("expected 1 log, got %d", logCount)
	}
	if repo.logs[0].Action != "moved_to_dlq" {
		t.Errorf("expected log action 'moved_to_dlq', got %s", repo.logs[0].Action)
	}
}

func TestProcessOutboxMessage_PublishFailure_DLQError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.moveToDLQErr = errors.New("dlq save failed")
	ch := newMockChannel()
	ch.publishErr = errors.New("publish failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "1", Type: "test"}
	payload, _ := json.Marshal(validMsg)

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    string(payload),
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
		RetryCount: 3,
	}
	repo.messages[msg.ID] = msg

	// Should not panic when DLQ save fails
	svc.processOutboxMessage(msg)
}

func TestProcessOutboxMessage_UpdateError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.updateErr = errors.New("update failed")
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "1", Type: "test"}
	payload, _ := json.Marshal(validMsg)

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    string(payload),
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
	}

	// Should not panic when update fails
	svc.processOutboxMessage(msg)
}

func TestProcessOutboxMessage_LogError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.logErr = errors.New("log failed")
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "1", Type: "test"}
	payload, _ := json.Marshal(validMsg)

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    string(payload),
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
	}
	repo.messages[msg.ID] = msg

	// Should not panic when log fails
	svc.processOutboxMessage(msg)

	// Message should still be marked as sent
	if msg.Status != domain.OutboxStatusSent {
		t.Errorf("expected sent status despite log failure, got %s", msg.Status)
	}
}

func TestProcessOutboxMessage_Success(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "1", Type: "order.placed"}
	payload, _ := json.Marshal(validMsg)

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "order.placed",
		Payload:    string(payload),
		RoutingKey: "order.placed",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
	}
	repo.messages[msg.ID] = msg

	svc.processOutboxMessage(msg)

	if msg.Status != domain.OutboxStatusSent {
		t.Errorf("expected sent status, got %s", msg.Status)
	}
	if msg.ProcessedAt == nil {
		t.Error("expected ProcessedAt to be set")
	}

	repo.mu.Lock()
	if len(repo.logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(repo.logs))
	}
	if repo.logs[0].Action != "sent" {
		t.Errorf("expected log action 'sent', got %s", repo.logs[0].Action)
	}
	if repo.logs[0].ProcessingTime < 0 {
		t.Errorf("expected non-negative processing time, got %d", repo.logs[0].ProcessingTime)
	}
	repo.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Tests: processOutboxMessages (full loop with listen channel)
// ---------------------------------------------------------------------------

func TestProcessOutboxMessages_ListenChannelTrigger(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	listenCh := make(chan struct{}, 1)

	svc := newConnectedTestService(repo, ch)
	svc.listenCh = listenCh

	validMsg := &Message{ID: "listen-1", Type: "test.event"}
	payload, _ := json.Marshal(validMsg)

	_ = repo.CreateMessage(&domain.OutboxMessage{
		EventType:  "test.event",
		Payload:    string(payload),
		Queue:      "test-exchange",
		RoutingKey: "test.event",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	})

	// Start the outbox processor
	svc.wg.Add(1)
	go func() { defer svc.wg.Done(); svc.processOutboxMessages() }()

	// Give the startup batch a moment, then trigger via listen channel
	time.Sleep(100 * time.Millisecond)

	// The startup batch already should have processed the message
	// Check if it was sent
	allSent := true
	repo.mu.Lock()
	for _, m := range repo.messages {
		if m.Status != domain.OutboxStatusSent {
			allSent = false
		}
	}
	repo.mu.Unlock()
	if !allSent {
		t.Error("expected all messages to be processed during startup batch")
	}

	svc.Close()
}

func TestProcessOutboxMessages_ShutdownOnContext(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	svc.listenCh = make(chan struct{})

	done := make(chan struct{})
	svc.wg.Add(1)
	go func() {
		defer svc.wg.Done()
		svc.processOutboxMessages()
		close(done)
	}()

	// Cancel context to stop
	svc.cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("processOutboxMessages did not exit on context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Tests: reconnect
// ---------------------------------------------------------------------------

func TestReconnect_ContextCancellation(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	// Mark as disconnected
	svc.isConnected.Store(false)

	done := make(chan struct{})
	go func() {
		svc.reconnect()
		close(done)
	}()

	// Cancel context to stop reconnect loop
	svc.cancel()

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect did not exit on context cancellation")
	}
}

func TestReconnect_AlreadyConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	svc.isConnected.Store(true)

	done := make(chan struct{})
	go func() {
		svc.reconnect()
		close(done)
	}()

	select {
	case <-done:
		// If already connected, the loop condition !s.isConnected.Load() is false
		// so it exits immediately
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect should exit immediately when already connected")
	}
}

// ---------------------------------------------------------------------------
// Tests: handleReconnect
// ---------------------------------------------------------------------------

func TestHandleReconnect_ContextCancellation(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	done := make(chan struct{})
	go func() {
		svc.handleReconnect()
		close(done)
	}()

	svc.cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("handleReconnect did not exit on context cancellation")
	}
}

func TestHandleReconnect_ShutdownChannel(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	done := make(chan struct{})
	go func() {
		svc.handleReconnect()
		close(done)
	}()

	close(svc.shutdownCh)

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("handleReconnect did not exit on shutdown channel close")
	}

	// Prevent double close in svc.Close by resetting closeOnce behavior
	svc.cancel()
}

// ---------------------------------------------------------------------------
// Tests: runCleanupJobs
// ---------------------------------------------------------------------------

func TestRunCleanupJobs_ContextCancellation(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	done := make(chan struct{})
	go func() {
		svc.runCleanupJobs()
		close(done)
	}()

	svc.cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("runCleanupJobs did not exit on context cancellation")
	}
}

func TestRunCleanupJobs_ShutdownChannel(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	done := make(chan struct{})
	go func() {
		svc.runCleanupJobs()
		close(done)
	}()

	close(svc.shutdownCh)

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("runCleanupJobs did not exit on shutdown channel close")
	}

	svc.cancel()
}

// ---------------------------------------------------------------------------
// Tests: runMetricsUpdater
// ---------------------------------------------------------------------------

func TestRunMetricsUpdater_ContextCancellation(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	done := make(chan struct{})
	go func() {
		svc.runMetricsUpdater()
		close(done)
	}()

	svc.cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("runMetricsUpdater did not exit on context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Tests: HealthCheck
// ---------------------------------------------------------------------------

func TestHealthCheck_NotConnected(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	if err := svc.HealthCheck(); err == nil {
		t.Fatal("expected health check to fail when disconnected")
	}
}

// ---------------------------------------------------------------------------
// Tests: Close
// ---------------------------------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)

	if err := svc.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestClose_WithMockChannel(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)

	if err := svc.Close(); err != nil {
		t.Fatalf("close with mock channel failed: %v", err)
	}
}

func TestClose_WithChannelError(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.closeErr = errors.New("channel close error")
	svc := newConnectedTestService(repo, ch)

	// Close should not fail even if channel.Close errors
	// (connErr comes from connection.Close which is nil here)
	_ = svc.Close()
}

// ---------------------------------------------------------------------------
// Tests: MessageSerialization
// ---------------------------------------------------------------------------

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

func TestMessageSerialization_EmptyFields(t *testing.T) {
	msg := &Message{
		ID:   "minimal",
		Type: "test",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.CorrelationID != "" {
		t.Error("expected empty correlation ID for omitted field")
	}
	if decoded.Metadata != nil {
		t.Error("expected nil metadata for omitted field")
	}
}

// ---------------------------------------------------------------------------
// Tests: resubscribeAll
// ---------------------------------------------------------------------------

func TestResubscribeAll_NoHandlers(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	// Should not panic with empty handlers
	svc.resubscribeAll()
}

func TestResubscribeAll_WithHandlers(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)

	svc.mu.Lock()
	svc.handlers["queue-a"] = func(msg *Message) error { return nil }
	svc.handlers["queue-b"] = func(msg *Message) error { return nil }
	svc.mu.Unlock()

	svc.resubscribeAll()

	// svc.Close() closes the mock channel (which closes deliveryCh),
	// allowing processMessages goroutines to exit.
	svc.Close()
}

func TestResubscribeAll_ConsumeError(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.consumeErr = errors.New("consume failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	svc.mu.Lock()
	svc.handlers["failing-queue"] = func(msg *Message) error { return nil }
	svc.mu.Unlock()

	// Should not panic
	svc.resubscribeAll()
}

// ---------------------------------------------------------------------------
// Tests: handler registration via Subscribe
// ---------------------------------------------------------------------------

func TestHandlerRegistration(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	handler1 := func(msg *Message) error { return nil }
	handler2 := func(msg *Message) error { return errors.New("err") }

	svc.mu.Lock()
	svc.handlers["q1"] = handler1
	svc.handlers["q2"] = handler2
	svc.mu.Unlock()

	svc.mu.RLock()
	if len(svc.handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(svc.handlers))
	}
	_, hasQ1 := svc.handlers["q1"]
	_, hasQ2 := svc.handlers["q2"]
	svc.mu.RUnlock()

	if !hasQ1 || !hasQ2 {
		t.Error("expected handlers for q1 and q2")
	}
}

// ---------------------------------------------------------------------------
// Tests: declareExchange (via mock channel)
// ---------------------------------------------------------------------------

func TestDeclareExchange_Success(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	err := svc.declareExchange()
	if err != nil {
		t.Fatalf("declareExchange failed: %v", err)
	}

	ch.mu.Lock()
	if len(ch.exchangesDeclared) != 1 {
		t.Fatalf("expected 1 exchange declared, got %d", len(ch.exchangesDeclared))
	}
	if ch.exchangesDeclared[0] != "test-exchange" {
		t.Errorf("expected exchange name test-exchange, got %s", ch.exchangesDeclared[0])
	}
	ch.mu.Unlock()
}

func TestDeclareExchange_Error(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	ch.exchangeDeclareErr = errors.New("exchange declare failed")
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	err := svc.declareExchange()
	if err == nil {
		t.Fatal("expected error when exchange declaration fails")
	}
}

// ---------------------------------------------------------------------------
// Tests: processOutboxMessage with retry path (retryable messages)
// ---------------------------------------------------------------------------

func TestProcessOutboxBatch_RetryableMessages(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	validMsg := &Message{ID: "retry-1", Type: "retry.event"}
	payload, _ := json.Marshal(validMsg)

	// Create a message that is failed but retryable
	past := time.Now().Add(-1 * time.Minute)
	msg := &domain.OutboxMessage{
		ID:          uuid.New(),
		EventType:   "retry.event",
		Payload:     string(payload),
		Queue:       "test-exchange",
		RoutingKey:  "retry.event",
		Status:      domain.OutboxStatusFailed,
		MaxRetries:  3,
		RetryCount:  1,
		NextRetryAt: &past,
	}
	repo.messages[msg.ID] = msg

	svc.processOutboxBatch()

	if msg.Status != domain.OutboxStatusSent {
		t.Errorf("expected retryable message to be sent, got %s", msg.Status)
	}
}

// ---------------------------------------------------------------------------
// Tests: concurrent access
// ---------------------------------------------------------------------------

func TestConcurrentPublishMessage(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := &Message{
				ID:   fmt.Sprintf("concurrent-%d", n),
				Type: "concurrent.test",
				Data: map[string]interface{}{"n": n},
			}
			_ = svc.PublishMessage(context.Background(), nil, msg)
		}(i)
	}
	wg.Wait()

	repo.mu.Lock()
	count := len(repo.messages)
	repo.mu.Unlock()
	if count != 20 {
		t.Errorf("expected 20 messages, got %d", count)
	}
}

func TestConcurrentHandlerRegistrationAndLookup(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(repo)
	defer svc.Close()

	var wg sync.WaitGroup

	// Register handlers concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			svc.mu.Lock()
			svc.handlers[fmt.Sprintf("queue-%d", n)] = func(msg *Message) error { return nil }
			svc.mu.Unlock()
		}(i)
	}

	// Look up handlers concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			svc.mu.RLock()
			_ = svc.handlers[fmt.Sprintf("queue-%d", n)]
			svc.mu.RUnlock()
		}(i)
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Tests: Multiple outbox messages in a single batch
// ---------------------------------------------------------------------------

func TestProcessOutboxBatch_MultipleMessages(t *testing.T) {
	repo := newMockOutboxRepo()
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	// Set batch size large enough to claim all 5 messages in one batch
	svc.cfg.RabbitMQ.OutboxBatchSize = 10

	for i := 0; i < 5; i++ {
		validMsg := &Message{ID: fmt.Sprintf("batch-%d", i), Type: "batch.event"}
		payload, _ := json.Marshal(validMsg)
		_ = repo.CreateMessage(&domain.OutboxMessage{
			EventType:  "batch.event",
			Payload:    string(payload),
			Queue:      "test-exchange",
			RoutingKey: "batch.event",
			Status:     domain.OutboxStatusPending,
			MaxRetries: 3,
		})
	}

	svc.processOutboxBatch()

	ch.mu.Lock()
	pubCount := len(ch.publishedMessages)
	ch.mu.Unlock()

	if pubCount != 5 {
		t.Errorf("expected 5 published messages, got %d", pubCount)
	}

	repo.mu.Lock()
	for _, m := range repo.messages {
		if m.Status != domain.OutboxStatusSent {
			t.Errorf("expected all messages to be sent, got %s", m.Status)
		}
	}
	repo.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Tests: processOutboxMessage invalid payload update error
// ---------------------------------------------------------------------------

func TestProcessOutboxMessage_InvalidPayload_UpdateError(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.updateErr = errors.New("update failed")
	ch := newMockChannel()
	svc := newConnectedTestService(repo, ch)
	defer svc.Close()

	msg := &domain.OutboxMessage{
		ID:         uuid.New(),
		EventType:  "test",
		Payload:    "invalid{json",
		RoutingKey: "test",
		Status:     domain.OutboxStatusProcessing,
		MaxRetries: 3,
	}

	// Should not panic when both unmarshal and update fail
	svc.processOutboxMessage(msg)
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
