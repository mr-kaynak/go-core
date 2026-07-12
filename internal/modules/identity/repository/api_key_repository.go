package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// APIKeyRepository defines the interface for API key data operations
type APIKeyRepository interface {
	// Create creates a new API key record
	Create(ctx context.Context, apiKey *domain.APIKey) error

	// GetByHash retrieves an API key by its hash
	GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error)

	// GetByHashWithRoles retrieves an API key by its hash with roles and permissions preloaded
	GetByHashWithRoles(ctx context.Context, keyHash string) (*domain.APIKey, error)

	// GetByID retrieves an API key by its ID
	GetByID(ctx context.Context, id uuid.UUID) (*domain.APIKey, error)

	// GetByIDWithRoles retrieves an API key by its ID with roles and permissions preloaded
	GetByIDWithRoles(ctx context.Context, id uuid.UUID) (*domain.APIKey, error)

	// GetUserKeys retrieves all API keys for a specific user
	GetUserKeys(ctx context.Context, userID uuid.UUID) ([]*domain.APIKey, error)

	// GetUserKeysPaginated retrieves paginated API keys for a specific user and total count
	GetUserKeysPaginated(ctx context.Context, userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error)

	// Revoke marks an API key as revoked
	Revoke(ctx context.Context, id uuid.UUID) error

	// UpdateLastUsed updates the last used timestamp for an API key
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error

	// GetAll retrieves all API keys with pagination and total count
	GetAll(ctx context.Context, offset, limit int) ([]*domain.APIKey, int64, error)

	// CleanupRevokedKeys soft-deletes revoked keys older than the given duration and expired keys
	CleanupRevokedKeys(ctx context.Context, olderThan time.Duration) error

	// AssignRole assigns a role to an API key
	AssignRole(ctx context.Context, apiKeyID, roleID uuid.UUID) error

	// RemoveRole removes a role from an API key
	RemoveRole(ctx context.Context, apiKeyID, roleID uuid.UUID) error
}
