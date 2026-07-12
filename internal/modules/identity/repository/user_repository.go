package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// UserReader provides read-only access to user data.
type UserReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	GetAll(ctx context.Context, offset, limit int) ([]*domain.User, error)
	ListFiltered(ctx context.Context, filter domain.UserListFilter) ([]*domain.User, int64, error)
	Count(ctx context.Context) (int64, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	ExistsByUsername(ctx context.Context, username string) (bool, error)
	LoadRoles(ctx context.Context, user *domain.User) error
}

// UserWriter provides write access to user data.
type UserWriter interface {
	Create(ctx context.Context, user *domain.User) error
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// RoleManager provides role assignment and lookup operations.
type RoleManager interface {
	CreateRole(ctx context.Context, role *domain.Role) error
	UpdateRole(ctx context.Context, role *domain.Role) error
	DeleteRole(ctx context.Context, id uuid.UUID) error
	GetRoleByID(ctx context.Context, id uuid.UUID) (*domain.Role, error)
	GetRoleByName(ctx context.Context, name string) (*domain.Role, error)
	GetAllRoles(ctx context.Context) ([]*domain.Role, error)
	AssignRole(ctx context.Context, userID, roleID uuid.UUID) error
	RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error
	GetUserRoles(ctx context.Context, userID uuid.UUID) ([]*domain.Role, error)
}

// PermissionManager provides permission CRUD and role-permission assignment operations.
type PermissionManager interface {
	CreatePermission(ctx context.Context, permission *domain.Permission) error
	UpdatePermission(ctx context.Context, permission *domain.Permission) error
	DeletePermission(ctx context.Context, id uuid.UUID) error
	GetPermissionByID(ctx context.Context, id uuid.UUID) (*domain.Permission, error)
	GetAllPermissions(ctx context.Context) ([]*domain.Permission, error)
	AssignPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error
	RemovePermissionFromRole(ctx context.Context, roleID, permissionID uuid.UUID) error
	GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]*domain.Permission, error)
}

// RefreshTokenManager provides refresh token lifecycle operations.
type RefreshTokenManager interface {
	CreateRefreshToken(ctx context.Context, token *domain.RefreshToken) error
	GetRefreshToken(ctx context.Context, token string) (*domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, token string) error
	RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	GetActiveRefreshTokensByUser(ctx context.Context, userID uuid.UUID) ([]*domain.RefreshToken, error)
	RevokeRefreshTokenByID(ctx context.Context, id uuid.UUID) error
	CleanExpiredRefreshTokens(ctx context.Context) error
}

// AdminUserManager provides admin-only user analytics and session operations.
type AdminUserManager interface {
	CountByStatus(ctx context.Context, status string) (int64, error)
	CountCreatedAfter(ctx context.Context, after time.Time) (int64, error)
	GetAllActiveSessions(ctx context.Context, offset, limit int, userID *uuid.UUID) ([]*domain.RefreshToken, error)
	CountActiveSessions(ctx context.Context, userID *uuid.UUID) (int64, error)
}

// UserRepository defines the composite interface for all user data operations.
// It embeds all sub-interfaces for backward compatibility. Services should
// depend on the narrowest sub-interface(s) they actually need.
type UserRepository interface {
	WithTx(tx *gorm.DB) UserRepository

	UserReader
	UserWriter
	RoleManager
	PermissionManager
	RefreshTokenManager
	AdminUserManager
}
