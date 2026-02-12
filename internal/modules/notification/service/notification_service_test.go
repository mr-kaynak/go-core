package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	"github.com/mr-kaynak/go-core/internal/test"
)

type notificationRepoStub struct {
	createNotificationFn   func(notification *domain.Notification) error
	updateNotificationFn   func(notification *domain.Notification) error
	getNotificationFn      func(id uuid.UUID) (*domain.Notification, error)
	getUserNotificationsFn func(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error)
	getFailedFn            func(limit int) ([]*domain.Notification, error)
	getUserPrefsFn         func(userID uuid.UUID) (*domain.NotificationPreference, error)
	createEmailLogFn       func(log *domain.EmailLog) error

	mu      sync.Mutex
	updates []*domain.Notification
}

var _ repository.NotificationRepository = (*notificationRepoStub)(nil)

func (s *notificationRepoStub) CreateNotification(notification *domain.Notification) error {
	if s.createNotificationFn != nil {
		return s.createNotificationFn(notification)
	}
	return nil
}

func (s *notificationRepoStub) UpdateNotification(notification *domain.Notification) error {
	s.mu.Lock()
	copyValue := *notification
	s.updates = append(s.updates, &copyValue)
	s.mu.Unlock()
	if s.updateNotificationFn != nil {
		return s.updateNotificationFn(notification)
	}
	return nil
}

func (s *notificationRepoStub) DeleteNotification(id uuid.UUID) error {
	_ = id
	return nil
}

func (s *notificationRepoStub) GetNotification(id uuid.UUID) (*domain.Notification, error) {
	if s.getNotificationFn != nil {
		return s.getNotificationFn(id)
	}
	return nil, nil
}

func (s *notificationRepoStub) GetUserNotifications(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
	if s.getUserNotificationsFn != nil {
		return s.getUserNotificationsFn(userID, limit, offset)
	}
	return nil, nil
}

func (s *notificationRepoStub) GetPendingNotifications(limit int) ([]*domain.Notification, error) {
	_ = limit
	return nil, nil
}

func (s *notificationRepoStub) GetFailedNotifications(limit int) ([]*domain.Notification, error) {
	if s.getFailedFn != nil {
		return s.getFailedFn(limit)
	}
	return nil, nil
}

func (s *notificationRepoStub) GetScheduledNotifications(limit int) ([]*domain.Notification, error) {
	_ = limit
	return nil, nil
}

func (s *notificationRepoStub) CountUserNotifications(userID uuid.UUID) (int64, error) {
	_ = userID
	return 0, nil
}

func (s *notificationRepoStub) MarkAsRead(id uuid.UUID) error {
	_ = id
	return nil
}

func (s *notificationRepoStub) MarkAllAsRead(userID uuid.UUID) error {
	_ = userID
	return nil
}

func (s *notificationRepoStub) CreateEmailLog(log *domain.EmailLog) error {
	if s.createEmailLogFn != nil {
		return s.createEmailLogFn(log)
	}
	return nil
}

func (s *notificationRepoStub) UpdateEmailLog(log *domain.EmailLog) error {
	_ = log
	return nil
}

func (s *notificationRepoStub) GetEmailLog(id uuid.UUID) (*domain.EmailLog, error) {
	_ = id
	return nil, nil
}

func (s *notificationRepoStub) GetEmailLogsByNotification(notificationID uuid.UUID) ([]*domain.EmailLog, error) {
	_ = notificationID
	return nil, nil
}

func (s *notificationRepoStub) GetEmailLogsByUser(userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error) {
	_ = userID
	_ = limit
	_ = offset
	return nil, nil
}

func (s *notificationRepoStub) CreateTemplate(template *domain.NotificationTemplate) error {
	_ = template
	return nil
}

func (s *notificationRepoStub) UpdateTemplate(template *domain.NotificationTemplate) error {
	_ = template
	return nil
}

func (s *notificationRepoStub) DeleteTemplate(id uuid.UUID) error {
	_ = id
	return nil
}

func (s *notificationRepoStub) GetTemplate(id uuid.UUID) (*domain.NotificationTemplate, error) {
	_ = id
	return nil, nil
}

