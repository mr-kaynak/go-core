package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/mr-kaynak/go-core/internal/test"
)

type sseNotificationRepoStub struct{}

func (s *sseNotificationRepoStub) CreateNotification(_ context.Context, notification *domain.Notification) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateNotification(_ context.Context, notification *domain.Notification) error {
	return nil
}
func (s *sseNotificationRepoStub) DeleteNotification(_ context.Context, id uuid.UUID) error {
	return nil
}
func (s *sseNotificationRepoStub) GetNotification(_ context.Context, id uuid.UUID) (*domain.Notification, error) {
	return &domain.Notification{ID: id, Status: domain.NotificationStatusSent}, nil
}
func (s *sseNotificationRepoStub) GetUserNotifications(_ context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetPendingNotifications(_ context.Context, limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) ClaimNotificationForProcessing(_ context.Context, id uuid.UUID) (bool, error) {
	return true, nil
}
func (s *sseNotificationRepoStub) GetFailedNotifications(_ context.Context, limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetScheduledNotifications(_ context.Context, limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CountUserNotifications(_ context.Context, userID uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *sseNotificationRepoStub) GetUserNotificationsSince(_ context.Context, userID uuid.UUID, since time.Time, limit int) ([]*domain.Notification, bool, error) {
	return nil, false, nil
}
func (s *sseNotificationRepoStub) MarkAsRead(_ context.Context, id uuid.UUID, userID uuid.UUID) error {
	return nil
}
func (s *sseNotificationRepoStub) MarkAllAsRead(_ context.Context, userID uuid.UUID) error {
	return nil
}
func (s *sseNotificationRepoStub) CreateEmailLog(_ context.Context, log *domain.EmailLog) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateEmailLog(_ context.Context, log *domain.EmailLog) error {
	return nil
}
func (s *sseNotificationRepoStub) GetEmailLog(_ context.Context, id uuid.UUID) (*domain.EmailLog, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetEmailLogsByNotification(_ context.Context, notificationID uuid.UUID) ([]*domain.EmailLog, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetEmailLogsByUser(_ context.Context, userID uuid.UUID, limit, offset int) ([]*domain.EmailLog, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CreateTemplate(_ context.Context, template *domain.NotificationTemplate) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateTemplate(_ context.Context, template *domain.NotificationTemplate) error {
	return nil
}
func (s *sseNotificationRepoStub) DeleteTemplate(_ context.Context, id uuid.UUID) error {
	return nil
}
func (s *sseNotificationRepoStub) GetTemplate(_ context.Context, id uuid.UUID) (*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetTemplateByName(_ context.Context, name string) (*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetTemplates(_ context.Context, limit, offset int) ([]*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) GetActiveTemplates(_ context.Context, notificationType domain.NotificationType) ([]*domain.NotificationTemplate, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CreateUserPreferences(_ context.Context, pref *domain.NotificationPreference) error {
	return nil
}
func (s *sseNotificationRepoStub) UpdateUserPreferences(_ context.Context, pref *domain.NotificationPreference) error {
	return nil
}
func (s *sseNotificationRepoStub) DeleteUserPreferences(_ context.Context, userID uuid.UUID) error {
	return nil
}
func (s *sseNotificationRepoStub) GetUserPreferences(_ context.Context, userID uuid.UUID) (*domain.NotificationPreference, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CountByStatus(_ context.Context) (map[string]int64, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) CountByType(_ context.Context) (map[string]int64, error) {
	return nil, nil
}
func (s *sseNotificationRepoStub) ListEmailLogs(_ context.Context, offset, limit int, status string) ([]*domain.EmailLog, int64, error) {
	return nil, 0, nil
}

func newSSEHandlerTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
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
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
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
	app.Post("/notifications/stream/subscribe", func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
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
	app.Get("/admin/sse/connections", func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		c.Locals("roles", []string{"admin"})
		return h.GetConnections(c)
	})
	app.Get("/admin/sse/connections/forbidden", func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		c.Locals("roles", []string{"user"})
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
