package service

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// CreateRoleRequest represents role creation request
type CreateRoleRequest struct {
	Name        string `json:"name" validate:"required,min=2,max=50"`
	Description string `json:"description" validate:"max=255"`
}

// UpdateRoleRequest represents role update request
type UpdateRoleRequest struct {
	Name        string `json:"name" validate:"omitempty,min=2,max=50"`
	Description string `json:"description" validate:"omitempty,max=255"`
}

// RoleService handles role-related business logic
type RoleService struct {
	roleRepo       repository.RoleRepository
	casbinService  *authorization.CasbinService
	logger         *logger.Logger
}

// NewRoleService creates a new role service
func NewRoleService(roleRepo repository.RoleRepository, casbinService *authorization.CasbinService) *RoleService {
	return &RoleService{
		roleRepo:      roleRepo,
		casbinService: casbinService,
		logger:        logger.Get().WithFields(logger.Fields{"service": "role"}),
	}
}

// CreateRole creates a new role
func (s *RoleService) CreateRole(req *CreateRoleRequest) (*domain.Role, error) {
	// Check if role already exists
	existingRole, err := s.roleRepo.GetByName(req.Name)
	if err == nil && existingRole != nil {
		return nil, errors.NewConflict("role with this name already exists")
	}

	// Create new role
	role := &domain.Role{
		ID:          uuid.New(),
		Name:        req.Name,
		Description: req.Description,
	}

	if err := s.roleRepo.Create(role); err != nil {
		s.logger.Error("Failed to create role", "name", req.Name, "error", err)
		return nil, errors.NewInternalError("Failed to create role")
	}

	s.logger.Info("Role created successfully", "role_id", role.ID, "role_name", req.Name)
	return role, nil
}

// ListRoles lists all roles with pagination
func (s *RoleService) ListRoles(offset, limit int) ([]domain.Role, int64, error) {
	roles, err := s.roleRepo.GetAll(offset, limit)
	if err != nil {
		s.logger.Error("Failed to list roles", "error", err)
		return nil, 0, errors.NewInternalError("Failed to list roles")
	}

	count, err := s.roleRepo.Count()
	if err != nil {
		s.logger.Error("Failed to count roles", "error", err)
		return nil, 0, errors.NewInternalError("Failed to count roles")
	}

	return roles, count, nil
}

// GetRoleByID gets a role by ID
func (s *RoleService) GetRoleByID(roleID uuid.UUID) (*domain.Role, error) {
	role, err := s.roleRepo.GetByID(roleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NewNotFound("Role", roleID.String())
		}
		s.logger.Error("Failed to get role", "role_id", roleID, "error", err)
		return nil, errors.NewInternalError("Failed to get role")
	}

	return role, nil
}

// UpdateRole updates a role
func (s *RoleService) UpdateRole(roleID uuid.UUID, req *UpdateRoleRequest) (*domain.Role, error) {
	role, err := s.roleRepo.GetByID(roleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NewNotFound("Role", roleID.String())
		}
		s.logger.Error("Failed to get role", "role_id", roleID, "error", err)
		return nil, errors.NewInternalError("Failed to get role")
	}

	// Update fields if provided
	if req.Name != "" {
		// Check if new name already exists
		existingRole, err := s.roleRepo.GetByName(req.Name)
		if err == nil && existingRole != nil && existingRole.ID != roleID {
			return nil, errors.NewConflict("role with this name already exists")
		}
		role.Name = req.Name
	}

	if req.Description != "" {
		role.Description = req.Description
	}

	if err := s.roleRepo.Update(role); err != nil {
		s.logger.Error("Failed to update role", "role_id", roleID, "error", err)
		return nil, errors.NewInternalError("Failed to update role")
	}

	s.logger.Info("Role updated successfully", "role_id", roleID)
	return role, nil
}

// DeleteRole deletes a role
func (s *RoleService) DeleteRole(roleID uuid.UUID) error {
	role, err := s.roleRepo.GetByID(roleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.NewNotFound("Role", roleID.String())
		}
		s.logger.Error("Failed to get role", "role_id", roleID, "error", err)
		return errors.NewInternalError("Failed to get role")
	}

	// Prevent deletion of system roles
	systemRoles := []string{"system_admin", "admin", "user"}
	for _, sysRole := range systemRoles {
		if role.Name == sysRole {
			return errors.NewBadRequest(fmt.Sprintf("cannot delete system role: %s", sysRole))
		}
	}

	if err := s.roleRepo.Delete(roleID); err != nil {
		s.logger.Error("Failed to delete role", "role_id", roleID, "error", err)
		return errors.NewInternalError("Failed to delete role")
	}

	s.logger.Info("Role deleted successfully", "role_id", roleID)
	return nil
}

// SetRoleHierarchy sets role inheritance (childRole inherits from parentRole)
func (s *RoleService) SetRoleHierarchy(childRoleID, parentRoleID uuid.UUID) error {
	// Check if both roles exist
	childRole, err := s.roleRepo.GetByID(childRoleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.NewNotFound("Child Role", childRoleID.String())
		}
		return errors.NewInternalError("Failed to get child role")
	}

	parentRole, err := s.roleRepo.GetByID(parentRoleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.NewNotFound("Parent Role", parentRoleID.String())
		}
		return errors.NewInternalError("Failed to get parent role")
	}

	// Prevent circular inheritance
	if childRoleID == parentRoleID {
		return errors.NewBadRequest("a role cannot inherit from itself")
	}

	// Add role inheritance to Casbin
	if err := s.casbinService.AddRoleInheritance(childRole.Name, parentRole.Name); err != nil {
		s.logger.Error("Failed to add role inheritance",
			"child_role", childRole.Name,
			"parent_role", parentRole.Name,
			"error", err)
		return errors.NewInternalError("Failed to set role hierarchy")
	}

	s.logger.Info("Role hierarchy set successfully",
		"child_role_id", childRoleID,
		"child_role_name", childRole.Name,
		"parent_role_id", parentRoleID,
		"parent_role_name", parentRole.Name)

	return nil
}

// RemoveRoleHierarchy removes role inheritance
func (s *RoleService) RemoveRoleHierarchy(childRoleID, parentRoleID uuid.UUID) error {
	// Check if both roles exist
	childRole, err := s.roleRepo.GetByID(childRoleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.NewNotFound("Child Role", childRoleID.String())
		}
		return errors.NewInternalError("Failed to get child role")
	}

	parentRole, err := s.roleRepo.GetByID(parentRoleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.NewNotFound("Parent Role", parentRoleID.String())
		}
		return errors.NewInternalError("Failed to get parent role")
	}

	// Remove role inheritance from Casbin
	if err := s.casbinService.RemoveRoleInheritance(childRole.Name, parentRole.Name); err != nil {
		s.logger.Error("Failed to remove role inheritance",
			"child_role", childRole.Name,
			"parent_role", parentRole.Name,
			"error", err)
		return errors.NewInternalError("Failed to remove role hierarchy")
	}

	s.logger.Info("Role hierarchy removed successfully",
		"child_role_id", childRoleID,
		"child_role_name", childRole.Name,
		"parent_role_id", parentRoleID,
		"parent_role_name", parentRole.Name)

	return nil
}
