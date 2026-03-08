package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
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
	) (*domain.MulticastResult, error)
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
	rabbitmq        *rabbitmq.RabbitMQService
	logger          *logger.Logger
	sem             chan struct{}
	wg              sync.WaitGroup
	schedulerCancel context.CancelFunc
	schedulerWg     sync.WaitGroup
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

// SetRabbitMQ sets the RabbitMQ service for queue-based dispatch
func (s *NotificationService) SetRabbitMQ(rmq *rabbitmq.RabbitMQService) {
	s.rabbitmq = rmq
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

	maxWorkers := cfg.Notification.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 50
	}

	return &NotificationService{
		cfg:        cfg,
		repo:       repo,
		emailSvc:   emailSvc,
		sseService: sseService,
		logger:     logger.Get().WithFields(logger.Fields{"service": "notification"}),
		sem:        make(chan struct{}, maxWorkers),
	}
}

// submit runs fn in a goroutine bounded by the semaphore.
// If the pool is full the task is skipped — the notification is transitioned to
// "failed" so that RetryFailedNotifications can pick it up.
func (s *NotificationService) submit(taskName string, fn func()) {
	select {
	case s.sem <- struct{}{}:
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.sem }()
			fn()
		}()
	default:
		metrics.GetMetrics().RecordNotificationSent(taskName, false)
		s.logger.Warn("Worker pool full, task dropped — will be retried",
			"task", taskName,
		)
	}
}

// dispatchNotification publishes the notification ID to RabbitMQ if available,
// otherwise falls back to the in-process goroutine pool.
func (s *NotificationService) dispatchNotification(notificationID uuid.UUID, taskName string, fallbackFn func()) {
	if s.cfg.Notification.UseRabbitMQ && s.rabbitmq != nil && s.rabbitmq.IsConnected() {
		msg := &rabbitmq.Message{
			ID:        uuid.New().String(),
			Type:      "notification.process",
			Source:    "notification-service",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"notification_id": notificationID.String(),
			},
		}
		if err := s.rabbitmq.PublishMessage(context.Background(), nil, msg); err != nil {
			s.logger.Warn("RabbitMQ publish failed, falling back to goroutine pool",
				"notification_id", notificationID,
				"error", err,
			)
			s.submit(taskName, fallbackFn)
			return
		}
		s.logger.Debug("Notification dispatched via RabbitMQ",
			"notification_id", notificationID,
		)
		return
	}
	s.submit(taskName, fallbackFn)
}

// handleNotificationMessage is the RabbitMQ consumer handler.
func (s *NotificationService) handleNotificationMessage(msg *rabbitmq.Message) error {
	idStr, ok := msg.Data["notification_id"].(string)
	if !ok || idStr == "" {
		return fmt.Errorf("missing notification_id in message data")
	}

	notificationID, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("invalid notification_id %q: %w", idStr, err)
	}

	notification, err := s.repo.GetNotification(notificationID)
	if err != nil {
		return fmt.Errorf("failed to get notification %s: %w", notificationID, err)
	}
	if notification == nil {
		s.logger.Warn("Notification not found, skipping", "notification_id", notificationID)
		return nil
	}

	// Idempotency: skip already-sent or read notifications
	if notification.Status == domain.NotificationStatusSent || notification.Status == domain.NotificationStatusRead {
		s.logger.Debug("Notification already processed, skipping",
			"notification_id", notificationID,
			"status", notification.Status,
		)
		return nil
	}

	s.processNotification(notification)
	return nil
}

// StartConsumer declares the notification queue and starts consuming messages.
func (s *NotificationService) StartConsumer() error {
	queueName := s.cfg.Notification.QueueName
	if queueName == "" {
		queueName = "notifications.process"
	}

	if err := s.rabbitmq.DeclareQueue(queueName, []string{"notification.process"}); err != nil {
		return fmt.Errorf("failed to declare notification queue: %w", err)
	}

	if err := s.rabbitmq.Subscribe(queueName, s.handleNotificationMessage); err != nil {
		return fmt.Errorf("failed to subscribe to notification queue: %w", err)
	}

	s.logger.Info("Notification RabbitMQ consumer started", "queue", queueName)
	return nil
}

