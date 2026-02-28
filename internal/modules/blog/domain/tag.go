package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Tag represents a blog tag
type Tag struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name      string    `gorm:"not null;size:100" json:"name"`
	Slug      string    `gorm:"uniqueIndex;not null;size:100" json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Runtime fields
	PostCount int `gorm:"-" json:"post_count,omitempty"`
}

// TableName specifies the table name for Tag
func (Tag) TableName() string {
	return "blog_tags"
}

// BeforeCreate hook for Tag
func (t *Tag) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// PostTag represents the many-to-many relationship between posts and tags
type PostTag struct {
	PostID uuid.UUID `gorm:"type:uuid;primaryKey" json:"post_id"`
	TagID  uuid.UUID `gorm:"type:uuid;primaryKey" json:"tag_id"`
}

// TableName specifies the table name for PostTag
func (PostTag) TableName() string {
	return "post_tags"
}
