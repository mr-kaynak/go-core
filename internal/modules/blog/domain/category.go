package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Category represents a blog category with nested tree support
type Category struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string     `gorm:"not null;size:100" json:"name"`
	Slug        string     `gorm:"uniqueIndex;not null;size:100" json:"slug"`
	Description string     `gorm:"size:500" json:"description,omitempty"`
	ParentID    *uuid.UUID `gorm:"type:uuid;index" json:"parent_id,omitempty"`
	SortOrder   int        `gorm:"default:0" json:"sort_order"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// Relations
	Parent   *Category  `gorm:"foreignKey:ParentID" json:"-"`
	Children []Category `gorm:"foreignKey:ParentID" json:"children,omitempty"`
}

// TableName specifies the table name for Category
func (Category) TableName() string {
	return "blog_categories"
}

// BeforeCreate hook for Category
func (c *Category) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// IsRoot checks if the category is a root category
func (c *Category) IsRoot() bool {
	return c.ParentID == nil
}