// StartScheduler starts background tickers for processing pending and retrying failed notifications.
func (s *NotificationService) StartScheduler() {
	ctx, cancel := context.WithCancel(context.Background())
	s.schedulerCancel = cancel

	pendingInterval := s.cfg.Notification.PendingInterval
	if pendingInterval <= 0 {
		pendingInterval = 60 * time.Second
	}
	retryInterval := s.cfg.Notification.RetryInterval
	if retryInterval <= 0 {
		retryInterval = 120 * time.Second
	}

	s.schedulerWg.Add(1)
	go func() {
		defer s.schedulerWg.Done()
		pendingTicker := time.NewTicker(pendingInterval)
		retryTicker := time.NewTicker(retryInterval)
		defer pendingTicker.Stop()
		defer retryTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-pendingTicker.C:
				if err := s.ProcessPendingNotifications(); err != nil {
					s.logger.Error("Scheduler: failed to process pending notifications", "error", err)
				}
			case <-retryTicker.C:
				if err := s.RetryFailedNotifications(); err != nil {
					s.logger.Error("Scheduler: failed to retry failed notifications", "error", err)
				}
			}
		}
	}()

	s.logger.Info("Notification scheduler started",
		"pending_interval", pendingInterval,
		"retry_interval", retryInterval,
	)
}

