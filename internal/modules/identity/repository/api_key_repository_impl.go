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

// GetByHashWithRoles retrieves an API key by its hash with roles and permissions preloaded
func (r *apiKeyRepositoryImpl) GetByHashWithRoles(keyHash string) (*domain.APIKey, error) {
	var apiKey domain.APIKey
	err := r.db.Preload("Roles.Permissions").Where("key_hash = ?", keyHash).First(&apiKey).Error
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

// GetByIDWithRoles retrieves an API key by its ID with roles and permissions preloaded
func (r *apiKeyRepositoryImpl) GetByIDWithRoles(id uuid.UUID) (*domain.APIKey, error) {
	var apiKey domain.APIKey
	err := r.db.Preload("Roles.Permissions").First(&apiKey, id).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetUserKeys retrieves all API keys for a specific user
func (r *apiKeyRepositoryImpl) GetUserKeys(userID uuid.UUID) ([]*domain.APIKey, error) {
	var keys []*domain.APIKey
	err := r.db.Preload("Roles").Where("user_id = ? AND revoked = ?", userID, false).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// GetUserKeysPaginated retrieves paginated API keys for a specific user and total count.
func (r *apiKeyRepositoryImpl) GetUserKeysPaginated(userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error) {
	var (
		keys  []*domain.APIKey
		total int64
	)

	base := r.db.Model(&domain.APIKey{}).Where("user_id = ? AND revoked = ?", userID, false)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := r.db.Preload("Roles").
		Where("user_id = ? AND revoked = ?", userID, false).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&keys).Error
	if err != nil {
		return nil, 0, err
	}

	return keys, total, nil
}

// GetAll retrieves all API keys with pagination and total count
func (r *apiKeyRepositoryImpl) GetAll(offset, limit int) ([]*domain.APIKey, int64, error) {
	var (
		keys  []*domain.APIKey
		total int64
	)

	if err := r.db.Model(&domain.APIKey{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := r.db.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&keys).Error
	if err != nil {
		return nil, 0, err
	}

	return keys, total, nil
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

// CleanupRevokedKeys soft-deletes revoked keys older than the given duration and expired keys
func (r *apiKeyRepositoryImpl) CleanupRevokedKeys(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return r.db.Where("(revoked = ? AND updated_at < ?) OR (expires_at IS NOT NULL AND expires_at < ?)",
		true, cutoff, time.Now()).Delete(&domain.APIKey{}).Error
}

// AssignRole assigns a role to an API key
func (r *apiKeyRepositoryImpl) AssignRole(apiKeyID, roleID uuid.UUID) error {
	join := domain.APIKeyRole{
		APIKeyID: apiKeyID,
		RoleID:   roleID,
	}
	return r.db.Create(&join).Error
}

// RemoveRole removes a role from an API key
func (r *apiKeyRepositoryImpl) RemoveRole(apiKeyID, roleID uuid.UUID) error {
	return r.db.Where("api_key_id = ? AND role_id = ?", apiKeyID, roleID).
		Delete(&domain.APIKeyRole{}).Error
}
