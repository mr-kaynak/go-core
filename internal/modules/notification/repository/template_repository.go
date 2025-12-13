package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"gorm.io/gorm"
)

// TemplateRepository defines the interface for template operations
type TemplateRepository interface {
	// Template CRUD
	CreateTemplate(template *domain.ExtendedNotificationTemplate) error
	GetTemplateByID(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error)
	GetTemplateByName(name string) (*domain.ExtendedNotificationTemplate, error)
	UpdateTemplate(template *domain.ExtendedNotificationTemplate) error
	DeleteTemplate(id uuid.UUID) error
	ListTemplates(filters map[string]interface{}, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error)

	// Language variants
	CreateLanguageVariant(variant *domain.TemplateLanguage) error
	GetLanguageVariant(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error)
	UpdateLanguageVariant(variant *domain.TemplateLanguage) error
	DeleteLanguageVariant(id uuid.UUID) error

	// Variables
	CreateVariable(variable *domain.TemplateVariable) error
	GetVariables(templateID uuid.UUID) ([]*domain.TemplateVariable, error)
	UpdateVariable(variable *domain.TemplateVariable) error
	DeleteVariable(id uuid.UUID) error

	// Categories
	CreateCategory(category *domain.TemplateCategory) error
	GetCategory(id uuid.UUID) (*domain.TemplateCategory, error)
	ListCategories() ([]*domain.TemplateCategory, error)
	UpdateCategory(category *domain.TemplateCategory) error
	DeleteCategory(id uuid.UUID) error

	// Usage tracking
	IncrementUsage(templateID uuid.UUID) error
	GetMostUsedTemplates(limit int) ([]*domain.ExtendedNotificationTemplate, error)
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
func (r *templateRepositoryImpl) CreateTemplate(template *domain.ExtendedNotificationTemplate) error {
	return r.db.Create(template).Error
}

// GetTemplateByID retrieves a template by ID with all relationships
func (r *templateRepositoryImpl) GetTemplateByID(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
	var template domain.ExtendedNotificationTemplate
	err := r.db.Preload("Category").
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
func (r *templateRepositoryImpl) GetTemplateByName(name string) (*domain.ExtendedNotificationTemplate, error) {
	var template domain.ExtendedNotificationTemplate
	err := r.db.Preload("Category").
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
func (r *templateRepositoryImpl) UpdateTemplate(template *domain.ExtendedNotificationTemplate) error {
	return r.db.Save(template).Error
}

// DeleteTemplate soft deletes a template
func (r *templateRepositoryImpl) DeleteTemplate(id uuid.UUID) error {
	return r.db.Where("id = ? AND is_system = false", id).Delete(&domain.ExtendedNotificationTemplate{}).Error
}

// ListTemplates lists templates with filters and pagination
func (r *templateRepositoryImpl) ListTemplates(filters map[string]interface{}, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	var templates []*domain.ExtendedNotificationTemplate
	var total int64

	query := r.db.Model(&domain.ExtendedNotificationTemplate{})

	// Apply filters
	if categoryID, ok := filters["category_id"]; ok {
		query = query.Where("category_id = ?", categoryID)
	}
	if templateType, ok := filters["type"]; ok {
		query = query.Where("type = ?", templateType)
	}
	if isActive, ok := filters["is_active"]; ok {
		query = query.Where("is_active = ?", isActive)
	}
	if search, ok := filters["search"]; ok {
		searchStr := "%" + search.(string) + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", searchStr, searchStr)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Fetch with pagination
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
func (r *templateRepositoryImpl) CreateLanguageVariant(variant *domain.TemplateLanguage) error {
	// If this is marked as default, unset other defaults for this template
	if variant.IsDefault {
		r.db.Model(&domain.TemplateLanguage{}).
			Where("template_id = ? AND id != ?", variant.TemplateID, variant.ID).
			Update("is_default", false)
	}
	return r.db.Create(variant).Error
}

// GetLanguageVariant retrieves a specific language variant
func (r *templateRepositoryImpl) GetLanguageVariant(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error) {
	var variant domain.TemplateLanguage
	err := r.db.Where("template_id = ? AND language_code = ?", templateID, languageCode).
		First(&variant).Error

	if err != nil {
		// Try to get the default variant
		err = r.db.Where("template_id = ? AND is_default = true", templateID).
			First(&variant).Error
		if err != nil {
			// Get any variant
			err = r.db.Where("template_id = ?", templateID).
				First(&variant).Error
		}
	}

	if err != nil {
		return nil, err
	}
	return &variant, nil
}

// UpdateLanguageVariant updates a language variant
func (r *templateRepositoryImpl) UpdateLanguageVariant(variant *domain.TemplateLanguage) error {
	// If this is marked as default, unset other defaults for this template
	if variant.IsDefault {
		r.db.Model(&domain.TemplateLanguage{}).
			Where("template_id = ? AND id != ?", variant.TemplateID, variant.ID).
			Update("is_default", false)
	}
	return r.db.Save(variant).Error
}

// DeleteLanguageVariant deletes a language variant
func (r *templateRepositoryImpl) DeleteLanguageVariant(id uuid.UUID) error {
	return r.db.Where("id = ?", id).Delete(&domain.TemplateLanguage{}).Error
}

// CreateVariable creates a new template variable
func (r *templateRepositoryImpl) CreateVariable(variable *domain.TemplateVariable) error {
	return r.db.Create(variable).Error
}

// GetVariables retrieves all variables for a template
func (r *templateRepositoryImpl) GetVariables(templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
	var variables []*domain.TemplateVariable
	err := r.db.Where("template_id = ?", templateID).Find(&variables).Error
	return variables, err
}

// UpdateVariable updates a template variable
func (r *templateRepositoryImpl) UpdateVariable(variable *domain.TemplateVariable) error {
	return r.db.Save(variable).Error
}

// DeleteVariable deletes a template variable
func (r *templateRepositoryImpl) DeleteVariable(id uuid.UUID) error {
	return r.db.Where("id = ?", id).Delete(&domain.TemplateVariable{}).Error
}

// CreateCategory creates a new template category
func (r *templateRepositoryImpl) CreateCategory(category *domain.TemplateCategory) error {
	return r.db.Create(category).Error
}

// GetCategory retrieves a category by ID
func (r *templateRepositoryImpl) GetCategory(id uuid.UUID) (*domain.TemplateCategory, error) {
	var category domain.TemplateCategory
	err := r.db.Where("id = ?", id).First(&category).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

// ListCategories lists all categories
func (r *templateRepositoryImpl) ListCategories() ([]*domain.TemplateCategory, error) {
	var categories []*domain.TemplateCategory
	err := r.db.Order("name ASC").Find(&categories).Error
	return categories, err
}

// UpdateCategory updates a category
func (r *templateRepositoryImpl) UpdateCategory(category *domain.TemplateCategory) error {
	return r.db.Save(category).Error
}

// DeleteCategory soft deletes a category
func (r *templateRepositoryImpl) DeleteCategory(id uuid.UUID) error {
	return r.db.Where("id = ?", id).Delete(&domain.TemplateCategory{}).Error
}

// IncrementUsage increments the usage count for a template
func (r *templateRepositoryImpl) IncrementUsage(templateID uuid.UUID) error {
	return r.db.Model(&domain.ExtendedNotificationTemplate{}).
		Where("id = ?", templateID).
		Updates(map[string]interface{}{
			"usage_count":  gorm.Expr("usage_count + ?", 1),
			"last_used_at": gorm.Expr("NOW()"),
		}).Error
}

// GetMostUsedTemplates retrieves the most frequently used templates
func (r *templateRepositoryImpl) GetMostUsedTemplates(limit int) ([]*domain.ExtendedNotificationTemplate, error) {
	var templates []*domain.ExtendedNotificationTemplate
	err := r.db.Preload("Category").
		Where("is_active = ?", true).
		Order("usage_count DESC").
		Limit(limit).
		Find(&templates).Error
	return templates, err
}
