package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"gorm.io/gorm"
)

// OutboxRepository defines the interface for outbox operations
type OutboxRepository interface {
	// Message operations
	CreateMessage(msg *domain.OutboxMessage) error
	GetMessage(id uuid.UUID) (*domain.OutboxMessage, error)
	GetPendingMessages(limit int) ([]*domain.OutboxMessage, error)
	GetMessagesForRetry(limit int) ([]*domain.OutboxMessage, error)
	UpdateMessage(msg *domain.OutboxMessage) error
	DeleteMessage(id uuid.UUID) error

	// Batch operations
	CreateMessages(msgs []*domain.OutboxMessage) error
	MarkMessagesAsProcessing(ids []uuid.UUID) error

	// Dead letter operations
	MoveToDLQ(msg *domain.OutboxMessage, reason string) error
	GetDLQMessages(limit int) ([]*domain.OutboxDeadLetter, error)
	ReprocessDLQMessage(id uuid.UUID) error

	// Processing log
	LogProcessing(log *domain.OutboxProcessingLog) error
	GetProcessingLogs(messageID uuid.UUID) ([]*domain.OutboxProcessingLog, error)

	// Cleanup operations
	CleanupProcessedMessages(olderThan time.Duration) error
	CleanupExpiredMessages() error

	// Statistics
	GetStatistics() (*OutboxStatistics, error)
}

// OutboxStatistics represents outbox queue statistics
type OutboxStatistics struct {
	PendingCount    int64
	ProcessingCount int64
	SentCount       int64
	FailedCount     int64
	DLQCount        int64
	TotalCount      int64
	OldestPending   *time.Time
}

// outboxRepositoryImpl implements OutboxRepository
type outboxRepositoryImpl struct {
	db *gorm.DB
}

// NewOutboxRepository creates a new outbox repository
func NewOutboxRepository(db *gorm.DB) OutboxRepository {
	return &outboxRepositoryImpl{db: db}
}

// CreateMessage creates a new outbox message
func (r *outboxRepositoryImpl) CreateMessage(msg *domain.OutboxMessage) error {
	return r.db.Create(msg).Error
}

// GetMessage retrieves a message by ID
func (r *outboxRepositoryImpl) GetMessage(id uuid.UUID) (*domain.OutboxMessage, error) {
	var msg domain.OutboxMessage
	err := r.db.Where("id = ?", id).First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetPendingMessages retrieves pending messages ordered by priority and creation time
func (r *outboxRepositoryImpl) GetPendingMessages(limit int) ([]*domain.OutboxMessage, error) {
	var messages []*domain.OutboxMessage
	err := r.db.Where("status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
		domain.OutboxStatusPending, time.Now()).
		Order("priority DESC, created_at ASC").
		Limit(limit).
		Find(&messages).Error
	return messages, err
}

// GetMessagesForRetry retrieves failed messages that can be retried
func (r *outboxRepositoryImpl) GetMessagesForRetry(limit int) ([]*domain.OutboxMessage, error) {
	var messages []*domain.OutboxMessage
	err := r.db.Where("status = ? AND retry_count < max_retries AND next_retry_at <= ?",
		domain.OutboxStatusFailed, time.Now()).
		Order("priority DESC, next_retry_at ASC").
		Limit(limit).
		Find(&messages).Error
	return messages, err
}

// UpdateMessage updates an existing message
func (r *outboxRepositoryImpl) UpdateMessage(msg *domain.OutboxMessage) error {
	return r.db.Save(msg).Error
}

// DeleteMessage deletes a message
func (r *outboxRepositoryImpl) DeleteMessage(id uuid.UUID) error {
	return r.db.Where("id = ?", id).Delete(&domain.OutboxMessage{}).Error
}

// CreateMessages creates multiple messages in a transaction
func (r *outboxRepositoryImpl) CreateMessages(msgs []*domain.OutboxMessage) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, msg := range msgs {
			if err := tx.Create(msg).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkMessagesAsProcessing marks multiple messages as processing
func (r *outboxRepositoryImpl) MarkMessagesAsProcessing(ids []uuid.UUID) error {
	return r.db.Model(&domain.OutboxMessage{}).
		Where("id IN ?", ids).
		Update("status", domain.OutboxStatusProcessing).Error
}

// MoveToDLQ moves a message to the Dead Letter Queue
func (r *outboxRepositoryImpl) MoveToDLQ(msg *domain.OutboxMessage, reason string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Create DLQ entry
		dlq := &domain.OutboxDeadLetter{
			OutboxMessageID: msg.ID,
			OriginalMessage: msg.Payload,
			FailureReason:   reason,
			RetryCount:      msg.RetryCount,
			LastError:       msg.Error,
			Queue:           msg.Queue,
			EventType:       msg.EventType,
		}

		if err := tx.Create(dlq).Error; err != nil {
			return err
		}

		// Update message status
		msg.MoveToDLQ()
		return tx.Save(msg).Error
	})
}

