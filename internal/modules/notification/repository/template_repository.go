package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"gorm.io/gorm"
)

// TemplateRepository defines the interface for template operations
type TemplateRepository interface {
	// Template CRUD
	CreateTemplate(ctx context.Context, template *domain.ExtendedNotificationTemplate) error
	GetTemplateByID(ctx context.Context, id uuid.UUID) (*domain.ExtendedNotificationTemplate, error)
	GetTemplateByName(ctx context.Context, name string) (*domain.ExtendedNotificationTemplate, error)
	UpdateTemplate(ctx context.Context, template *domain.ExtendedNotificationTemplate) error
	DeleteTemplate(ctx context.Context, id uuid.UUID) error
	ListTemplates(
		ctx context.Context, filter ListTemplatesFilter, offset, limit int,
	) ([]*domain.ExtendedNotificationTemplate, int64, error)

	// Language variants
	CreateLanguageVariant(ctx context.Context, variant *domain.TemplateLanguage) error
	GetLanguageVariant(ctx context.Context, templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error)
	UpdateLanguageVariant(ctx context.Context, variant *domain.TemplateLanguage) error
	DeleteLanguageVariant(ctx context.Context, id uuid.UUID) error

	// Variables
	CreateVariable(ctx context.Context, variable *domain.TemplateVariable) error
	GetVariables(ctx context.Context, templateID uuid.UUID) ([]*domain.TemplateVariable, error)
	UpdateVariable(ctx context.Context, variable *domain.TemplateVariable) error
	DeleteVariable(ctx context.Context, id uuid.UUID) error

	// Categories
	CreateCategory(ctx context.Context, category *domain.TemplateCategory) error
	GetCategory(ctx context.Context, id uuid.UUID) (*domain.TemplateCategory, error)
	ListCategories(ctx context.Context) ([]*domain.TemplateCategory, error)
	UpdateCategory(ctx context.Context, category *domain.TemplateCategory) error
	DeleteCategory(ctx context.Context, id uuid.UUID) error

	// Category helpers
	CountTemplatesByCategory(ctx context.Context, categoryID uuid.UUID) (int64, error)

	// Bulk operations
	BulkUpdate(
		ctx context.Context, templateIDs []uuid.UUID, isActive *bool, categoryID *uuid.UUID,
	) (updated int, skipped []uuid.UUID, err error)

	// Usage tracking
	IncrementUsage(ctx context.Context, templateID uuid.UUID) error
	GetMostUsedTemplates(ctx context.Context, limit int) ([]*domain.ExtendedNotificationTemplate, error)
}

// ListTemplatesFilter holds typed filter parameters for listing templates.
type ListTemplatesFilter struct {
	CategoryID *uuid.UUID
	Type       string
	IsActive   *bool
	Search     string
}

// templateRepositoryImpl implements TemplateRepository
type templateRepositoryImpl struct {
	db *gorm.DB
}

// NewTemplateRepository creates a new template repository
func NewTemplateRepository(db *gorm.DB) TemplateRepository {
	return &templateRepositoryImpl{db: db}
}

// CreateTemplate creates a new template
func (r *templateRepositoryImpl) CreateTemplate(ctx context.Context, template *domain.ExtendedNotificationTemplate) error {
	db := r.db.WithContext(ctx)
	return db.Create(template).Error
}

// GetTemplateByID retrieves a template by ID with all relationships
func (r *templateRepositoryImpl) GetTemplateByID(ctx context.Context, id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var template domain.ExtendedNotificationTemplate
	err := db.Preload("Category").
		Preload("Languages").
		Preload("TemplateVariables").
		Where("id = ?", id).
		First(&template).Error

	if err != nil {
		return nil, err
	}
	return &template, nil
}

// GetTemplateByName retrieves a template by name with all relationships
func (r *templateRepositoryImpl) GetTemplateByName(
	ctx context.Context, name string,
) (*domain.ExtendedNotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var template domain.ExtendedNotificationTemplate
	err := db.Preload("Category").
		Preload("Languages").
		Preload("TemplateVariables").
		Where("name = ?", name).
		First(&template).Error

	if err != nil {
		return nil, err
	}
	return &template, nil
}

