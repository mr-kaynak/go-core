package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// PostListFilter contains filters for listing posts
type PostListFilter struct {
	Offset     int
	Limit      int
	SortBy     string
	Order      string
	Search     string
	Status     string
	AuthorID   *uuid.UUID
	CategoryID *uuid.UUID
	TagSlugs   []string
	IsFeatured *bool
}

// PostRepository defines the interface for post data operations
type PostRepository interface {
	WithTx(tx *gorm.DB) PostRepository
	Create(post *domain.Post) error
	Update(post *domain.Post) error
	Delete(id uuid.UUID) error
	GetByID(id uuid.UUID) (*domain.Post, error)
	GetBySlug(slug string) (*domain.Post, error)
	GetBySlugPublished(slug string) (*domain.Post, error)
	ListFiltered(filter PostListFilter) ([]*domain.Post, int64, error)
	ExistsBySlug(slug string) (bool, error)
	ExistsBySlugExcluding(slug string, excludeID uuid.UUID) (bool, error)
	CountByStatus(status string) (int64, error)

	// Revisions
	CreateRevision(revision *domain.PostRevision) error
	ListRevisions(postID uuid.UUID, offset, limit int) ([]*domain.PostRevision, int64, error)
	GetRevision(id uuid.UUID) (*domain.PostRevision, error)
	GetLatestRevisionVersion(postID uuid.UUID) (int, error)

	// Media
	CreateMedia(media *domain.PostMedia) error
	DeleteMedia(id uuid.UUID) error
	GetMediaByID(id uuid.UUID) (*domain.PostMedia, error)
	ListMediaByPost(postID uuid.UUID) ([]*domain.PostMedia, error)

	// Tags
	ReplaceTags(postID uuid.UUID, tagIDs []uuid.UUID) error
}
