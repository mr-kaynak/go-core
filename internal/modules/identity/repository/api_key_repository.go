package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// APIKeyRepository defines the interface for API key data operations
type APIKeyRepository interface {
	// Create creates a new API key record
	Create(apiKey *domain.APIKey) error

	// GetByHash retrieves an API key by its hash
	GetByHash(keyHash string) (*domain.APIKey, error)

	// GetByHashWithRoles retrieves an API key by its hash with roles and permissions preloaded
	GetByHashWithRoles(keyHash string) (*domain.APIKey, error)

	// GetByID retrieves an API key by its ID
	GetByID(id uuid.UUID) (*domain.APIKey, error)

	// GetByIDWithRoles retrieves an API key by its ID with roles and permissions preloaded
	GetByIDWithRoles(id uuid.UUID) (*domain.APIKey, error)

	// GetUserKeys retrieves all API keys for a specific user
	GetUserKeys(userID uuid.UUID) ([]*domain.APIKey, error)

	// GetUserKeysPaginated retrieves paginated API keys for a specific user and total count
	GetUserKeysPaginated(userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error)

	// Revoke marks an API key as revoked
	Revoke(id uuid.UUID) error

	// UpdateLastUsed updates the last used timestamp for an API key
	UpdateLastUsed(id uuid.UUID) error

	// CleanupRevokedKeys soft-deletes revoked keys older than the given duration and expired keys
	CleanupRevokedKeys(olderThan time.Duration) error

	// AssignRole assigns a role to an API key
	AssignRole(apiKeyID, roleID uuid.UUID) error

	// RemoveRole removes a role from an API key
	RemoveRole(apiKeyID, roleID uuid.UUID) error
}
