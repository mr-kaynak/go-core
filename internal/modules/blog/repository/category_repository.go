package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// CategoryRepository defines the interface for category data operations
type CategoryRepository interface {
	WithTx(tx *gorm.DB) CategoryRepository
	Create(ctx context.Context, category *domain.Category) error
	Update(ctx context.Context, category *domain.Category) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Category, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Category, error)
	GetAll(ctx context.Context) ([]*domain.Category, error)
	GetTree(ctx context.Context) ([]*domain.Category, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	ExistsBySlugExcluding(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	HasChildren(ctx context.Context, id uuid.UUID) (bool, error)
	HasPosts(ctx context.Context, id uuid.UUID) (bool, error)
	Count(ctx context.Context) (int64, error)
	GetAncestorIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)
}
