package repository

import (
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

func (r *postRepositoryImpl) Create(post *domain.Post) error {
	return r.db.Create(post).Error
}

func (r *postRepositoryImpl) Update(post *domain.Post) error {
	return r.db.Save(post).Error
}

func (r *postRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Post{}, id).Error
}

func (r *postRepositoryImpl) GetByID(id uuid.UUID) (*domain.Post, error) {
	var post domain.Post
	err := r.db.Preload("Category").Preload("Tags").Preload("Stats").First(&post, id).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *postRepositoryImpl) GetBySlug(slug string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.Preload("Category").Preload("Tags").Preload("Stats").
		Where("slug = ?", slug).First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *postRepositoryImpl) GetBySlugPublished(slug string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.Preload("Category").Preload("Tags").Preload("Stats").
		Where("slug = ? AND status = ?", slug, domain.PostStatusPublished).First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *postRepositoryImpl) ListFiltered(filter PostListFilter) ([]*domain.Post, int64, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}

	query := r.db.Model(&domain.Post{})

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
		query = query.Where("id IN (?)",
			r.db.Table("post_tags").
				Select("post_id").
				Joins("JOIN blog_tags ON blog_tags.id = post_tags.tag_id").
				Where("blog_tags.slug IN ?", filter.TagSlugs),
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
	query = query.Order(sortBy + " " + order)

	var posts []*domain.Post
	err := query.Preload("Category").Preload("Tags").Preload("Stats").
		Offset(filter.Offset).Limit(filter.Limit).Find(&posts).Error
	return posts, total, err
}

func (r *postRepositoryImpl) CountByStatus(status string) (int64, error) {
	var count int64
	query := r.db.Model(&domain.Post{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err := query.Count(&count).Error
	return count, err
}

func (r *postRepositoryImpl) ExistsBySlug(slug string) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Post{}).Where("slug = ?", slug).Count(&count).Error
	return count > 0, err
}

func (r *postRepositoryImpl) ExistsBySlugExcluding(slug string, excludeID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Post{}).Where("slug = ? AND id != ?", slug, excludeID).Count(&count).Error
	return count > 0, err
}

// Revisions

func (r *postRepositoryImpl) CreateRevision(revision *domain.PostRevision) error {
	return r.db.Create(revision).Error
}

func (r *postRepositoryImpl) ListRevisions(postID uuid.UUID) ([]*domain.PostRevision, error) {
	var revisions []*domain.PostRevision
	err := r.db.Where("post_id = ?", postID).Order("version DESC").Limit(100).Find(&revisions).Error
	return revisions, err
}

func (r *postRepositoryImpl) GetRevision(id uuid.UUID) (*domain.PostRevision, error) {
	var revision domain.PostRevision
	err := r.db.First(&revision, id).Error
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *postRepositoryImpl) GetLatestRevisionVersion(postID uuid.UUID) (int, error) {
	var version int
	err := r.db.Model(&domain.PostRevision{}).
		Where("post_id = ?", postID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error
	return version, err
}

// Media

func (r *postRepositoryImpl) CreateMedia(media *domain.PostMedia) error {
	return r.db.Create(media).Error
}

func (r *postRepositoryImpl) DeleteMedia(id uuid.UUID) error {
	return r.db.Delete(&domain.PostMedia{}, id).Error
}

func (r *postRepositoryImpl) GetMediaByID(id uuid.UUID) (*domain.PostMedia, error) {
	var media domain.PostMedia
	err := r.db.First(&media, id).Error
	if err != nil {
		return nil, err
	}
	return &media, nil
}

func (r *postRepositoryImpl) ListMediaByPost(postID uuid.UUID) ([]*domain.PostMedia, error) {
	var media []*domain.PostMedia
	err := r.db.Where("post_id = ?", postID).Order("created_at DESC").Limit(maxMediaPerPost).Find(&media).Error
	return media, err
}

// Tags

func (r *postRepositoryImpl) ReplaceTags(postID uuid.UUID, tagIDs []uuid.UUID) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
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
