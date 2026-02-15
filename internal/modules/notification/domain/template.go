package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TemplateLanguage represents a language variant of a template
type TemplateLanguage struct {
	ID           uuid.UUID             `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	TemplateID   uuid.UUID             `gorm:"type:uuid;not null;index" json:"template_id"`
	Template     *NotificationTemplate `gorm:"foreignKey:TemplateID" json:"-"`
	LanguageCode string                `gorm:"type:varchar(10);not null;index" json:"language_code"` // e.g., "en", "tr", "es"
	Subject      string                `json:"subject,omitempty"`
	Body         string                `gorm:"type:text" json:"body"`
	HTMLContent  string                `gorm:"type:text" json:"html_content,omitempty"` // Full HTML override for this language
	IsDefault    bool                  `gorm:"default:false" json:"is_default"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

// TemplateVariable represents a variable that can be used in a template
type TemplateVariable struct {
	ID           uuid.UUID             `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	TemplateID   uuid.UUID             `gorm:"type:uuid;not null;index" json:"template_id"`
	Template     *NotificationTemplate `gorm:"foreignKey:TemplateID" json:"-"`
	Name         string                `gorm:"not null" json:"name"`         // e.g., "username"
	Type         string                `gorm:"default:'string'" json:"type"` // string, number, boolean, date
	Required     bool                  `gorm:"default:true" json:"required"`
	DefaultValue string                `json:"default_value,omitempty"`
	Description  string                `json:"description,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

// TemplateCategory represents a category for organizing templates
type TemplateCategory struct {
	ID          uuid.UUID         `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string            `gorm:"uniqueIndex;not null" json:"name"`
	Description string            `json:"description,omitempty"`
	ParentID    *uuid.UUID        `gorm:"type:uuid" json:"parent_id,omitempty"`
	Parent      *TemplateCategory `gorm:"foreignKey:ParentID" json:"-"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	DeletedAt   gorm.DeletedAt    `gorm:"index" json:"-"`
}

// ExtendedNotificationTemplate extends the base NotificationTemplate with relationships
type ExtendedNotificationTemplate struct {
	NotificationTemplate
	CategoryID        *uuid.UUID         `gorm:"type:uuid;index" json:"category_id,omitempty"`
	Category          *TemplateCategory  `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Languages         []TemplateLanguage `gorm:"foreignKey:TemplateID" json:"languages,omitempty"`
	TemplateVariables []TemplateVariable `gorm:"foreignKey:TemplateID" json:"template_variables,omitempty"`
	Tags              string             `gorm:"type:jsonb;default:'[]'" json:"tags,omitempty"` // JSON array of tags
	HTMLContent       string             `gorm:"type:text" json:"html_content,omitempty"`       // Full HTML template (<!DOCTYPE html>...) for email rendering
	Version           int                `gorm:"default:1" json:"version"`
	IsSystem          bool               `gorm:"default:false" json:"is_system"` // System templates cannot be deleted
	LastUsedAt        *time.Time         `json:"last_used_at,omitempty"`
	UsageCount        int                `gorm:"default:0" json:"usage_count"`
}

// TableName specifies the table name for ExtendedNotificationTemplate
func (ExtendedNotificationTemplate) TableName() string {
	return "notification_templates"
}

// TableName specifies the table name for TemplateLanguage
func (TemplateLanguage) TableName() string {
	return "template_languages"
}

// TableName specifies the table name for TemplateVariable
func (TemplateVariable) TableName() string {
	return "template_variables"
}

// TableName specifies the table name for TemplateCategory
func (TemplateCategory) TableName() string {
	return "template_categories"
}

// GetLanguage returns the template content for a specific language
func (t *ExtendedNotificationTemplate) GetLanguage(languageCode string) *TemplateLanguage {
	for i := range t.Languages {
		if t.Languages[i].LanguageCode == languageCode {
			return &t.Languages[i]
		}
	}
	// Return default language if specific language not found
	for i := range t.Languages {
		if t.Languages[i].IsDefault {
			return &t.Languages[i]
		}
	}
	// Return first language if no default
	if len(t.Languages) > 0 {
		return &t.Languages[0]
	}
	return nil
}

// GetRequiredVariables returns all required variables for the template
func (t *ExtendedNotificationTemplate) GetRequiredVariables() []string {
	var required []string
	for i := range t.TemplateVariables {
		if t.TemplateVariables[i].Required {
			required = append(required, t.TemplateVariables[i].Name)
		}
	}
	return required
}

// ValidateVariables validates that all required variables are provided
func (t *ExtendedNotificationTemplate) ValidateVariables(data map[string]interface{}) error {
	required := t.GetRequiredVariables()
	for _, varName := range required {
		if _, exists := data[varName]; !exists {
			return gorm.ErrInvalidData
		}
	}
	return nil
}

// GetTags returns the tags as a string slice
func (t *ExtendedNotificationTemplate) GetTags() []string {
	var tags []string
	if t.Tags != "" {
		_ = json.Unmarshal([]byte(t.Tags), &tags)
	}
	return tags
}

// SetTags sets the tags from a string slice
func (t *ExtendedNotificationTemplate) SetTags(tags []string) {
	data, _ := json.Marshal(tags)
	t.Tags = string(data)
}

// IncrementUsage increments the usage count and updates last used time
func (t *ExtendedNotificationTemplate) IncrementUsage() {
	t.UsageCount++
	now := time.Now()
	t.LastUsedAt = &now
}
