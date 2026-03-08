package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// CommentRepository defines the interface for comment data operations
type CommentRepository interface {
	WithTx(tx *gorm.DB) CommentRepository
	Create(comment *domain.Comment) error
	Update(comment *domain.Comment) error
	Delete(id uuid.UUID) error
	GetByID(id uuid.UUID) (*domain.Comment, error)
	GetThreaded(postID uuid.UUID) ([]*domain.Comment, error)
	CountByPost(postID uuid.UUID) (int64, error)
	ListPending(offset, limit int) ([]*domain.Comment, int64, error)
}
