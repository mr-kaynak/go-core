package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
)

// NotificationService handles notification operations
type NotificationService struct {
	cfg      *config.Config
	repo     repository.NotificationRepository
	emailSvc *email.EmailService
	logger   *logger.Logger
}

// NewNotificationService creates a new notification service
func NewNotificationService(
	cfg *config.Config,
	repo repository.NotificationRepository,
	emailSvc *email.EmailService,
) *NotificationService {
	return &NotificationService{
		cfg:      cfg,
		repo:     repo,
		emailSvc: emailSvc,
		logger:   logger.Get().WithFields(logger.Fields{"service": "notification"}),
	}
}

// SendEmailRequest represents a request to send an email notification
type SendEmailRequest struct {
	UserID      uuid.UUID              `json:"user_id" validate:"required"`
	To          []string               `json:"to" validate:"required,dive,email"`
	CC          []string               `json:"cc" validate:"omitempty,dive,email"`
	BCC         []string               `json:"bcc" validate:"omitempty,dive,email"`
	Subject     string                 `json:"subject" validate:"required"`
	Template    string                 `json:"template" validate:"required"`
	Data        map[string]interface{} `json:"data"`
	Priority    string                 `json:"priority" validate:"omitempty,oneof=low normal high urgent"`
	ScheduledAt *time.Time             `json:"scheduled_at"`
}

// SendNotificationRequest represents a generic notification request
type SendNotificationRequest struct {
	UserID      uuid.UUID                   `json:"user_id" validate:"required"`
	Type        domain.NotificationType     `json:"type" validate:"required,oneof=email sms push in_app webhook"`
	Priority    domain.NotificationPriority `json:"priority" validate:"omitempty,oneof=low normal high urgent"`
	Subject     string                      `json:"subject" validate:"required"`
	Content     string                      `json:"content" validate:"required"`
	Template    string                      `json:"template"`
	Recipients  []string                    `json:"recipients" validate:"required,min=1"`
	Metadata    map[string]interface{}      `json:"metadata"`
	ScheduledAt *time.Time                  `json:"scheduled_at"`
}

// SendEmail sends an email notification
func (s *NotificationService) SendEmail(req *SendEmailRequest) (*domain.Notification, error) {
	// Check user preferences
	pref, err := s.repo.GetUserPreferences(req.UserID)
	if err != nil {
		s.logger.Warn("Failed to get user preferences, using defaults", "user_id", req.UserID)
		// Continue with default preferences
	} else if pref != nil && !pref.EmailEnabled {
		return nil, errors.NewBadRequest("Email notifications are disabled for this user")
	}

	// Create notification record
	notification := &domain.Notification{
		UserID:      req.UserID,
		Type:        domain.NotificationTypeEmail,
		Status:      domain.NotificationStatusPending,
		Priority:    s.parsePriority(req.Priority),
		Subject:     req.Subject,
		Template:    req.Template,
		Recipients:  s.marshalRecipients(req.To),
		ScheduledAt: req.ScheduledAt,
	}

	// Set metadata
	if req.Data != nil {
		metadata, _ := json.Marshal(req.Data)
		notification.Metadata = string(metadata)
	}

	// Save notification
	if err := s.repo.CreateNotification(notification); err != nil {
		s.logger.Error("Failed to create notification", "error", err)
		return nil, errors.NewInternalError("Failed to create notification")
	}

	// If scheduled, don't send immediately
	if notification.IsScheduled() {
		s.logger.Info("Notification scheduled",
			"notification_id", notification.ID,
			"scheduled_at", notification.ScheduledAt,
		)
		return notification, nil
	}

	// Send immediately
	go s.processEmailNotification(notification, req)

	return notification, nil
}

// SendNotification sends a generic notification
func (s *NotificationService) SendNotification(req *SendNotificationRequest) (*domain.Notification, error) {
	// Create notification record
	notification := &domain.Notification{
		UserID:      req.UserID,
		Type:        req.Type,
		Status:      domain.NotificationStatusPending,
		Priority:    req.Priority,
		Subject:     req.Subject,
		Content:     req.Content,
		Template:    req.Template,
		Recipients:  s.marshalRecipients(req.Recipients),
		ScheduledAt: req.ScheduledAt,
	}

	// Set metadata
	if req.Metadata != nil {
		metadata, _ := json.Marshal(req.Metadata)
		notification.Metadata = string(metadata)
	}

	// Save notification
	if err := s.repo.CreateNotification(notification); err != nil {
		s.logger.Error("Failed to create notification", "error", err)
		return nil, errors.NewInternalError("Failed to create notification")
	}

	// If scheduled, don't send immediately
	if notification.IsScheduled() {
		s.logger.Info("Notification scheduled",
			"notification_id", notification.ID,
			"type", notification.Type,
			"scheduled_at", notification.ScheduledAt,
		)
		return notification, nil
	}

	// Process based on type
	go s.processNotification(notification)

	return notification, nil
}

