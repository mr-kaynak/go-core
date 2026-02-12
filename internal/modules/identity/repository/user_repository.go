package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// UserRepository defines the interface for user data operations
type UserRepository interface {
	// WithTx returns a new repository instance that uses the given transaction
	WithTx(tx *gorm.DB) UserRepository

	// User operations
	Create(user *domain.User) error
	Update(user *domain.User) error
	Delete(id uuid.UUID) error
	GetByID(id uuid.UUID) (*domain.User, error)
	GetByEmail(email string) (*domain.User, error)
	GetByUsername(username string) (*domain.User, error)
	GetAll(offset, limit int) ([]*domain.User, error)
	Count() (int64, error)
	ExistsByEmail(email string) (bool, error)
	ExistsByUsername(username string) (bool, error)
	LoadRoles(user *domain.User) error

	// Role operations
	CreateRole(role *domain.Role) error
	UpdateRole(role *domain.Role) error
	DeleteRole(id uuid.UUID) error
	GetRoleByID(id uuid.UUID) (*domain.Role, error)
	GetRoleByName(name string) (*domain.Role, error)
	GetAllRoles() ([]*domain.Role, error)
	AssignRole(userID, roleID uuid.UUID) error
	RemoveRole(userID, roleID uuid.UUID) error
	GetUserRoles(userID uuid.UUID) ([]*domain.Role, error)

	// Permission operations
	CreatePermission(permission *domain.Permission) error
	UpdatePermission(permission *domain.Permission) error
	DeletePermission(id uuid.UUID) error
	GetPermissionByID(id uuid.UUID) (*domain.Permission, error)
	GetAllPermissions() ([]*domain.Permission, error)
	AssignPermissionToRole(roleID, permissionID uuid.UUID) error
	RemovePermissionFromRole(roleID, permissionID uuid.UUID) error
	GetRolePermissions(roleID uuid.UUID) ([]*domain.Permission, error)

	// Refresh token operations
	CreateRefreshToken(token *domain.RefreshToken) error
	GetRefreshToken(token string) (*domain.RefreshToken, error)
	RevokeRefreshToken(token string) error
	RevokeAllUserRefreshTokens(userID uuid.UUID) error
	CleanExpiredRefreshTokens() error
}
