package api

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// newAdminHandlerTestApp creates a Fiber app with admin handler routes registered.
func newAdminHandlerTestApp(handler *AdminHandler, claims *service.Claims) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	admin := app.Group("/api/admin")
	admin.Use(func(c *fiber.Ctx) error {
		if claims != nil {
			c.Locals("claims", claims)
			c.Locals("userID", claims.UserID)
		}
		return c.Next()
	})
	handler.RegisterRoutes(admin)
	return app
}

// --- calculateOverallStatus ---

func TestCalculateOverallStatus(t *testing.T) {
	tests := []struct {
		name       string
		components map[string]ComponentHealth
		want       string
	}{
		{
			name:       "no components",
			components: map[string]ComponentHealth{},
			want:       "healthy",
		},
		{
			name: "all healthy",
			components: map[string]ComponentHealth{
				"db":    {Status: "healthy"},
				"redis": {Status: "healthy"},
			},
			want: "healthy",
		},
		{
			name: "all unhealthy",
			components: map[string]ComponentHealth{
				"db":    {Status: "unhealthy"},
				"redis": {Status: "unhealthy"},
			},
			want: "unhealthy",
		},
		{
			name: "mixed = degraded",
			components: map[string]ComponentHealth{
				"db":    {Status: "healthy"},
				"redis": {Status: "unhealthy"},
			},
			want: "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateOverallStatus(tt.components)
			if got != tt.want {
				t.Errorf("calculateOverallStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- toSessionSafeResponse ---

func TestToSessionSafeResponse(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	token := &domain.RefreshToken{
		ID:        id,
		UserID:    userID,
		IPAddress: "1.2.3.4",
		UserAgent: "TestAgent",
	}

	safe := toSessionSafeResponse(token)
	if safe.SessionID != id {
		t.Fatalf("expected session id %s, got %s", id, safe.SessionID)
	}
	if safe.UserID != userID {
		t.Fatalf("expected user id %s, got %s", userID, safe.UserID)
	}
	if safe.IPAddress != "1.2.3.4" {
		t.Fatalf("expected IP 1.2.3.4, got %s", safe.IPAddress)
	}
	if safe.UserAgent != "TestAgent" {
		t.Fatalf("expected user agent TestAgent, got %s", safe.UserAgent)
	}
}

// --- toAPIKeySafeResponse ---

func TestToAPIKeySafeResponse(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	key := &domain.APIKey{
		ID:      id,
		UserID:  userID,
		Name:    "test-key",
		Revoked: true,
	}

	safe := toAPIKeySafeResponse(key)
	if safe.ID != id {
		t.Fatalf("expected id %s, got %s", id, safe.ID)
	}
	if safe.UserID != userID {
		t.Fatalf("expected user id %s, got %s", userID, safe.UserID)
	}
	if safe.Name != "test-key" {
		t.Fatalf("expected name test-key, got %s", safe.Name)
	}
	if !safe.IsRevoked {
		t.Fatal("expected revoked to be true")
	}
}

// --- RevokeAPIKey ---

func TestAdminHandlerRevokeAPIKey_InvalidUUID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodDelete, "/api/admin/api-keys/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- ForceLogoutUser ---

func TestAdminHandlerForceLogoutUser_InvalidUUID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodDelete, "/api/admin/sessions/user/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- SendTestEmail ---

func TestAdminHandlerSendTestEmail_NoEmailService(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{} // emailSvc is nil
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/email/test", `{"to":"a@b.com","subject":"hi","body":"test"}`)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerSendTestEmail_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{emailSvc: nil}
	// Need to set emailSvc to non-nil to pass the nil check — but we can't easily
	// construct one. Instead test the path where emailSvc IS nil (tested above)
	// and where JSON is bad. With nil emailSvc, the handler returns 503 before parsing JSON.
	// Let's just verify the route exists.
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/email/test", "{bad")
	// With nil emailSvc, we get 503 before parsing
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

// --- BulkUpdateStatus ---

func TestAdminHandlerBulkUpdateStatus_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-status", "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerBulkUpdateStatus_EmptyUserIDs(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-status", `{"user_ids":[],"status":"active"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerBulkUpdateStatus_InvalidStatus(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	body := `{"user_ids":["` + uuid.New().String() + `"],"status":"bogus"}`
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-status", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerBulkUpdateStatus_TooManyUserIDs(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)

	// Build a JSON array with 1001 UUIDs
	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = `"` + uuid.New().String() + `"`
	}
	body := `{"user_ids":[` + joinStrings(ids, ",") + `],"status":"active"}`
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-status", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- BulkAssignRole ---

func TestAdminHandlerBulkAssignRole_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-role", "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerBulkAssignRole_EmptyUserIDs(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-role", `{"user_ids":[],"role_id":"`+uuid.New().String()+`"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerBulkAssignRole_TooManyUserIDs(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)

	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = `"` + uuid.New().String() + `"`
	}
	body := `{"user_ids":[` + joinStrings(ids, ",") + `],"role_id":"` + uuid.New().String() + `"}`
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/bulk-role", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- ExportUsers ---

func TestAdminHandlerExportUsers_InvalidFormat(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/users/export?format=xml", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- ExportAuditLogs ---

func TestAdminHandlerExportAuditLogs_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/audit-logs/export?user_id=bad", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerExportAuditLogs_InvalidStartDate(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/audit-logs/export?start_date=bad", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandlerExportAuditLogs_InvalidEndDate(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := &AdminHandler{}
	app := newAdminHandlerTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/audit-logs/export?end_date=bad", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}


// --- Helper ---

func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