// GetNotification retrieves a notification by ID
func (s *NotificationService) GetNotification(id uuid.UUID) (*domain.Notification, error) {
	notification, err := s.repo.GetNotification(id)
	if err != nil {
		return nil, errors.NewNotFound("Notification", id.String())
	}
	return notification, nil
}

// GetUserNotifications retrieves notifications for a user
func (s *NotificationService) GetUserNotifications(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
	return s.repo.GetUserNotifications(userID, limit, offset)
}

// GetUserPreferences retrieves user notification preferences
func (s *NotificationService) GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error) {
	pref, err := s.repo.GetUserPreferences(userID)
	if err != nil {
		return nil, err
	}

	// Create default preferences if not exist
	if pref == nil {
		pref = &domain.NotificationPreference{
			UserID:       userID,
			EmailEnabled: true,
			InAppEnabled: true,
		}
		if err := s.repo.CreateUserPreferences(pref); err != nil {
			return nil, err
		}
	}

	return pref, nil
}

// UpdateUserPreferences updates user notification preferences
func (s *NotificationService) UpdateUserPreferences(userID uuid.UUID, pref *domain.NotificationPreference) error {
	pref.UserID = userID
	return s.repo.UpdateUserPreferences(pref)
}

// ProcessPendingNotifications processes all pending notifications
func (s *NotificationService) ProcessPendingNotifications() error {
	notifications, err := s.repo.GetPendingNotifications(100) // Process 100 at a time
	if err != nil {
		return err
	}

	for _, notification := range notifications {
		// Skip scheduled notifications that are not due yet
		if notification.IsScheduled() {
			continue
		}

		go s.processNotification(notification)
	}

	return nil
}

// RetryFailedNotifications retries failed notifications
func (s *NotificationService) RetryFailedNotifications() error {
	notifications, err := s.repo.GetFailedNotifications(50) // Retry 50 at a time
	if err != nil {
		return err
	}

	for _, notification := range notifications {
		if !notification.CanRetry() {
			continue
		}

		notification.IncrementRetry()
		notification.Status = domain.NotificationStatusPending

		if err := s.repo.UpdateNotification(notification); err != nil {
			s.logger.Error("Failed to update notification for retry",
				"notification_id", notification.ID,
				"error", err,
			)
			continue
		}

		go s.processNotification(notification)
	}

	return nil
}

// processNotification processes a notification based on its type
func (s *NotificationService) processNotification(notification *domain.Notification) {
	// Update status to processing
	notification.Status = domain.NotificationStatusProcessing
	if err := s.repo.UpdateNotification(notification); err != nil {
		s.logger.Error("Failed to update notification status",
			"notification_id", notification.ID,
			"error", err,
		)
		return
	}

	var err error
	switch notification.Type {
	case domain.NotificationTypeEmail:
		err = s.sendEmailNotification(notification)
	case domain.NotificationTypeSMS:
		err = fmt.Errorf("SMS notifications not yet implemented")
	case domain.NotificationTypePush:
		err = fmt.Errorf("Push notifications not yet implemented")
	case domain.NotificationTypeInApp:
		err = s.sendInAppNotification(notification)
	case domain.NotificationTypeWebhook:
		err = fmt.Errorf("Webhook notifications not yet implemented")
	default:
		err = fmt.Errorf("unknown notification type: %s", notification.Type)
	}

	if err != nil {
		notification.MarkAsFailed(err)
		s.logger.Error("Failed to send notification",
			"notification_id", notification.ID,
			"type", notification.Type,
			"error", err,
		)
	} else {
		notification.MarkAsSent()
		s.logger.Info("Notification sent successfully",
			"notification_id", notification.ID,
			"type", notification.Type,
		)
	}

	// Update notification status
	if err := s.repo.UpdateNotification(notification); err != nil {
		s.logger.Error("Failed to update notification after processing",
			"notification_id", notification.ID,
			"error", err,
		)
	}
}

