package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// apiKeyRepositoryImpl implements APIKeyRepository using GORM
type apiKeyRepositoryImpl struct {
	db *gorm.DB
}

// NewAPIKeyRepository creates a new API key repository
func NewAPIKeyRepository(db *gorm.DB) APIKeyRepository {
	return &apiKeyRepositoryImpl{
		db: db,
	}
}

// Create creates a new API key record
func (r *apiKeyRepositoryImpl) Create(apiKey *domain.APIKey) error {
	return r.db.Create(apiKey).Error
}

// GetByHash retrieves an API key by its hash
func (r *apiKeyRepositoryImpl) GetByHash(keyHash string) (*domain.APIKey, error) {
	var apiKey domain.APIKey
	err := r.db.Where("key_hash = ?", keyHash).First(&apiKey).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetByID retrieves an API key by its ID
func (r *apiKeyRepositoryImpl) GetByID(id uuid.UUID) (*domain.APIKey, error) {
	var apiKey domain.APIKey
	err := r.db.First(&apiKey, id).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetUserKeys retrieves all API keys for a specific user
func (r *apiKeyRepositoryImpl) GetUserKeys(userID uuid.UUID) ([]*domain.APIKey, error) {
	var keys []*domain.APIKey
	err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// Revoke marks an API key as revoked
func (r *apiKeyRepositoryImpl) Revoke(id uuid.UUID) error {
	return r.db.Model(&domain.APIKey{}).Where("id = ?", id).Update("revoked", true).Error
}

// UpdateLastUsed updates the last used timestamp for an API key
func (r *apiKeyRepositoryImpl) UpdateLastUsed(id uuid.UUID) error {
	now := time.Now()
	return r.db.Model(&domain.APIKey{}).Where("id = ?", id).Update("last_used_at", &now).Error
}
