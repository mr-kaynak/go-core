package repository

import (
	"context"
	"time"

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

	// Cursor-based pagination (takes precedence over Offset when set)
	CursorPublishedAt *time.Time
	CursorID          *uuid.UUID
}

// PostRepository defines the interface for post data operations
type PostRepository interface {
	WithTx(tx *gorm.DB) PostRepository
	Create(ctx context.Context, post *domain.Post) error
	Update(ctx context.Context, post *domain.Post) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Post, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Post, error)
	GetBySlugPublished(ctx context.Context, slug string) (*domain.Post, error)
	ListFiltered(ctx context.Context, filter PostListFilter) ([]*domain.Post, int64, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	ExistsBySlugExcluding(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	CountByStatus(ctx context.Context, status string) (int64, error)

	// Revisions
	CreateRevision(ctx context.Context, revision *domain.PostRevision) error
	ListRevisions(ctx context.Context, postID uuid.UUID, offset, limit int) ([]*domain.PostRevision, int64, error)
	GetRevision(ctx context.Context, id uuid.UUID) (*domain.PostRevision, error)
	GetLatestRevisionVersion(ctx context.Context, postID uuid.UUID) (int, error)

	// Media
	CreateMedia(ctx context.Context, media *domain.PostMedia) error
	DeleteMedia(ctx context.Context, id uuid.UUID) error
	GetMediaByID(ctx context.Context, id uuid.UUID) (*domain.PostMedia, error)
	ListMediaByPost(ctx context.Context, postID uuid.UUID) ([]*domain.PostMedia, error)

	// Tags
	ReplaceTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error
}
