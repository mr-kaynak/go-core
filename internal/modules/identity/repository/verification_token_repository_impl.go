package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// verificationTokenRepositoryImpl implements VerificationTokenRepository using GORM
type verificationTokenRepositoryImpl struct {
	db *gorm.DB
}

// NewVerificationTokenRepository creates a new verification token repository
func NewVerificationTokenRepository(db *gorm.DB) VerificationTokenRepository {
	return &verificationTokenRepositoryImpl{db: db}
}

// WithTx returns a new repository instance that uses the given transaction
func (r *verificationTokenRepositoryImpl) WithTx(tx *gorm.DB) VerificationTokenRepository {
	if tx == nil {
		return r
	}
	return &verificationTokenRepositoryImpl{db: tx}
}

// Create creates a new verification token
func (r *verificationTokenRepositoryImpl) Create(ctx context.Context, token *domain.VerificationToken) error {
	db := r.db.WithContext(ctx)
	return db.Create(token).Error
}

// FindByToken finds a token by its raw token string (hashes before lookup).
func (r *verificationTokenRepositoryImpl) FindByToken(ctx context.Context, tokenStr string) (*domain.VerificationToken, error) {
	db := r.db.WithContext(ctx)
	tokenHash := domain.HashVerificationToken(tokenStr)
	var token domain.VerificationToken
	err := db.Preload("User").Where("token = ? AND deleted_at IS NULL", tokenHash).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// FindByUserAndType finds a token by user ID and type
func (r *verificationTokenRepositoryImpl) FindByUserAndType(
	ctx context.Context, userID uuid.UUID, tokenType domain.TokenType,
) (*domain.VerificationToken, error) {
	db := r.db.WithContext(ctx)
	var token domain.VerificationToken
	err := db.Where("user_id = ? AND type = ? AND used = ? AND deleted_at IS NULL", userID, tokenType, false).
		Order("created_at DESC").
		First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// Update updates a verification token
func (r *verificationTokenRepositoryImpl) Update(ctx context.Context, token *domain.VerificationToken) error {
	db := r.db.WithContext(ctx)
	return db.Save(token).Error
}

// Delete soft deletes a verification token
func (r *verificationTokenRepositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.VerificationToken{}, id).Error
}

// DeleteExpiredTokens deletes all expired tokens
func (r *verificationTokenRepositoryImpl) DeleteExpiredTokens(ctx context.Context) error {
	db := r.db.WithContext(ctx)
	return db.Where("expires_at < ?", time.Now()).Delete(&domain.VerificationToken{}).Error
}

// DeleteByUserAndType deletes all tokens for a user of a specific type
func (r *verificationTokenRepositoryImpl) DeleteByUserAndType(ctx context.Context, userID uuid.UUID, tokenType domain.TokenType) error {
	db := r.db.WithContext(ctx)
	return db.Where("user_id = ? AND type = ?", userID, tokenType).Delete(&domain.VerificationToken{}).Error
}

// CountByUserAndType counts tokens created by a user of a specific type since a given time
func (r *verificationTokenRepositoryImpl) CountByUserAndType(
	ctx context.Context, userID uuid.UUID, tokenType domain.TokenType, since time.Time,
) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.VerificationToken{}).
		Where("user_id = ? AND type = ? AND created_at >= ?", userID, tokenType, since).
		Count(&count).Error
	return count, err
}
