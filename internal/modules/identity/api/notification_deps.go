package api

import "context"

// AdminNotificationProcessor is the narrow view of the notification service
// that the admin endpoints use to trigger queue maintenance. Keeping this
// interface in the identity api package avoids a compile-time dependency on
// the notification module; the concrete service satisfies it implicitly.
type AdminNotificationProcessor interface {
	RetryFailedNotifications(ctx context.Context) error
	ProcessPendingNotifications(ctx context.Context) error
}

// AdminSSEMonitor is the narrow view of the SSE service that the admin
// endpoints use to report connection stats and health. The concrete SSE
// service satisfies it implicitly, so no notification import is needed here.
type AdminSSEMonitor interface {
	GetStats() map[string]interface{}
	IsHealthy() bool
}
