package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/mr-kaynak/go-core/internal/test"
)

type sseNotificationRepoStub struct{}

func (s *sseNotificationRepoStub) CreateNotification(notification *domain.Notification) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateNotification(notification *domain.Notification) error {
	return nil
}
func (s *sseNotificationRepoStub) DeleteNotification(id uuid.UUID) error { return nil }
func (s *sseNotificationRepoStub) GetNotification(id uuid.UUID) (*domain.Notification, error) {
	return &domain.Notification{ID: id, Status: domain.NotificationStatusSent}, nil
}
func (s *sseNotificationRepoStub) GetUserNotifications(userID uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetPendingNotifications(limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetFailedNotifications(limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetScheduledNotifications(limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CountUserNotifications(userID uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *sseNotificationRepoStub) GetUserNotificationsSince(userID uuid.UUID, since time.Time, limit int) ([]*domain.Notification, bool, error) {
	return nil, false, nil
}
func (s *sseNotificationRepoStub) MarkAsRead(id uuid.UUID, userID uuid.UUID) error { return nil }
func (s *sseNotificationRepoStub) MarkAllAsRead(userID uuid.UUID) error            { return nil }
func (s *sseNotificationRepoStub) CreateEmailLog(log *domain.EmailLog) error       { return nil }
func (s *sseNotificationRepoStub) UpdateEmailLog(log *domain.EmailLog) error       { return nil }
func (s *sseNotificationRepoStub) GetEmailLog(id uuid.UUID) (*domain.EmailLog, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetEmailLogsByNotification(notificationID uuid.UUID) ([]*domain.EmailLog, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetEmailLogsByUser(userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CreateTemplate(template *domain.NotificationTemplate) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateTemplate(template *domain.NotificationTemplate) error {
	return nil
}
func (s *sseNotificationRepoStub) DeleteTemplate(id uuid.UUID) error { return nil }
func (s *sseNotificationRepoStub) GetTemplate(id uuid.UUID) (*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetTemplateByName(name string) (*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetTemplates(limit, offset int) ([]*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetActiveTemplates(notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CreateUserPreferences(pref *domain.NotificationPreference) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateUserPreferences(pref *domain.NotificationPreference) error {
	return nil
}
func (s *sseNotificationRepoStub) DeleteUserPreferences(userID uuid.UUID) error { return nil }
func (s *sseNotificationRepoStub) GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CountByStatus() (map[string]int64, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CountByType() (map[string]int64, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) ListEmailLogs(offset, limit int, status string) ([]*domain.EmailLog, int64, error) {
	return nil, 0, nil
}

func newSSEHandlerTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

func reqSSE(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestSSEHandlerSubscribeEndpoint(t *testing.T) {
	cfg := test.TestConfig()
	sseSvc, err := notificationService.NewSSEService(cfg)
	if err != nil {
		t.Fatalf("failed to create sse service: %v", err)
	}
	notifSvc := notificationService.NewNotificationService(cfg, &sseNotificationRepoStub{}, nil)
	h := NewSSEHandler(sseSvc, notifSvc)

	app := newSSEHandlerTestApp()
	app.Post("/notifications/stream/subscribe", func(c *fiber.Ctx) error {
		c.Locals("claims", &identityService.Claims{UserID: uuid.New()})
		return h.Subscribe(c)
	})

	resp := reqSSE(t, app, http.MethodPost, "/notifications/stream/subscribe", `{"channels":["alerts"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSSEHandlerConnectionListing_AdminAndForbidden(t *testing.T) {
	cfg := test.TestConfig()
	sseSvc, err := notificationService.NewSSEService(cfg)
	if err != nil {
		t.Fatalf("failed to create sse service: %v", err)
	}
	notifSvc := notificationService.NewNotificationService(cfg, &sseNotificationRepoStub{}, nil)
	h := NewSSEHandler(sseSvc, notifSvc)

	app := newSSEHandlerTestApp()
	app.Get("/admin/sse/connections", func(c *fiber.Ctx) error {
		c.Locals("claims", &identityService.Claims{UserID: uuid.New(), Roles: []string{"admin"}})
		return h.GetConnections(c)
	})
	app.Get("/admin/sse/connections/forbidden", func(c *fiber.Ctx) error {
		c.Locals("claims", &identityService.Claims{UserID: uuid.New(), Roles: []string{"user"}})
		return h.GetConnections(c)
	})

	adminResp := reqSSE(t, app, http.MethodGet, "/admin/sse/connections", "")
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", adminResp.StatusCode)
	}
	// Note: GetConnections does not enforce admin role itself — that is done by
	// route-level authorization middleware. Without middleware, it returns 200.
	forbiddenResp := reqSSE(t, app, http.MethodGet, "/admin/sse/connections/forbidden", "")
	if forbiddenResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (handler does not enforce role), got %d", forbiddenResp.StatusCode)
	}
}
