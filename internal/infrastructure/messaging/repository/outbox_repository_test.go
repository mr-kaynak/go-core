package repository

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupOutboxTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?_busy_timeout=5000"), &gorm.Config{
		Logger: logger.New(log.New(os.Stdout, "", 0), logger.Config{LogLevel: logger.Silent}),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&domain.OutboxMessage{}, &domain.OutboxDeadLetter{}, &domain.OutboxProcessingLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestOutboxRepository_CreateAndGet(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "user.created",
		Payload:    `{"user_id":"abc"}`,
		Queue:      "events",
		RoutingKey: "user.created",
		Priority:   5,
		MaxRetries: 3,
		Status:     domain.OutboxStatusPending,
	}

	if err := repo.CreateMessage(msg); err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	if msg.ID == uuid.Nil {
		t.Fatal("expected ID to be set after create")
	}

	got, err := repo.GetMessage(msg.ID)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if got.EventType != "user.created" {
		t.Errorf("expected event type user.created, got %s", got.EventType)
	}
}

func TestOutboxRepository_CreateMessageTx(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "order.placed",
		Payload:    `{}`,
		Queue:      "events",
		RoutingKey: "order.placed",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.CreateMessageTx(tx, msg)
	})
	if err != nil {
		t.Fatalf("CreateMessageTx failed: %v", err)
	}

	got, err := repo.GetMessage(msg.ID)
	if err != nil || got == nil {
		t.Fatalf("expected message to exist after tx commit")
	}
}

func TestOutboxRepository_UpdateAndDelete(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "test.update",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "test.update",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	}
	repo.CreateMessage(msg)

	msg.Status = domain.OutboxStatusSent
	if err := repo.UpdateMessage(msg); err != nil {
		t.Fatalf("UpdateMessage failed: %v", err)
	}

	got, _ := repo.GetMessage(msg.ID)
	if got.Status != domain.OutboxStatusSent {
		t.Errorf("expected sent status, got %s", got.Status)
	}

	if err := repo.DeleteMessage(msg.ID); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	_, err := repo.GetMessage(msg.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestOutboxRepository_CreateMessages(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msgs := []*domain.OutboxMessage{
		{EventType: "batch.1", Payload: `{}`, Queue: "q", RoutingKey: "batch.1", Status: domain.OutboxStatusPending, MaxRetries: 3},
		{EventType: "batch.2", Payload: `{}`, Queue: "q", RoutingKey: "batch.2", Status: domain.OutboxStatusPending, MaxRetries: 3},
	}

	if err := repo.CreateMessages(msgs); err != nil {
		t.Fatalf("CreateMessages failed: %v", err)
	}

	for _, m := range msgs {
		got, err := repo.GetMessage(m.ID)
		if err != nil || got == nil {
			t.Fatalf("expected message %s to exist", m.ID)
		}
	}
}

func TestOutboxRepository_MarkMessagesAsProcessing(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "mark.test",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "mark.test",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
	}
	repo.CreateMessage(msg)

	if err := repo.MarkMessagesAsProcessing([]uuid.UUID{msg.ID}); err != nil {
		t.Fatalf("MarkMessagesAsProcessing failed: %v", err)
	}

	got, _ := repo.GetMessage(msg.ID)
	if got.Status != domain.OutboxStatusProcessing {
		t.Errorf("expected processing, got %s", got.Status)
	}
}

func TestOutboxRepository_MoveToDLQAndGet(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "dlq.test",
		Payload:    `{"key":"val"}`,
		Queue:      "q",
		RoutingKey: "dlq.test",
		Status:     domain.OutboxStatusFailed,
		MaxRetries: 3,
		RetryCount: 3,
		Error:      "max retries",
	}
	repo.CreateMessage(msg)

	if err := repo.MoveToDLQ(msg, "Max retries exceeded"); err != nil {
		t.Fatalf("MoveToDLQ failed: %v", err)
	}

	got, _ := repo.GetMessage(msg.ID)
	if got.Status != domain.OutboxStatusDLQ {
		t.Errorf("expected dlq status, got %s", got.Status)
	}

	dlqMsgs, err := repo.GetDLQMessages(10)
	if err != nil {
		t.Fatalf("GetDLQMessages failed: %v", err)
	}
	if len(dlqMsgs) == 0 {
		t.Fatal("expected at least one DLQ message")
	}
	if dlqMsgs[0].FailureReason != "Max retries exceeded" {
		t.Errorf("unexpected failure reason: %s", dlqMsgs[0].FailureReason)
	}
}

