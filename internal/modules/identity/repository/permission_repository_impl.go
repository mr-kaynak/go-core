package repository

import (
	"context"
	stderrors "errors"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// permissionRepositoryImpl implements PermissionRepository using GORM
type permissionRepositoryImpl struct {
	db *gorm.DB
}

// NewPermissionRepository creates a new permission repository
func NewPermissionRepository(db *gorm.DB) PermissionRepository {
	return &permissionRepositoryImpl{db: db}
}

// Create creates a new permission
func (r *permissionRepositoryImpl) Create(ctx context.Context, permission *domain.Permission) error {
	db := r.db.WithContext(ctx)
	if err := db.Create(permission).Error; err != nil {
		return errors.NewInternalError("Failed to create permission")
	}
	return nil
}

// GetByID retrieves a permission by ID
func (r *permissionRepositoryImpl) GetByID(ctx context.Context, id uuid.UUID) (*domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permission domain.Permission
	if err := db.First(&permission, "id = ?", id).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFound("Permission", id.String())
		}
		return nil, errors.NewInternalError("Failed to fetch permission")
	}
	return &permission, nil
}

// GetByName retrieves a permission by name
func (r *permissionRepositoryImpl) GetByName(ctx context.Context, name string) (*domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permission domain.Permission
	if err := db.First(&permission, "name = ?", name).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFound("Permission", name)
		}
		return nil, errors.NewInternalError("Failed to fetch permission")
	}
	return &permission, nil
}

// GetAll retrieves all permissions with pagination
func (r *permissionRepositoryImpl) GetAll(ctx context.Context, offset, limit int) ([]domain.Permission, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var permissions []domain.Permission
	if err := db.Offset(offset).Limit(limit).Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch permissions")
	}
	return permissions, nil
}

// GetByCategory retrieves permissions by category
func (r *permissionRepositoryImpl) GetByCategory(ctx context.Context, category string) ([]domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permissions []domain.Permission
	if err := db.Where("category = ?", category).Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch permissions")
	}
	return permissions, nil
}

// GetByCategoryPaginated retrieves permissions by category with LIMIT/OFFSET pagination
// and a separate COUNT query, mirroring the behavior of GetAll.
func (r *permissionRepositoryImpl) GetByCategoryPaginated(
	ctx context.Context, category string, offset, limit int,
) ([]domain.Permission, int64, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var permissions []domain.Permission
	if err := db.Where("category = ?", category).Offset(offset).Limit(limit).Find(&permissions).Error; err != nil {
		return nil, 0, errors.NewInternalError("Failed to fetch permissions")
	}
	var count int64
	if err := db.Model(&domain.Permission{}).Where("category = ?", category).Count(&count).Error; err != nil {
		return nil, 0, errors.NewInternalError("Failed to count permissions")
	}
	return permissions, count, nil
}

// Count returns the total number of permissions
func (r *permissionRepositoryImpl) Count(ctx context.Context) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	if err := db.Model(&domain.Permission{}).Count(&count).Error; err != nil {
		return 0, errors.NewInternalError("Failed to count permissions")
	}
	return count, nil
}

// Update updates a permission
func (r *permissionRepositoryImpl) Update(ctx context.Context, permission *domain.Permission) error {
	db := r.db.WithContext(ctx)
	if err := db.Save(permission).Error; err != nil {
		return errors.NewInternalError("Failed to update permission")
	}
	return nil
}

// Delete deletes a permission
func (r *permissionRepositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if err := db.Delete(&domain.Permission{}, "id = ?", id).Error; err != nil {
		return errors.NewInternalError("Failed to delete permission")
	}
	return nil
}

// AddPermissionToRole adds a permission to a role
func (r *permissionRepositoryImpl) AddPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	rolePermission := domain.RolePermission{
		RoleID:       roleID,
		PermissionID: permissionID,
	}

	if err := db.Create(&rolePermission).Error; err != nil {
		return errors.NewInternalError("Failed to add permission to role")
	}
	return nil
}

// RemovePermissionFromRole removes a permission from a role
func (r *permissionRepositoryImpl) RemovePermissionFromRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if err := db.Delete(&domain.RolePermission{}, "role_id = ? AND permission_id = ?", roleID, permissionID).Error; err != nil {
		return errors.NewInternalError("Failed to remove permission from role")
	}
	return nil
}

// GetRolePermissions retrieves all permissions for a role (including inherited from parent roles)
func (r *permissionRepositoryImpl) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permissions []domain.Permission
	if err := db.
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ?", roleID).
		Distinct("permissions.*").
		Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch role permissions")
	}
	return permissions, nil
}

// GetUserPermissions retrieves all permissions for a user based on their roles
func (r *permissionRepositoryImpl) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permissions []domain.Permission

	// Get permissions from user's roles
	if err := db.
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Where("user_roles.user_id = ?", userID).
		Distinct("permissions.*").
		Find(&permissions).Error; err != nil {
		return nil, errors.NewInternalError("Failed to fetch user permissions")
	}

	return permissions, nil
}
