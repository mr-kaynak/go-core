package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"gorm.io/gorm"
)

// notificationRepositoryImpl implements NotificationRepository using GORM
type notificationRepositoryImpl struct {
	db *gorm.DB
}

// NewNotificationRepository creates a new notification repository
func NewNotificationRepository(db *gorm.DB) NotificationRepository {
	return &notificationRepositoryImpl{
		db: db,
	}
}

// CreateNotification creates a new notification
func (r *notificationRepositoryImpl) CreateNotification(ctx context.Context, notification *domain.Notification) error {
	db := r.db.WithContext(ctx)
	return db.Create(notification).Error
}

// UpdateNotification updates an existing notification
func (r *notificationRepositoryImpl) UpdateNotification(ctx context.Context, notification *domain.Notification) error {
	db := r.db.WithContext(ctx)
	return db.Save(notification).Error
}

// DeleteNotification soft deletes a notification
func (r *notificationRepositoryImpl) DeleteNotification(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.Notification{}, id).Error
}

// GetNotification retrieves a notification by ID
func (r *notificationRepositoryImpl) GetNotification(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	db := r.db.WithContext(ctx)
	var notification domain.Notification
	err := db.First(&notification, id).Error
	if err != nil {
		return nil, err
	}
	return &notification, nil
}

// GetUserNotifications retrieves notifications for a user
func (r *notificationRepositoryImpl) GetUserNotifications(
	ctx context.Context, userID uuid.UUID, limit, offset int,
) ([]*domain.Notification, error) {
	db := r.db.WithContext(ctx)
	var notifications []*domain.Notification
	err := db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&notifications).Error
	return notifications, err
}

