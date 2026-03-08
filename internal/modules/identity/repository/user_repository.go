package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// UserReader provides read-only access to user data.
type UserReader interface {
	GetByID(id uuid.UUID) (*domain.User, error)
	GetByEmail(email string) (*domain.User, error)
	GetByUsername(username string) (*domain.User, error)
	GetAll(offset, limit int) ([]*domain.User, error)
	ListFiltered(filter domain.UserListFilter) ([]*domain.User, int64, error)
	Count() (int64, error)
	ExistsByEmail(email string) (bool, error)
	ExistsByUsername(username string) (bool, error)
	LoadRoles(user *domain.User) error
}

// UserWriter provides write access to user data.
type UserWriter interface {
	Create(user *domain.User) error
	Update(user *domain.User) error
	Delete(id uuid.UUID) error
}

// RoleManager provides role assignment and lookup operations.
type RoleManager interface {
	CreateRole(role *domain.Role) error
	UpdateRole(role *domain.Role) error
	DeleteRole(id uuid.UUID) error
	GetRoleByID(id uuid.UUID) (*domain.Role, error)
	GetRoleByName(name string) (*domain.Role, error)
	GetAllRoles() ([]*domain.Role, error)
	AssignRole(userID, roleID uuid.UUID) error
	RemoveRole(userID, roleID uuid.UUID) error
	GetUserRoles(userID uuid.UUID) ([]*domain.Role, error)
}

// PermissionManager provides permission CRUD and role-permission assignment operations.
type PermissionManager interface {
	CreatePermission(permission *domain.Permission) error
	UpdatePermission(permission *domain.Permission) error
	DeletePermission(id uuid.UUID) error
	GetPermissionByID(id uuid.UUID) (*domain.Permission, error)
	GetAllPermissions() ([]*domain.Permission, error)
	AssignPermissionToRole(roleID, permissionID uuid.UUID) error
	RemovePermissionFromRole(roleID, permissionID uuid.UUID) error
	GetRolePermissions(roleID uuid.UUID) ([]*domain.Permission, error)
}

// RefreshTokenManager provides refresh token lifecycle operations.
type RefreshTokenManager interface {
	CreateRefreshToken(token *domain.RefreshToken) error
	GetRefreshToken(token string) (*domain.RefreshToken, error)
	RevokeRefreshToken(token string) error
	RevokeAllUserRefreshTokens(userID uuid.UUID) error
	GetActiveRefreshTokensByUser(userID uuid.UUID) ([]*domain.RefreshToken, error)
	RevokeRefreshTokenByID(id uuid.UUID) error
	CleanExpiredRefreshTokens() error
}

// AdminUserManager provides admin-only user analytics and session operations.
type AdminUserManager interface {
	CountByStatus(status string) (int64, error)
	CountCreatedAfter(after time.Time) (int64, error)
	GetAllActiveSessions(offset, limit int) ([]*domain.RefreshToken, error)
	CountActiveSessions() (int64, error)
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
