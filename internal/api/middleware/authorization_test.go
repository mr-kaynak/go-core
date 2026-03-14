package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	authmw "github.com/mr-kaynak/go-core/internal/middleware/auth"
)

// --- isUserOwnResource ---

func TestIsUserOwnResource(t *testing.T) {
	uid := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name   string
		path   string
		userID string
		want   bool
	}{
		{"exact /users/me", "/api/v1/users/me", uid, true},
		{"exact /users/me with trailing", "/api/v1/users/me/settings", uid, true},
		{"exact /users/{id}", "/api/v1/users/" + uid, uid, true},
		{"users/{id} with sub-resource", "/api/v1/users/" + uid + "/roles", uid, true},
		{"different user id", "/api/v1/users/other-id", uid, false},
		{"substring injection in query", "/api/v1/admin/reports?ref=/users/me", uid, false},
		{"substring injection in nested path", "/api/v1/posts/users/me/comments", uid, true}, // /users/me is a valid segment pair here
		{"no users segment", "/api/v1/roles/123", uid, false},
		{"empty path", "", uid, false},
		{"profile with userID prefix", "/api/v1/" + uid + "/profile", uid, true},
		{"profile without userID", "/api/v1/other/profile", uid, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUserOwnResource(tt.path, tt.userID)
			if got != tt.want {
				t.Errorf("isUserOwnResource(%q, %q) = %v, want %v", tt.path, tt.userID, got, tt.want)
			}
		})
	}
}

// --- isPublicEndpoint ---

func TestIsPublicEndpoint(t *testing.T) {
	// All entries from PublicPaths should be public
	for _, p := range authmw.PublicPaths {
		if !isPublicEndpoint(p) {
			t.Errorf("expected %q to be public", p)
		}
	}

	// Non-public paths
	nonPublic := []string{
		"/api/v1/users/me",
		"/api/v1/admin/dashboard",
		"/api/v1/auth/logout",
		"/random",
	}
	for _, p := range nonPublic {
		if isPublicEndpoint(p) {
			t.Errorf("expected %q to NOT be public", p)
		}
	}
}

// --- mapHTTPMethodToAction ---

func TestMapHTTPMethodToAction(t *testing.T) {
	tests := []struct {
		method string
		want   authorization.Action
	}{
		{"GET", authorization.ActionRead},
		{"POST", authorization.ActionCreate},
		{"PUT", authorization.ActionUpdate},
		{"PATCH", authorization.ActionUpdate},
		{"DELETE", authorization.ActionDelete},
		{"OPTIONS", authorization.ActionRead},
		{"HEAD", authorization.ActionRead},
		{"UNKNOWN", authorization.ActionManage}, // unknown methods require manage (deny by default)
		{"TRACE", authorization.ActionManage},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := mapHTTPMethodToAction(tt.method)
			if got != tt.want {
				t.Errorf("mapHTTPMethodToAction(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}


// --- RequireOwnership middleware ---

func TestRequireOwnership_OwnerAccess(t *testing.T) {
	uid := uuid.New()
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uid)
		return c.Next()
	})
	app.Get("/users/:user_id", RequireOwnership(nil), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/users/"+uid.String(), nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for owner, got %d", resp.StatusCode)
	}
}

func TestRequireOwnership_AdminBypass(t *testing.T) {
	uid := uuid.New()
	otherUID := uuid.New()
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uid)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	})
	app.Get("/users/:user_id", RequireOwnership(nil), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/users/"+otherUID.String(), nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", resp.StatusCode)
	}
}

func TestRequireOwnership_NonOwnerDenied(t *testing.T) {
	uid := uuid.New()
	otherUID := uuid.New()
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			return c.Status(fiber.StatusForbidden).SendString(err.Error())
		},
	})
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uid)
		c.Locals("roles", []string{"user"})
		return c.Next()
	})
	app.Get("/users/:user_id", RequireOwnership(nil), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/users/"+otherUID.String(), nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for non-owner")
	}
}

// --- AuthorizationMiddleware without auth context ---

func TestAuthorizationMiddleware_NoUserID(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).SendString(err.Error())
		},
	})
	app.Use(AuthorizationMiddleware(nil))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 401, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestAuthorizationMiddleware_PublicEndpointBypass(t *testing.T) {
	app := fiber.New()
	app.Use(AuthorizationMiddleware(nil))
	// Use a known public path from PublicPaths
	publicPath := authmw.PublicPaths[0]
	app.Get(publicPath, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, publicPath, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public endpoint, got %d", resp.StatusCode)
	}
}