// Shutdown stops the scheduler and waits for all in-flight goroutines to finish.
// Returns early with ctx.Err() if the context expires.
func (s *NotificationService) Shutdown(ctx context.Context) error {
	// Stop scheduler first
	if s.schedulerCancel != nil {
		s.schedulerCancel()
	}
	s.schedulerWg.Wait()

	// Wait for in-flight fallback goroutines
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

	// Set metadata (include CC/BCC so processNotification can read them)
	metadataMap := req.Data
	if metadataMap == nil {
		metadataMap = make(map[string]interface{})
	}
	if len(req.CC) > 0 {
		metadataMap["_cc"] = req.CC
	}
	if len(req.BCC) > 0 {
		metadataMap["_bcc"] = req.BCC
	}
	if len(metadataMap) > 0 {
		metadata, _ := json.Marshal(metadataMap)
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

	// Dispatch via RabbitMQ or fallback to goroutine pool
	s.dispatchNotification(notification.ID, "email", func() { s.processNotification(notification) })

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

	// Dispatch via RabbitMQ or fallback to goroutine pool
	s.dispatchNotification(notification.ID, "notification", func() { s.processNotification(notification) })

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

// CountUserNotifications returns total notification count for a user.
func (s *NotificationService) CountUserNotifications(userID uuid.UUID) (int64, error) {
	return s.repo.CountUserNotifications(userID)
}

// GetNotificationsSince retrieves notifications for a user created after `since`.
func (s *NotificationService) GetNotificationsSince(userID uuid.UUID, since time.Time) ([]*domain.Notification, bool, error) {
	return s.repo.GetUserNotificationsSince(userID, since, 100)
}

// MarkAsRead marks a notification as read after verifying ownership.
func (s *NotificationService) MarkAsRead(notificationID uuid.UUID, userID uuid.UUID) error {
	return s.repo.MarkAsRead(notificationID, userID)
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

		n := notification // loop variable capture
		s.dispatchNotification(n.ID, "pending", func() { s.processNotification(n) })
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
		notification.Status = domain.NotificationStatusProcessing

		if err := s.repo.UpdateNotification(notification); err != nil {
			s.logger.Error("Failed to update notification for retry",
				"notification_id", notification.ID,
				"error", err,
			)
			continue
		}

		n := notification // loop variable capture
		s.dispatchNotification(n.ID, "retry", func() { s.processNotification(n) })
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
	ctx := context.Background()
	switch notification.Type {
	case domain.NotificationTypeEmail:
		err = s.sendEmailNotification(ctx, notification)
	case domain.NotificationTypeSMS:
		err = s.sendSMSNotification(ctx, notification)
	case domain.NotificationTypePush:
		err = s.sendPushNotification(ctx, notification)
	case domain.NotificationTypeInApp:
		err = s.sendInAppNotification(notification)
	case domain.NotificationTypeWebhook:
		err = s.sendWebhookNotification(ctx, notification)
	default:
		err = fmt.Errorf("unknown notification type: %s", notification.Type)
	}

	if err != nil {
		notification.MarkAsFailed(err)
		metrics.GetMetrics().RecordNotificationSent(string(notification.Type), false)
		s.logger.Error("Failed to send notification",
			"notification_id", notification.ID,
			"type", notification.Type,
			"error", err,
		)
	} else {
		if markErr := notification.MarkAsSent(); markErr != nil {
			s.logger.Warn("Failed to mark notification as sent", "notification_id", notification.ID, "error", markErr)
		}
		metrics.GetMetrics().RecordNotificationSent(string(notification.Type), true)
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

// sendEmailNotification sends an email notification
func (s *NotificationService) sendEmailNotification(ctx context.Context, notification *domain.Notification) error {
	// Check if email service is configured
	if s.emailSvc == nil {
		return fmt.Errorf("email service is not configured")
	}

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

	// Read CC/BCC from metadata if present
	var cc, bcc []string
	if data != nil {
		if ccRaw, ok := data["_cc"]; ok {
			if ccSlice, ok := ccRaw.([]interface{}); ok {
				for _, v := range ccSlice {
					if str, ok := v.(string); ok {
						cc = append(cc, str)
					}
				}
			}
			delete(data, "_cc")
		}
		if bccRaw, ok := data["_bcc"]; ok {
			if bccSlice, ok := bccRaw.([]interface{}); ok {
				for _, v := range bccSlice {
					if str, ok := v.(string); ok {
						bcc = append(bcc, str)
					}
				}
			}
			delete(data, "_bcc")
		}
	}

	// Prepare email data
	emailData := email.EmailData{
		To:       recipients,
		CC:       cc,
		BCC:      bcc,
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
	return s.emailSvc.Send(ctx, emailData)
}

// sendSMSNotification sends an SMS notification via the configured SMS provider.
// To enable SMS, implement the SMSProvider interface and call SetSMSProvider().
func (s *NotificationService) sendSMSNotification(ctx context.Context, notification *domain.Notification) error {
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
		return fmt.Errorf("SMS provider not configured: implement SMSProvider interface and call SetSMSProvider()")
	}

	return s.smsProvider.Send(ctx, phoneNumber, notification.Content)
}

// sendPushNotification sends a push notification via the configured push provider
func (s *NotificationService) sendPushNotification(ctx context.Context, notification *domain.Notification) error {
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
		return fmt.Errorf("push provider not configured: enable FCM via FCM_ENABLED=true and set FCM_SERVER_KEY, FCM_PROJECT_ID")
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

	result, err := s.pushProvider.SendMulticast(
		ctx,
		tokens,
		notification.Subject,
		notification.Content,
		data,
	)
	if result != nil && len(result.FailedTokens) > 0 {
		s.logger.Warn("Stale FCM tokens detected — should be purged",
			"notification_id", notification.ID,
			"failed_tokens", len(result.FailedTokens),
		)
	}
	return err
}

// sendWebhookNotification sends a webhook notification via the configured webhook provider
func (s *NotificationService) sendWebhookNotification(ctx context.Context, notification *domain.Notification) error {
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
		return fmt.Errorf("webhook provider not configured: enable via WEBHOOK_ENABLED=true")
	}

	return s.webhookProvider.Send(ctx, webhookURL, payload)
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
