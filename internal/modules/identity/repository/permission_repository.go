package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// PermissionRepository defines permission data access interface
type PermissionRepository interface {
	Create(ctx context.Context, permission *domain.Permission) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Permission, error)
	GetByName(ctx context.Context, name string) (*domain.Permission, error)
	GetAll(ctx context.Context, offset, limit int) ([]domain.Permission, error)
	GetByCategory(ctx context.Context, category string) ([]domain.Permission, error)
	GetByCategoryPaginated(ctx context.Context, category string, offset, limit int) ([]domain.Permission, int64, error)
	Count(ctx context.Context) (int64, error)
	Update(ctx context.Context, permission *domain.Permission) error
	Delete(ctx context.Context, id uuid.UUID) error

	// Role-Permission operations
	AddPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error
	RemovePermissionFromRole(ctx context.Context, roleID, permissionID uuid.UUID) error
	GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]domain.Permission, error)
	GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]domain.Permission, error)
}
