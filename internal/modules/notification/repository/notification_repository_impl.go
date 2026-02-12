package repository

import (
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
func (r *notificationRepositoryImpl) CreateNotification(notification *domain.Notification) error {
	return r.db.Create(notification).Error
}

// UpdateNotification updates an existing notification
func (r *notificationRepositoryImpl) UpdateNotification(notification *domain.Notification) error {
	return r.db.Save(notification).Error
}

// DeleteNotification soft deletes a notification
func (r *notificationRepositoryImpl) DeleteNotification(id uuid.UUID) error {
	return r.db.Delete(&domain.Notification{}, id).Error
}

// GetNotification retrieves a notification by ID
func (r *notificationRepositoryImpl) GetNotification(id uuid.UUID) (*domain.Notification, error) {
	var notification domain.Notification
	err := r.db.First(&notification, id).Error
	if err != nil {
		return nil, err
	}
	return &notification, nil
}

// GetUserNotifications retrieves notifications for a user
func (r *notificationRepositoryImpl) GetUserNotifications(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	err := r.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&notifications).Error
	return notifications, err
}

// GetPendingNotifications retrieves pending notifications
func (r *notificationRepositoryImpl) GetPendingNotifications(limit int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	err := r.db.Where("status = ?", domain.NotificationStatusPending).
		Where("scheduled_at IS NULL OR scheduled_at <= ?", time.Now()).
		Order("priority DESC, created_at ASC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

// GetFailedNotifications retrieves failed notifications that can be retried
func (r *notificationRepositoryImpl) GetFailedNotifications(limit int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	err := r.db.Where("status = ?", domain.NotificationStatusFailed).
		Where("retry_count < max_retries").
		Order("priority DESC, created_at ASC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

// GetScheduledNotifications retrieves scheduled notifications that are due
func (r *notificationRepositoryImpl) GetScheduledNotifications(limit int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	err := r.db.Where("status = ?", domain.NotificationStatusPending).
		Where("scheduled_at IS NOT NULL AND scheduled_at <= ?", time.Now()).
		Order("scheduled_at ASC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

// CountUserNotifications counts notifications for a user
func (r *notificationRepositoryImpl) CountUserNotifications(userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&domain.Notification{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// MarkAsRead marks a notification as read (for in-app notifications)
func (r *notificationRepositoryImpl) MarkAsRead(id uuid.UUID) error {
	return r.db.Model(&domain.Notification{}).
		Where("id = ?", id).
		Update("status", domain.NotificationStatusRead).Error
}

// MarkAllAsRead marks all notifications for a user as read
func (r *notificationRepositoryImpl) MarkAllAsRead(userID uuid.UUID) error {
	return r.db.Model(&domain.Notification{}).
		Where("user_id = ? AND status != ?", userID, domain.NotificationStatusRead).
		Update("status", domain.NotificationStatusRead).Error
}

// CreateEmailLog creates a new email log
func (r *notificationRepositoryImpl) CreateEmailLog(log *domain.EmailLog) error {
	return r.db.Create(log).Error
}

// UpdateEmailLog updates an email log
func (r *notificationRepositoryImpl) UpdateEmailLog(log *domain.EmailLog) error {
	return r.db.Save(log).Error
}

// GetEmailLog retrieves an email log by ID
func (r *notificationRepositoryImpl) GetEmailLog(id uuid.UUID) (*domain.EmailLog, error) {
	var log domain.EmailLog
	err := r.db.First(&log, id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// GetEmailLogsByNotification retrieves email logs for a notification
func (r *notificationRepositoryImpl) GetEmailLogsByNotification(notificationID uuid.UUID) ([]*domain.EmailLog, error) {
	var logs []*domain.EmailLog
	err := r.db.Where("notification_id = ?", notificationID).Find(&logs).Error
	return logs, err
}

// GetEmailLogsByUser retrieves email logs for a user (requires join with notifications)
func (r *notificationRepositoryImpl) GetEmailLogsByUser(userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error) {
	var logs []*domain.EmailLog
	err := r.db.Joins("JOIN notifications ON email_logs.notification_id = notifications.id").
		Where("notifications.user_id = ?", userID).
		Order("email_logs.created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error
	return logs, err
}

// CreateTemplate creates a new notification template
func (r *notificationRepositoryImpl) CreateTemplate(template *domain.NotificationTemplate) error {
	return r.db.Create(template).Error
}

// UpdateTemplate updates a notification template
func (r *notificationRepositoryImpl) UpdateTemplate(template *domain.NotificationTemplate) error {
	return r.db.Save(template).Error
}

// DeleteTemplate soft deletes a notification template
func (r *notificationRepositoryImpl) DeleteTemplate(id uuid.UUID) error {
	return r.db.Delete(&domain.NotificationTemplate{}, id).Error
}

// GetTemplate retrieves a template by ID
func (r *notificationRepositoryImpl) GetTemplate(id uuid.UUID) (*domain.NotificationTemplate, error) {
	var template domain.NotificationTemplate
	err := r.db.First(&template, id).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// GetTemplateByName retrieves a template by name
func (r *notificationRepositoryImpl) GetTemplateByName(name string) (*domain.NotificationTemplate, error) {
	var template domain.NotificationTemplate
	err := r.db.Where("name = ?", name).First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// GetTemplates retrieves all templates with pagination
func (r *notificationRepositoryImpl) GetTemplates(limit, offset int) ([]*domain.NotificationTemplate, error) {
	var templates []*domain.NotificationTemplate
	err := r.db.Order("name ASC").Limit(limit).Offset(offset).Find(&templates).Error
	return templates, err
}

// GetActiveTemplates retrieves active templates for a notification type
func (r *notificationRepositoryImpl) GetActiveTemplates(notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error) {
	var templates []*domain.NotificationTemplate
	err := r.db.Where("type = ? AND is_active = ?", notificationType, true).
		Order("name ASC").
		Find(&templates).Error
	return templates, err
}

// CreateUserPreferences creates user notification preferences
func (r *notificationRepositoryImpl) CreateUserPreferences(pref *domain.NotificationPreference) error {
	return r.db.Create(pref).Error
}

// UpdateUserPreferences updates user notification preferences
func (r *notificationRepositoryImpl) UpdateUserPreferences(pref *domain.NotificationPreference) error {
	return r.db.Save(pref).Error
}

// DeleteUserPreferences deletes user notification preferences
func (r *notificationRepositoryImpl) DeleteUserPreferences(userID uuid.UUID) error {
	return r.db.Where("user_id = ?", userID).Delete(&domain.NotificationPreference{}).Error
}

// GetUserPreferences retrieves user notification preferences
func (r *notificationRepositoryImpl) GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error) {
	var pref domain.NotificationPreference
	err := r.db.Where("user_id = ?", userID).First(&pref).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // Return nil if not found, not an error
		}
		return nil, err
	}
	return &pref, nil
}
