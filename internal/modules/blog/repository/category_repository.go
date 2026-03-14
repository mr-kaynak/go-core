package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// CategoryRepository defines the interface for category data operations
type CategoryRepository interface {
	WithTx(tx *gorm.DB) CategoryRepository
	Create(category *domain.Category) error
	Update(category *domain.Category) error
	Delete(id uuid.UUID) error
	GetByID(id uuid.UUID) (*domain.Category, error)
	GetBySlug(slug string) (*domain.Category, error)
	GetAll() ([]*domain.Category, error)
	GetTree() ([]*domain.Category, error)
	ExistsBySlug(slug string) (bool, error)
	ExistsBySlugExcluding(slug string, excludeID uuid.UUID) (bool, error)
	HasChildren(id uuid.UUID) (bool, error)
	HasPosts(id uuid.UUID) (bool, error)
	Count() (int64, error)
	GetAncestorIDs(id uuid.UUID) ([]uuid.UUID, error)
}
