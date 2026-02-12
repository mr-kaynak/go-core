package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// APIKeyRepository defines the interface for API key data operations
type APIKeyRepository interface {
	// Create creates a new API key record
	Create(apiKey *domain.APIKey) error

	// GetByHash retrieves an API key by its hash
	GetByHash(keyHash string) (*domain.APIKey, error)

	// GetByID retrieves an API key by its ID
	GetByID(id uuid.UUID) (*domain.APIKey, error)

	// GetUserKeys retrieves all API keys for a specific user
	GetUserKeys(userID uuid.UUID) ([]*domain.APIKey, error)

	// Revoke marks an API key as revoked
	Revoke(id uuid.UUID) error

	// UpdateLastUsed updates the last used timestamp for an API key
	UpdateLastUsed(id uuid.UUID) error
}
