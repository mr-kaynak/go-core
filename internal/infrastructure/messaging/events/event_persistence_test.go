package events

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"gorm.io/gorm"
)

// newOutboxHarness builds a REAL outbox pipeline on SQLite: gorm DB with the
// outbox schema, real outbox repository, and a real RabbitMQ service pointed
// at an unreachable broker — since the broker-down-at-startup fix, the
// service still persists outbox rows, which is exactly the durable path this
// test asserts.
func newOutboxHarness(t *testing.T) (*gorm.DB, *EventDispatcher) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.OutboxMessage{}, &domain.OutboxProcessingLog{}); err != nil {
		t.Fatalf("automigrate outbox: %v", err)
	}

	cfg := &config.Config{RabbitMQ: config.RabbitMQConfig{ //nolint:gosec // Loopback credentials are disposable test fixture data.
		URL:             "amqp://guest:guest@127.0.0.1:1/",
		Exchange:        "test-exchange",
		QueuePrefix:     "test",
		OutboxBatchSize: 10,
		OutboxMaxRetry:  3,
	}}
	svc, err := rabbitmq.NewRabbitMQService(cfg, messagingRepo.NewOutboxRepository(db), nil)
	if err != nil {
		t.Fatalf("rabbitmq service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	return db, NewEventDispatcher(svc)
}

func outboxRows(t *testing.T, db *gorm.DB) []domain.OutboxMessage {
	t.Helper()
	var rows []domain.OutboxMessage
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	return rows
}

// TestDispatch_PersistsAggregateIDAndPayload asserts the public event
// contract: the persisted outbox record carries event type, aggregate
// identity and payload — a bare row-exists check is not enough.
func TestDispatch_PersistsAggregateIDAndPayload(t *testing.T) {
	db, dispatcher := newOutboxHarness(t)

	err := dispatcher.Dispatch(context.Background(), &DomainEvent{
		Type:        EventType("order.created"),
		AggregateID: "order-42",
		Data:        map[string]interface{}{"total": "99.90"},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	rows := outboxRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("expected 1 outbox row, got %d", len(rows))
	}
	if rows[0].EventType != "order.created" {
		t.Fatalf("event type = %q, want order.created", rows[0].EventType)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if payload["aggregate_id"] != "order-42" {
		t.Fatalf("aggregate_id lost in persisted payload: %v", payload["aggregate_id"])
	}
	data, _ := payload["data"].(map[string]interface{})
	if data["total"] != "99.90" {
		t.Fatalf("payload data lost: %v", payload["data"])
	}
}

// TestDispatch_TxCommitAndRollback asserts transactional-outbox atomicity
// through the public ContextWithTx contract: committed tx → row persisted;
// rolled-back tx → no row.
func TestDispatch_TxCommitAndRollback(t *testing.T) {
	db, dispatcher := newOutboxHarness(t)

	// Commit path.
	err := db.Transaction(func(tx *gorm.DB) error {
		return dispatcher.Dispatch(ContextWithTx(context.Background(), tx), &DomainEvent{
			Type:        EventType("order.created"),
			AggregateID: "order-commit",
			Data:        map[string]interface{}{},
		})
	})
	if err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if got := len(outboxRows(t, db)); got != 1 {
		t.Fatalf("after commit: %d rows, want 1", got)
	}

	// Rollback path: the dispatch succeeds inside the tx, then the business
	// operation fails and rolls back — the outbox row must vanish with it.
	sentinel := context.DeadlineExceeded
	err = db.Transaction(func(tx *gorm.DB) error {
		if dErr := dispatcher.Dispatch(ContextWithTx(context.Background(), tx), &DomainEvent{
			Type:        EventType("order.created"),
			AggregateID: "order-rollback",
			Data:        map[string]interface{}{},
		}); dErr != nil {
			return dErr
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("transaction should have rolled back")
	}
	if got := len(outboxRows(t, db)); got != 1 {
		t.Fatalf("after rollback: %d rows, want still 1 (rolled-back event must not persist)", got)
	}
}
