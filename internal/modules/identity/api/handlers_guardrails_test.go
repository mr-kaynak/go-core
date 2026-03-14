package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

func newGuardrailApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
}

func doGuardrailReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestAPIKeyHandlerCreate_Unauthenticated(t *testing.T) {
	app := newGuardrailApp()
	h := NewAPIKeyHandler(nil)
	app.Post("/api-keys", h.CreateAPIKey)

	resp := doGuardrailReq(t, app, http.MethodPost, "/api-keys", `{}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAPIKeyHandlerCreate_InvalidJSON(t *testing.T) {
	app := newGuardrailApp()
	h := NewAPIKeyHandler(nil)
	app.Post("/api-keys", func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return h.CreateAPIKey(c)
	})

	resp := doGuardrailReq(t, app, http.MethodPost, "/api-keys", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAPIKeyHandlerRevoke_InvalidIDFormat(t *testing.T) {
	app := newGuardrailApp()
	h := NewAPIKeyHandler(nil)
	app.Delete("/api-keys/:id", func(c fiber.Ctx) error {
		c.Locals("userID", "not-uuid")
		return h.RevokeAPIKey(c)
	})

	resp := doGuardrailReq(t, app, http.MethodDelete, "/api-keys/not-a-uuid", ``)
	// wrong user local type is checked first.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTwoFactorHandlerEnable_MissingClaims(t *testing.T) {
	app := newGuardrailApp()
	h := NewTwoFactorHandler(nil)
	app.Post("/2fa/enable", h.Enable)

	resp := doGuardrailReq(t, app, http.MethodPost, "/2fa/enable", `{}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTwoFactorHandlerHandleAction_InvalidBody(t *testing.T) {
	app := newGuardrailApp()
	h := NewTwoFactorHandler(nil)
	app.Post("/2fa/action", func(c fiber.Ctx) error {
		// no claims: should fail before action invocation.
		return h.handle2FAAction(c, func(userID uuid.UUID, code string) error { return nil }, "ok", "test.action")
	})

	resp := doGuardrailReq(t, app, http.MethodPost, "/2fa/action", `{invalid`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTwoFactorHandlerHandleAction_EmptyCode(t *testing.T) {
	app := newGuardrailApp()
	h := NewTwoFactorHandler(nil)
	app.Post("/2fa/action", func(c fiber.Ctx) error {
		c.Locals("claims", &service.Claims{UserID: uuid.New()})
		return h.handle2FAAction(c, func(userID uuid.UUID, code string) error { return nil }, "ok", "test.action")
	})

	resp := doGuardrailReq(t, app, http.MethodPost, "/2fa/action", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerUserRole_InvalidUserID(t *testing.T) {
	app := newGuardrailApp()
	stub := &policyAuthorizerStub{}
	h := NewPolicyHandler(stub)
	app.Post("/policies/users/:user_id/roles", h.AddRoleToUser)

	resp := doGuardrailReq(t, app, http.MethodPost, "/policies/users/not-a-uuid/roles", `{"role":"admin"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerCheckPermission_InvalidJSON(t *testing.T) {
	app := newGuardrailApp()
	h := NewPolicyHandler(nil)
	app.Post("/policies/check", h.CheckPermission)

	resp := doGuardrailReq(t, app, http.MethodPost, "/policies/check", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerResourceGroup_InvalidJSON(t *testing.T) {
	app := newGuardrailApp()
	stub := &policyAuthorizerStub{}
	h := NewPolicyHandler(stub)
	app.Post("/policies/resource-groups", h.AddResourceGroup)

	resp := doGuardrailReq(t, app, http.MethodPost, "/policies/resource-groups", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerAddPolicy_InvalidJSON(t *testing.T) {
	app := newGuardrailApp()
	h := NewPolicyHandler(nil)
	app.Post("/policies", h.AddPolicy)

	resp := doGuardrailReq(t, app, http.MethodPost, "/policies", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerRemovePolicy_InvalidJSON(t *testing.T) {
	app := newGuardrailApp()
	h := NewPolicyHandler(nil)
	app.Delete("/policies", h.RemovePolicy)

	resp := doGuardrailReq(t, app, http.MethodDelete, "/policies", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerHandleUserRole_DefaultDomain(t *testing.T) {
	app := newGuardrailApp()
	h := NewPolicyHandler(nil)
	called := false
	app.Post("/users/:user_id/roles", func(c fiber.Ctx) error {
		return h.handleUserRole(c, func(userID uuid.UUID, role, domain string) error {
			called = true
			if domain != authorization.DomainDefault {
				t.Fatalf("expected default domain, got %q", domain)
			}
			if role != "admin" {
				t.Fatalf("expected role admin, got %q", role)
			}
			return nil
		}, "ok", "test.action")
	})

	resp := doGuardrailReq(t, app, http.MethodPost, "/users/"+uuid.NewString()+"/roles", `{"role":"admin"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatalf("expected action callback to be called")
	}
}

func TestPolicyHandlerHandleResourceGroup_DefaultDomain(t *testing.T) {
	app := newGuardrailApp()
	h := NewPolicyHandler(nil)
	called := false
	app.Post("/resource-groups", func(c fiber.Ctx) error {
		return h.handleResourceGroup(c, func(resource, group, domain string) error {
			called = true
			if domain != authorization.DomainDefault {
				t.Fatalf("expected default domain, got %q", domain)
			}
			return nil
		}, "ok")
	})

	resp := doGuardrailReq(t, app, http.MethodPost, "/resource-groups", `{"resource":"orders","group":"ops"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatalf("expected action callback to be called")
	}
}

func TestGetTokenFromHeader_InvalidFormat(t *testing.T) {
	app := newGuardrailApp()
	app.Get("/token", func(c fiber.Ctx) error {
		_, err := GetTokenFromHeader(c)
		return err
	})

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	req.Header.Set("Authorization", "Token abc")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
