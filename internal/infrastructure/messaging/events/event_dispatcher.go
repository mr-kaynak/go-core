package events

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
)

// EventType represents the type of domain event
type EventType string

// Domain event types
const (
	// User events
	EventUserRegistered      EventType = "user.registered"
	EventUserVerified        EventType = "user.email_verified"
	EventUserPasswordReset   EventType = "user.password_reset"
	EventUserPasswordChanged EventType = "user.password_changed"
	EventUserProfileUpdated  EventType = "user.profile_updated"
	EventUserDeleted         EventType = "user.deleted"
	EventUserLocked          EventType = "user.account_locked"
	EventUserUnlocked        EventType = "user.account_unlocked"

	// Auth events
	EventLoginSuccess   EventType = "auth.login_success"
	EventLoginFailed    EventType = "auth.login_failed"
	EventLogout         EventType = "auth.logout"
	EventTokenRefreshed EventType = "auth.token_refreshed"
	EventTokenRevoked   EventType = "auth.token_revoked"

	// Notification events
	EventNotificationSent      EventType = "notification.sent"
	EventNotificationFailed    EventType = "notification.failed"
	EventNotificationScheduled EventType = "notification.scheduled"

	// Email events
	EventEmailSent    EventType = "email.sent"
	EventEmailBounced EventType = "email.bounced"
	EventEmailOpened  EventType = "email.opened"
	EventEmailClicked EventType = "email.clicked"

	// Template events
	EventTemplateCreated EventType = "template.created"
	EventTemplateUpdated EventType = "template.updated"
	EventTemplateDeleted EventType = "template.deleted"
	EventTemplateUsed    EventType = "template.used"
)

// DomainEvent represents a domain event
type DomainEvent struct {
	ID            string                 `json:"id"`
	Type          EventType              `json:"type"`
	AggregateID   string                 `json:"aggregate_id"`
	AggregateType string                 `json:"aggregate_type"`
	Timestamp     time.Time              `json:"timestamp"`
	UserID        string                 `json:"user_id,omitempty"`
	TenantID      string                 `json:"tenant_id,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	CausationID   string                 `json:"causation_id,omitempty"`
	Data          map[string]interface{} `json:"data"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Version       int                    `json:"version"`
}

// EventDispatcher dispatches domain events
type EventDispatcher struct {
	rabbitmq *rabbitmq.RabbitMQService
	logger   *logger.Logger
	handlers map[EventType][]EventHandler
}

// EventHandler handles domain events
type EventHandler func(event *DomainEvent) error

// NewEventDispatcher creates a new event dispatcher
func NewEventDispatcher(rabbitmqService *rabbitmq.RabbitMQService) *EventDispatcher {
	return &EventDispatcher{
		rabbitmq: rabbitmqService,
		logger:   logger.Get().WithFields(logger.Fields{"service": "event_dispatcher"}),
		handlers: make(map[EventType][]EventHandler),
	}
}

// Dispatch dispatches a domain event
func (d *EventDispatcher) Dispatch(ctx context.Context, event *DomainEvent) error {
	// Set defaults
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Version == 0 {
		event.Version = 1
	}

	// Execute local handlers first
	if handlers, exists := d.handlers[event.Type]; exists {
		for _, handler := range handlers {
			if err := handler(event); err != nil {
				d.logger.Error("Local event handler failed",
					"event_type", event.Type,
					"event_id", event.ID,
					"error", err,
				)
				// Continue with other handlers
			}
		}
	}

	// Convert to RabbitMQ message
	message := &rabbitmq.Message{
		ID:            event.ID,
		Type:          string(event.Type),
		Source:        "go-core",
		Timestamp:     event.Timestamp,
		CorrelationID: event.CorrelationID,
		CausationID:   event.CausationID,
		UserID:        event.UserID,
		TenantID:      event.TenantID,
		Data:          event.Data,
		Metadata:      event.Metadata,
	}

	// Publish via RabbitMQ (uses outbox pattern)
	if err := d.rabbitmq.PublishMessage(ctx, message); err != nil {
		d.logger.Error("Failed to publish event",
			"event_type", event.Type,
			"event_id", event.ID,
			"error", err,
		)
		return fmt.Errorf("failed to publish event: %w", err)
	}

	d.logger.Debug("Event dispatched",
		"event_type", event.Type,
		"event_id", event.ID,
		"aggregate_id", event.AggregateID,
	)

	return nil
}

