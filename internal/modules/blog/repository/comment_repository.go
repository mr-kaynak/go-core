package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// CommentRepository defines the interface for comment data operations
type CommentRepository interface {
	WithTx(tx *gorm.DB) CommentRepository
	Create(ctx context.Context, comment *domain.Comment) error
	Update(ctx context.Context, comment *domain.Comment) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error)
	GetThreaded(ctx context.Context, postID uuid.UUID) ([]*domain.Comment, error)
	CountByPost(ctx context.Context, postID uuid.UUID) (int64, error)
	ListPending(ctx context.Context, offset, limit int) ([]*domain.Comment, int64, error)
}
