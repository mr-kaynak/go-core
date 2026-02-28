package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

type tagRepositoryImpl struct {
	db *gorm.DB
}

// NewTagRepository creates a new TagRepository
func NewTagRepository(db *gorm.DB) TagRepository {
	return &tagRepositoryImpl{db: db}
}

func (r *tagRepositoryImpl) Create(tag *domain.Tag) error {
	return r.db.Create(tag).Error
}

func (r *tagRepositoryImpl) Update(tag *domain.Tag) error {
	return r.db.Save(tag).Error
}

func (r *tagRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Tag{}, id).Error
}

func (r *tagRepositoryImpl) GetByID(id uuid.UUID) (*domain.Tag, error) {
	var tag domain.Tag
	err := r.db.First(&tag, id).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *tagRepositoryImpl) GetBySlug(slug string) (*domain.Tag, error) {
	var tag domain.Tag
	err := r.db.Where("slug = ?", slug).First(&tag).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *tagRepositoryImpl) GetAll(offset, limit int) ([]*domain.Tag, int64, error) {
	var total int64
	if err := r.db.Model(&domain.Tag{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tags []*domain.Tag
	err := r.db.Order("name ASC").Offset(offset).Limit(limit).Find(&tags).Error
	return tags, total, err
}

func (r *tagRepositoryImpl) GetPopular(limit int) ([]*domain.Tag, error) {
	var tags []*domain.Tag
	err := r.db.
		Select("blog_tags.*, COUNT(post_tags.post_id) as post_count").
		Joins("LEFT JOIN post_tags ON post_tags.tag_id = blog_tags.id").
		Group("blog_tags.id").
		Order("post_count DESC").
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func (r *tagRepositoryImpl) GetOrCreateByNames(names []string, slugFn func(string) string) ([]*domain.Tag, error) {
	var tags []*domain.Tag
	for _, name := range names {
		slug := slugFn(name)
		var tag domain.Tag
		result := r.db.Where("slug = ?", slug).FirstOrCreate(&tag, domain.Tag{
			ID:   uuid.New(),
			Name: name,
			Slug: slug,
		})
		if result.Error != nil {
			return nil, result.Error
		}
		tags = append(tags, &tag)
	}
	return tags, nil
}

func (r *tagRepositoryImpl) ExistsBySlug(slug string) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Tag{}).Where("slug = ?", slug).Count(&count).Error
	return count > 0, err
}
