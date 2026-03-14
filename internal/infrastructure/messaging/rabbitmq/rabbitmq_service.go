package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

// amqpChannel abstracts the subset of *amqp.Channel methods used by
// RabbitMQService so that tests can supply a mock without a live broker.
type amqpChannel interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Close() error
}

// MessageHandler is a function that processes incoming messages
type MessageHandler func(message *Message) error

// Message represents a RabbitMQ message
type Message struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Source        string                 `json:"source"`
	Timestamp     time.Time              `json:"timestamp"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	CausationID   string                 `json:"causation_id,omitempty"`
	UserID        string                 `json:"user_id,omitempty"`
	TenantID      string                 `json:"tenant_id,omitempty"`
	Data          map[string]interface{} `json:"data"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// RabbitMQService handles RabbitMQ connections and operations
type RabbitMQService struct {
	cfg          *config.Config
	connection   *amqp.Connection
	channel      amqpChannel
	confirmCh    chan amqp.Confirmation // channel-level confirm listener, reused across publishes
	outboxRepo   repository.OutboxRepository
	listenCh     <-chan struct{}
	logger       *logger.Logger
	handlers     map[string]MessageHandler
	mu           sync.RWMutex
	publishMu    sync.Mutex // protects channel for publish operations (AMQP channels are not thread-safe)
	isConnected  atomic.Bool
	reconnectMux sync.Mutex
	shutdownCh   chan bool
	closeOnce    sync.Once
	errorCh      chan *amqp.Error
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

// recordMQ safely records a publish metric (no-op if metrics not initialized).
func recordMQ(exchange, routingKey string, success bool) {
	if m := metrics.GetMetrics(); m != nil {
		m.RecordMQMessagePublished(exchange, routingKey, success)
	}
}

// NewRabbitMQService creates a new RabbitMQ service.
// outboxSignal may be nil — in that case only the 60s fallback polling runs.
func NewRabbitMQService(
	cfg *config.Config,
	outboxRepo repository.OutboxRepository,
	outboxSignal <-chan struct{},
) (*RabbitMQService, error) {
	ctx, cancel := context.WithCancel(context.Background())

	service := &RabbitMQService{
		cfg:        cfg,
		outboxRepo: outboxRepo,
		listenCh:   outboxSignal,
		logger:     logger.Get().WithFields(logger.Fields{"service": "rabbitmq"}),
		handlers:   make(map[string]MessageHandler),
		shutdownCh: make(chan bool),
		errorCh:    make(chan *amqp.Error),
		ctx:        ctx,
		cancel:     cancel,
	}

	if err := service.connect(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Start monitoring connection
	service.wg.Add(4)
	go func() { defer service.wg.Done(); service.handleReconnect() }()
	go func() { defer service.wg.Done(); service.processOutboxMessages() }()
	go func() { defer service.wg.Done(); service.runCleanupJobs() }()
	go func() { defer service.wg.Done(); service.runMetricsUpdater() }()

	return service, nil
}

// connect establishes connection to RabbitMQ
func (s *RabbitMQService) connect() error {
	s.reconnectMux.Lock()
	defer s.reconnectMux.Unlock()

	// Close existing connection if any
	if s.connection != nil && !s.connection.IsClosed() {
		s.connection.Close()
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(s.cfg.RabbitMQ.URL)
	if err != nil {
		s.logger.Error("Failed to connect to RabbitMQ", "error", err)
		return err
	}

	// Create channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Set QoS
	if err := ch.Qos(s.cfg.RabbitMQ.PrefetchCount, 0, false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Enable publisher confirms so we know when the broker has persisted messages
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to enable publisher confirms: %w", err)
	}

	s.connection = conn
	s.channel = ch
	s.isConnected.Store(true)

	// Register a single channel-level confirm listener, reused by all PublishDirectly calls.
	// NotifyPublish must be called once per channel; calling it per-publish leaks listeners.
	s.confirmCh = make(chan amqp.Confirmation, 1)
	s.channel.NotifyPublish(s.confirmCh)

	// Setup error handling
	s.errorCh = make(chan *amqp.Error)
	s.connection.NotifyClose(s.errorCh)

	// Declare exchange
	if err := s.declareExchange(); err != nil {
		return err
	}

	s.logger.Info("Connected to RabbitMQ successfully")
	return nil
}

// declareExchange declares the main exchange
func (s *RabbitMQService) declareExchange() error {
	return s.channel.ExchangeDeclare(
		s.cfg.RabbitMQ.Exchange, // name
		"topic",                 // type
		true,                    // durable
		false,                   // auto-deleted
		false,                   // internal
		false,                   // no-wait
		nil,                     // arguments
	)
}

// DeclareQueue declares a queue with dead letter configuration
func (s *RabbitMQService) DeclareQueue(name string, routingKeys []string) error {
	if !s.isConnected.Load() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Declare DLQ first
	dlqName := fmt.Sprintf("%s.dlq", name)
	_, err := s.channel.QueueDeclare(
		dlqName,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare DLQ: %w", err)
	}

	// Bind DLQ to exchange
	if err := s.channel.QueueBind(
		dlqName,
		fmt.Sprintf("%s.dlq", name), // routing key
		s.cfg.RabbitMQ.Exchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("failed to bind DLQ: %w", err)
	}

	// Declare main queue with DLQ configuration
	args := amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": dlqName,
	}

	_, err = s.channel.QueueDeclare(
		name,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		args,  // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Bind queue to exchange for each routing key
	for _, key := range routingKeys {
		if err := s.channel.QueueBind(
			name,
			key,
			s.cfg.RabbitMQ.Exchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("failed to bind queue: %w", err)
		}
	}

	s.logger.Info("Queue declared successfully", "queue", name, "routing_keys", routingKeys)
	return nil
}

// PublishMessage publishes a message via outbox pattern.
// If tx is non-nil, the outbox insert is performed within that transaction
// so the outbox write is atomic with the business operation (transactional outbox).
// If tx is nil, a standalone insert is used (non-transactional, backward-compatible).
func (s *RabbitMQService) PublishMessage(ctx context.Context, tx *gorm.DB, message *Message) error {
	// Serialize message
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Create outbox entry
	outboxMsg := &domain.OutboxMessage{
		EventType:     message.Type,
		Payload:       string(payload),
		Queue:         s.cfg.RabbitMQ.Exchange,
		RoutingKey:    message.Type,
		Priority:      5,
		MaxRetries:    3,
		CorrelationID: message.CorrelationID,
		CausationID:   message.CausationID,
		TTL:           300, // 5 minutes
	}

	// Store in outbox — use the provided transaction if available
	if tx != nil {
		if err := s.outboxRepo.CreateMessageTx(tx, outboxMsg); err != nil {
			return fmt.Errorf("failed to store message in outbox: %w", err)
		}
	} else {
		if err := s.outboxRepo.CreateMessage(outboxMsg); err != nil {
			return fmt.Errorf("failed to store message in outbox: %w", err)
		}
	}

	s.logger.Debug("Message queued in outbox", "type", message.Type, "id", message.ID)
	return nil
}

// PublishDirectly publishes a message directly to RabbitMQ (bypasses outbox)
func (s *RabbitMQService) PublishDirectly(ctx context.Context, routingKey string, message *Message) error {
	if !s.isConnected.Load() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	publishing := amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		Timestamp:     time.Now(),
		MessageId:     message.ID,
		CorrelationId: message.CorrelationID,
		Body:          payload,
		Headers: amqp.Table{
			"type":      message.Type,
			"source":    message.Source,
			"user_id":   message.UserID,
			"tenant_id": message.TenantID,
		},
	}

	s.publishMu.Lock()
	defer s.publishMu.Unlock()

	err = s.channel.PublishWithContext(
		ctx,
		s.cfg.RabbitMQ.Exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		publishing,
	)

	if err != nil {
		s.logger.Error("Failed to publish message", "error", err, "routing_key", routingKey)
		recordMQ(s.cfg.RabbitMQ.Exchange, routingKey, false)
		return err
	}

	// Wait for broker acknowledgment on the shared channel-level confirm listener
	select {
	case confirmed := <-s.confirmCh:
		if !confirmed.Ack {
			recordMQ(s.cfg.RabbitMQ.Exchange, routingKey, false)
			return fmt.Errorf("broker nacked message (delivery tag %d)", confirmed.DeliveryTag)
		}
	case <-ctx.Done():
		recordMQ(s.cfg.RabbitMQ.Exchange, routingKey, false)
		return fmt.Errorf("timed out waiting for publisher confirm: %w", ctx.Err())
	}

	recordMQ(s.cfg.RabbitMQ.Exchange, routingKey, true)
	s.logger.Debug("Message published", "type", message.Type, "routing_key", routingKey)
	return nil
}

// Subscribe subscribes to a queue with a handler
func (s *RabbitMQService) Subscribe(queueName string, handler MessageHandler) error {
	if !s.isConnected.Load() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Register handler
	s.mu.Lock()
	s.handlers[queueName] = handler
	s.mu.Unlock()

	// Start consuming
	msgs, err := s.channel.Consume(
		queueName,
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	// Process messages in goroutine
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.processMessages(queueName, msgs) }()

	s.logger.Info("Subscribed to queue", "queue", queueName)
	return nil
}

// processMessages processes incoming messages from a queue
func (s *RabbitMQService) processMessages(queueName string, msgs <-chan amqp.Delivery) {
	for msg := range msgs {
		s.handleMessage(queueName, msg)
	}
}

// handleMessage handles a single message
func (s *RabbitMQService) handleMessage(queueName string, delivery amqp.Delivery) {
	var message Message
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		s.logger.Error("Failed to unmarshal message", "error", err)
		_ = delivery.Nack(false, false) // Don't requeue malformed messages
		return
	}

	// Get handler
	s.mu.RLock()
	handler, exists := s.handlers[queueName]
	s.mu.RUnlock()

	if !exists {
		s.logger.Error("No handler for queue", "queue", queueName)
		_ = delivery.Nack(false, true) // Requeue
		return
	}

	// Process message
	if err := handler(&message); err != nil {
		s.logger.Error("Handler failed", "error", err, "message_type", message.Type)
		if m := metrics.GetMetrics(); m != nil {
			m.RecordMQMessageConsumed(queueName, false)
		}

		// Nack without requeue — send to DLQ via dead-letter exchange.
		// Modifying delivery.Headers and requeuing does NOT persist header changes,
		// which would cause an infinite poison-message loop. The DLX/DLQ mechanism
		// provides proper retry tracking via x-death headers.
		_ = delivery.Nack(false, false)
		return
	}

	// Acknowledge successful processing
	_ = delivery.Ack(false)
	if m := metrics.GetMetrics(); m != nil {
		m.RecordMQMessageConsumed(queueName, true)
	}
	s.logger.Debug("Message processed", "type", message.Type, "queue", queueName)
}

// processOutboxMessages continuously processes messages from the outbox.
// It uses LISTEN/NOTIFY signals for immediate processing and a 60s fallback ticker as a safety net.
func (s *RabbitMQService) processOutboxMessages() {
	const outboxFallbackInterval = 60
	fallback := time.NewTicker(outboxFallbackInterval * time.Second)
	defer fallback.Stop()

	// Process once at startup to catch messages inserted before the listener connected
	s.processOutboxBatch()

	for {
		select {
		case <-s.listenCh:
			s.processOutboxBatch()
		case <-fallback.C:
			s.processOutboxBatch()
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		}
	}
}

// processOutboxBatch processes a batch of outbox messages
func (s *RabbitMQService) processOutboxBatch() {
	if !s.isConnected.Load() {
		return
	}

	// Atomically claim both pending and retryable messages in a single transaction
	// to prevent duplicate processing across pods (FOR UPDATE SKIP LOCKED).
	messages, err := s.outboxRepo.ClaimMessagesForProcessing(10, 5)
	if err != nil {
		s.logger.Error("Failed to claim outbox messages for processing", "error", err)
		return
	}

	for _, msg := range messages {
		s.processOutboxMessage(msg)
	}
}

// processOutboxMessage processes a single outbox message
func (s *RabbitMQService) processOutboxMessage(msg *domain.OutboxMessage) {
	startTime := time.Now()

	// Note: message is already marked as "processing" by ClaimMessagesForProcessing

	// Parse message
	var message Message
	if err := json.Unmarshal([]byte(msg.Payload), &message); err != nil {
		s.logger.Error("Failed to unmarshal outbox message", "error", err, "id", msg.ID)
		msg.MarkAsFailed(err)
		if updateErr := s.outboxRepo.UpdateMessage(msg); updateErr != nil {
			s.logger.Error("Failed to update outbox message after unmarshal error", "error", updateErr, "id", msg.ID)
		}
		return
	}

	// Publish to RabbitMQ
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.PublishDirectly(ctx, msg.RoutingKey, &message)

	// Log processing
	processingTime := time.Since(startTime).Milliseconds()
	log := &domain.OutboxProcessingLog{
		OutboxMessageID: msg.ID,
		ProcessingTime:  processingTime,
	}

	if err != nil {
		// Handle failure
		msg.MarkAsFailed(err)

		if msg.CanRetry() {
			msg.IncrementRetry()
			log.Action = "retried"
			log.Status = "pending"
		} else {
			// Move to DLQ after max retries
			if dlqErr := s.outboxRepo.MoveToDLQ(msg, "Max retries exceeded"); dlqErr != nil {
				s.logger.Error("Failed to move outbox message to DLQ", "error", dlqErr, "id", msg.ID)
			}
			log.Action = "moved_to_dlq"
			log.Status = "failed"
		}

		log.Error = err.Error()
		s.logger.Error("Failed to publish outbox message", "error", err, "id", msg.ID, "retries", msg.RetryCount)
	} else {
		// Mark as sent
		msg.MarkAsSent()
		log.Action = "sent"
		log.Status = "success"
		s.logger.Debug("Outbox message published", "id", msg.ID, "type", msg.EventType)
	}

	// Update message and log
	if updateErr := s.outboxRepo.UpdateMessage(msg); updateErr != nil {
		s.logger.Error("Failed to update outbox message status", "error", updateErr, "id", msg.ID, "status", msg.Status)
	}
	if logErr := s.outboxRepo.LogProcessing(log); logErr != nil {
		s.logger.Error("Failed to log outbox processing", "error", logErr, "id", msg.ID)
	}
}

// handleReconnect handles connection failures and reconnection
func (s *RabbitMQService) handleReconnect() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case err := <-s.errorCh:
			if err != nil {
				s.logger.Error("RabbitMQ connection error", "error", err)
				s.isConnected.Store(false)
				s.reconnect()
			}
		case <-s.shutdownCh:
			return
		}
	}
}

