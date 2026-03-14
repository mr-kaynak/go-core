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

func (r *categoryRepositoryImpl) WithTx(tx *gorm.DB) CategoryRepository {
	if tx == nil {
		return r
	}
	return &categoryRepositoryImpl{db: tx}
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
	// Fetch all categories in a single query and assemble the tree in-memory.
	// Previous implementation only Preloaded one level of children; this
	// supports arbitrary depth without N+1 queries.
	var all []*domain.Category
	err := r.db.Order("sort_order ASC, name ASC").Find(&all).Error
	if err != nil {
		return nil, err
	}

	byID := make(map[uuid.UUID]*domain.Category, len(all))
	for _, c := range all {
		c.Children = nil // reset to avoid stale data
		byID[c.ID] = c
	}

	var roots []*domain.Category
	for _, c := range all {
		if c.ParentID == nil {
			roots = append(roots, c)
		} else if parent, ok := byID[*c.ParentID]; ok {
			parent.Children = append(parent.Children, *c)
		}
	}
	return roots, nil
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

func (r *categoryRepositoryImpl) HasChildren(id uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Category{}).Where("parent_id = ?", id).Count(&count).Error
	return count > 0, err
}

func (r *categoryRepositoryImpl) HasPosts(id uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Post{}).Where("category_id = ?", id).Count(&count).Error
	return count > 0, err
}

func (r *categoryRepositoryImpl) Count() (int64, error) {
	var count int64
	err := r.db.Model(&domain.Category{}).Count(&count).Error
	return count, err
}

// GetAncestorIDs returns all ancestor IDs from the given category up to the root
// using a single recursive CTE query instead of N+1 individual lookups.
func (r *categoryRepositoryImpl) GetAncestorIDs(id uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.Raw(`
		WITH RECURSIVE ancestors AS (
			SELECT parent_id FROM blog_categories WHERE id = ?
			UNION ALL
			SELECT c.parent_id FROM blog_categories c
			INNER JOIN ancestors a ON c.id = a.parent_id
			WHERE a.parent_id IS NOT NULL
		)
		SELECT parent_id FROM ancestors WHERE parent_id IS NOT NULL
	`, id).Scan(&ids).Error
	return ids, err
}
