package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type tagRepositoryImpl struct {
	db *gorm.DB
}

// NewTagRepository creates a new TagRepository
func NewTagRepository(db *gorm.DB) TagRepository {
	return &tagRepositoryImpl{db: db}
}

func (r *tagRepositoryImpl) WithTx(tx *gorm.DB) TagRepository {
	if tx == nil {
		return r
	}
	return &tagRepositoryImpl{db: tx}
}

func (r *tagRepositoryImpl) Create(ctx context.Context, tag *domain.Tag) error {
	db := r.db.WithContext(ctx)
	return db.Create(tag).Error
}

func (r *tagRepositoryImpl) Update(ctx context.Context, tag *domain.Tag) error {
	db := r.db.WithContext(ctx)
	return db.Save(tag).Error
}

func (r *tagRepositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.Tag{}, id).Error
}

func (r *tagRepositoryImpl) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error) {
	db := r.db.WithContext(ctx)
	var tag domain.Tag
	err := db.First(&tag, id).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *tagRepositoryImpl) GetBySlug(ctx context.Context, slug string) (*domain.Tag, error) {
	db := r.db.WithContext(ctx)
	var tag domain.Tag
	err := db.Where("slug = ?", slug).First(&tag).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *tagRepositoryImpl) GetAll(ctx context.Context, offset, limit int) ([]*domain.Tag, int64, error) {
	db := r.db.WithContext(ctx)
	var total int64
	if err := db.Model(&domain.Tag{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tags []*domain.Tag
	err := db.Order("name ASC").Offset(offset).Limit(limit).Find(&tags).Error
	return tags, total, err
}

func (r *tagRepositoryImpl) GetPopular(ctx context.Context, limit int) ([]*domain.Tag, error) {
	db := r.db.WithContext(ctx)
	var tags []*domain.Tag
	err := db.
		Select("blog_tags.*, COUNT(post_tags.post_id) as post_count").
		Joins("LEFT JOIN post_tags ON post_tags.tag_id = blog_tags.id").
		Group("blog_tags.id").
		Order("post_count DESC").
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func (r *tagRepositoryImpl) GetOrCreateByNames(
	ctx context.Context, names []string, slugFn func(string) string,
) ([]*domain.Tag, error) {
	db := r.db.WithContext(ctx)
	if len(names) == 0 {
		return nil, nil
	}

	// Batch insert all tags, ignoring conflicts with existing slugs
	newTags := make([]*domain.Tag, 0, len(names))
	for _, name := range names {
		newTags = append(newTags, &domain.Tag{
			ID:   uuid.New(),
			Name: name,
			Slug: slugFn(name),
		})
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&newTags).Error; err != nil {
		return nil, fmt.Errorf("batch insert tags: %w", err)
	}

	// Fetch all tags by slug in a single query
	slugs := make([]string, len(names))
	for i, name := range names {
		slugs[i] = slugFn(name)
	}
	var tags []*domain.Tag
	if err := db.Where("slug IN ?", slugs).Find(&tags).Error; err != nil {
		return nil, fmt.Errorf("fetch tags by slug: %w", err)
	}
	return tags, nil
}

func (r *tagRepositoryImpl) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.Tag{}).Where("slug = ?", slug).Count(&count).Error
	return count > 0, err
}
