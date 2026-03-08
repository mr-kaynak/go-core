package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// PermissionRepository defines permission data access interface
type PermissionRepository interface {
	Create(permission *domain.Permission) error
	GetByID(id uuid.UUID) (*domain.Permission, error)
	GetByName(name string) (*domain.Permission, error)
	GetAll(offset, limit int) ([]domain.Permission, error)
	GetByCategory(category string) ([]domain.Permission, error)
	Count() (int64, error)
	Update(permission *domain.Permission) error
	Delete(id uuid.UUID) error

	// Role-Permission operations
	AddPermissionToRole(roleID, permissionID uuid.UUID) error
	RemovePermissionFromRole(roleID, permissionID uuid.UUID) error
	GetRolePermissions(roleID uuid.UUID) ([]domain.Permission, error)
	GetUserPermissions(userID uuid.UUID) ([]domain.Permission, error)
}
