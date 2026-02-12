package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/test"
)

func newAuthMiddlewareTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

func issueAccessToken(t *testing.T, ts *service.TokenService) string {
	t.Helper()

	u := test.CreateTestUserWithDefaults()
	u.Roles = []domain.Role{
		{
			Name: "admin",
			Permissions: []domain.Permission{
				{Name: "users:read"},
			},
		},
	}

	token, _, err := ts.GenerateAccessToken(u)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	return token
}

func doFiberRequest(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestMiddlewareHandle_MissingAuthorizationHeaderReturnsUnauthorized(t *testing.T) {
	cfg := test.TestConfig()
	ts := service.NewTokenService(cfg)
	mw := New(ts)

	app := newAuthMiddlewareTestApp()
	app.Use(mw.Handle)
	app.Get("/private", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	resp := doFiberRequest(t, app, req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestMiddlewareHandle_InvalidBearerFormatReturnsUnauthorized(t *testing.T) {
	cfg := test.TestConfig()
	ts := service.NewTokenService(cfg)
	mw := New(ts)

	app := newAuthMiddlewareTestApp()
	app.Use(mw.Handle)
	app.Get("/private", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Token only")
	resp := doFiberRequest(t, app, req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestMiddlewareHandle_ExpiredTokenReturnsUnauthorized(t *testing.T) {
	cfg := test.TestConfig()
	cfg.JWT.Expiry = -time.Second
	ts := service.NewTokenService(cfg)
	mw := New(ts)

	expiredToken := issueAccessToken(t, ts)

	app := newAuthMiddlewareTestApp()
	app.Use(mw.Handle)
	app.Get("/private", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken)
	resp := doFiberRequest(t, app, req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestMiddlewareHandle_ValidTokenWritesClaimsAndCallsNext(t *testing.T) {
	cfg := test.TestConfig()
	ts := service.NewTokenService(cfg)
	mw := New(ts)
	accessToken := issueAccessToken(t, ts)

	app := newAuthMiddlewareTestApp()
	app.Use(mw.Handle)
	app.Get("/private", func(c *fiber.Ctx) error {
		resp := fiber.Map{
			"user_id":      c.Locals("userID"),
			"username":     c.Locals("username"),
			"email":        c.Locals("email"),
			"roles":        c.Locals("roles"),
			"permissions":  c.Locals("permissions"),
			"claims_exist": c.Locals("claims") != nil,
		}
		return c.JSON(resp)
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp := doFiberRequest(t, app, req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["claims_exist"] != true {
		t.Fatalf("expected claims to be written into context")
	}
	if body["username"] != "testuser" {
		t.Fatalf("expected username testuser, got %#v", body["username"])
	}
	if body["email"] != "test@example.com" {
		t.Fatalf("expected email test@example.com, got %#v", body["email"])
	}
}

func TestRequireRoles_AllowsMatchingRoleAndRejectsMismatch(t *testing.T) {
	claims := &service.Claims{Roles: []string{"admin"}, Permissions: []string{"users:read"}}

	app := newAuthMiddlewareTestApp()
	app.Get("/allowed", func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	}, RequireRoles("admin"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/denied", func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	}, RequireRoles("support"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	allowedResp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, "/allowed", nil))
	if allowedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected allowed status 200, got %d", allowedResp.StatusCode)
	}

	deniedResp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, "/denied", nil))
	if deniedResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected denied status 403, got %d", deniedResp.StatusCode)
	}
}

func TestRequirePermissions_AllowsMatchingPermissionAndRejectsMismatch(t *testing.T) {
	claims := &service.Claims{Roles: []string{"admin"}, Permissions: []string{"users:read"}}

	app := newAuthMiddlewareTestApp()
	app.Get("/allowed", func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	}, RequirePermissions("users:read"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/denied", func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	}, RequirePermissions("users:write"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	allowedResp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, "/allowed", nil))
	if allowedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected allowed status 200, got %d", allowedResp.StatusCode)
	}

	deniedResp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, "/denied", nil))
	if deniedResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected denied status 403, got %d", deniedResp.StatusCode)
	}
}

func TestRequireAuth_AuthenticatedAndUnauthenticatedScenarios(t *testing.T) {
	app := newAuthMiddlewareTestApp()
	app.Get("/authenticated", func(c *fiber.Ctx) error {
		c.Locals("claims", &service.Claims{})
		return c.Next()
	}, RequireAuth(), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/unauthenticated", RequireAuth(), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	authResp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, "/authenticated", nil))
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", authResp.StatusCode)
	}

	unauthResp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, "/unauthenticated", nil))
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", unauthResp.StatusCode)
	}
}

func TestMiddlewareHandle_SkipPathsBypassAuthentication(t *testing.T) {
	cfg := test.TestConfig()
	ts := service.NewTokenService(cfg)
	mw := New(ts)

	app := newAuthMiddlewareTestApp()
	app.Use(mw.Handle)
	app.Get("/metrics", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/livez", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/api/v1/auth/login", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	for _, path := range []string{"/metrics", "/livez", "/api/v1/auth/login"} {
		resp := doFiberRequest(t, app, httptest.NewRequest(http.MethodGet, path, nil))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200 for skip path %s, got %d", path, resp.StatusCode)
		}
	}
}
