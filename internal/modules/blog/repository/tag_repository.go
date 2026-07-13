package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// TagRepository defines the interface for tag data operations
type TagRepository interface {
	WithTx(tx *gorm.DB) TagRepository
	Create(ctx context.Context, tag *domain.Tag) error
	Update(ctx context.Context, tag *domain.Tag) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Tag, error)
	GetAll(ctx context.Context, offset, limit int) ([]*domain.Tag, int64, error)
	GetPopular(ctx context.Context, limit int) ([]*domain.Tag, error)
	GetOrCreateByNames(ctx context.Context, names []string, slugFn func(string) string) ([]*domain.Tag, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
}
