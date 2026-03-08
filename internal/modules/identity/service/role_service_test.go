package service

import (
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

type roleRepoStub struct {
	createFn    func(role *domain.Role) error
	getByIDFn   func(id uuid.UUID) (*domain.Role, error)
	getByNameFn func(name string) (*domain.Role, error)
	getAllFn    func(offset, limit int) ([]domain.Role, error)
	countFn     func() (int64, error)
	updateFn    func(role *domain.Role) error
	deleteFn    func(id uuid.UUID) error
}

var _ repository.RoleRepository = (*roleRepoStub)(nil)

func (s *roleRepoStub) Create(role *domain.Role) error {
	if s.createFn != nil {
		return s.createFn(role)
	}
	return nil
}

func (s *roleRepoStub) GetByID(id uuid.UUID) (*domain.Role, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

func (s *roleRepoStub) GetByName(name string) (*domain.Role, error) {
	if s.getByNameFn != nil {
		return s.getByNameFn(name)
	}
	return nil, nil
}

func (s *roleRepoStub) GetAll(offset, limit int) ([]domain.Role, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return nil, nil
}

func (s *roleRepoStub) Count() (int64, error) {
	if s.countFn != nil {
		return s.countFn()
	}
	return 0, nil
}

func (s *roleRepoStub) Update(role *domain.Role) error {
	if s.updateFn != nil {
		return s.updateFn(role)
	}
	return nil
}

func (s *roleRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}

func assertRoleProblem(t *testing.T, err error, status int, detail string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected problem detail, got %T", err)
	}
	if pd.Status != status {
		t.Fatalf("expected status %d, got %d", status, pd.Status)
	}
	if detail != "" && pd.Detail != detail {
		t.Fatalf("expected detail %q, got %q", detail, pd.Detail)
	}
}

func TestRoleServiceCreateRole_Success(t *testing.T) {
	var created *domain.Role
	svc := NewRoleService(&roleRepoStub{
		getByNameFn: func(name string) (*domain.Role, error) { return nil, gorm.ErrRecordNotFound },
		createFn: func(role *domain.Role) error {
			created = role
			return nil
		},
	}, nil)

	role, err := svc.CreateRole(&CreateRoleRequest{Name: "auditor", Description: "read only"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if role == nil || created == nil {
		t.Fatalf("expected created role")
	}
	if created.Name != "auditor" || created.Description != "read only" {
		t.Fatalf("unexpected role payload persisted")
	}
}

func TestRoleServiceCreateRole_Conflict(t *testing.T) {
	svc := NewRoleService(&roleRepoStub{
		getByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: uuid.New(), Name: name}, nil
		},
	}, nil)

	_, err := svc.CreateRole(&CreateRoleRequest{Name: "admin"})
	assertRoleProblem(t, err, http.StatusConflict, "role with this name already exists")
}

func TestRoleServiceListRoles_CountFailure(t *testing.T) {
	svc := NewRoleService(&roleRepoStub{
		getAllFn: func(offset, limit int) ([]domain.Role, error) {
			return []domain.Role{{ID: uuid.New(), Name: "user"}}, nil
		},
		countFn: func() (int64, error) {
			return 0, stderrors.New("count failed")
		},
	}, nil)

	_, _, err := svc.ListRoles(0, 10)
	assertRoleProblem(t, err, http.StatusInternalServerError, "Failed to count roles")
}

func TestRoleServiceGetRoleByID_NotFound(t *testing.T) {
	roleID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}, nil)

	_, err := svc.GetRoleByID(roleID)
	assertRoleProblem(t, err, http.StatusNotFound, "Role with identifier '"+roleID.String()+"' not found")
}

func TestRoleServiceUpdateRole_NameConflict(t *testing.T) {
	roleID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "old"}, nil
		},
		getByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: uuid.New(), Name: name}, nil
		},
	}, nil)

	_, err := svc.UpdateRole(roleID, &UpdateRoleRequest{Name: "admin"})
	assertRoleProblem(t, err, http.StatusConflict, "role with this name already exists")
}