func (s *notificationRepoStub) GetTemplateByName(name string) (*domain.NotificationTemplate, error) {
	_ = name
	return nil, nil
}

func (s *notificationRepoStub) GetTemplates(limit, offset int) ([]*domain.NotificationTemplate, error) {
	_ = limit
	_ = offset
	return nil, nil
}

func (s *notificationRepoStub) GetActiveTemplates(notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error) {
	_ = notificationType
	return nil, nil
}

func (s *notificationRepoStub) CreateUserPreferences(pref *domain.NotificationPreference) error {
	_ = pref
	return nil
}

func (s *notificationRepoStub) UpdateUserPreferences(pref *domain.NotificationPreference) error {
	_ = pref
	return nil
}

func (s *notificationRepoStub) DeleteUserPreferences(userID uuid.UUID) error {
	_ = userID
	return nil
}

func (s *notificationRepoStub) GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error) {
	if s.getUserPrefsFn != nil {
		return s.getUserPrefsFn(userID)
	}
	return nil, nil
}

func (s *notificationRepoStub) snapshotUpdates() []*domain.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]*domain.Notification, 0, len(s.updates))
	for _, n := range s.updates {
		v := *n
		cp = append(cp, &v)
	}
	return cp
}

type smsProviderStub struct {
	sendFn func(ctx context.Context, phoneNumber, message string) error
}

func (s *smsProviderStub) Send(ctx context.Context, phoneNumber, message string) error {
	if s.sendFn != nil {
		return s.sendFn(ctx, phoneNumber, message)
	}
	return nil
}

type pushProviderStub struct {
	sendFn          func(ctx context.Context, deviceToken, title, body string, data map[string]string) error
	sendMulticastFn func(ctx context.Context, tokens []string, title, body string, data map[string]string) error
}

func (s *pushProviderStub) Send(ctx context.Context, deviceToken, title, body string, data map[string]string) error {
	if s.sendFn != nil {
		return s.sendFn(ctx, deviceToken, title, body, data)
	}
	return nil
}

func (s *pushProviderStub) SendMulticast(
	ctx context.Context, tokens []string,
	title, body string, data map[string]string,
) error {
	if s.sendMulticastFn != nil {
		return s.sendMulticastFn(ctx, tokens, title, body, data)
	}
	return nil
}

type webhookProviderStub struct {
	sendFn func(ctx context.Context, url string, payload interface{}) error
}

func (s *webhookProviderStub) Send(ctx context.Context, url string, payload interface{}) error {
	if s.sendFn != nil {
		return s.sendFn(ctx, url, payload)
	}
	return nil
}

func newNotificationServiceForTest(repo repository.NotificationRepository) *NotificationService {
	cfg := test.TestConfig()
	return &NotificationService{
		cfg:    cfg,
		repo:   repo,
		logger: logger.Get().WithFields(logger.Fields{"service": "notification-test"}),
	}
}

