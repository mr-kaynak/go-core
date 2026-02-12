package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	amqp "github.com/rabbitmq/amqp091-go"
)

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
	channel      *amqp.Channel
	outboxRepo   repository.OutboxRepository
	logger       *logger.Logger
	handlers     map[string]MessageHandler
	mu           sync.RWMutex
	isConnected  bool
	reconnectMux sync.Mutex
	shutdownCh   chan bool
	errorCh      chan *amqp.Error
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewRabbitMQService creates a new RabbitMQ service
func NewRabbitMQService(cfg *config.Config, outboxRepo repository.OutboxRepository) (*RabbitMQService, error) {
	ctx, cancel := context.WithCancel(context.Background())

	service := &RabbitMQService{
		cfg:        cfg,
		outboxRepo: outboxRepo,
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
	go service.handleReconnect()

	// Start outbox processor
	go service.processOutboxMessages()

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
	if err := ch.Qos(10, 0, false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	s.connection = conn
	s.channel = ch
	s.isConnected = true

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
	if !s.isConnected {
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
		"x-message-ttl":             300000, // 5 minutes
		"x-max-retries":             3,
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

// PublishMessage publishes a message via outbox pattern
func (s *RabbitMQService) PublishMessage(ctx context.Context, message *Message) error {
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

	// Store in outbox
	if err := s.outboxRepo.CreateMessage(outboxMsg); err != nil {
		return fmt.Errorf("failed to store message in outbox: %w", err)
	}

	s.logger.Debug("Message queued in outbox", "type", message.Type, "id", message.ID)
	return nil
}

// PublishDirectly publishes a message directly to RabbitMQ (bypasses outbox)
func (s *RabbitMQService) PublishDirectly(ctx context.Context, routingKey string, message *Message) error {
	if !s.isConnected {
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
		return err
	}

	s.logger.Debug("Message published", "type", message.Type, "routing_key", routingKey)
	return nil
}

// Subscribe subscribes to a queue with a handler
func (s *RabbitMQService) Subscribe(queueName string, handler MessageHandler) error {
	if !s.isConnected {
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
	go s.processMessages(queueName, msgs)

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

		// Check retry count from headers
		retryCount := 0
		if val, ok := delivery.Headers["x-retry-count"].(int32); ok {
			retryCount = int(val)
		}

		if retryCount >= 3 {
			// Move to DLQ
			_ = delivery.Nack(false, false)
		} else {
			// Requeue with incremented retry count
			delivery.Headers["x-retry-count"] = int32(retryCount + 1)
			_ = delivery.Nack(false, true)
		}
		return
	}

	// Acknowledge successful processing
	_ = delivery.Ack(false)
	s.logger.Debug("Message processed", "type", message.Type, "queue", queueName)
}

// processOutboxMessages continuously processes messages from the outbox
func (s *RabbitMQService) processOutboxMessages() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			s.processOutboxBatch()
		}
	}
}

// processOutboxBatch processes a batch of outbox messages
func (s *RabbitMQService) processOutboxBatch() {
	if !s.isConnected {
		return
	}

	// Get pending messages
	messages, err := s.outboxRepo.GetPendingMessages(10)
	if err != nil {
		s.logger.Error("Failed to get pending messages", "error", err)
		return
	}

	// Get messages for retry
	retryMessages, err := s.outboxRepo.GetMessagesForRetry(5)
	if err != nil {
		s.logger.Error("Failed to get retry messages", "error", err)
	} else {
		messages = append(messages, retryMessages...)
	}

	for _, msg := range messages {
		s.processOutboxMessage(msg)
	}
}

// processOutboxMessage processes a single outbox message
func (s *RabbitMQService) processOutboxMessage(msg *domain.OutboxMessage) {
	startTime := time.Now()

	// Mark as processing
	msg.MarkAsProcessing()
	_ = s.outboxRepo.UpdateMessage(msg)

	// Parse message
	var message Message
	if err := json.Unmarshal([]byte(msg.Payload), &message); err != nil {
		s.logger.Error("Failed to unmarshal outbox message", "error", err, "id", msg.ID)
		msg.MarkAsFailed(err)
		_ = s.outboxRepo.UpdateMessage(msg)
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
			_ = s.outboxRepo.MoveToDLQ(msg, "Max retries exceeded")
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
	_ = s.outboxRepo.UpdateMessage(msg)
	_ = s.outboxRepo.LogProcessing(log)
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
				s.isConnected = false
				s.reconnect()
			}
		case <-s.shutdownCh:
			return
		}
	}
}

// reconnect attempts to reconnect to RabbitMQ
func (s *RabbitMQService) reconnect() {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for !s.isConnected {
		s.logger.Info("Attempting to reconnect to RabbitMQ", "backoff", backoff)

		if err := s.connect(); err != nil {
			s.logger.Error("Reconnection failed", "error", err)

			// Exponential backoff
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			s.logger.Info("Reconnected to RabbitMQ successfully")
			backoff = 1 * time.Second
		}
	}
}

// Close closes the RabbitMQ connection
func (s *RabbitMQService) Close() error {
	// Cancel context to signal goroutines to stop
	s.cancel()

	close(s.shutdownCh)

	if s.channel != nil {
		s.channel.Close()
	}
	if s.connection != nil {
		return s.connection.Close()
	}
	return nil
}

// HealthCheck checks if the service is healthy
func (s *RabbitMQService) HealthCheck() error {
	if !s.isConnected {
		return fmt.Errorf("not connected to RabbitMQ")
	}
	if s.connection.IsClosed() {
		return fmt.Errorf("connection is closed")
	}
	return nil
}