// processEmailNotification processes an email notification
func (s *NotificationService) processEmailNotification(notification *domain.Notification, req *SendEmailRequest) {
	// Update status to processing
	notification.Status = domain.NotificationStatusProcessing
	if err := s.repo.UpdateNotification(notification); err != nil {
		s.logger.Error("Failed to update notification status",
			"notification_id", notification.ID,
			"error", err,
		)
		return
	}

	// Prepare email data
	emailData := email.EmailData{
		To:       req.To,
		CC:       req.CC,
		BCC:      req.BCC,
		Subject:  req.Subject,
		Template: req.Template,
		Data:     req.Data,
		Priority: s.convertPriority(notification.Priority),
	}

	// Send email
	err := s.emailSvc.Send(emailData)

	// Create email log
	emailLog := &domain.EmailLog{
		NotificationID: &notification.ID,
		From:           s.cfg.Email.FromEmail,
		To:             s.marshalRecipients(req.To),
		CC:             s.marshalRecipients(req.CC),
		BCC:            s.marshalRecipients(req.BCC),
		Subject:        req.Subject,
		Template:       req.Template,
	}

	if err != nil {
		notification.MarkAsFailed(err)
		emailLog.Status = "failed"
		emailLog.Error = err.Error()
		s.logger.Error("Failed to send email",
			"notification_id", notification.ID,
			"to", req.To,
			"error", err,
		)
	} else {
		notification.MarkAsSent()
		emailLog.Status = "sent"
		s.logger.Info("Email sent successfully",
			"notification_id", notification.ID,
			"to", req.To,
			"template", req.Template,
		)
	}

	// Save email log
	if err := s.repo.CreateEmailLog(emailLog); err != nil {
		s.logger.Error("Failed to create email log", "error", err)
	}

	// Update notification
	if err := s.repo.UpdateNotification(notification); err != nil {
		s.logger.Error("Failed to update notification after email sending",
			"notification_id", notification.ID,
			"error", err,
		)
	}
}

// sendEmailNotification sends an email notification
func (s *NotificationService) sendEmailNotification(notification *domain.Notification) error {
	// Parse recipients
	recipients := s.unmarshalRecipients(notification.Recipients)
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients specified")
	}

	// Parse metadata
	var data map[string]interface{}
	if notification.Metadata != "" {
		if err := json.Unmarshal([]byte(notification.Metadata), &data); err != nil {
			s.logger.Warn("Failed to parse notification metadata", "error", err)
		}
	}

	// Prepare email data
	emailData := email.EmailData{
		To:       recipients,
		Subject:  notification.Subject,
		Template: notification.Template,
		Data:     data,
		Priority: s.convertPriority(notification.Priority),
	}

	// If no template, use notification template
	if emailData.Template == "" {
		emailData.Template = "notification"
		if emailData.Data == nil {
			emailData.Data = make(map[string]interface{})
		}
		emailData.Data["Content"] = notification.Content
	}

	// Send email
	return s.emailSvc.Send(emailData)
}

// sendInAppNotification sends an in-app notification
func (s *NotificationService) sendInAppNotification(notification *domain.Notification) error {
	// For now, just mark as sent
	// In a real application, this would push to a websocket or notification queue
	s.logger.Info("In-app notification created",
		"notification_id", notification.ID,
		"user_id", notification.UserID,
	)
	return nil
}

// Helper functions

func (s *NotificationService) parsePriority(priority string) domain.NotificationPriority {
	switch priority {
	case "low":
		return domain.NotificationPriorityLow
	case "high":
		return domain.NotificationPriorityHigh
	case "urgent":
		return domain.NotificationPriorityUrgent
	default:
		return domain.NotificationPriorityNormal
	}
}

func (s *NotificationService) convertPriority(priority domain.NotificationPriority) email.Priority {
	switch priority {
	case domain.NotificationPriorityLow:
		return email.PriorityLow
	case domain.NotificationPriorityHigh, domain.NotificationPriorityUrgent:
		return email.PriorityHigh
	default:
		return email.PriorityNormal
	}
}

func (s *NotificationService) marshalRecipients(recipients []string) string {
	if len(recipients) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(recipients)
	return string(data)
}

func (s *NotificationService) unmarshalRecipients(recipients string) []string {
	var result []string
	_ = json.Unmarshal([]byte(recipients), &result)
	return result
}
