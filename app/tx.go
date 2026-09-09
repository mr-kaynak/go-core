package app

import (
	"context"

	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"gorm.io/gorm"
)

// ContextWithTx returns a context carrying the caller's open business
// transaction. EventPublisher.Dispatch calls made with this context write
// their outbox row inside that transaction (transactional outbox): commit
// persists the event, rollback discards it. WITHOUT it the outbox insert runs
// standalone and a crash between the business commit and the insert silently
// drops the event — always pass the transaction for events tied to business
// writes.
func ContextWithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return events.ContextWithTx(ctx, tx)
}
