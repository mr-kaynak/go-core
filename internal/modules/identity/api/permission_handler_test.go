package api

import (
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

type permRepoStub struct {
	getAllFn               func(offset, limit int) ([]domain.Permission, error)
	getByCategoryFn        func(category string) ([]domain.Permission, error)
	countFn                func() (int64, error)
	getByIDFn              func(id uuid.UUID) (*domain.Permission, error)
	updateFn               func(permission *domain.Permission) error
	deleteFn               func(id uuid.UUID) error
	addPermissionToRoleFn  func(roleID, permissionID uuid.UUID) error
	removePermissionRoleFn func(roleID, permissionID uuid.UUID) error
	getRolePermissionsFn   func(roleID uuid.UUID) ([]domain.Permission, error)
	getByNameFn            func(name string) (*domain.Permission, error)
	getUserPermissionsFn   func(userID uuid.UUID) ([]domain.Permission, error)
	createFn               func(permission *domain.Permission) error
}

var _ repository.PermissionRepository = (*permRepoStub)(nil)

func (s *permRepoStub) Create(permission *domain.Permission) error {
	if s.createFn != nil {
		return s.createFn(permission)
	}
	return nil
}

func (s *permRepoStub) GetByID(id uuid.UUID) (*domain.Permission, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

func (s *permRepoStub) GetByName(name string) (*domain.Permission, error) {
	if s.getByNameFn != nil {
		return s.getByNameFn(name)
	}
	return nil, nil
}

func (s *permRepoStub) GetAll(offset, limit int) ([]domain.Permission, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return nil, nil
}

func (s *permRepoStub) GetByCategory(category string) ([]domain.Permission, error) {
	if s.getByCategoryFn != nil {
		return s.getByCategoryFn(category)
	}
	return nil, nil
}

func (s *permRepoStub) Count() (int64, error) {
	if s.countFn != nil {
		return s.countFn()
	}
	return 0, nil
}

func (s *permRepoStub) Update(permission *domain.Permission) error {
	if s.updateFn != nil {
		return s.updateFn(permission)
	}
	return nil
}

func (s *permRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}

func (s *permRepoStub) AddPermissionToRole(roleID, permissionID uuid.UUID) error {
	if s.addPermissionToRoleFn != nil {
		return s.addPermissionToRoleFn(roleID, permissionID)
	}
	return nil
}

func (s *permRepoStub) RemovePermissionFromRole(roleID, permissionID uuid.UUID) error {
	if s.removePermissionRoleFn != nil {
		return s.removePermissionRoleFn(roleID, permissionID)
	}
	return nil
}

func (s *permRepoStub) GetRolePermissions(roleID uuid.UUID) ([]domain.Permission, error) {
	if s.getRolePermissionsFn != nil {
		return s.getRolePermissionsFn(roleID)
	}
	return nil, nil
}

func (s *permRepoStub) GetUserPermissions(userID uuid.UUID) ([]domain.Permission, error) {
	if s.getUserPermissionsFn != nil {
		return s.getUserPermissionsFn(userID)
	}
	return nil, nil
}

// roleRepoStub implements repository.RoleRepository for tests.
type roleRepoStub struct {
	getByIDFn func(id uuid.UUID) (*domain.Role, error)
}

var _ repository.RoleRepository = (*roleRepoStub)(nil)

func (s *roleRepoStub) Create(_ *domain.Role) error              { return nil }
func (s *roleRepoStub) GetByName(_ string) (*domain.Role, error) { return nil, nil }
func (s *roleRepoStub) GetAll(_, _ int) ([]domain.Role, error)   { return nil, nil }
func (s *roleRepoStub) Count() (int64, error)                    { return 0, nil }
func (s *roleRepoStub) Update(_ *domain.Role) error              { return nil }
func (s *roleRepoStub) Delete(_ uuid.UUID) error                 { return nil }
func (s *roleRepoStub) GetByID(id uuid.UUID) (*domain.Role, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

// newTestPermissionHandler creates a PermissionHandler with nil roleRepo and casbinService (sufficient for most tests).
func newTestPermissionHandler(repo *permRepoStub) *PermissionHandler {
	return NewPermissionHandler(repo, nil, nil)
}

func newPermissionTestApp(h *PermissionHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
	return app
}

func permReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestPermissionHandlerGetPermission_InvalidID(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Get("/permissions/:id", h.GetPermission)

	resp := permReq(t, app, http.MethodGet, "/permissions/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_InvalidBody(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", "{invalid")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_InvalidPayload(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", `{"name":"a","category":"x"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_PlaceholderSuccess(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", `{"name":"users.read","category":"users","description":"read users"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerListPermissions_NormalizesPagination(t *testing.T) {
	capturedOffset := -1
	capturedLimit := -1
	h := newTestPermissionHandler(&permRepoStub{
		getAllFn: func(offset, limit int) ([]domain.Permission, error) {
			capturedOffset = offset
			capturedLimit = limit
			return []domain.Permission{}, nil
		},
		countFn: func() (int64, error) { return 0, nil },
	})
	app := newPermissionTestApp(h)
	app.Get("/permissions", h.ListPermissions)

	resp := permReq(t, app, http.MethodGet, "/permissions?page=-1&limit=500", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedOffset != 0 || capturedLimit != 100 {
		t.Fatalf("expected normalized pagination offset=0 limit=100, got offset=%d limit=%d", capturedOffset, capturedLimit)
	}
}

func TestPermissionHandlerListPermissions_CountFailure(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{
		getAllFn: func(offset, limit int) ([]domain.Permission, error) {
			return []domain.Permission{}, nil
		},
		countFn: func() (int64, error) {
			return 0, stderrors.New("count failed")
		},
	})
	app := newPermissionTestApp(h)
	app.Get("/permissions", h.ListPermissions)

	resp := permReq(t, app, http.MethodGet, "/permissions", "")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerDeletePermission_InvalidID(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Delete("/permissions/:id", h.DeletePermission)

	resp := permReq(t, app, http.MethodDelete, "/permissions/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerGetRolePermissions_InvalidRoleID(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Get("/roles/:id/permissions", h.GetRolePermissions)

	resp := permReq(t, app, http.MethodGet, "/roles/not-uuid/permissions", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerUpdatePermission_InvalidID(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Put("/permissions/:id", h.UpdatePermission)

	resp := permReq(t, app, http.MethodPut, "/permissions/not-uuid", `{"name":"users.read"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerUpdatePermission_InvalidBody(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Put("/permissions/:id", h.UpdatePermission)

	resp := permReq(t, app, http.MethodPut, "/permissions/"+uuid.NewString(), `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerAddPermissionToRole_InvalidBody(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	resp := permReq(t, app, http.MethodPost, "/roles/"+uuid.NewString()+"/permissions", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerAddPermissionToRole_NotFoundPermission(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Permission, error) {
			return nil, stderrors.New("missing")
		},
	})
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	resp := permReq(t, app, http.MethodPost, "/roles/"+uuid.NewString()+"/permissions", `{"permission_id":"`+uuid.NewString()+`"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerListPermissions_ByCategory(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{
		getByCategoryFn: func(category string) ([]domain.Permission, error) {
			return []domain.Permission{{ID: uuid.New(), Name: "users.read", Category: category}}, nil
		},
	})
	app := newPermissionTestApp(h)
	app.Get("/permissions", h.ListPermissions)

	resp := permReq(t, app, http.MethodGet, "/permissions?category=users", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerAddPermissionToRole_InvalidRoleID(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	resp := permReq(t, app, http.MethodPost, "/roles/not-uuid/permissions", `{"permission_id":"`+uuid.NewString()+`"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerRemovePermissionFromRole_InvalidPermissionID(t *testing.T) {
	h := newTestPermissionHandler(&permRepoStub{})
	app := newPermissionTestApp(h)
	app.Delete("/roles/:id/permissions/:permission_id", h.RemovePermissionFromRole)

	resp := permReq(t, app, http.MethodDelete, "/roles/"+uuid.NewString()+"/permissions/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerUpdatePermission_Success(t *testing.T) {
	permID := uuid.New()
	repo := &permRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Permission, error) {
			return &domain.Permission{
				ID:          permID,
				Name:        "users.read",
				Description: "old",
				Category:    "users",
			}, nil
		},
		updateFn: func(permission *domain.Permission) error { return nil },
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Put("/permissions/:id", h.UpdatePermission)

	resp := permReq(t, app, http.MethodPut, "/permissions/"+permID.String(), `{"description":"new desc"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerDeletePermission_Success(t *testing.T) {
	repo := &permRepoStub{
		deleteFn: func(id uuid.UUID) error { return nil },
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Delete("/permissions/:id", h.DeletePermission)

	resp := permReq(t, app, http.MethodDelete, "/permissions/"+uuid.NewString(), "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerAddPermissionToRole_Success(t *testing.T) {
	roleID := uuid.New()
	permID := uuid.New()
	repo := &permRepoStub{
		getByIDFn:             func(id uuid.UUID) (*domain.Permission, error) { return &domain.Permission{ID: permID}, nil },
		addPermissionToRoleFn: func(gotRoleID, gotPermID uuid.UUID) error { return nil },
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	resp := permReq(t, app, http.MethodPost, "/roles/"+roleID.String()+"/permissions", `{"permission_id":"`+permID.String()+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerRemovePermissionFromRole_Success(t *testing.T) {
	roleID := uuid.New()
	permID := uuid.New()
	repo := &permRepoStub{
		removePermissionRoleFn: func(gotRoleID, gotPermID uuid.UUID) error { return nil },
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Delete("/roles/:id/permissions/:permission_id", h.RemovePermissionFromRole)

	resp := permReq(t, app, http.MethodDelete, "/roles/"+roleID.String()+"/permissions/"+permID.String(), "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_Conflict(t *testing.T) {
	repo := &permRepoStub{
		getByNameFn: func(name string) (*domain.Permission, error) {
			return &domain.Permission{ID: uuid.New(), Name: name, Category: "test"}, nil
		},
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", `{"name":"users.read","category":"users"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_GetByNameDBError(t *testing.T) {
	repo := &permRepoStub{
		getByNameFn: func(name string) (*domain.Permission, error) {
			return nil, stderrors.New("db connection lost")
		},
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", `{"name":"users.read","category":"users"}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_CreateDBError(t *testing.T) {
	repo := &permRepoStub{
		getByNameFn: func(name string) (*domain.Permission, error) {
			return nil, coreerrors.NewNotFound("Permission", name)
		},
		createFn: func(permission *domain.Permission) error {
			return coreerrors.NewInternalError("db write failed")
		},
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", `{"name":"users.read","category":"users"}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestPermissionHandlerCreatePermission_Success(t *testing.T) {
	var createdPerm *domain.Permission
	repo := &permRepoStub{
		getByNameFn: func(name string) (*domain.Permission, error) {
			return nil, coreerrors.NewNotFound("Permission", name)
		},
		createFn: func(permission *domain.Permission) error {
			createdPerm = permission
			return nil
		},
	}
	h := newTestPermissionHandler(repo)
	app := newPermissionTestApp(h)
	app.Post("/permissions", h.CreatePermission)

	resp := permReq(t, app, http.MethodPost, "/permissions", `{"name":"users.read","category":"users","description":"read users"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if createdPerm == nil {
		t.Fatal("expected Create to be called")
	}
	if createdPerm.Name != "users.read" {
		t.Fatalf("expected name 'users.read', got '%s'", createdPerm.Name)
	}
	if createdPerm.Category != "users" {
		t.Fatalf("expected category 'users', got '%s'", createdPerm.Category)
	}
	if createdPerm.Description != "read users" {
		t.Fatalf("expected description 'read users', got '%s'", createdPerm.Description)
	}
	if createdPerm.ID == uuid.Nil {
		t.Fatal("expected non-nil UUID")
	}
}

// ---------------------------------------------------------------------------
// Casbin sync integration tests
// ---------------------------------------------------------------------------

func TestAddPermissionToRole_SyncsToCasbin(t *testing.T) {
	casbinSvc, err := authorization.NewTestCasbinService()
	if err != nil {
		t.Fatalf("failed to create test casbin service: %v", err)
	}

	roleID := uuid.New()
	permID := uuid.New()

	permRepo := &permRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Permission, error) {
			return &domain.Permission{ID: permID, Name: "users.view"}, nil
		},
		addPermissionToRoleFn: func(_, _ uuid.UUID) error { return nil },
	}
	roleRepo := &roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "admin"}, nil
		},
	}

	h := NewPermissionHandler(permRepo, roleRepo, casbinSvc)
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	resp := permReq(t, app, http.MethodPost, "/roles/"+roleID.String()+"/permissions", `{"permission_id":"`+permID.String()+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	// Verify Casbin policy was created
	mapping, _ := authorization.GetCasbinMapping("users.view")
	allowed, err := casbinSvc.Enforce("role:admin", authorization.DomainDefault, string(mapping.Resource), mapping.Action)
	if err != nil {
		t.Fatalf("enforce failed: %v", err)
	}
	if !allowed {
		t.Fatal("expected Casbin to allow role:admin users.view after sync")
	}
}

func TestRemovePermissionFromRole_SyncsRemovalToCasbin(t *testing.T) {
	casbinSvc, err := authorization.NewTestCasbinService()
	if err != nil {
		t.Fatalf("failed to create test casbin service: %v", err)
	}

	roleID := uuid.New()
	permID := uuid.New()

	// Pre-seed the Casbin policy so removal has something to remove
	mapping, _ := authorization.GetCasbinMapping("templates.create")
	_ = casbinSvc.AddPolicy("role:editor", authorization.DomainDefault, string(mapping.Resource), mapping.Action, "allow")

	permRepo := &permRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Permission, error) {
			return &domain.Permission{ID: permID, Name: "templates.create"}, nil
		},
		removePermissionRoleFn: func(_, _ uuid.UUID) error { return nil },
	}
	roleRepo := &roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "editor"}, nil
		},
	}

	h := NewPermissionHandler(permRepo, roleRepo, casbinSvc)
	app := newPermissionTestApp(h)
	app.Delete("/roles/:id/permissions/:permission_id", h.RemovePermissionFromRole)

	resp := permReq(t, app, http.MethodDelete, "/roles/"+roleID.String()+"/permissions/"+permID.String(), "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// Verify Casbin policy was removed
	allowed, err := casbinSvc.Enforce("role:editor", authorization.DomainDefault, string(mapping.Resource), mapping.Action)
	if err != nil {
		t.Fatalf("enforce failed: %v", err)
	}
	if allowed {
		t.Fatal("expected Casbin to deny role:editor templates.create after removal sync")
	}
}

func TestSyncPermissionToCasbin_NilDependenciesNoOp(t *testing.T) {
	// Handler with nil roleRepo and casbinService should not panic on sync
	permRepo := &permRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Permission, error) {
			return &domain.Permission{ID: id, Name: "users.view"}, nil
		},
		addPermissionToRoleFn: func(_, _ uuid.UUID) error { return nil },
	}
	h := NewPermissionHandler(permRepo, nil, nil)
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	roleID := uuid.New()
	permID := uuid.New()
	resp := permReq(t, app, http.MethodPost, "/roles/"+roleID.String()+"/permissions", `{"permission_id":"`+permID.String()+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 even with nil casbin, got %d", resp.StatusCode)
	}
}

func TestSyncPermissionToCasbin_UnmappedPermissionNoOp(t *testing.T) {
	casbinSvc, err := authorization.NewTestCasbinService()
	if err != nil {
		t.Fatalf("failed to create test casbin service: %v", err)
	}

	roleID := uuid.New()
	permID := uuid.New()

	permRepo := &permRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Permission, error) {
			// Return a permission that has no Casbin mapping
			return &domain.Permission{ID: permID, Name: "custom.unknown"}, nil
		},
		addPermissionToRoleFn: func(_, _ uuid.UUID) error { return nil },
	}
	roleRepo := &roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "admin"}, nil
		},
	}

	h := NewPermissionHandler(permRepo, roleRepo, casbinSvc)
	app := newPermissionTestApp(h)
	app.Post("/roles/:id/permissions", h.AddPermissionToRole)

	// Should succeed (DB write works) even though Casbin mapping doesn't exist
	resp := permReq(t, app, http.MethodPost, "/roles/"+roleID.String()+"/permissions", `{"permission_id":"`+permID.String()+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for unmapped permission, got %d", resp.StatusCode)
	}
}
