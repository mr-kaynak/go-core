package service

import (
	"context"

	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

// SSEBroadcaster is the narrow view of the SSE service that blog services use
// to publish real-time events. Keeping this interface in the blog module
// avoids a compile-time dependency on the notification module; an adapter at
// the composition root converts blog events to the concrete SSE service.
type SSEBroadcaster interface {
	BroadcastToChannel(ctx context.Context, channel string, event *domain.SSEEvent) error
}
