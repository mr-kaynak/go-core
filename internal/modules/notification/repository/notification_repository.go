package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

// NotificationRepository defines the interface for notification data operations
type NotificationRepository interface {
	// Notification operations
	CreateNotification(ctx context.Context, notification *domain.Notification) error
	UpdateNotification(ctx context.Context, notification *domain.Notification) error
	DeleteNotification(ctx context.Context, id uuid.UUID) error
	GetNotification(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	GetUserNotifications(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Notification, error)
	GetPendingNotifications(ctx context.Context, limit int) ([]*domain.Notification, error)
	GetFailedNotifications(ctx context.Context, limit int) ([]*domain.Notification, error)
	GetScheduledNotifications(ctx context.Context, limit int) ([]*domain.Notification, error)
	ClaimNotificationForProcessing(ctx context.Context, id uuid.UUID) (bool, error)
	CountUserNotifications(ctx context.Context, userID uuid.UUID) (int64, error)
	GetUserNotificationsSince(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]*domain.Notification, bool, error)
	MarkAsRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	MarkAllAsRead(ctx context.Context, userID uuid.UUID) error

	// Email log operations
	CreateEmailLog(ctx context.Context, log *domain.EmailLog) error
	UpdateEmailLog(ctx context.Context, log *domain.EmailLog) error
	GetEmailLog(ctx context.Context, id uuid.UUID) (*domain.EmailLog, error)
	GetEmailLogsByNotification(ctx context.Context, notificationID uuid.UUID) ([]*domain.EmailLog, error)
	GetEmailLogsByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error)
	ListEmailLogs(ctx context.Context, offset, limit int, status string) ([]*domain.EmailLog, int64, error)

	// Notification statistics
	CountByStatus(ctx context.Context) (map[string]int64, error)
	CountByType(ctx context.Context) (map[string]int64, error)

	// Template operations
	CreateTemplate(ctx context.Context, template *domain.NotificationTemplate) error
	UpdateTemplate(ctx context.Context, template *domain.NotificationTemplate) error
	DeleteTemplate(ctx context.Context, id uuid.UUID) error
	GetTemplate(ctx context.Context, id uuid.UUID) (*domain.NotificationTemplate, error)
	GetTemplateByName(ctx context.Context, name string) (*domain.NotificationTemplate, error)
	GetTemplates(ctx context.Context, limit, offset int) ([]*domain.NotificationTemplate, error)
	GetActiveTemplates(ctx context.Context, notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error)

	// User preference operations
	CreateUserPreferences(ctx context.Context, pref *domain.NotificationPreference) error
	UpdateUserPreferences(ctx context.Context, pref *domain.NotificationPreference) error
	DeleteUserPreferences(ctx context.Context, userID uuid.UUID) error
	GetUserPreferences(ctx context.Context, userID uuid.UUID) (*domain.NotificationPreference, error)
}
