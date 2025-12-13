package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

// NotificationRepository defines the interface for notification data operations
type NotificationRepository interface {
	// Notification operations
	CreateNotification(notification *domain.Notification) error
	UpdateNotification(notification *domain.Notification) error
	DeleteNotification(id uuid.UUID) error
	GetNotification(id uuid.UUID) (*domain.Notification, error)
	GetUserNotifications(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error)
	GetPendingNotifications(limit int) ([]*domain.Notification, error)
	GetFailedNotifications(limit int) ([]*domain.Notification, error)
	GetScheduledNotifications(limit int) ([]*domain.Notification, error)
	CountUserNotifications(userID uuid.UUID) (int64, error)
	MarkAsRead(id uuid.UUID) error
	MarkAllAsRead(userID uuid.UUID) error

	// Email log operations
	CreateEmailLog(log *domain.EmailLog) error
	UpdateEmailLog(log *domain.EmailLog) error
	GetEmailLog(id uuid.UUID) (*domain.EmailLog, error)
	GetEmailLogsByNotification(notificationID uuid.UUID) ([]*domain.EmailLog, error)
	GetEmailLogsByUser(userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error)

	// Template operations
	CreateTemplate(template *domain.NotificationTemplate) error
	UpdateTemplate(template *domain.NotificationTemplate) error
	DeleteTemplate(id uuid.UUID) error
	GetTemplate(id uuid.UUID) (*domain.NotificationTemplate, error)
	GetTemplateByName(name string) (*domain.NotificationTemplate, error)
	GetTemplates(limit, offset int) ([]*domain.NotificationTemplate, error)
	GetActiveTemplates(notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error)

	// User preference operations
	CreateUserPreferences(pref *domain.NotificationPreference) error
	UpdateUserPreferences(pref *domain.NotificationPreference) error
	DeleteUserPreferences(userID uuid.UUID) error
	GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error)
}
