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
	CreateMessageTx(tx *gorm.DB, msg *domain.OutboxMessage) error
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

// CreateMessageTx creates a new outbox message within a provided transaction.
func (r *outboxRepositoryImpl) CreateMessageTx(tx *gorm.DB, msg *domain.OutboxMessage) error {
	return tx.Create(msg).Error
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

// GetPendingMessages retrieves pending messages ordered by priority and creation time.
// Uses SELECT FOR UPDATE SKIP LOCKED to prevent duplicate processing across instances.
func (r *outboxRepositoryImpl) GetPendingMessages(limit int) ([]*domain.OutboxMessage, error) {
	var messages []*domain.OutboxMessage
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`
			SELECT * FROM outbox_messages
			WHERE status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)
			ORDER BY priority DESC, created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED`,
			domain.OutboxStatusPending, time.Now(), limit).
			Scan(&messages).Error; err != nil {
			return err
		}
		if len(messages) == 0 {
			return nil
		}
		// Batch update status to processing in a single query
		ids := make([]uuid.UUID, len(messages))
		for i, msg := range messages {
			ids[i] = msg.ID
			msg.MarkAsProcessing()
		}
		return tx.Model(&domain.OutboxMessage{}).
			Where("id IN ?", ids).
			Update("status", domain.OutboxStatusProcessing).Error
	})
	return messages, err
}

// GetMessagesForRetry retrieves failed messages that can be retried.
// Uses SELECT FOR UPDATE SKIP LOCKED to prevent duplicate processing across instances.
func (r *outboxRepositoryImpl) GetMessagesForRetry(limit int) ([]*domain.OutboxMessage, error) {
	var messages []*domain.OutboxMessage
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`
			SELECT * FROM outbox_messages
			WHERE status = ? AND retry_count < max_retries AND next_retry_at <= ?
			ORDER BY priority DESC, next_retry_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED`,
			domain.OutboxStatusFailed, time.Now(), limit).
			Scan(&messages).Error; err != nil {
			return err
		}
		if len(messages) == 0 {
			return nil
		}
		// Batch update status to processing in a single query
		ids := make([]uuid.UUID, len(messages))
		for i, msg := range messages {
			ids[i] = msg.ID
			msg.MarkAsProcessing()
		}
		return tx.Model(&domain.OutboxMessage{}).
			Where("id IN ?", ids).
			Update("status", domain.OutboxStatusProcessing).Error
	})
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
	return r.db.Where("status = ? AND processed_at < ?",
		domain.OutboxStatusSent, cutoff).
		Delete(&domain.OutboxMessage{}).Error
}

// CleanupExpiredMessages removes expired messages based on TTL
func (r *outboxRepositoryImpl) CleanupExpiredMessages() error {
	// Find expired messages using SQL instead of loading all into memory
	var expired []*domain.OutboxMessage
	if err := r.db.Where(
		"ttl > 0 AND status = ? AND created_at + make_interval(secs => ttl) < NOW()",
		domain.OutboxStatusPending,
	).Find(&expired).Error; err != nil {
		return err
	}

	for _, msg := range expired {
		if err := r.MoveToDLQ(msg, "Message expired (TTL exceeded)"); err != nil {
			continue
		}
	}

	return nil
}

// GetStatistics returns outbox statistics
func (r *outboxRepositoryImpl) GetStatistics() (*OutboxStatistics, error) {
	stats := &OutboxStatistics{}

	// Single query to count all statuses
	type statusCount struct {
		Status string
		Count  int64
	}
	var counts []statusCount
	if err := r.db.Model(&domain.OutboxMessage{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Find(&counts).Error; err != nil {
		return nil, err
	}

	for _, sc := range counts {
		switch domain.OutboxStatus(sc.Status) {
		case domain.OutboxStatusPending:
			stats.PendingCount = sc.Count
		case domain.OutboxStatusProcessing:
			stats.ProcessingCount = sc.Count
		case domain.OutboxStatusSent:
			stats.SentCount = sc.Count
		case domain.OutboxStatusFailed:
			stats.FailedCount = sc.Count
		case domain.OutboxStatusDLQ:
			stats.DLQCount = sc.Count
		}
		stats.TotalCount += sc.Count
	}

	// Get oldest pending message
	var oldest domain.OutboxMessage
	if err := r.db.Where("status = ?", domain.OutboxStatusPending).
		Order("created_at ASC").
		First(&oldest).Error; err == nil {
		stats.OldestPending = &oldest.CreatedAt
	}

	return stats, nil
}
