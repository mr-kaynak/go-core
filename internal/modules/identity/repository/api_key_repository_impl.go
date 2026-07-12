package repository

import (
	"context"
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
func (r *apiKeyRepositoryImpl) Create(ctx context.Context, apiKey *domain.APIKey) error {
	db := r.db.WithContext(ctx)
	return db.Create(apiKey).Error
}

// GetByHash retrieves an API key by its hash
func (r *apiKeyRepositoryImpl) GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	db := r.db.WithContext(ctx)
	var apiKey domain.APIKey
	err := db.Where("key_hash = ?", keyHash).First(&apiKey).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetByHashWithRoles retrieves an API key by its hash with roles and permissions preloaded
func (r *apiKeyRepositoryImpl) GetByHashWithRoles(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	db := r.db.WithContext(ctx)
	var apiKey domain.APIKey
	err := db.Preload("Roles.Permissions").Where("key_hash = ?", keyHash).First(&apiKey).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetByID retrieves an API key by its ID
func (r *apiKeyRepositoryImpl) GetByID(ctx context.Context, id uuid.UUID) (*domain.APIKey, error) {
	db := r.db.WithContext(ctx)
	var apiKey domain.APIKey
	err := db.First(&apiKey, id).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetByIDWithRoles retrieves an API key by its ID with roles and permissions preloaded
func (r *apiKeyRepositoryImpl) GetByIDWithRoles(ctx context.Context, id uuid.UUID) (*domain.APIKey, error) {
	db := r.db.WithContext(ctx)
	var apiKey domain.APIKey
	err := db.Preload("Roles.Permissions").First(&apiKey, id).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// GetUserKeys retrieves all API keys for a specific user
func (r *apiKeyRepositoryImpl) GetUserKeys(ctx context.Context, userID uuid.UUID) ([]*domain.APIKey, error) {
	db := r.db.WithContext(ctx)
	var keys []*domain.APIKey
	err := db.Preload("Roles").Where("user_id = ? AND revoked = ?", userID, false).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// GetUserKeysPaginated retrieves paginated API keys for a specific user and total count.
func (r *apiKeyRepositoryImpl) GetUserKeysPaginated(
	ctx context.Context, userID uuid.UUID, offset, limit int,
) ([]*domain.APIKey, int64, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var (
		keys  []*domain.APIKey
		total int64
	)

	base := db.Model(&domain.APIKey{}).Where("user_id = ? AND revoked = ?", userID, false)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := db.Preload("Roles").
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
func (r *apiKeyRepositoryImpl) GetAll(ctx context.Context, offset, limit int) ([]*domain.APIKey, int64, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var (
		keys  []*domain.APIKey
		total int64
	)

	if err := db.Model(&domain.APIKey{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := db.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&keys).Error
	if err != nil {
		return nil, 0, err
	}

	return keys, total, nil
}

// Revoke marks an API key as revoked
func (r *apiKeyRepositoryImpl) Revoke(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	result := db.Model(&domain.APIKey{}).Where("id = ?", id).Update("revoked", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateLastUsed updates the last used timestamp for an API key
func (r *apiKeyRepositoryImpl) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	now := time.Now()
	return db.Model(&domain.APIKey{}).Where("id = ?", id).Update("last_used_at", &now).Error
}

// CleanupRevokedKeys soft-deletes revoked keys older than the given duration and expired keys
func (r *apiKeyRepositoryImpl) CleanupRevokedKeys(ctx context.Context, olderThan time.Duration) error {
	db := r.db.WithContext(ctx)
	cutoff := time.Now().Add(-olderThan)
	return db.Where("(revoked = ? AND updated_at < ?) OR (expires_at IS NOT NULL AND expires_at < ?)",
		true, cutoff, time.Now()).Delete(&domain.APIKey{}).Error
}

// AssignRole assigns a role to an API key
func (r *apiKeyRepositoryImpl) AssignRole(ctx context.Context, apiKeyID, roleID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	join := domain.APIKeyRole{
		APIKeyID: apiKeyID,
		RoleID:   roleID,
	}
	return db.Create(&join).Error
}

// RemoveRole removes a role from an API key
func (r *apiKeyRepositoryImpl) RemoveRole(ctx context.Context, apiKeyID, roleID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("api_key_id = ? AND role_id = ?", apiKeyID, roleID).
		Delete(&domain.APIKeyRole{}).Error
}
