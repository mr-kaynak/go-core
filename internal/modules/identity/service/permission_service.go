package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// PermissionService handles permission-related business logic.
type PermissionService struct {
	permRepo      repository.PermissionRepository
	roleRepo      repository.RoleRepository
	casbinService *authorization.CasbinService
	logger        *logger.Logger
}

// NewPermissionService creates a new PermissionService.
func NewPermissionService(
	permRepo repository.PermissionRepository,
	roleRepo repository.RoleRepository,
	casbinService *authorization.CasbinService,
) *PermissionService {
	return &PermissionService{
		permRepo:      permRepo,
		roleRepo:      roleRepo,
		casbinService: casbinService,
		logger:        logger.Get().WithFields(logger.Fields{"service": "permission"}),
	}
}

// ListPermissions returns a paginated list of permissions, optionally filtered by category.
func (s *PermissionService) ListPermissions(
	ctx context.Context, category string, offset, limit int,
) ([]domain.Permission, int64, error) {
	if category != "" {
		perms, count, err := s.permRepo.GetByCategoryPaginated(ctx, category, offset, limit)
		if err != nil {
			s.logger.Error("Failed to fetch permissions by category", "category", category, "error", err)
			return nil, 0, errors.NewInternalError("Failed to fetch permissions")
		}
		return perms, count, nil
	}

	perms, err := s.permRepo.GetAll(ctx, offset, limit)
	if err != nil {
		s.logger.Error("Failed to fetch permissions", "error", err)
		return nil, 0, errors.NewInternalError("Failed to fetch permissions")
	}
	count, err := s.permRepo.Count(ctx)
	if err != nil {
		s.logger.Error("Failed to count permissions", "error", err)
		return nil, 0, errors.NewInternalError("Failed to fetch permissions count")
	}
	return perms, count, nil
}

// GetPermission returns a single permission by ID.
func (s *PermissionService) GetPermission(ctx context.Context, id uuid.UUID) (*domain.Permission, error) {
	return s.permRepo.GetByID(ctx, id)
}

// CreatePermission creates a new permission after checking for duplicates.
func (s *PermissionService) CreatePermission(ctx context.Context, name, description, category string) (*domain.Permission, error) {
	existing, err := s.permRepo.GetByName(ctx, name)
	if err != nil {
		pd := errors.GetProblemDetail(err)
		if pd == nil || pd.Code != errors.CodeNotFound {
			s.logger.Error("Failed to check existing permission", "name", name, "error", err)
			return nil, errors.NewInternalError("Failed to create permission")
		}
	}
	if existing != nil {
		return nil, errors.NewConflict("Permission with name '" + name + "' already exists")
	}

	perm := &domain.Permission{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		Category:    category,
	}
	if err := s.permRepo.Create(ctx, perm); err != nil {
		s.logger.Error("Failed to create permission", "name", name, "error", err)
		return nil, errors.NewInternalError("Failed to create permission")
	}
	return perm, nil
}

// UpdatePermission applies partial updates to an existing permission.
func (s *PermissionService) UpdatePermission(
	ctx context.Context, id uuid.UUID, name, description, category string,
) (*domain.Permission, error) {
	perm, err := s.permRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if name != "" {
		perm.Name = name
	}
	if description != "" {
		perm.Description = description
	}
	if category != "" {
		perm.Category = category
	}

	if err := s.permRepo.Update(ctx, perm); err != nil {
		s.logger.Error("Failed to update permission", "id", id, "error", err)
		return nil, errors.NewInternalError("Failed to update permission")
	}
	return perm, nil
}

// DeletePermission removes a permission by ID.
func (s *PermissionService) DeletePermission(ctx context.Context, id uuid.UUID) error {
	if err := s.permRepo.Delete(ctx, id); err != nil {
		s.logger.Error("Failed to delete permission", "id", id, "error", err)
		return errors.NewInternalError("Failed to delete permission")
	}
	return nil
}

// GetRolePermissions returns all permissions assigned to a role.
func (s *PermissionService) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]domain.Permission, error) {
	perms, err := s.permRepo.GetRolePermissions(ctx, roleID)
	if err != nil {
		s.logger.Error("Failed to fetch role permissions", "role_id", roleID, "error", err)
		return nil, errors.NewInternalError("Failed to fetch role permissions")
	}
	return perms, nil
}

// AddPermissionToRole assigns a permission to a role and syncs Casbin.
func (s *PermissionService) AddPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	if _, err := s.permRepo.GetByID(ctx, permissionID); err != nil {
		return errors.NewNotFound("Permission", permissionID.String())
	}

	if err := s.permRepo.AddPermissionToRole(ctx, roleID, permissionID); err != nil {
		s.logger.Error("Failed to add permission to role", "role_id", roleID, "permission_id", permissionID, "error", err)
		return errors.NewInternalError("Failed to add permission to role")
	}

	s.syncPermissionToCasbin(ctx, roleID, permissionID, true)
	return nil
}

// RemovePermissionFromRole removes a permission from a role and syncs Casbin.
func (s *PermissionService) RemovePermissionFromRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	if err := s.permRepo.RemovePermissionFromRole(ctx, roleID, permissionID); err != nil {
		s.logger.Error("Failed to remove permission from role", "role_id", roleID, "permission_id", permissionID, "error", err)
		return errors.NewInternalError("Failed to remove permission from role")
	}

	s.syncPermissionToCasbin(ctx, roleID, permissionID, false)
	return nil
}

// syncPermissionToCasbin adds or removes a Casbin policy for a role-permission pair.
// It is best-effort: failures are logged but do not affect the return value.
func (s *PermissionService) syncPermissionToCasbin(ctx context.Context, roleID, permissionID uuid.UUID, add bool) {
	if s.casbinService == nil || s.roleRepo == nil {
		return
	}

	role, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		s.logger.Error("Casbin sync: failed to fetch role", "role_id", roleID, "error", err)
		return
	}

	perm, err := s.permRepo.GetByID(ctx, permissionID)
	if err != nil {
		s.logger.Error("Casbin sync: failed to fetch permission", "permission_id", permissionID, "error", err)
		return
	}

	mapping, ok := authorization.GetCasbinMapping(perm.Name)
	if !ok {
		s.logger.Warn("Casbin sync: no mapping for permission", "permission", perm.Name)
		return
	}

	subject := "role:" + role.Name
	resource := string(mapping.Resource)

	if add {
		if err := s.casbinService.AddPolicy(subject, authorization.DomainDefault, resource, mapping.Action, "allow"); err != nil {
			s.logger.Error("Casbin sync: failed to add policy", "role", role.Name, "permission", perm.Name, "error", err)
		}
	} else {
		if err := s.casbinService.RemovePolicy(subject, authorization.DomainDefault, resource, mapping.Action, "allow"); err != nil {
			s.logger.Error("Casbin sync: failed to remove policy", "role", role.Name, "permission", perm.Name, "error", err)
		}
	}
}