// reconnect attempts to reconnect to RabbitMQ with context-aware backoff.
// Returns immediately when the service context is canceled (shutdown).
func (s *RabbitMQService) reconnect() {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for !s.isConnected.Load() {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		s.logger.Info("Attempting to reconnect to RabbitMQ", "backoff", backoff)

		if err := s.connect(); err != nil {
			s.logger.Error("Reconnection failed", "error", err)

			// Context-aware exponential backoff
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			s.logger.Info("Reconnected to RabbitMQ successfully")
			s.resubscribeAll()
			backoff = 1 * time.Second
		}
	}
}

// resubscribeAll re-registers all consumers after a reconnection.
// Old delivery channels are dead after reconnect, so Consume must be called again.
func (s *RabbitMQService) resubscribeAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for queueName, handler := range s.handlers {
		msgs, err := s.channel.Consume(
			queueName,
			"",    // consumer tag
			false, // auto-ack
			false, // exclusive
			false, // no-local
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			s.logger.Error("Failed to re-subscribe after reconnect", "queue", queueName, "error", err)
			continue
		}
		s.wg.Add(1)
		go func() { defer s.wg.Done(); s.processMessages(queueName, msgs) }()
		s.logger.Info("Re-subscribed to queue after reconnect", "queue", queueName)
		_ = handler // handler is already stored, processMessages reads it via s.handlers
	}
}