// Register registers a local event handler
func (d *EventDispatcher) Register(eventType EventType, handler EventHandler) {
	d.handlers[eventType] = append(d.handlers[eventType], handler)
	d.logger.Debug("Event handler registered", "event_type", eventType)
}

// DispatchUserRegistered dispatches a user registered event
func (d *EventDispatcher) DispatchUserRegistered(ctx context.Context, userID uuid.UUID, email, username string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventUserRegistered,
		AggregateID:   userID.String(),
		AggregateType: "User",
		UserID:        userID.String(),
		Data: map[string]interface{}{
			"user_id":  userID.String(),
			"email":    email,
			"username": username,
		},
	})
}

// DispatchUserVerified dispatches a user email verified event
func (d *EventDispatcher) DispatchUserVerified(ctx context.Context, userID uuid.UUID, email string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventUserVerified,
		AggregateID:   userID.String(),
		AggregateType: "User",
		UserID:        userID.String(),
		Data: map[string]interface{}{
			"user_id": userID.String(),
			"email":   email,
		},
	})
}

// DispatchLoginSuccess dispatches a successful login event
func (d *EventDispatcher) DispatchLoginSuccess(ctx context.Context, userID uuid.UUID, username, ipAddress, userAgent string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventLoginSuccess,
		AggregateID:   userID.String(),
		AggregateType: "User",
		UserID:        userID.String(),
		Data: map[string]interface{}{
			"user_id":    userID.String(),
			"username":   username,
			"ip_address": ipAddress,
			"user_agent": userAgent,
			"timestamp":  time.Now(),
		},
	})
}

// DispatchLoginFailed dispatches a failed login event
func (d *EventDispatcher) DispatchLoginFailed(ctx context.Context, email, reason, ipAddress string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventLoginFailed,
		AggregateType: "Auth",
		Data: map[string]interface{}{
			"email":      email,
			"reason":     reason,
			"ip_address": ipAddress,
			"timestamp":  time.Now(),
		},
	})
}

// DispatchPasswordReset dispatches a password reset event
func (d *EventDispatcher) DispatchPasswordReset(ctx context.Context, userID uuid.UUID, email string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventUserPasswordReset,
		AggregateID:   userID.String(),
		AggregateType: "User",
		UserID:        userID.String(),
		Data: map[string]interface{}{
			"user_id": userID.String(),
			"email":   email,
		},
	})
}

// DispatchEmailSent dispatches an email sent event
func (d *EventDispatcher) DispatchEmailSent(ctx context.Context, to []string, subject, template string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventEmailSent,
		AggregateType: "Email",
		Data: map[string]interface{}{
			"recipients": to,
			"subject":    subject,
			"template":   template,
			"sent_at":    time.Now(),
		},
	})
}

// DispatchNotificationSent dispatches a notification sent event
func (d *EventDispatcher) DispatchNotificationSent(ctx context.Context, notificationID uuid.UUID, userID uuid.UUID, notificationType string) error {
	return d.Dispatch(ctx, &DomainEvent{
		Type:          EventNotificationSent,
		AggregateID:   notificationID.String(),
		AggregateType: "Notification",
		UserID:        userID.String(),
		Data: map[string]interface{}{
			"notification_id":   notificationID.String(),
			"user_id":           userID.String(),
			"notification_type": notificationType,
			"sent_at":           time.Now(),
		},
	})
}

// CreateEventFromMessage creates a DomainEvent from a RabbitMQ message
func CreateEventFromMessage(msg *rabbitmq.Message) *DomainEvent {
	return &DomainEvent{
		ID:            msg.ID,
		Type:          EventType(msg.Type),
		Timestamp:     msg.Timestamp,
		UserID:        msg.UserID,
		TenantID:      msg.TenantID,
		CorrelationID: msg.CorrelationID,
		CausationID:   msg.CausationID,
		Data:          msg.Data,
		Metadata:      msg.Metadata,
	}
}