func TestNotificationServiceProcessNotification_AllChannels(t *testing.T) {
	t.Run("sms_graceful_without_provider", func(t *testing.T) {
		repo := &notificationRepoStub{}
		svc := newNotificationServiceForTest(repo)
		n := &domain.Notification{
			ID:       uuid.New(),
			Type:     domain.NotificationTypeSMS,
			Status:   domain.NotificationStatusPending,
			UserID:   uuid.New(),
			Content:  "hello",
			Metadata: `{"phone":"+905001112233"}`,
		}

		svc.processNotification(n)
		if n.Status != domain.NotificationStatusSent {
			t.Fatalf("expected sent status, got %s", n.Status)
		}
	})

	t.Run("push_sent_with_provider", func(t *testing.T) {
		repo := &notificationRepoStub{}
		sent := false
		svc := newNotificationServiceForTest(repo)
		svc.SetPushProvider(&pushProviderStub{
			sendMulticastFn: func(ctx context.Context, tokens []string, title, body string, data map[string]string) error {
				sent = true
				if len(tokens) != 2 {
					t.Fatalf("expected 2 push tokens, got %d", len(tokens))
				}
				return nil
			},
		})
		n := &domain.Notification{
			ID:       uuid.New(),
			Type:     domain.NotificationTypePush,
			Status:   domain.NotificationStatusPending,
			UserID:   uuid.New(),
			Subject:  "subject",
			Content:  "body",
			Metadata: `{"device_tokens":["t1","t2"]}`,
		}

		svc.processNotification(n)
		if !sent {
			t.Fatalf("expected push provider to be called")
		}
		if n.Status != domain.NotificationStatusSent {
			t.Fatalf("expected sent status, got %s", n.Status)
		}
	})

	t.Run("webhook_graceful_without_provider", func(t *testing.T) {
		repo := &notificationRepoStub{}
		svc := newNotificationServiceForTest(repo)
		n := &domain.Notification{
			ID:       uuid.New(),
			Type:     domain.NotificationTypeWebhook,
			Status:   domain.NotificationStatusPending,
			UserID:   uuid.New(),
			Subject:  "subject",
			Content:  "body",
			Metadata: `{"webhook_url":"https://example.com/hook"}`,
		}

		svc.processNotification(n)
		if n.Status != domain.NotificationStatusSent {
			t.Fatalf("expected sent status, got %s", n.Status)
		}
	})

	t.Run("in_app_sent", func(t *testing.T) {
		repo := &notificationRepoStub{}
		svc := newNotificationServiceForTest(repo)
		n := &domain.Notification{
			ID:      uuid.New(),
			Type:    domain.NotificationTypeInApp,
			Status:  domain.NotificationStatusPending,
			UserID:  uuid.New(),
			Subject: "subject",
			Content: "body",
		}

		svc.processNotification(n)
		if n.Status != domain.NotificationStatusSent {
			t.Fatalf("expected sent status, got %s", n.Status)
		}
	})
}

