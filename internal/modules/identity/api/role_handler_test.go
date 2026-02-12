package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"gorm.io/gorm"
)

type roleHandlerRepoStub struct {
	createFn    func(role *domain.Role) error
	getByIDFn   func(id uuid.UUID) (*domain.Role, error)
	getByNameFn func(name string) (*domain.Role, error)
	getAllFn    func(offset, limit int) ([]domain.Role, error)
	countFn     func() (int64, error)
	updateFn    func(role *domain.Role) error
	deleteFn    func(id uuid.UUID) error
}

var _ repository.RoleRepository = (*roleHandlerRepoStub)(nil)

func (s *roleHandlerRepoStub) Create(role *domain.Role) error {
	if s.createFn != nil {
		return s.createFn(role)
	}
	return nil
}
func (s *roleHandlerRepoStub) GetByID(id uuid.UUID) (*domain.Role, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *roleHandlerRepoStub) GetByName(name string) (*domain.Role, error) {
	if s.getByNameFn != nil {
		return s.getByNameFn(name)
	}
	return nil, nil
}
func (s *roleHandlerRepoStub) GetAll(offset, limit int) ([]domain.Role, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return nil, nil
}
func (s *roleHandlerRepoStub) Count() (int64, error) {
	if s.countFn != nil {
		return s.countFn()
	}
	return 0, nil
}
func (s *roleHandlerRepoStub) Update(role *domain.Role) error {
	if s.updateFn != nil {
		return s.updateFn(role)
	}
	return nil
}
func (s *roleHandlerRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}

func newRoleHandlerApp(h *RoleHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
	return app
}

func roleReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestRoleHandlerCreateRole_Success(t *testing.T) {
	repo := &roleHandlerRepoStub{
		getByNameFn: func(name string) (*domain.Role, error) { return nil, gorm.ErrRecordNotFound },
		createFn:    func(role *domain.Role) error { return nil },
	}
	svc := service.NewRoleService(repo, nil)
	h := NewRoleHandler(svc)
	app := newRoleHandlerApp(h)
	app.Post("/roles", h.CreateRole)

	resp := roleReq(t, app, http.MethodPost, "/roles", `{"name":"auditor","description":"read only"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerCreateRole_InvalidBody(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Post("/roles", h.CreateRole)

	resp := roleReq(t, app, http.MethodPost, "/roles", `{invalid`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerListRoles_NormalizesPagination(t *testing.T) {
	gotOffset := -1
	gotLimit := -1
	repo := &roleHandlerRepoStub{
		getAllFn: func(offset, limit int) ([]domain.Role, error) {
			gotOffset = offset
			gotLimit = limit
			return []domain.Role{}, nil
		},
		countFn: func() (int64, error) { return 0, nil },
	}
	h := NewRoleHandler(service.NewRoleService(repo, nil))
	app := newRoleHandlerApp(h)
	app.Get("/roles", h.ListRoles)

	resp := roleReq(t, app, http.MethodGet, "/roles?page=-2&limit=500", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if gotOffset != 0 || gotLimit != 10 {
		t.Fatalf("expected normalized offset=0 limit=10, got offset=%d limit=%d", gotOffset, gotLimit)
	}
}

func TestRoleHandlerGetRole_InvalidID(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Get("/roles/:id", h.GetRole)

	resp := roleReq(t, app, http.MethodGet, "/roles/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerDeleteRole_InvalidID(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Delete("/roles/:id", h.DeleteRole)

	resp := roleReq(t, app, http.MethodDelete, "/roles/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerSetRoleHierarchy_InvalidParentID(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Post("/roles/:id/inherit/:parent_id", h.SetRoleHierarchy)

	resp := roleReq(t, app, http.MethodPost, "/roles/"+uuid.NewString()+"/inherit/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerSetRoleHierarchy_InvalidChildID(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Post("/roles/:id/inherit/:parent_id", h.SetRoleHierarchy)

	resp := roleReq(t, app, http.MethodPost, "/roles/not-uuid/inherit/"+uuid.NewString(), "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerRemoveRoleHierarchy_InvalidChildID(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Delete("/roles/:id/inherit/:parent_id", h.RemoveRoleHierarchy)

	resp := roleReq(t, app, http.MethodDelete, "/roles/not-uuid/inherit/"+uuid.NewString(), "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRoleHandlerRemoveRoleHierarchy_InvalidParentID(t *testing.T) {
	h := NewRoleHandler(service.NewRoleService(&roleHandlerRepoStub{}, nil))
	app := newRoleHandlerApp(h)
	app.Delete("/roles/:id/inherit/:parent_id", h.RemoveRoleHierarchy)

	resp := roleReq(t, app, http.MethodDelete, "/roles/"+uuid.NewString()+"/inherit/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
