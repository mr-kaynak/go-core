package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
)

type policyAuthorizerStub struct {
	addPolicyFn         func(subject, domain, object string, action authorization.Action, effect string) error
	removePolicyFn      func(subject, domain, object string, action authorization.Action, effect string) error
	addRoleForUserFn    func(userID uuid.UUID, role, domain string) error
	removeRoleForUserFn func(userID uuid.UUID, role, domain string) error
	getRolesForUserFn   func(userID uuid.UUID, domain string) ([]string, error)
	getPermsForUserFn   func(userID uuid.UUID, domain string) ([][]string, error)
	getUsersForRoleFn   func(role, domain string) ([]string, error)
	addResourceGroupFn  func(resource, group, domain string) error
	removeResourceFn    func(resource, group, domain string) error
	enforceFn           func(subject, domain, object string, action authorization.Action) (bool, error)
	reloadFn            func() error
	saveFn              func() error
}

func (s *policyAuthorizerStub) AddPolicy(subject, domain, object string, action authorization.Action, effect string) error {
	if s.addPolicyFn != nil {
		return s.addPolicyFn(subject, domain, object, action, effect)
	}
	return nil
}

func (s *policyAuthorizerStub) RemovePolicy(subject, domain, object string, action authorization.Action, effect string) error {
	if s.removePolicyFn != nil {
		return s.removePolicyFn(subject, domain, object, action, effect)
	}
	return nil
}

func (s *policyAuthorizerStub) AddRoleForUser(userID uuid.UUID, role, domain string) error {
	if s.addRoleForUserFn != nil {
		return s.addRoleForUserFn(userID, role, domain)
	}
	return nil
}

func (s *policyAuthorizerStub) RemoveRoleForUser(userID uuid.UUID, role, domain string) error {
	if s.removeRoleForUserFn != nil {
		return s.removeRoleForUserFn(userID, role, domain)
	}
	return nil
}

func (s *policyAuthorizerStub) GetRolesForUser(userID uuid.UUID, domain string) ([]string, error) {
	if s.getRolesForUserFn != nil {
		return s.getRolesForUserFn(userID, domain)
	}
	return nil, nil
}

func (s *policyAuthorizerStub) GetPermissionsForUser(userID uuid.UUID, domain string) ([][]string, error) {
	if s.getPermsForUserFn != nil {
		return s.getPermsForUserFn(userID, domain)
	}
	return nil, nil
}

func (s *policyAuthorizerStub) GetUsersForRole(role, domain string) ([]string, error) {
	if s.getUsersForRoleFn != nil {
		return s.getUsersForRoleFn(role, domain)
	}
	return nil, nil
}

func (s *policyAuthorizerStub) AddResourceGroup(resource, group, domain string) error {
	if s.addResourceGroupFn != nil {
		return s.addResourceGroupFn(resource, group, domain)
	}
	return nil
}

func (s *policyAuthorizerStub) RemoveResourceGroup(resource, group, domain string) error {
	if s.removeResourceFn != nil {
		return s.removeResourceFn(resource, group, domain)
	}
	return nil
}

func (s *policyAuthorizerStub) Enforce(subject, domain, object string, action authorization.Action) (bool, error) {
	if s.enforceFn != nil {
		return s.enforceFn(subject, domain, object, action)
	}
	return false, nil
}

func (s *policyAuthorizerStub) ReloadPolicy() error {
	if s.reloadFn != nil {
		return s.reloadFn()
	}
	return nil
}

func (s *policyAuthorizerStub) SavePolicy() error {
	if s.saveFn != nil {
		return s.saveFn()
	}
	return nil
}

func newPolicyTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

func doPolicyReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func decodeJSONBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return data
}