func TestOutboxRepository_ReprocessDLQMessage(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "reprocess.test",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "reprocess.test",
		Status:     domain.OutboxStatusFailed,
		MaxRetries: 3,
		RetryCount: 3,
	}
	repo.CreateMessage(msg)
	repo.MoveToDLQ(msg, "failed")

	dlqMsgs, _ := repo.GetDLQMessages(10)
	if len(dlqMsgs) == 0 {
		t.Fatal("expected DLQ message")
	}

	if err := repo.ReprocessDLQMessage(dlqMsgs[0].ID); err != nil {
		t.Fatalf("ReprocessDLQMessage failed: %v", err)
	}

	// Original message should be back to pending
	got, _ := repo.GetMessage(msg.ID)
	if got.Status != domain.OutboxStatusPending {
		t.Errorf("expected pending after reprocess, got %s", got.Status)
	}
	if got.RetryCount != 0 {
		t.Errorf("expected retry count reset to 0, got %d", got.RetryCount)
	}
}

func TestOutboxRepository_LogProcessingAndGet(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msgID := uuid.New()
	logEntry := &domain.OutboxProcessingLog{
		OutboxMessageID: msgID,
		Action:          "sent",
		Status:          "success",
		ProcessingTime:  42,
	}

	if err := repo.LogProcessing(logEntry); err != nil {
		t.Fatalf("LogProcessing failed: %v", err)
	}

	logs, err := repo.GetProcessingLogs(msgID)
	if err != nil {
		t.Fatalf("GetProcessingLogs failed: %v", err)
	}
	if len(logs) != 1 || logs[0].Action != "sent" {
		t.Errorf("unexpected logs: %+v", logs)
	}
}

func TestOutboxRepository_CleanupProcessedMessages(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	past := time.Now().Add(-48 * time.Hour)
	msg := &domain.OutboxMessage{
		EventType:  "cleanup.test",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "cleanup.test",
		Status:     domain.OutboxStatusSent,
		MaxRetries: 3,
	}
	repo.CreateMessage(msg)
	// Manually set processed_at to the past
	db.Model(msg).Update("processed_at", past)

	if err := repo.CleanupProcessedMessages(24 * time.Hour); err != nil {
		t.Fatalf("CleanupProcessedMessages failed: %v", err)
	}

	_, err := repo.GetMessage(msg.ID)
	if err == nil {
		t.Fatal("expected message to be cleaned up")
	}
}

func TestOutboxRepository_CleanupExpiredMessages(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msg := &domain.OutboxMessage{
		EventType:  "expire.test",
		Payload:    `{}`,
		Queue:      "q",
		RoutingKey: "expire.test",
		Status:     domain.OutboxStatusPending,
		MaxRetries: 3,
		TTL:        1, // 1 second TTL
	}
	repo.CreateMessage(msg)
	// Backdate creation to ensure expired
	db.Model(msg).Update("created_at", time.Now().Add(-10*time.Second))

	if err := repo.CleanupExpiredMessages(); err != nil {
		t.Fatalf("CleanupExpiredMessages failed: %v", err)
	}

	got, _ := repo.GetMessage(msg.ID)
	if got.Status != domain.OutboxStatusDLQ {
		t.Errorf("expected expired message to be moved to DLQ, got %s", got.Status)
	}
}

func TestOutboxRepository_GetStatistics(t *testing.T) {
	db := setupOutboxTestDB(t)
	repo := NewOutboxRepository(db)

	msgs := []*domain.OutboxMessage{
		{EventType: "s.1", Payload: `{}`, Queue: "q", RoutingKey: "s.1", Status: domain.OutboxStatusPending, MaxRetries: 3},
		{EventType: "s.2", Payload: `{}`, Queue: "q", RoutingKey: "s.2", Status: domain.OutboxStatusPending, MaxRetries: 3},
		{EventType: "s.3", Payload: `{}`, Queue: "q", RoutingKey: "s.3", Status: domain.OutboxStatusSent, MaxRetries: 3},
	}
	repo.CreateMessages(msgs)

	stats, err := repo.GetStatistics()
	if err != nil {
		t.Fatalf("GetStatistics failed: %v", err)
	}
	if stats.PendingCount != 2 {
		t.Errorf("expected 2 pending, got %d", stats.PendingCount)
	}
	if stats.SentCount != 1 {
		t.Errorf("expected 1 sent, got %d", stats.SentCount)
	}
	if stats.TotalCount != 3 {
		t.Errorf("expected 3 total, got %d", stats.TotalCount)
	}
	if stats.OldestPending == nil {
		t.Error("expected oldest pending timestamp")
	}
}