// GetDLQMessages retrieves messages from the Dead Letter Queue
func (r *outboxRepositoryImpl) GetDLQMessages(limit int) ([]*domain.OutboxDeadLetter, error) {
	var messages []*domain.OutboxDeadLetter
	err := r.db.Where("reprocessed = ?", false).
		Order("created_at DESC").
		Limit(limit).
		Find(&messages).Error
	return messages, err
}

// ReprocessDLQMessage marks a DLQ message for reprocessing
func (r *outboxRepositoryImpl) ReprocessDLQMessage(id uuid.UUID) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Get DLQ message
		var dlq domain.OutboxDeadLetter
		if err := tx.Where("id = ?", id).First(&dlq).Error; err != nil {
			return err
		}

		// Get original outbox message
		var original domain.OutboxMessage
		if err := tx.Where("id = ?", dlq.OutboxMessageID).First(&original).Error; err != nil {
			return err
		}

		// Reset message for reprocessing
		original.Status = domain.OutboxStatusPending
		original.RetryCount = 0
		original.Error = ""
		original.FailedAt = nil
		original.ProcessedAt = nil
		original.NextRetryAt = nil

		if err := tx.Save(&original).Error; err != nil {
			return err
		}

		// Mark DLQ message as reprocessed
		now := time.Now()
		dlq.Reprocessed = true
		dlq.ReprocessedAt = &now
		return tx.Save(&dlq).Error
	})
}

// LogProcessing logs a processing event
func (r *outboxRepositoryImpl) LogProcessing(log *domain.OutboxProcessingLog) error {
	return r.db.Create(log).Error
}

// GetProcessingLogs retrieves processing logs for a message
func (r *outboxRepositoryImpl) GetProcessingLogs(messageID uuid.UUID) ([]*domain.OutboxProcessingLog, error) {
	var logs []*domain.OutboxProcessingLog
	err := r.db.Where("outbox_message_id = ?", messageID).
		Order("created_at DESC").
		Find(&logs).Error
	return logs, err
}

// CleanupProcessedMessages deletes old processed messages
func (r *outboxRepositoryImpl) CleanupProcessedMessages(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return r.db.Where("status IN ? AND processed_at < ?",
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusDLQ}, cutoff).
		Delete(&domain.OutboxMessage{}).Error
}

// CleanupExpiredMessages removes expired messages based on TTL
func (r *outboxRepositoryImpl) CleanupExpiredMessages() error {
	// Find and move expired messages to DLQ
	var expired []*domain.OutboxMessage
	if err := r.db.Where("ttl > 0 AND status = ?", domain.OutboxStatusPending).Find(&expired).Error; err != nil {
		return err
	}

	for _, msg := range expired {
		if msg.HasExpired() {
			if err := r.MoveToDLQ(msg, "Message expired (TTL exceeded)"); err != nil {
				// Log error but continue with other messages
				continue
			}
		}
	}

	return nil
}

// GetStatistics returns outbox statistics
func (r *outboxRepositoryImpl) GetStatistics() (*OutboxStatistics, error) {
	stats := &OutboxStatistics{}

	// Count by status
	r.db.Model(&domain.OutboxMessage{}).Where("status = ?", domain.OutboxStatusPending).Count(&stats.PendingCount)
	r.db.Model(&domain.OutboxMessage{}).Where("status = ?", domain.OutboxStatusProcessing).Count(&stats.ProcessingCount)
	r.db.Model(&domain.OutboxMessage{}).Where("status = ?", domain.OutboxStatusSent).Count(&stats.SentCount)
	r.db.Model(&domain.OutboxMessage{}).Where("status = ?", domain.OutboxStatusFailed).Count(&stats.FailedCount)
	r.db.Model(&domain.OutboxMessage{}).Where("status = ?", domain.OutboxStatusDLQ).Count(&stats.DLQCount)
	r.db.Model(&domain.OutboxMessage{}).Count(&stats.TotalCount)

	// Get oldest pending message
	var oldest domain.OutboxMessage
	if err := r.db.Where("status = ?", domain.OutboxStatusPending).
		Order("created_at ASC").
		First(&oldest).Error; err == nil {
		stats.OldestPending = &oldest.CreatedAt
	}

	return stats, nil
}
