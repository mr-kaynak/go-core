package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

type categoryRepositoryImpl struct {
	db *gorm.DB
}

// NewCategoryRepository creates a new CategoryRepository
func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepositoryImpl{db: db}
}

func (r *categoryRepositoryImpl) Create(category *domain.Category) error {
	return r.db.Create(category).Error
}

func (r *categoryRepositoryImpl) Update(category *domain.Category) error {
	return r.db.Save(category).Error
}

func (r *categoryRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Category{}, id).Error
}

func (r *categoryRepositoryImpl) GetByID(id uuid.UUID) (*domain.Category, error) {
	var category domain.Category
	err := r.db.First(&category, id).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepositoryImpl) GetBySlug(slug string) (*domain.Category, error) {
	var category domain.Category
	err := r.db.Where("slug = ?", slug).First(&category).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepositoryImpl) GetAll() ([]*domain.Category, error) {
	var categories []*domain.Category
	err := r.db.Order("sort_order ASC, name ASC").Find(&categories).Error
	return categories, err
}

func (r *categoryRepositoryImpl) GetTree() ([]*domain.Category, error) {
	var categories []*domain.Category
	err := r.db.Where("parent_id IS NULL").
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, name ASC")
		}).
		Order("sort_order ASC, name ASC").
		Find(&categories).Error
	return categories, err
}

func (r *categoryRepositoryImpl) ExistsBySlug(slug string) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Category{}).Where("slug = ?", slug).Count(&count).Error
	return count > 0, err
}

func (r *categoryRepositoryImpl) ExistsBySlugExcluding(slug string, excludeID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Category{}).Where("slug = ? AND id != ?", slug, excludeID).Count(&count).Error
	return count > 0, err
}

func (r *categoryRepositoryImpl) Count() (int64, error) {
	var count int64
	err := r.db.Model(&domain.Category{}).Count(&count).Error
	return count, err
}
