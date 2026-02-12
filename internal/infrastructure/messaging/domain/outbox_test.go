package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOutboxStateTransitionsAndChecks(t *testing.T) {
	msg := &OutboxMessage{
		ID:         uuid.New(),
		Status:     OutboxStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	if !msg.IsPending() {
		t.Fatalf("expected pending state")
	}

	msg.MarkAsProcessing()
	if !msg.IsProcessing() {
		t.Fatalf("expected processing state")
	}

	msg.MarkAsSent()
	if msg.Status != OutboxStatusSent || msg.ProcessedAt == nil {
		t.Fatalf("expected sent state with processed timestamp")
	}

	msg.MarkAsFailed(errors.New("failed"))
	if msg.Status != OutboxStatusFailed || msg.FailedAt == nil {
		t.Fatalf("expected failed state with failed timestamp")
	}
}

func TestOutboxRetryAndDLQAndTTLAndPriority(t *testing.T) {
	msg := &OutboxMessage{
		ID:         uuid.New(),
		Status:     OutboxStatusFailed,
		RetryCount: 0,
		MaxRetries: 2,
		CreatedAt:  time.Now().Add(-5 * time.Second),
		TTL:        2,
		Priority:   99,
	}

	if !msg.CanRetry() {
		t.Fatalf("expected retryable message")
	}

	msg.IncrementRetry()
	if msg.RetryCount != 1 || msg.NextRetryAt == nil {
		t.Fatalf("expected retry count increment and next retry timestamp")
	}

	msg.IncrementRetry()
	if msg.CanRetry() {
		t.Fatalf("expected non-retryable after reaching max retries")
	}

	msg.MoveToDLQ()
	if msg.Status != OutboxStatusDLQ {
		t.Fatalf("expected DLQ status")
	}

	if !msg.HasExpired() {
		t.Fatalf("expected message to be expired by TTL")
	}

	if got := msg.GetPriorityLevel(); got != 9 {
		t.Fatalf("expected priority clamp to 9, got %d", got)
	}

	msg.Priority = -1
	if got := msg.GetPriorityLevel(); got != 0 {
		t.Fatalf("expected priority floor at 0, got %d", got)
	}
}