func TestRoleServiceUpdateRole_Success(t *testing.T) {
	roleID := uuid.New()
	var updated *domain.Role
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "old", Description: "d1"}, nil
		},
		getByNameFn: func(name string) (*domain.Role, error) {
			return nil, gorm.ErrRecordNotFound
		},
		updateFn: func(role *domain.Role) error {
			updated = role
			return nil
		},
	}, nil)

	got, err := svc.UpdateRole(roleID, &UpdateRoleRequest{Name: "new", Description: "d2"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got == nil || updated == nil {
		t.Fatalf("expected updated role")
	}
	if updated.Name != "new" || updated.Description != "d2" {
		t.Fatalf("expected updated fields to be persisted")
	}
}

func TestRoleServiceDeleteRole_SystemRoleForbidden(t *testing.T) {
	roleID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "admin"}, nil
		},
	}, nil)

	err := svc.DeleteRole(roleID)
	assertRoleProblem(t, err, http.StatusBadRequest, "cannot delete system role: admin")
}

func TestRoleServiceDeleteRole_Success(t *testing.T) {
	roleID := uuid.New()
	deleted := false
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "auditor"}, nil
		},
		deleteFn: func(id uuid.UUID) error {
			deleted = true
			if id != roleID {
				t.Fatalf("expected delete id %s, got %s", roleID, id)
			}
			return nil
		},
	}, nil)

	if err := svc.DeleteRole(roleID); err != nil {
		t.Fatalf("expected delete success, got %v", err)
	}
	if !deleted {
		t.Fatalf("expected repository delete call")
	}
}

func TestRoleServiceSetRoleHierarchy_ChildNotFound(t *testing.T) {
	childID := uuid.New()
	parentID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			if id == childID {
				return nil, gorm.ErrRecordNotFound
			}
			return &domain.Role{ID: id, Name: "x"}, nil
		},
	}, nil)

	err := svc.SetRoleHierarchy(childID, parentID)
	assertRoleProblem(t, err, http.StatusNotFound, "Child Role with identifier '"+childID.String()+"' not found")
}

func TestRoleServiceSetRoleHierarchy_ParentNotFound(t *testing.T) {
	childID := uuid.New()
	parentID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			if id == parentID {
				return nil, gorm.ErrRecordNotFound
			}
			return &domain.Role{ID: id, Name: "child"}, nil
		},
	}, nil)

	err := svc.SetRoleHierarchy(childID, parentID)
	assertRoleProblem(t, err, http.StatusNotFound, "Parent Role with identifier '"+parentID.String()+"' not found")
}

func TestRoleServiceSetRoleHierarchy_SelfInheritanceRejected(t *testing.T) {
	roleID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: id, Name: "same"}, nil
		},
	}, nil)

	err := svc.SetRoleHierarchy(roleID, roleID)
	assertRoleProblem(t, err, http.StatusBadRequest, "a role cannot inherit from itself")
}

func TestRoleServiceRemoveRoleHierarchy_ChildNotFound(t *testing.T) {
	childID := uuid.New()
	parentID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			if id == childID {
				return nil, gorm.ErrRecordNotFound
			}
			return &domain.Role{ID: id, Name: "x"}, nil
		},
	}, nil)

	err := svc.RemoveRoleHierarchy(childID, parentID)
	assertRoleProblem(t, err, http.StatusNotFound, "Child Role with identifier '"+childID.String()+"' not found")
}

func TestRoleServiceRemoveRoleHierarchy_ParentNotFound(t *testing.T) {
	childID := uuid.New()
	parentID := uuid.New()
	svc := NewRoleService(&roleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			if id == parentID {
				return nil, gorm.ErrRecordNotFound
			}
			return &domain.Role{ID: id, Name: "x"}, nil
		},
	}, nil)

	err := svc.RemoveRoleHierarchy(childID, parentID)
	assertRoleProblem(t, err, http.StatusNotFound, "Parent Role with identifier '"+parentID.String()+"' not found")
}
