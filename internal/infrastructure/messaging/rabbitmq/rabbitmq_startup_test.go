package rabbitmq

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
)

// TestNewRabbitMQService_BrokerDownAtStartup verifies the durable event
// contract when the broker is unreachable at startup: the service must still
// be created so outbox writes reach the database, and the relay must report
// itself as disconnected instead of the process degrading to a nil publisher.
func TestNewRabbitMQService_BrokerDownAtStartup(t *testing.T) {
	repo := newMockOutboxRepo()
	cfg := &config.Config{
		RabbitMQ: config.RabbitMQConfig{ //nolint:gosec // Loopback credentials are disposable test fixture data.
			URL:             "amqp://guest:guest@127.0.0.1:1/",
			Exchange:        "test-exchange",
			QueuePrefix:     "test",
			OutboxBatchSize: 10,
			OutboxMaxRetry:  3,
		},
	}

	svc, err := NewRabbitMQService(cfg, repo, make(chan struct{}))
	if err != nil {
		t.Fatalf("startup must survive an unreachable broker, got: %v", err)
	}
	if svc == nil {
		t.Fatal("service must be created even when the broker is down")
	}
	defer svc.Close()

	if svc.IsConnected() {
		t.Fatal("service must report disconnected while the broker is down")
	}

	msg := &Message{
		ID:        uuid.New().String(),
		Type:      "user.registered",
		Source:    "identity",
		Timestamp: time.Now(),
	}
	if err := svc.PublishMessage(context.Background(), nil, msg); err != nil {
		t.Fatalf("outbox write must succeed without a broker connection: %v", err)
	}

	repo.mu.Lock()
	got := len(repo.messages)
	repo.mu.Unlock()
	if got != 1 {
		t.Fatalf("expected 1 outbox message persisted, got %d", got)
	}
}

// TestSubscribe_DeferredWhileBrokerDown verifies that queue declarations and
// subscriptions made while the broker is down are recorded instead of
// rejected, so consumers start automatically once the connection is
// established. Without this, an email consumer wired at startup would never
// run even after the broker recovers.
func TestSubscribe_DeferredWhileBrokerDown(t *testing.T) {
	repo := newMockOutboxRepo()
	svc := newTestService(t, repo)
	defer svc.Close()

	if err := svc.DeclareQueue("email.process", []string{"email.verification"}); err != nil {
		t.Fatalf("DeclareQueue while disconnected must defer, not fail: %v", err)
	}

	handled := func(_ *Message) error { return nil }
	if err := svc.Subscribe("email.process", handled); err != nil {
		t.Fatalf("Subscribe while disconnected must defer, not fail: %v", err)
	}

	svc.mu.RLock()
	_, handlerRegistered := svc.handlers["email.process"]
	routingKeys, queueRecorded := svc.declaredQueues["email.process"]
	svc.mu.RUnlock()

	if !handlerRegistered {
		t.Fatal("handler must be registered for resubscribe after reconnect")
	}
	if !queueRecorded || len(routingKeys) != 1 || routingKeys[0] != "email.verification" {
		t.Fatalf("queue declaration must be recorded for replay after reconnect, got %v", routingKeys)
	}
}
