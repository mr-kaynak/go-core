package repository

import (
	stderrors "errors"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// PermissionRepositoryImpl implements PermissionRepository using GORM
type PermissionRepositoryImpl struct {
	db *gorm.DB
}

// NewPermissionRepository creates a new permission repository
func NewPermissionRepository(db *gorm.DB) PermissionRepository {
	return &PermissionRepositoryImpl{db: db}
}

// Create creates a new permission
func (r *PermissionRepositoryImpl) Create(permission *domain.Permission) error {
	if err := r.db.Create(permission).Error; err != nil {
		return errors.NewInternalError("Failed to create permission")
	}
	return nil
}

// GetByID retrieves a permission by ID
func (r *PermissionRepositoryImpl) GetByID(id uuid.UUID) (*domain.Permission, error) {
	var permission domain.Permission
	if err := r.db.First(&permission, "id = ?", id).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFound("Permission", id.String())
		}
		return nil, errors.NewInternalError("Failed to fetch permission")
	}
	return &permission, nil
}

// GetByName retrieves a permission by name
func (r *PermissionRepositoryImpl) GetByName(name string) (*domain.Permission, error) {
	var permission domain.Permission
	if err := r.db.First(&permission, "name = ?", name).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFound("Permission", name)
		}
		return nil, errors.NewInternalError("Failed to fetch permission")
	}
	return &permission, nil
}

// GetAll retrieves all permissions with pagination
func (r *PermissionRepositoryImpl) GetAll(offset, limit int) ([]domain.Permission, error) {
	var permissions []domain.Permission
	if err := r.db.Offset(offset).Limit(limit).Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch permissions")
	}
	return permissions, nil
}

// GetByCategory retrieves permissions by category
func (r *PermissionRepositoryImpl) GetByCategory(category string) ([]domain.Permission, error) {
	var permissions []domain.Permission
	if err := r.db.Where("category = ?", category).Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch permissions")
	}
	return permissions, nil
}

// Count returns the total number of permissions
func (r *PermissionRepositoryImpl) Count() (int64, error) {
	var count int64
	if err := r.db.Model(&domain.Permission{}).Count(&count).Error; err != nil {
		return 0, errors.NewInternalError("Failed to count permissions")
	}
	return count, nil
}

// Update updates a permission
func (r *PermissionRepositoryImpl) Update(permission *domain.Permission) error {
	if err := r.db.Save(permission).Error; err != nil {
		return errors.NewInternalError("Failed to update permission")
	}
	return nil
}

// Delete deletes a permission
func (r *PermissionRepositoryImpl) Delete(id uuid.UUID) error {
	if err := r.db.Delete(&domain.Permission{}, "id = ?", id).Error; err != nil {
		return errors.NewInternalError("Failed to delete permission")
	}
	return nil
}

// AddPermissionToRole adds a permission to a role
func (r *PermissionRepositoryImpl) AddPermissionToRole(roleID, permissionID uuid.UUID) error {
	rolePermission := domain.RolePermission{
		RoleID:       roleID,
		PermissionID: permissionID,
	}

	if err := r.db.Create(&rolePermission).Error; err != nil {
		return errors.NewInternalError("Failed to add permission to role")
	}
	return nil
}

// RemovePermissionFromRole removes a permission from a role
func (r *PermissionRepositoryImpl) RemovePermissionFromRole(roleID, permissionID uuid.UUID) error {
	if err := r.db.Delete(&domain.RolePermission{}, "role_id = ? AND permission_id = ?", roleID, permissionID).Error; err != nil {
		return errors.NewInternalError("Failed to remove permission from role")
	}
	return nil
}

// GetRolePermissions retrieves all permissions for a role (including inherited from parent roles)
func (r *PermissionRepositoryImpl) GetRolePermissions(roleID uuid.UUID) ([]domain.Permission, error) {
	var permissions []domain.Permission
	if err := r.db.
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ?", roleID).
		Distinct("permissions.*").
		Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch role permissions")
	}
	return permissions, nil
}

// GetUserPermissions retrieves all permissions for a user based on their roles
func (r *PermissionRepositoryImpl) GetUserPermissions(userID uuid.UUID) ([]domain.Permission, error) {
	var permissions []domain.Permission

	// Get permissions from user's roles
	if err := r.db.
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Where("user_roles.user_id = ?", userID).
		Distinct("permissions.*").
		Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch user permissions")
	}

	return permissions, nil
}
