package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/mr-kaynak/go-core/internal/test"
)

func newNotificationHandlerTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

func reqNotification(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

type notificationRepoForHandlerStub struct {
	items []*domain.Notification
	pref  *domain.NotificationPreference
}

func (s *notificationRepoForHandlerStub) CreateNotification(notification *domain.Notification) error {
	return nil
}
func (s *notificationRepoForHandlerStub) UpdateNotification(notification *domain.Notification) error {
	return nil
}
func (s *notificationRepoForHandlerStub) DeleteNotification(id uuid.UUID) error { return nil }
func (s *notificationRepoForHandlerStub) GetNotification(id uuid.UUID) (*domain.Notification, error) {
	return &domain.Notification{ID: id, Status: domain.NotificationStatusSent}, nil
}
func (s *notificationRepoForHandlerStub) GetUserNotifications(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
	_ = userID
	_ = limit
	_ = offset
	if s.items == nil {
		return []*domain.Notification{}, nil
	}
	return s.items, nil
}
func (s *notificationRepoForHandlerStub) GetPendingNotifications(limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetFailedNotifications(limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetScheduledNotifications(limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) CountUserNotifications(userID uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *notificationRepoForHandlerStub) MarkAsRead(id uuid.UUID) error             { return nil }
func (s *notificationRepoForHandlerStub) MarkAllAsRead(userID uuid.UUID) error      { return nil }
func (s *notificationRepoForHandlerStub) CreateEmailLog(log *domain.EmailLog) error { return nil }
func (s *notificationRepoForHandlerStub) UpdateEmailLog(log *domain.EmailLog) error { return nil }
func (s *notificationRepoForHandlerStub) GetEmailLog(id uuid.UUID) (*domain.EmailLog, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetEmailLogsByNotification(notificationID uuid.UUID) ([]*domain.EmailLog, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetEmailLogsByUser(userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) CreateTemplate(template *domain.NotificationTemplate) error {
	return nil
}
func (s *notificationRepoForHandlerStub) UpdateTemplate(template *domain.NotificationTemplate) error {
	return nil
}
func (s *notificationRepoForHandlerStub) DeleteTemplate(id uuid.UUID) error { return nil }
func (s *notificationRepoForHandlerStub) GetTemplate(id uuid.UUID) (*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetTemplateByName(name string) (*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetTemplates(limit, offset int) ([]*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) GetActiveTemplates(notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoForHandlerStub) CreateUserPreferences(pref *domain.NotificationPreference) error {
	s.pref = pref
	return nil
}
func (s *notificationRepoForHandlerStub) UpdateUserPreferences(pref *domain.NotificationPreference) error {
	s.pref = pref
	return nil
}
func (s *notificationRepoForHandlerStub) DeleteUserPreferences(userID uuid.UUID) error { return nil }
func (s *notificationRepoForHandlerStub) GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error) {
	_ = userID
	return s.pref, nil
}

func newNotificationHandlerForTest(repo *notificationRepoForHandlerStub) *NotificationHandler {
	cfg := test.TestConfig()
	svc := notificationService.NewNotificationService(cfg, repo, nil)
	return NewNotificationHandler(svc)
}

func TestNotificationHandlerCreateListReadAndPreferences(t *testing.T) {
	userID := uuid.New()
	repo := &notificationRepoForHandlerStub{
		items: []*domain.Notification{
			{ID: uuid.New(), UserID: userID, Subject: "a"},
			{ID: uuid.New(), UserID: userID, Subject: "b"},
		},
	}
	h := newNotificationHandlerForTest(repo)
	app := newNotificationHandlerTestApp()

	app.Get("/notifications", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.ListNotifications(c)
	})
	app.Post("/notifications", h.CreateNotification)
	app.Put("/notifications/:id/read", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.MarkAsRead(c)
	})
	app.Get("/notifications/preferences", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.GetPreferences(c)
	})
	app.Put("/notifications/preferences", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.UpdatePreferences(c)
	})

	createResp := reqNotification(t, app, http.MethodPost, "/notifications", `{"any":"payload"}`)
	if createResp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501 for create placeholder, got %d", createResp.StatusCode)
	}

	listResp := reqNotification(t, app, http.MethodGet, "/notifications?page=1&limit=10", "")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for list notifications, got %d", listResp.StatusCode)
	}

	readResp := reqNotification(t, app, http.MethodPut, "/notifications/"+uuid.NewString()+"/read", "")
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for mark read, got %d", readResp.StatusCode)
	}

	prefsResp := reqNotification(t, app, http.MethodGet, "/notifications/preferences", "")
	if prefsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for get preferences, got %d", prefsResp.StatusCode)
	}

	updatePrefs := reqNotification(t, app, http.MethodPut, "/notifications/preferences", `{"email_enabled":true}`)
	if updatePrefs.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for update preferences, got %d", updatePrefs.StatusCode)
	}
}

func TestNotificationHandlerAuthAndValidationGuards(t *testing.T) {
	h := newNotificationHandlerForTest(&notificationRepoForHandlerStub{})
	app := newNotificationHandlerTestApp()

	app.Get("/notifications", h.ListNotifications)
	app.Put("/notifications/:id/read", h.MarkAsRead)
	app.Put("/notifications/preferences", h.UpdatePreferences)

	unauthList := reqNotification(t, app, http.MethodGet, "/notifications", "")
	if unauthList.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated list, got %d", unauthList.StatusCode)
	}

	badID := reqNotification(t, app, http.MethodPut, "/notifications/not-uuid/read", "")
	if badID.StatusCode != http.StatusUnauthorized {
		// middleware guard hits before ID parsing in this route setup
		t.Fatalf("expected 401 when no auth provided, got %d", badID.StatusCode)
	}

	// With auth, invalid ID should return 400.
	app.Put("/notifications-auth/:id/read", func(c *fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return h.MarkAsRead(c)
	})
	badIDAuthed := reqNotification(t, app, http.MethodPut, "/notifications-auth/not-uuid/read", "")
	if badIDAuthed.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid notification id, got %d", badIDAuthed.StatusCode)
	}

	invalidBody := reqNotification(t, app, http.MethodPut, "/notifications/preferences", "{invalid")
	if invalidBody.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated update prefs, got %d", invalidBody.StatusCode)
	}
}