func TestNotificationServiceSendNotification_ScheduledAndCreateError(t *testing.T) {
	t.Run("scheduled_notification_not_processed_immediately", func(t *testing.T) {
		created := false
		repo := &notificationRepoStub{
			createNotificationFn: func(notification *domain.Notification) error {
				created = true
				return nil
			},
		}
		svc := newNotificationServiceForTest(repo)
		scheduled := time.Now().Add(5 * time.Minute)

		n, err := svc.SendNotification(&SendNotificationRequest{
			UserID:      uuid.New(),
			Type:        domain.NotificationTypeInApp,
			Priority:    domain.NotificationPriorityHigh,
			Subject:     "subject",
			Content:     "content",
			Recipients:  []string{"user@example.com"},
			ScheduledAt: &scheduled,
		})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !created || n == nil {
			t.Fatalf("expected notification to be created")
		}
		if n.Status != domain.NotificationStatusPending {
			t.Fatalf("expected pending status, got %s", n.Status)
		}
		if len(repo.snapshotUpdates()) != 0 {
			t.Fatalf("expected no immediate update for scheduled notification")
		}
	})

	t.Run("create_notification_failure_returns_error", func(t *testing.T) {
		repo := &notificationRepoStub{
			createNotificationFn: func(notification *domain.Notification) error {
				return errors.New("db failure")
			},
		}
		svc := newNotificationServiceForTest(repo)

		_, err := svc.SendNotification(&SendNotificationRequest{
			UserID:     uuid.New(),
			Type:       domain.NotificationTypeInApp,
			Priority:   domain.NotificationPriorityNormal,
			Subject:    "subject",
			Content:    "content",
			Recipients: []string{"user@example.com"},
		})
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestNotificationServiceGetUserNotificationsAndFiltering(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	callCount := 0
	repo := &notificationRepoStub{
		getUserNotificationsFn: func(id uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
			callCount++
			if id != userID {
				t.Fatalf("unexpected user id")
			}
			if callCount == 1 && (limit != 10 || offset != 20) {
				t.Fatalf("expected pagination limit=10 offset=20, got limit=%d offset=%d", limit, offset)
			}
			if callCount == 2 && (limit != 100 || offset != 0) {
				t.Fatalf("expected since-query limit=100 offset=0, got limit=%d offset=%d", limit, offset)
			}
			return []*domain.Notification{
				{ID: uuid.New(), CreatedAt: now.Add(-2 * time.Hour)},
				{ID: uuid.New(), CreatedAt: now.Add(-30 * time.Minute)},
			}, nil
		},
	}
	svc := newNotificationServiceForTest(repo)

	list, err := svc.GetUserNotifications(userID, 10, 20)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(list))
	}

	since, err := svc.GetNotificationsSince(userID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(since) != 1 {
		t.Fatalf("expected 1 notification after filtering by time, got %d", len(since))
	}
}

func TestNotificationServiceMarkAsRead_SuccessAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		id := uuid.New()
		ownerID := uuid.New()
		n := &domain.Notification{ID: id, UserID: ownerID, Status: domain.NotificationStatusSent}
		repo := &notificationRepoStub{
			getNotificationFn: func(input uuid.UUID) (*domain.Notification, error) {
				if input != id {
					t.Fatalf("unexpected id")
				}
				return n, nil
			},
		}
		svc := newNotificationServiceForTest(repo)

		if err := svc.MarkAsRead(id, ownerID); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if n.Status != domain.NotificationStatusRead {
			t.Fatalf("expected read status, got %s", n.Status)
		}
	})

	t.Run("wrong_user_forbidden", func(t *testing.T) {
		id := uuid.New()
		ownerID := uuid.New()
		otherID := uuid.New()
		n := &domain.Notification{ID: id, UserID: ownerID, Status: domain.NotificationStatusSent}
		repo := &notificationRepoStub{
			getNotificationFn: func(input uuid.UUID) (*domain.Notification, error) {
				return n, nil
			},
		}
		svc := newNotificationServiceForTest(repo)

		if err := svc.MarkAsRead(id, otherID); err == nil {
			t.Fatalf("expected forbidden error for wrong user")
		}
	})

	t.Run("get_notification_failure", func(t *testing.T) {
		id := uuid.New()
		repo := &notificationRepoStub{
			getNotificationFn: func(input uuid.UUID) (*domain.Notification, error) {
				return nil, errors.New("not found")
			},
		}
		svc := newNotificationServiceForTest(repo)

		if err := svc.MarkAsRead(id, uuid.New()); err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestNotificationServiceRetryFailedNotifications_IncrementsAndSkipsMaxRetries(t *testing.T) {
	retriable := &domain.Notification{
		ID:         uuid.New(),
		Type:       domain.NotificationTypeSMS,
		Status:     domain.NotificationStatusFailed,
		UserID:     uuid.New(),
		Content:    "retry",
		RetryCount: 0,
		MaxRetries: 3,
		Metadata:   `{"phone":"+905001112233"}`,
	}
	atLimit := &domain.Notification{
		ID:         uuid.New(),
		Type:       domain.NotificationTypeSMS,
		Status:     domain.NotificationStatusFailed,
		UserID:     uuid.New(),
		Content:    "skip",
		RetryCount: 3,
		MaxRetries: 3,
		Metadata:   `{"phone":"+905001112233"}`,
	}
	repo := &notificationRepoStub{
		getFailedFn: func(limit int) ([]*domain.Notification, error) {
			if limit != 50 {
				t.Fatalf("expected retry batch limit 50, got %d", limit)
			}
			return []*domain.Notification{retriable, atLimit}, nil
		},
	}
	svc := newNotificationServiceForTest(repo)

	if err := svc.RetryFailedNotifications(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if retriable.RetryCount != 1 {
		t.Fatalf("expected retry count to increment, got %d", retriable.RetryCount)
	}
	if atLimit.RetryCount != 3 {
		t.Fatalf("expected maxed-out notification to remain unchanged")
	}

	updates := repo.snapshotUpdates()
	foundRetriableUpdate := false
	for _, u := range updates {
		if u.ID == retriable.ID && u.RetryCount >= 1 {
			foundRetriableUpdate = true
			break
		}
	}
	if !foundRetriableUpdate {
		t.Fatalf("expected retriable notification update to be persisted")
	}
}
