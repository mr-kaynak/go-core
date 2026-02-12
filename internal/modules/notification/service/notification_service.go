package service

import (
	"context"
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

// SMSProvider defines the interface for SMS notification providers.
// Implement this interface with your preferred SMS provider (Twilio, AWS SNS, etc.)
type SMSProvider interface {
	Send(ctx context.Context, phoneNumber, message string) error
}

// PushProvider defines the interface for push notification providers.
type PushProvider interface {
	Send(ctx context.Context, deviceToken, title, body string, data map[string]string) error
	SendMulticast(
		ctx context.Context, tokens []string,
		title, body string, data map[string]string,
	) error
}

// WebhookProvider defines the interface for webhook notification delivery.
type WebhookProvider interface {
	Send(ctx context.Context, url string, payload interface{}) error
}

// NotificationService handles notification operations
type NotificationService struct {
	cfg             *config.Config
	repo            repository.NotificationRepository
	emailSvc        *email.EmailService
	sseService      *SSEService
	pushProvider    PushProvider
	webhookProvider WebhookProvider
	smsProvider     SMSProvider
	logger          *logger.Logger
}

// SetPushProvider sets the push notification provider
func (s *NotificationService) SetPushProvider(p PushProvider) {
	s.pushProvider = p
}

// SetWebhookProvider sets the webhook notification provider
func (s *NotificationService) SetWebhookProvider(w WebhookProvider) {
	s.webhookProvider = w
}

// SetSMSProvider sets the SMS notification provider
func (s *NotificationService) SetSMSProvider(p SMSProvider) {
	s.smsProvider = p
}

// NewNotificationService creates a new notification service
func NewNotificationService(
	cfg *config.Config,
	repo repository.NotificationRepository,
	emailSvc *email.EmailService,
) *NotificationService {
	// Create SSE service if enabled
	var sseService *SSEService
	if cfg.GetBool("sse.enabled") {
		svc, err := NewSSEService(cfg)
		if err != nil {
			logger.Get().Error("Failed to create SSE service", "error", err)
		} else {
			sseService = svc
			// Start SSE service
			if startErr := sseService.Start(); startErr != nil {
				logger.Get().Error("Failed to start SSE service", "error", startErr)
			}
		}
	}

	return &NotificationService{
		cfg:        cfg,
		repo:       repo,
		emailSvc:   emailSvc,
		sseService: sseService,
		logger:     logger.Get().WithFields(logger.Fields{"service": "notification"}),
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

// GetNotificationsSince retrieves notifications for a user since a specific time
func (s *NotificationService) GetNotificationsSince(userID uuid.UUID, since time.Time) ([]*domain.Notification, error) {
	// This would need to be implemented in the repository
	// For now, we'll use GetUserNotifications with a large limit
	notifications, err := s.repo.GetUserNotifications(userID, 100, 0)
	if err != nil {
		return nil, err
	}

	// Filter by timestamp
	var result []*domain.Notification
	for _, n := range notifications {
		if n.CreatedAt.After(since) {
			result = append(result, n)
		}
	}

	return result, nil
}

// MarkAsRead marks a notification as read after verifying ownership.
func (s *NotificationService) MarkAsRead(notificationID uuid.UUID, userID uuid.UUID) error {
	notification, err := s.repo.GetNotification(notificationID)
	if err != nil {
		return err
	}

	if notification.UserID != userID {
		return errors.NewForbidden("Notification does not belong to user")
	}

	notification.MarkAsRead()
	return s.repo.UpdateNotification(notification)
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
		err = s.sendSMSNotification(notification)
	case domain.NotificationTypePush:
		err = s.sendPushNotification(notification)
	case domain.NotificationTypeInApp:
		err = s.sendInAppNotification(notification)
	case domain.NotificationTypeWebhook:
		err = s.sendWebhookNotification(notification)
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

	// Send email with context
	err := s.emailSvc.Send(context.Background(), emailData)

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

	// Send email with context
	return s.emailSvc.Send(context.Background(), emailData)
}

// sendSMSNotification sends an SMS notification via the configured SMS provider.
// To enable SMS, implement the SMSProvider interface and call SetSMSProvider().
func (s *NotificationService) sendSMSNotification(notification *domain.Notification) error {
	var metadata map[string]interface{}
	if notification.Metadata != "" {
		if err := json.Unmarshal([]byte(notification.Metadata), &metadata); err != nil {
			return fmt.Errorf("failed to parse SMS metadata: %w", err)
		}
	}

	phoneNumber, ok := metadata["phone"].(string)
	if !ok || phoneNumber == "" {
		return fmt.Errorf("missing or invalid phone number in metadata")
	}

	if s.smsProvider == nil {
		s.logger.Info("SMS notification queued (no provider configured)",
			"notification_id", notification.ID,
			"phone", phoneNumber,
		)
		return nil
	}

	return s.smsProvider.Send(context.Background(), phoneNumber, notification.Content)
}

// sendPushNotification sends a push notification via the configured push provider
func (s *NotificationService) sendPushNotification(notification *domain.Notification) error {
	var metadata map[string]interface{}
	if notification.Metadata != "" {
		if err := json.Unmarshal([]byte(notification.Metadata), &metadata); err != nil {
			return fmt.Errorf("failed to parse push metadata: %w", err)
		}
	}

	deviceTokens, ok := metadata["device_tokens"].([]interface{})
	if !ok || len(deviceTokens) == 0 {
		return fmt.Errorf("missing or invalid device tokens in metadata")
	}

	if s.pushProvider == nil {
		s.logger.Info("Push notification queued (no provider configured)",
			"notification_id", notification.ID,
			"device_count", len(deviceTokens),
		)
		return nil
	}

	tokens := make([]string, 0, len(deviceTokens))
	for _, t := range deviceTokens {
		if str, ok := t.(string); ok {
			tokens = append(tokens, str)
		}
	}

	data := make(map[string]string)
	data["notification_id"] = notification.ID.String()
	data["type"] = string(notification.Type)

	return s.pushProvider.SendMulticast(
		context.Background(),
		tokens,
		notification.Subject,
		notification.Content,
		data,
	)
}

// sendWebhookNotification sends a webhook notification via the configured webhook provider
func (s *NotificationService) sendWebhookNotification(notification *domain.Notification) error {
	var metadata map[string]interface{}
	if notification.Metadata != "" {
		if err := json.Unmarshal([]byte(notification.Metadata), &metadata); err != nil {
			return fmt.Errorf("failed to parse webhook metadata: %w", err)
		}
	}

	webhookURL, ok := metadata["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return fmt.Errorf("missing or invalid webhook URL in metadata")
	}

	payload := map[string]interface{}{
		"notification_id": notification.ID,
		"type":            notification.Type,
		"subject":         notification.Subject,
		"content":         notification.Content,
		"priority":        notification.Priority,
		"user_id":         notification.UserID,
		"timestamp":       notification.CreatedAt,
	}

	if s.webhookProvider == nil {
		s.logger.Info("Webhook notification queued (no provider configured)",
			"notification_id", notification.ID,
			"webhook_url", webhookURL,
		)
		return nil
	}

	return s.webhookProvider.Send(context.Background(), webhookURL, payload)
}

// sendInAppNotification sends an in-app notification.
//
//nolint:unparam // error return is intentional to match the send method signature used by processNotification
func (s *NotificationService) sendInAppNotification(notification *domain.Notification) error {
	// In-app notifications are stored in database and delivered via SSE
	s.logger.Info("In-app notification created and ready for client delivery",
		"notification_id", notification.ID,
		"user_id", notification.UserID,
		"subject", notification.Subject,
	)

	// Send via SSE if service is available
	if s.sseService != nil && s.sseService.IsHealthy() {
		if err := s.sseService.SendNotificationEvent(notification); err != nil {
			s.logger.Warn("Failed to send notification via SSE",
				"notification_id", notification.ID,
				"error", err,
			)
			// Continue even if SSE fails - notification is still in database
		}
	}

	return nil
}

// GetSSEService returns the SSE service instance
func (s *NotificationService) GetSSEService() *SSEService {
	return s.sseService
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