func TestPolicyHandlerAddAndRemovePolicy(t *testing.T) {
	stub := &policyAuthorizerStub{}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Post("/policies", h.AddPolicy)
	app.Delete("/policies", h.RemovePolicy)

	t.Run("AddPolicy", func(t *testing.T) {
		resp := doPolicyReq(
			t, app, http.MethodPost, "/policies",
			`{"subject":"role:admin","domain":"default","object":"/api/users/*","action":"read","effect":"allow"}`,
		)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("RemovePolicy", func(t *testing.T) {
		resp := doPolicyReq(
			t, app, http.MethodDelete, "/policies",
			`{"subject":"role:admin","domain":"default","object":"/api/users/*","action":"read","effect":"allow"}`,
		)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestPolicyHandlerAddPolicy_InvalidPayloadReturnsBadRequest(t *testing.T) {
	h := NewPolicyHandler(&policyAuthorizerStub{})
	app := newPolicyTestApp()
	app.Post("/policies", h.AddPolicy)

	resp := doPolicyReq(t, app, http.MethodPost, "/policies", "{invalid")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestPolicyHandlerRoleOperations_ExistingAndMissingRole(t *testing.T) {
	userID := uuid.New()
	stub := &policyAuthorizerStub{
		addRoleForUserFn: func(id uuid.UUID, role, domain string) error {
			if id != userID || role != "admin" || domain != authorization.DomainDefault {
				t.Fatalf("unexpected role assignment request")
			}
			return nil
		},
		removeRoleForUserFn: func(id uuid.UUID, role, domain string) error {
			if role == "ghost" {
				return coreerrors.NewNotFound("role", role)
			}
			return nil
		},
	}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Post("/policies/users/:user_id/roles", h.AddRoleToUser)
	app.Delete("/policies/users/:user_id/roles", h.RemoveRoleFromUser)

	t.Run("AddRoleSuccess", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodPost,
			"/policies/users/"+userID.String()+"/roles", `{"role":"admin"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("RemoveMissingRoleReturns404", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodDelete,
			"/policies/users/"+userID.String()+"/roles", `{"role":"ghost"}`)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", resp.StatusCode)
		}
	})
}

func TestPolicyHandlerGetUserRolesAndPermissions(t *testing.T) {
	userID := uuid.New()
	stub := &policyAuthorizerStub{
		getRolesForUserFn: func(id uuid.UUID, domain string) ([]string, error) {
			if id != userID || domain != authorization.DomainDefault {
				t.Fatalf("unexpected get roles request")
			}
			return []string{"admin", "support"}, nil
		},
		getPermsForUserFn: func(id uuid.UUID, domain string) ([][]string, error) {
			if id != userID || domain != authorization.DomainDefault {
				t.Fatalf("unexpected get permissions request")
			}
			return [][]string{
				{"role:admin", "default", "/api/users/*", "read", "allow"},
			}, nil
		},
	}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Get("/policies/users/:user_id/roles", h.GetUserRoles)
	app.Get("/policies/users/:user_id/permissions", h.GetUserPermissions)

	t.Run("GetRoles", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodGet, "/policies/users/"+userID.String()+"/roles", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		roles, ok := body["roles"].([]interface{})
		if !ok || len(roles) != 2 {
			t.Fatalf("expected two roles in response, got %#v", body["roles"])
		}
	})

	t.Run("GetPermissions", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodGet, "/policies/users/"+userID.String()+"/permissions", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		perms, ok := body["permissions"].([]interface{})
		if !ok || len(perms) != 1 {
			t.Fatalf("expected one permission in response, got %#v", body["permissions"])
		}
	})
}

func TestPolicyHandlerCheckPermissionAllowAndDeny(t *testing.T) {
	stub := &policyAuthorizerStub{
		enforceFn: func(subject, domain, object string, action authorization.Action) (bool, error) {
			return subject == "role:admin", nil
		},
	}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Post("/policies/check", h.CheckPermission)

	t.Run("Allowed", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodPost, "/policies/check",
			`{"subject":"role:admin","object":"/api/users/1","action":"read"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		if body["allowed"] != true {
			t.Fatalf("expected allowed=true, got %#v", body["allowed"])
		}
	})

	t.Run("Denied", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodPost, "/policies/check",
			`{"subject":"role:viewer","object":"/api/users/1","action":"read"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		if body["allowed"] != false {
			t.Fatalf("expected allowed=false, got %#v", body["allowed"])
		}
	})
}

func TestPolicyHandlerBulkAddPolicies_WithPartialFailure(t *testing.T) {
	stub := &policyAuthorizerStub{
		addPolicyFn: func(subject, domain, object string, action authorization.Action, effect string) error {
			if subject == "role:bad" {
				return errors.New("failed to add policy")
			}
			return nil
		},
	}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Post("/policies/bulk", h.BulkAddPolicies)

	resp := doPolicyReq(
		t,
		app,
		http.MethodPost,
		"/policies/bulk",
		`{"policies":[{"subject":"role:ok","domain":"default","object":"/api/a","action":"read","effect":"allow"},{"subject":"role:bad","domain":"default","object":"/api/b","action":"read","effect":"allow"}]}`,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["success"] != float64(1) || body["failed"] != float64(1) {
		t.Fatalf("expected success=1 and failed=1, got %#v", body)
	}
}

func TestPolicyHandlerReloadAndSavePolicies_SuccessAndFailure(t *testing.T) {
	t.Run("Failure", func(t *testing.T) {
		failStub := &policyAuthorizerStub{
			reloadFn: func() error { return coreerrors.NewInternalError("reload failed") },
			saveFn:   func() error { return coreerrors.NewInternalError("save failed") },
		}
		h := NewPolicyHandler(failStub)
		app := newPolicyTestApp()
		app.Get("/policies/reload", h.ReloadPolicies)
		app.Post("/policies/save", h.SavePolicies)

		reloadResp := doPolicyReq(t, app, http.MethodGet, "/policies/reload", "")
		if reloadResp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", reloadResp.StatusCode)
		}
		saveResp := doPolicyReq(t, app, http.MethodPost, "/policies/save", "")
		if saveResp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", saveResp.StatusCode)
		}
	})

	t.Run("Success", func(t *testing.T) {
		h := NewPolicyHandler(&policyAuthorizerStub{})
		app := newPolicyTestApp()
		app.Get("/policies/reload", h.ReloadPolicies)
		app.Post("/policies/save", h.SavePolicies)

		reloadResp := doPolicyReq(t, app, http.MethodGet, "/policies/reload", "")
		if reloadResp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", reloadResp.StatusCode)
		}
		saveResp := doPolicyReq(t, app, http.MethodPost, "/policies/save", "")
		if saveResp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", saveResp.StatusCode)
		}
	})
}

func TestPolicyHandlerGetUsersForRole(t *testing.T) {
	stub := &policyAuthorizerStub{
		getUsersForRoleFn: func(role, domain string) ([]string, error) {
			if role == "admin" && domain == authorization.DomainDefault {
				return []string{"user-1", "user-2"}, nil
			}
			return nil, nil
		},
	}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Get("/policies/roles/:role/users", h.GetUsersForRole)

	t.Run("ReturnsUsersForRole", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodGet, "/policies/roles/admin/users", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		users, ok := body["users"].([]interface{})
		if !ok || len(users) != 2 {
			t.Fatalf("expected 2 users, got %#v", body["users"])
		}
		if body["role"] != "admin" {
			t.Fatalf("expected role=admin, got %v", body["role"])
		}
	})

	t.Run("EmptyRoleReturnsEmptyList", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodGet, "/policies/roles/unknown/users", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestPolicyHandlerResourceGroupOperations(t *testing.T) {
	var addedResource, addedGroup, addedDomain string
	stub := &policyAuthorizerStub{
		addResourceGroupFn: func(resource, group, domain string) error {
			addedResource = resource
			addedGroup = group
			addedDomain = domain
			return nil
		},
		removeResourceFn: func(resource, group, domain string) error {
			if resource == "missing" {
				return coreerrors.NewNotFound("resource", "not in group")
			}
			return nil
		},
	}
	h := NewPolicyHandler(stub)
	app := newPolicyTestApp()
	app.Post("/policies/resource-groups", h.AddResourceGroup)
	app.Delete("/policies/resource-groups", h.RemoveResourceGroup)

	t.Run("AddResourceGroup", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodPost, "/policies/resource-groups",
			`{"resource":"/api/items/1","group":"/api/items/*","domain":"default"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		if addedResource != "/api/items/1" || addedGroup != "/api/items/*" || addedDomain != "default" {
			t.Fatalf("unexpected resource group params: %s %s %s", addedResource, addedGroup, addedDomain)
		}
	})

	t.Run("AddResourceGroupDefaultDomain", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodPost, "/policies/resource-groups",
			`{"resource":"/api/x/1","group":"/api/x/*"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		if addedDomain != authorization.DomainDefault {
			t.Fatalf("expected default domain, got %s", addedDomain)
		}
	})

	t.Run("RemoveResourceGroupSuccess", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodDelete, "/policies/resource-groups",
			`{"resource":"/api/items/1","group":"/api/items/*"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("RemoveMissingResourceReturns404", func(t *testing.T) {
		resp := doPolicyReq(t, app, http.MethodDelete, "/policies/resource-groups",
			`{"resource":"missing","group":"/api/items/*"}`)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", resp.StatusCode)
		}
	})
}

func TestPolicyHandlerNilService(t *testing.T) {
	h := NewPolicyHandler(nil)
	app := newPolicyTestApp()
	h.RegisterRoutes(app.Group(""))

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/policies/", `{"subject":"x","domain":"d","object":"o","action":"r","effect":"allow"}`},
		{http.MethodDelete, "/policies/", `{"subject":"x","domain":"d","object":"o","action":"r","effect":"allow"}`},
		{http.MethodPost, "/policies/users/00000000-0000-0000-0000-000000000001/roles", `{"role":"admin"}`},
		{http.MethodDelete, "/policies/users/00000000-0000-0000-0000-000000000001/roles", `{"role":"admin"}`},
		{http.MethodGet, "/policies/users/00000000-0000-0000-0000-000000000001/roles", ""},
		{http.MethodGet, "/policies/users/00000000-0000-0000-0000-000000000001/permissions", ""},
		{http.MethodGet, "/policies/roles/admin/users", ""},
		{http.MethodPost, "/policies/resource-groups", `{"resource":"r","group":"g"}`},
		{http.MethodDelete, "/policies/resource-groups", `{"resource":"r","group":"g"}`},
		{http.MethodPost, "/policies/check", `{"subject":"x","object":"o","action":"r"}`},
		{http.MethodGet, "/policies/reload", ""},
		{http.MethodPost, "/policies/save", ""},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			resp := doPolicyReq(t, app, ep.method, ep.path, ep.body)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected status 500 for nil service on %s %s, got %d",
					ep.method, ep.path, resp.StatusCode)
			}
		})
	}
}