// GetPendingNotifications retrieves pending notifications
func (r *notificationRepositoryImpl) GetPendingNotifications(ctx context.Context, limit int) ([]*domain.Notification, error) {
	db := r.db.WithContext(ctx)
	var notifications []*domain.Notification
	err := db.Where("status = ?", domain.NotificationStatusPending).
		Where("scheduled_at IS NULL OR scheduled_at <= ?", time.Now()).
		Order("priority DESC, created_at ASC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

// GetFailedNotifications retrieves failed notifications that can be retried
func (r *notificationRepositoryImpl) GetFailedNotifications(ctx context.Context, limit int) ([]*domain.Notification, error) {
	db := r.db.WithContext(ctx)
	var notifications []*domain.Notification
	err := db.Where("status = ?", domain.NotificationStatusFailed).
		Where("retry_count < max_retries").
		Order("priority DESC, created_at ASC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

// ClaimNotificationForProcessing atomically transitions a notification from
// pending (or retryable failed, incrementing its retry count) to processing.
// Returns false when another worker already claimed or completed it — this is
// the single dedup gate for concurrent schedulers, RabbitMQ consumers, and
// message redeliveries.
func (r *notificationRepositoryImpl) ClaimNotificationForProcessing(ctx context.Context, id uuid.UUID) (bool, error) {
	db := r.db.WithContext(ctx)
	res := db.Exec(`
		UPDATE notifications
		SET status = ?,
		    retry_count = retry_count + (CASE WHEN status = ? THEN 1 ELSE 0 END),
		    updated_at = NOW()
		WHERE id = ?
		  AND deleted_at IS NULL
		  AND (status = ? OR (status = ? AND retry_count < max_retries))`,
		domain.NotificationStatusProcessing,
		domain.NotificationStatusFailed,
		id,
		domain.NotificationStatusPending,
		domain.NotificationStatusFailed,
	)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// GetScheduledNotifications retrieves scheduled notifications that are due
func (r *notificationRepositoryImpl) GetScheduledNotifications(ctx context.Context, limit int) ([]*domain.Notification, error) {
	db := r.db.WithContext(ctx)
	var notifications []*domain.Notification
	err := db.Where("status = ?", domain.NotificationStatusPending).
		Where("scheduled_at IS NOT NULL AND scheduled_at <= ?", time.Now()).
		Order("scheduled_at ASC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

// CountUserNotifications counts notifications for a user
func (r *notificationRepositoryImpl) CountUserNotifications(ctx context.Context, userID uuid.UUID) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.Notification{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// GetUserNotificationsSince retrieves notifications for a user created after `since`.
// Returns up to `limit` records and a boolean indicating whether more exist.
func (r *notificationRepositoryImpl) GetUserNotificationsSince(
	ctx context.Context, userID uuid.UUID, since time.Time, limit int,
) ([]*domain.Notification, bool, error) {
	db := r.db.WithContext(ctx)
	// Fetch one extra row to determine HasMore
	var notifications []*domain.Notification
	err := db.Where("user_id = ? AND created_at > ?", userID, since).
		Order("created_at ASC").
		Limit(limit + 1).
		Find(&notifications).Error
	if err != nil {
		return nil, false, err
	}
	hasMore := len(notifications) > limit
	if hasMore {
		notifications = notifications[:limit]
	}
	return notifications, hasMore, nil
}

// MarkAsRead marks a notification as read (for in-app notifications).
// Includes a user_id guard to prevent horizontal privilege escalation.
func (r *notificationRepositoryImpl) MarkAsRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Model(&domain.Notification{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("status", domain.NotificationStatusRead).Error
}

// MarkAllAsRead marks all notifications for a user as read
func (r *notificationRepositoryImpl) MarkAllAsRead(ctx context.Context, userID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Model(&domain.Notification{}).
		Where("user_id = ? AND status != ?", userID, domain.NotificationStatusRead).
		Update("status", domain.NotificationStatusRead).Error
}

// CreateEmailLog creates a new email log
func (r *notificationRepositoryImpl) CreateEmailLog(ctx context.Context, log *domain.EmailLog) error {
	db := r.db.WithContext(ctx)
	return db.Create(log).Error
}

// UpdateEmailLog updates an email log
func (r *notificationRepositoryImpl) UpdateEmailLog(ctx context.Context, log *domain.EmailLog) error {
	db := r.db.WithContext(ctx)
	return db.Save(log).Error
}

// GetEmailLog retrieves an email log by ID
func (r *notificationRepositoryImpl) GetEmailLog(ctx context.Context, id uuid.UUID) (*domain.EmailLog, error) {
	db := r.db.WithContext(ctx)
	var log domain.EmailLog
	err := db.First(&log, id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// GetEmailLogsByNotification retrieves email logs for a notification.
// Limited to 100 rows to prevent memory exhaustion on high-retry notifications.
func (r *notificationRepositoryImpl) GetEmailLogsByNotification(
	ctx context.Context, notificationID uuid.UUID,
) ([]*domain.EmailLog, error) {
	db := r.db.WithContext(ctx)
	var logs []*domain.EmailLog
	err := db.Where("notification_id = ?", notificationID).
		Order("created_at DESC").
		Limit(100).
		Find(&logs).Error
	return logs, err
}

// GetEmailLogsByUser retrieves email logs for a user (requires join with notifications)
func (r *notificationRepositoryImpl) GetEmailLogsByUser(
	ctx context.Context, userID uuid.UUID, limit, offset int,
) ([]*domain.EmailLog, error) {
	db := r.db.WithContext(ctx)
	var logs []*domain.EmailLog
	err := db.Joins("JOIN notifications ON email_logs.notification_id = notifications.id").
		Where("notifications.user_id = ?", userID).
		Order("email_logs.created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error
	return logs, err
}

// countGroupedBy counts notifications grouped by the given column
// (column is always a compile-time constant, never user input).
func (r *notificationRepositoryImpl) countGroupedBy(ctx context.Context, column string) (map[string]int64, error) {
	db := r.db.WithContext(ctx)
	type result struct {
		Grp   string `gorm:"column:grp"`
		Count int64
	}
	var results []result
	err := db.Model(&domain.Notification{}).
		Select(column + " AS grp, COUNT(*) AS count").
		Group(column).
		Find(&results).Error
	if err != nil {
		return nil, err
	}
	m := make(map[string]int64)
	for _, r := range results {
		m[r.Grp] = r.Count
	}
	return m, nil
}

// CountByStatus counts notifications grouped by status
func (r *notificationRepositoryImpl) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.countGroupedBy(ctx, "status")
}

// CountByType counts notifications grouped by type
func (r *notificationRepositoryImpl) CountByType(ctx context.Context) (map[string]int64, error) {
	return r.countGroupedBy(ctx, "type")
}

// ListEmailLogs returns paginated email logs with optional status filter
func (r *notificationRepositoryImpl) ListEmailLogs(
	ctx context.Context, offset, limit int, status string,
) ([]*domain.EmailLog, int64, error) {
	db := r.db.WithContext(ctx)
	var logs []*domain.EmailLog
	var total int64

	query := db.Model(&domain.EmailLog{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// CreateTemplate creates a new notification template
func (r *notificationRepositoryImpl) CreateTemplate(ctx context.Context, template *domain.NotificationTemplate) error {
	db := r.db.WithContext(ctx)
	return db.Create(template).Error
}

// UpdateTemplate updates a notification template
func (r *notificationRepositoryImpl) UpdateTemplate(ctx context.Context, template *domain.NotificationTemplate) error {
	db := r.db.WithContext(ctx)
	return db.Save(template).Error
}

// DeleteTemplate soft deletes a notification template
func (r *notificationRepositoryImpl) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.NotificationTemplate{}, id).Error
}

// GetTemplate retrieves a template by ID
func (r *notificationRepositoryImpl) GetTemplate(ctx context.Context, id uuid.UUID) (*domain.NotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var template domain.NotificationTemplate
	err := db.First(&template, id).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// GetTemplateByName retrieves a template by name
func (r *notificationRepositoryImpl) GetTemplateByName(ctx context.Context, name string) (*domain.NotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var template domain.NotificationTemplate
	err := db.Where("name = ?", name).First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// GetTemplates retrieves all templates with pagination
func (r *notificationRepositoryImpl) GetTemplates(ctx context.Context, limit, offset int) ([]*domain.NotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var templates []*domain.NotificationTemplate
	err := db.Order("name ASC").Limit(limit).Offset(offset).Find(&templates).Error
	return templates, err
}

// GetActiveTemplates retrieves active templates for a notification type
func (r *notificationRepositoryImpl) GetActiveTemplates(
	ctx context.Context, notificationType domain.NotificationType,
) ([]*domain.NotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var templates []*domain.NotificationTemplate
	err := db.Where("type = ? AND is_active = ?", notificationType, true).
		Order("name ASC").
		Find(&templates).Error
	return templates, err
}

// CreateUserPreferences creates user notification preferences
func (r *notificationRepositoryImpl) CreateUserPreferences(ctx context.Context, pref *domain.NotificationPreference) error {
	db := r.db.WithContext(ctx)
	return db.Create(pref).Error
}

// UpdateUserPreferences updates user notification preferences
func (r *notificationRepositoryImpl) UpdateUserPreferences(ctx context.Context, pref *domain.NotificationPreference) error {
	db := r.db.WithContext(ctx)
	return db.Save(pref).Error
}

// DeleteUserPreferences deletes user notification preferences
func (r *notificationRepositoryImpl) DeleteUserPreferences(ctx context.Context, userID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("user_id = ?", userID).Delete(&domain.NotificationPreference{}).Error
}

// GetUserPreferences retrieves user notification preferences
func (r *notificationRepositoryImpl) GetUserPreferences(
	ctx context.Context, userID uuid.UUID,
) (*domain.NotificationPreference, error) {
	db := r.db.WithContext(ctx)
	var pref domain.NotificationPreference
	err := db.Where("user_id = ?", userID).First(&pref).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Return nil if not found, not an error
		}
		return nil, err
	}
	return &pref, nil
}