// UpdateTemplate updates an existing template
func (r *templateRepositoryImpl) UpdateTemplate(ctx context.Context, template *domain.ExtendedNotificationTemplate) error {
	db := r.db.WithContext(ctx)
	return db.Save(template).Error
}

// DeleteTemplate soft deletes a template
func (r *templateRepositoryImpl) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("id = ? AND is_system = false", id).Delete(&domain.ExtendedNotificationTemplate{}).Error
}

// ListTemplates lists templates with filters and pagination
func (r *templateRepositoryImpl) ListTemplates(
	ctx context.Context, filter ListTemplatesFilter, offset, limit int,
) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	db := r.db.WithContext(ctx)
	var templates []*domain.ExtendedNotificationTemplate
	var total int64

	query := db.Model(&domain.ExtendedNotificationTemplate{})

	if filter.CategoryID != nil {
		query = query.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.IsActive != nil {
		query = query.Where("is_active = ?", *filter.IsActive)
	}
	if filter.Search != "" {
		searchStr := "%" + filter.Search + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", searchStr, searchStr)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Preload("Category").
		Preload("Languages").
		Preload("TemplateVariables").
		Offset(offset).
		Limit(limit).
		Order("created_at DESC").
		Find(&templates).Error

	return templates, total, err
}

// CreateLanguageVariant creates a new language variant for a template
func (r *templateRepositoryImpl) CreateLanguageVariant(ctx context.Context, variant *domain.TemplateLanguage) error {
	db := r.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		// If this is marked as default, unset other defaults for this template
		if variant.IsDefault {
			if err := tx.Model(&domain.TemplateLanguage{}).
				Where("template_id = ? AND id != ?", variant.TemplateID, variant.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(variant).Error
	})
}

// GetLanguageVariant retrieves a specific language variant
func (r *templateRepositoryImpl) GetLanguageVariant(
	ctx context.Context, templateID uuid.UUID, languageCode string,
) (*domain.TemplateLanguage, error) {
	db := r.db.WithContext(ctx)
	var variant domain.TemplateLanguage
	err := db.Where("template_id = ? AND language_code = ?", templateID, languageCode).
		First(&variant).Error

	if err != nil {
		// Try to get the default variant
		err = db.Where("template_id = ? AND is_default = true", templateID).
			First(&variant).Error
		if err != nil {
			// Get any variant
			err = db.Where("template_id = ?", templateID).
				First(&variant).Error
		}
	}

	if err != nil {
		return nil, err
	}
	return &variant, nil
}

// UpdateLanguageVariant updates a language variant
func (r *templateRepositoryImpl) UpdateLanguageVariant(ctx context.Context, variant *domain.TemplateLanguage) error {
	db := r.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		// If this is marked as default, unset other defaults for this template
		if variant.IsDefault {
			if err := tx.Model(&domain.TemplateLanguage{}).
				Where("template_id = ? AND id != ?", variant.TemplateID, variant.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Save(variant).Error
	})
}

// DeleteLanguageVariant deletes a language variant
func (r *templateRepositoryImpl) DeleteLanguageVariant(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("id = ?", id).Delete(&domain.TemplateLanguage{}).Error
}

// CreateVariable creates a new template variable
func (r *templateRepositoryImpl) CreateVariable(ctx context.Context, variable *domain.TemplateVariable) error {
	db := r.db.WithContext(ctx)
	return db.Create(variable).Error
}

// GetVariables retrieves all variables for a template
func (r *templateRepositoryImpl) GetVariables(ctx context.Context, templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
	db := r.db.WithContext(ctx)
	var variables []*domain.TemplateVariable
	err := db.Where("template_id = ?", templateID).Find(&variables).Error
	return variables, err
}

// UpdateVariable updates a template variable
func (r *templateRepositoryImpl) UpdateVariable(ctx context.Context, variable *domain.TemplateVariable) error {
	db := r.db.WithContext(ctx)
	return db.Save(variable).Error
}

// DeleteVariable deletes a template variable
func (r *templateRepositoryImpl) DeleteVariable(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("id = ?", id).Delete(&domain.TemplateVariable{}).Error
}

// CreateCategory creates a new template category
func (r *templateRepositoryImpl) CreateCategory(ctx context.Context, category *domain.TemplateCategory) error {
	db := r.db.WithContext(ctx)
	return db.Create(category).Error
}

// GetCategory retrieves a category by ID
func (r *templateRepositoryImpl) GetCategory(ctx context.Context, id uuid.UUID) (*domain.TemplateCategory, error) {
	db := r.db.WithContext(ctx)
	var category domain.TemplateCategory
	err := db.Where("id = ?", id).First(&category).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

// ListCategories lists all categories
func (r *templateRepositoryImpl) ListCategories(ctx context.Context) ([]*domain.TemplateCategory, error) {
	db := r.db.WithContext(ctx)
	var categories []*domain.TemplateCategory
	err := db.Order("name ASC").Find(&categories).Error
	return categories, err
}

// UpdateCategory updates a category
func (r *templateRepositoryImpl) UpdateCategory(ctx context.Context, category *domain.TemplateCategory) error {
	db := r.db.WithContext(ctx)
	return db.Save(category).Error
}

// DeleteCategory soft deletes a category
func (r *templateRepositoryImpl) DeleteCategory(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("id = ?", id).Delete(&domain.TemplateCategory{}).Error
}

// CountTemplatesByCategory counts how many templates belong to a category
func (r *templateRepositoryImpl) CountTemplatesByCategory(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.ExtendedNotificationTemplate{}).Where("category_id = ?", categoryID).Count(&count).Error
	return count, err
}

// IncrementUsage increments the usage count for a template
func (r *templateRepositoryImpl) IncrementUsage(ctx context.Context, templateID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Model(&domain.ExtendedNotificationTemplate{}).
		Where("id = ?", templateID).
		Updates(map[string]interface{}{
			"usage_count":  gorm.Expr("usage_count + ?", 1),
			"last_used_at": gorm.Expr("NOW()"),
		}).Error
}

// GetMostUsedTemplates retrieves the most frequently used templates
func (r *templateRepositoryImpl) GetMostUsedTemplates(
	ctx context.Context, limit int,
) ([]*domain.ExtendedNotificationTemplate, error) {
	db := r.db.WithContext(ctx)
	var templates []*domain.ExtendedNotificationTemplate
	err := db.Preload("Category").
		Where("is_active = ?", true).
		Order("usage_count DESC").
		Limit(limit).
		Find(&templates).Error
	return templates, err
}

// BulkUpdate updates multiple templates by ID, only modifying is_active and category_id fields.
// Templates that are not found or are system templates are skipped and reported in the skipped slice.
// The entire operation runs in a single transaction for atomicity.
func (r *templateRepositoryImpl) BulkUpdate(
	ctx context.Context, templateIDs []uuid.UUID, isActive *bool, categoryID *uuid.UUID,
) (updated int, skipped []uuid.UUID, err error) {
	if len(templateIDs) == 0 {
		return 0, nil, nil
	}

	updates := map[string]interface{}{}
	if isActive != nil {
		updates["is_active"] = *isActive
	}
	if categoryID != nil {
		updates["category_id"] = *categoryID
	}
	if len(updates) == 0 {
		return 0, nil, nil
	}

	db := r.db.WithContext(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		// Single UPDATE for all matching non-system templates
		result := tx.Model(&domain.ExtendedNotificationTemplate{}).
			Where("id IN ? AND is_system = false", templateIDs).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		updated = int(result.RowsAffected)

		// Determine which IDs were skipped (not found or system templates)
		if updated < len(templateIDs) {
			var foundIDs []uuid.UUID
			if txErr := tx.Model(&domain.ExtendedNotificationTemplate{}).
				Where("id IN ? AND is_system = false", templateIDs).
				Pluck("id", &foundIDs).Error; txErr != nil {
				return txErr
			}
			foundSet := make(map[uuid.UUID]bool, len(foundIDs))
			for _, id := range foundIDs {
				foundSet[id] = true
			}
			for _, id := range templateIDs {
				if !foundSet[id] {
					skipped = append(skipped, id)
				}
			}
		}
		return nil
	})
	return updated, skipped, err
}