// runCleanupJobs periodically cleans up processed and expired outbox messages
func (s *RabbitMQService) runCleanupJobs() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			if err := s.outboxRepo.CleanupProcessedMessages(s.cfg.RabbitMQ.ProcessedMessageRetention); err != nil {
				s.logger.Error("Failed to cleanup processed messages", "error", err)
			}
			if err := s.outboxRepo.CleanupExpiredMessages(); err != nil {
				s.logger.Error("Failed to cleanup expired messages", "error", err)
			}
		}
	}
}

// runMetricsUpdater periodically reports outbox/DLQ/connection metrics to Prometheus.
func (s *RabbitMQService) runMetricsUpdater() {
	const metricsUpdateInterval = 30
	ticker := time.NewTicker(metricsUpdateInterval * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			var outboxCount, dlqCount int64
			if stats, err := s.outboxRepo.GetStatistics(); err == nil {
				outboxCount = stats.PendingCount + stats.ProcessingCount
				dlqCount = stats.DLQCount
			}
			if m := metrics.GetMetrics(); m != nil {
				m.UpdateMQMetrics(int(outboxCount), int(dlqCount), s.isConnected.Load())
			}
		}
	}
}

// IsConnected returns whether the service has an active RabbitMQ connection.
func (s *RabbitMQService) IsConnected() bool { return s.isConnected.Load() }

// Close closes the RabbitMQ connection and waits for all goroutines to exit.
func (s *RabbitMQService) Close() error {
	// Signal all goroutines to stop
	s.cancel()
	s.closeOnce.Do(func() { close(s.shutdownCh) })

	// Close AMQP resources so delivery channels drain and processMessages goroutines exit
	if s.channel != nil {
		s.channel.Close()
	}
	var connErr error
	if s.connection != nil {
		connErr = s.connection.Close()
	}

	// Wait for all tracked goroutines to finish
	s.wg.Wait()
	return connErr
}

// HealthCheck checks if the service is healthy
func (s *RabbitMQService) HealthCheck() error {
	if !s.isConnected.Load() {
		return fmt.Errorf("not connected to RabbitMQ")
	}
	if s.connection.IsClosed() {
		return fmt.Errorf("connection is closed")
	}
	return nil
}
