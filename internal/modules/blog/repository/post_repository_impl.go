package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

const maxMediaPerPost = 200

var postAllowedSortFields = map[string]bool{
	"created_at":   true,
	"updated_at":   true,
	"published_at": true,
	"title":        true,
}

type postRepositoryImpl struct {
	db *gorm.DB
}

// NewPostRepository creates a new PostRepository
func NewPostRepository(db *gorm.DB) PostRepository {
	return &postRepositoryImpl{db: db}
}

func (r *postRepositoryImpl) WithTx(tx *gorm.DB) PostRepository {
	if tx == nil {
		return r
	}
	return &postRepositoryImpl{db: tx}
}

func (r *postRepositoryImpl) Create(ctx context.Context, post *domain.Post) error {
	db := r.db.WithContext(ctx)
	return db.Create(post).Error
}

func (r *postRepositoryImpl) Update(ctx context.Context, post *domain.Post) error {
	db := r.db.WithContext(ctx)
	return db.Model(post).
		Omit("DeletedAt", "CreatedAt").
		Save(post).Error
}

func (r *postRepositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.Post{}, id).Error
}

func (r *postRepositoryImpl) GetByID(ctx context.Context, id uuid.UUID) (*domain.Post, error) {
	db := r.db.WithContext(ctx)
	var post domain.Post
	err := db.Preload("Category").Preload("Tags").Preload("Stats").First(&post, id).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *postRepositoryImpl) GetBySlug(ctx context.Context, slug string) (*domain.Post, error) {
	db := r.db.WithContext(ctx)
	var post domain.Post
	err := db.Preload("Category").Preload("Tags").Preload("Stats").
		Where("slug = ?", slug).First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *postRepositoryImpl) GetBySlugPublished(ctx context.Context, slug string) (*domain.Post, error) {
	db := r.db.WithContext(ctx)
	var post domain.Post
	err := db.Preload("Category").Preload("Tags").Preload("Stats").
		Where("slug = ? AND status = ?", slug, domain.PostStatusPublished).First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *postRepositoryImpl) ListFiltered(ctx context.Context, filter PostListFilter) ([]*domain.Post, int64, error) {
	db := r.db.WithContext(ctx)
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}

	query := db.Model(&domain.Post{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.AuthorID != nil {
		query = query.Where("author_id = ?", *filter.AuthorID)
	}
	if filter.CategoryID != nil {
		query = query.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.Search != "" {
		query = query.Where(
			"to_tsvector('english', coalesce(content_plain, '')) @@ plainto_tsquery('english', ?) OR title ILIKE ?",
			filter.Search, "%"+filter.Search+"%",
		)
	}
	if filter.IsFeatured != nil {
		query = query.Where("is_featured = ?", *filter.IsFeatured)
	}
	if len(filter.TagSlugs) > 0 {
		postTagTable := domain.PostTag{}.TableName()
		tagTable := domain.Tag{}.TableName()
		query = query.Where("id IN (?)",
			db.Table(postTagTable).
				Select("post_id").
				Joins("JOIN "+tagTable+" ON "+tagTable+".id = "+postTagTable+".tag_id").
				Where(tagTable+".slug IN ?", filter.TagSlugs),
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Sort
	sortBy := "created_at"
	if filter.SortBy != "" && postAllowedSortFields[filter.SortBy] {
		sortBy = filter.SortBy
	}
	order := "DESC"
	if filter.Order == "asc" {
		order = "ASC"
	}

	// Cursor-based keyset pagination (takes precedence over offset)
	if filter.CursorPublishedAt != nil && filter.CursorID != nil {
		if order == "DESC" {
			query = query.Where(
				"("+sortBy+", id) < (?, ?)",
				*filter.CursorPublishedAt, *filter.CursorID,
			)
		} else {
			query = query.Where(
				"("+sortBy+", id) > (?, ?)",
				*filter.CursorPublishedAt, *filter.CursorID,
			)
		}
	}

	query = query.Order(sortBy + " " + order + ", id " + order)

	var posts []*domain.Post
	if filter.CursorPublishedAt == nil {
		query = query.Offset(filter.Offset)
	}
	err := query.Preload("Category").Preload("Tags").Preload("Stats").
		Limit(filter.Limit).Find(&posts).Error
	return posts, total, err
}

func (r *postRepositoryImpl) CountByStatus(ctx context.Context, status string) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	query := db.Model(&domain.Post{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err := query.Count(&count).Error
	return count, err
}

func (r *postRepositoryImpl) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.Post{}).Where("slug = ?", slug).Count(&count).Error
	return count > 0, err
}

func (r *postRepositoryImpl) ExistsBySlugExcluding(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.Post{}).Where("slug = ? AND id != ?", slug, excludeID).Count(&count).Error
	return count > 0, err
}

// Revisions

func (r *postRepositoryImpl) CreateRevision(ctx context.Context, revision *domain.PostRevision) error {
	db := r.db.WithContext(ctx)
	return db.Create(revision).Error
}

func (r *postRepositoryImpl) ListRevisions(
	ctx context.Context, postID uuid.UUID, offset, limit int,
) ([]*domain.PostRevision, int64, error) {
	db := r.db.WithContext(ctx)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := db.Model(&domain.PostRevision{}).Where("post_id = ?", postID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var revisions []*domain.PostRevision
	err := query.Order("version DESC").Offset(offset).Limit(limit).Find(&revisions).Error
	return revisions, total, err
}

func (r *postRepositoryImpl) GetRevision(ctx context.Context, id uuid.UUID) (*domain.PostRevision, error) {
	db := r.db.WithContext(ctx)
	var revision domain.PostRevision
	err := db.First(&revision, id).Error
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *postRepositoryImpl) GetLatestRevisionVersion(ctx context.Context, postID uuid.UUID) (int, error) {
	db := r.db.WithContext(ctx)
	var version int
	err := db.Model(&domain.PostRevision{}).
		Where("post_id = ?", postID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error
	return version, err
}

// Media

func (r *postRepositoryImpl) CreateMedia(ctx context.Context, media *domain.PostMedia) error {
	db := r.db.WithContext(ctx)
	return db.Create(media).Error
}

func (r *postRepositoryImpl) DeleteMedia(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.PostMedia{}, id).Error
}

func (r *postRepositoryImpl) GetMediaByID(ctx context.Context, id uuid.UUID) (*domain.PostMedia, error) {
	db := r.db.WithContext(ctx)
	var media domain.PostMedia
	err := db.First(&media, id).Error
	if err != nil {
		return nil, err
	}
	return &media, nil
}

func (r *postRepositoryImpl) ListMediaByPost(ctx context.Context, postID uuid.UUID) ([]*domain.PostMedia, error) {
	db := r.db.WithContext(ctx)
	var media []*domain.PostMedia
	err := db.Where("post_id = ?", postID).Order("created_at DESC").Limit(maxMediaPerPost).Find(&media).Error
	return media, err
}

// Tags

func (r *postRepositoryImpl) ReplaceTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		// Delete existing associations
		if err := tx.Where("post_id = ?", postID).Delete(&domain.PostTag{}).Error; err != nil {
			return err
		}

		// Create new associations
		if len(tagIDs) == 0 {
			return nil
		}
		postTags := make([]domain.PostTag, len(tagIDs))
		for i, tagID := range tagIDs {
			postTags[i] = domain.PostTag{PostID: postID, TagID: tagID}
		}
		return tx.Create(&postTags).Error
	})
}
