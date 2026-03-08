package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// TagRepository defines the interface for tag data operations
type TagRepository interface {
	WithTx(tx *gorm.DB) TagRepository
	Create(tag *domain.Tag) error
	Update(tag *domain.Tag) error
	Delete(id uuid.UUID) error
	GetByID(id uuid.UUID) (*domain.Tag, error)
	GetBySlug(slug string) (*domain.Tag, error)
	GetAll(offset, limit int) ([]*domain.Tag, int64, error)
	GetPopular(limit int) ([]*domain.Tag, error)
	GetOrCreateByNames(names []string, slugFn func(string) string) ([]*domain.Tag, error)
	ExistsBySlug(slug string) (bool, error)
}
