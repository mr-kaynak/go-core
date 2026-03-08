package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// VerificationTokenRepository defines the interface for verification token operations
type VerificationTokenRepository interface {
	// WithTx returns a new repository instance that uses the given transaction
	WithTx(tx *gorm.DB) VerificationTokenRepository

	Create(token *domain.VerificationToken) error
	FindByToken(token string) (*domain.VerificationToken, error)
	FindByUserAndType(userID uuid.UUID, tokenType domain.TokenType) (*domain.VerificationToken, error)
	Update(token *domain.VerificationToken) error
	Delete(id uuid.UUID) error
	DeleteExpiredTokens() error
	DeleteByUserAndType(userID uuid.UUID, tokenType domain.TokenType) error
	CountByUserAndType(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error)
}
