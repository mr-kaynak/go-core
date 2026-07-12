package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// VerificationTokenRepository defines the interface for verification token operations
type VerificationTokenRepository interface {
	// WithTx returns a new repository instance that uses the given transaction
	WithTx(tx *gorm.DB) VerificationTokenRepository

	Create(ctx context.Context, token *domain.VerificationToken) error
	FindByToken(ctx context.Context, token string) (*domain.VerificationToken, error)
	FindByUserAndType(ctx context.Context, userID uuid.UUID, tokenType domain.TokenType) (*domain.VerificationToken, error)
	Update(ctx context.Context, token *domain.VerificationToken) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteExpiredTokens(ctx context.Context) error
	DeleteByUserAndType(ctx context.Context, userID uuid.UUID, tokenType domain.TokenType) error
	CountByUserAndType(ctx context.Context, userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error)
}
