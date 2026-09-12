package api

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestManagementRoutesRequireBothSecurityMiddleware(t *testing.T) {
	pass := func(c fiber.Ctx) error { return c.Next() }
	for _, middleware := range []struct {
		name        string
		auth, authz fiber.Handler
	}{{"missing_auth", nil, pass}, {"missing_authz", pass, nil}, {"missing_both", nil, nil}} {
		t.Run(middleware.name, func(t *testing.T) {
			h := newTestPermissionHandler(&permRepoStub{})
			a := fiber.New()
			h.RegisterRoutes(a, middleware.auth, middleware.authz)
			NewRoleHandler(nil).RegisterRoutes(a, middleware.auth, middleware.authz)
			for _, path := range []string{"/permissions", "/roles", "/roles/any/permissions"} {
				for _, method := range []string{http.MethodGet, http.MethodPost} {
					response := permReq(t, a, method, path, "")
					if response.StatusCode != http.StatusNotFound {
						t.Fatalf("%s %s: expected unregistered route (404), got %d", method, path, response.StatusCode)
					}
				}
			}
		})
	}
}
