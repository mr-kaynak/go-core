package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OutboxStatus represents the status of an outbox message
type OutboxStatus string

const (
	OutboxStatusPending    OutboxStatus = "pending"
	OutboxStatusProcessing OutboxStatus = "processing"
	OutboxStatusSent       OutboxStatus = "sent"
	OutboxStatusFailed     OutboxStatus = "failed"
	OutboxStatusDLQ        OutboxStatus = "dlq" // Dead Letter Queue

	// maxBackoffSeconds is the maximum backoff duration in seconds for message retries
	maxBackoffSeconds = 300
	// maxPriority is the maximum allowed priority level for outbox messages
	maxPriority = 9
)

// OutboxMessage represents a message in the transactional outbox
type OutboxMessage struct {
	ID            uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	AggregateID   uuid.UUID      `gorm:"type:uuid;index" json:"aggregate_id"`           // ID of the aggregate that generated this event
	AggregateType string         `gorm:"type:varchar(100);index" json:"aggregate_type"` // Type of aggregate (e.g., "User", "Order")
	EventType     string         `gorm:"type:varchar(100);index" json:"event_type"`     // e.g., "UserRegistered"
	EventVersion  int            `gorm:"default:1" json:"event_version"`                // Version of the event schema
	Payload       string         `gorm:"type:jsonb" json:"payload"`                     // JSON payload of the message
	Metadata      string         `gorm:"type:jsonb" json:"metadata,omitempty"`          // Additional metadata
	Status        OutboxStatus   `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	Queue         string         `gorm:"type:varchar(100);index" json:"queue"`           // Target queue/exchange
	RoutingKey    string         `gorm:"type:varchar(100)" json:"routing_key,omitempty"` // RabbitMQ routing key
	Priority      int            `gorm:"default:0" json:"priority"`                      // Message priority (0-9)
	RetryCount    int            `gorm:"default:0" json:"retry_count"`
	MaxRetries    int            `gorm:"default:3" json:"max_retries"`
	NextRetryAt   *time.Time     `json:"next_retry_at,omitempty"`
	ProcessedAt   *time.Time     `json:"processed_at,omitempty"`
	FailedAt      *time.Time     `json:"failed_at,omitempty"`
	Error         string         `json:"error,omitempty"`
	CorrelationID string         `gorm:"type:varchar(100);index" json:"correlation_id,omitempty"`
	CausationID   string         `gorm:"type:varchar(100)" json:"causation_id,omitempty"`
	UserID        *uuid.UUID     `gorm:"type:uuid" json:"user_id,omitempty"`
	TenantID      *uuid.UUID     `gorm:"type:uuid" json:"tenant_id,omitempty"` // For multi-tenancy
	TTL           int            `gorm:"default:0" json:"ttl"`                 // Time to live in seconds
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// OutboxDeadLetter represents messages that failed permanently
type OutboxDeadLetter struct {
	ID              uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OutboxMessageID uuid.UUID  `gorm:"type:uuid;index" json:"outbox_message_id"`
	OriginalMessage string     `gorm:"type:jsonb" json:"original_message"` // Full original message
	FailureReason   string     `gorm:"type:text" json:"failure_reason"`
	RetryCount      int        `json:"retry_count"`
	LastError       string     `json:"last_error"`
	Queue           string     `gorm:"type:varchar(100)" json:"queue"`
	EventType       string     `gorm:"type:varchar(100);index" json:"event_type"`
	Reprocessed     bool       `gorm:"default:false" json:"reprocessed"`
	ReprocessedAt   *time.Time `json:"reprocessed_at,omitempty"`
	Notes           string     `json:"notes,omitempty"` // Manual notes for debugging
	CreatedAt       time.Time  `json:"created_at"`
}

// OutboxProcessingLog tracks processing history for audit
type OutboxProcessingLog struct {
	ID              uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OutboxMessageID uuid.UUID `gorm:"type:uuid;index" json:"outbox_message_id"`
	Action          string    `gorm:"type:varchar(50)" json:"action"` // sent, failed, retried, etc.
	Status          string    `gorm:"type:varchar(20)" json:"status"`
	Error           string    `json:"error,omitempty"`
	ProcessingTime  int64     `json:"processing_time"` // In milliseconds
	CreatedAt       time.Time `json:"created_at"`
}

// TableName specifies the table name for OutboxMessage
func (OutboxMessage) TableName() string {
	return "outbox_messages"
}

// TableName specifies the table name for OutboxDeadLetter
func (OutboxDeadLetter) TableName() string {
	return "outbox_dead_letters"
}

// TableName specifies the table name for OutboxProcessingLog
func (OutboxProcessingLog) TableName() string {
	return "outbox_processing_logs"
}

// BeforeCreate hook for OutboxMessage
func (o *OutboxMessage) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.CorrelationID == "" {
		o.CorrelationID = o.ID.String()
	}
	return nil
}

// IsPending checks if the message is pending
func (o *OutboxMessage) IsPending() bool {
	return o.Status == OutboxStatusPending
}

// IsProcessing checks if the message is being processed
func (o *OutboxMessage) IsProcessing() bool {
	return o.Status == OutboxStatusProcessing
}

// CanRetry checks if the message can be retried
func (o *OutboxMessage) CanRetry() bool {
	return o.Status == OutboxStatusFailed && o.RetryCount < o.MaxRetries
}

// IncrementRetry increments the retry count and sets next retry time
func (o *OutboxMessage) IncrementRetry() {
	o.RetryCount++
	// Exponential backoff: 1s, 2s, 4s, 8s, etc.
	backoffSeconds := 1 << o.RetryCount
	if backoffSeconds > maxBackoffSeconds {
		backoffSeconds = maxBackoffSeconds
	}
	nextRetry := time.Now().Add(time.Duration(backoffSeconds) * time.Second)
	o.NextRetryAt = &nextRetry
}

// MarkAsProcessing marks the message as being processed
func (o *OutboxMessage) MarkAsProcessing() {
	o.Status = OutboxStatusProcessing
}

// MarkAsSent marks the message as successfully sent
func (o *OutboxMessage) MarkAsSent() {
	now := time.Now()
	o.Status = OutboxStatusSent
	o.ProcessedAt = &now
}

// MarkAsFailed marks the message as failed
func (o *OutboxMessage) MarkAsFailed(err error) {
	now := time.Now()
	o.Status = OutboxStatusFailed
	o.FailedAt = &now
	if err != nil {
		o.Error = err.Error()
	}
}

// MoveToDLQ moves the message to Dead Letter Queue
func (o *OutboxMessage) MoveToDLQ() {
	o.Status = OutboxStatusDLQ
}

// HasExpired checks if the message has expired based on TTL
func (o *OutboxMessage) HasExpired() bool {
	if o.TTL <= 0 {
		return false
	}
	expirationTime := o.CreatedAt.Add(time.Duration(o.TTL) * time.Second)
	return time.Now().After(expirationTime)
}

// GetPriorityLevel returns the priority level for ordering
func (o *OutboxMessage) GetPriorityLevel() int {
	// Higher priority messages should be processed first
	// Priority ranges from 0 (lowest) to 9 (highest)
	if o.Priority > maxPriority {
		return maxPriority
	}
	if o.Priority < 0 {
		return 0
	}
	return o.Priority
}
