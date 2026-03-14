package api

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// newUserTestApp creates a Fiber app wired with user self-service routes.
// The auth middleware injects claims and userID from Locals so handlers can
// read them without a real JWT.
func newUserTestApp(handler *UserHandler, claims *service.Claims) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	api := app.Group("/api")
	authMw := func(c fiber.Ctx) error {
		if claims != nil {
			c.Locals("claims", claims)
			c.Locals("userID", claims.UserID)
		}
		return c.Next()
	}
	handler.RegisterSelfServiceRoutes(api, authMw, nil)
	return app
}

// newAdminUserTestApp creates a Fiber app wired with admin user routes on /api/admin.
func newAdminUserTestApp(handler *UserHandler, claims *service.Claims) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	admin := app.Group("/api/admin")
	admin.Use(func(c fiber.Ctx) error {
		if claims != nil {
			c.Locals("claims", claims)
			c.Locals("userID", claims.UserID)
		}
		return c.Next()
	})
	handler.RegisterAdminRoutes(admin)
	return app
}

// --- Self-Service: GetProfile ---

func TestUserHandlerGetProfile_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodGet, "/api/users/profile", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Self-Service: UpdateProfile ---

func TestUserHandlerUpdateProfile_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodPut, "/api/users/profile", `{"first_name":"A"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUserHandlerUpdateProfile_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/users/profile", "{invalid")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerUpdateProfile_ValidationError(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, claims)
	// phone field with invalid value should trigger validation
	resp := doRequest(t, app, http.MethodPut, "/api/users/profile", `{"phone":"not-a-phone"}`)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", resp.StatusCode, body)
	}
}

// --- Self-Service: DeleteAccount ---

func TestUserHandlerDeleteAccount_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodDelete, "/api/users/profile", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Self-Service: ChangePassword ---

func TestUserHandlerChangePassword_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodPut, "/api/users/change-password", `{"old_password":"old","new_password":"New1234!"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUserHandlerChangePassword_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/users/change-password", "{invalid")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerChangePassword_MissingFields(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/users/change-password", `{"old_password":""}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerChangePassword_WeakPassword(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/users/change-password", `{"old_password":"old","new_password":"123"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Self-Service: GetSessions ---

func TestUserHandlerGetSessions_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodGet, "/api/users/sessions", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Self-Service: RevokeAllSessions ---

func TestUserHandlerRevokeAllSessions_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodDelete, "/api/users/sessions", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Self-Service: RevokeSession ---

func TestUserHandlerRevokeSession_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodDelete, "/api/users/sessions/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUserHandlerRevokeSession_InvalidSessionID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodDelete, "/api/users/sessions/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Self-Service: GetMyAuditLogs ---

func TestUserHandlerGetMyAuditLogs_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodGet, "/api/users/audit-logs", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminGetUser ---

func TestUserHandlerAdminGetUser_InvalidUUID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/users/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminCreateUser ---

func TestUserHandlerAdminCreateUser_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users", "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminCreateUser_ValidationError(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users", `{"email":"bad","username":"!","password":"weak"}`)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", resp.StatusCode, body)
	}
}

// --- Admin: AdminUpdateUser ---

func TestUserHandlerAdminUpdateUser_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/admin/users/not-a-uuid", `{"first_name":"A"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminUpdateUser_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/admin/users/"+uuid.New().String(), "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminUpdateUser_ValidationError(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	// email with bad format
	resp := doRequest(t, app, http.MethodPut, "/api/admin/users/"+uuid.New().String(), `{"email":"not-an-email"}`)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", resp.StatusCode, body)
	}
}

// --- Admin: AdminDeleteUser ---

func TestUserHandlerAdminDeleteUser_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodDelete, "/api/admin/users/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminDeleteUser_NoClaims(t *testing.T) {
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, nil)
	resp := doRequest(t, app, http.MethodDelete, "/api/admin/users/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminUpdateStatus ---

func TestUserHandlerAdminUpdateStatus_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/admin/users/not-a-uuid/status", `{"status":"active"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminUpdateStatus_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/admin/users/"+uuid.New().String()+"/status", "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminUpdateStatus_InvalidStatus(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPut, "/api/admin/users/"+uuid.New().String()+"/status", `{"status":"invalid"}`)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", resp.StatusCode, body)
	}
}

// --- Admin: AdminAssignRole ---

func TestUserHandlerAdminAssignRole_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/not-a-uuid/roles", `{"role_id":"`+uuid.New().String()+`"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminAssignRole_InvalidJSON(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/"+uuid.New().String()+"/roles", "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminAssignRole_MissingRoleID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/"+uuid.New().String()+"/roles", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminRemoveRole ---

func TestUserHandlerAdminRemoveRole_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodDelete, "/api/admin/users/not-a-uuid/roles/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminRemoveRole_InvalidRoleID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodDelete, "/api/admin/users/"+uuid.New().String()+"/roles/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminUnlockUser ---

func TestUserHandlerAdminUnlockUser_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/not-a-uuid/unlock", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminResetPassword ---

func TestUserHandlerAdminResetPassword_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/not-a-uuid/reset-password", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminDisable2FA ---

func TestUserHandlerAdminDisable2FA_InvalidUserID(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodPost, "/api/admin/users/not-a-uuid/disable-2fa", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: AdminListAuditLogs ---

func TestUserHandlerAdminListAuditLogs_InvalidUserIDParam(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/audit-logs?user_id=not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminListAuditLogs_InvalidStartDate(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/audit-logs?start_date=invalid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUserHandlerAdminListAuditLogs_InvalidEndDate(t *testing.T) {
	claims := &service.Claims{UserID: uuid.New()}
	h := NewUserHandler(nil, nil)
	app := newAdminUserTestApp(h, claims)
	resp := doRequest(t, app, http.MethodGet, "/api/admin/audit-logs?end_date=invalid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin: SetAuditService ---

func TestUserHandlerSetAuditService(t *testing.T) {
	h := NewUserHandler(nil, nil)
	if h.auditService != nil {
		t.Fatal("expected nil audit service initially")
	}
	// Just exercise the setter path — nil-safe
	h.SetAuditService(nil)
	if h.auditService != nil {
		t.Fatal("expected nil audit service after setting nil")
	}
}
