package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PostRevision represents a historical version of a blog post
type PostRevision struct {
	ID          uuid.UUID   `gorm:"type:uuid;primaryKey" json:"id"`
	PostID      uuid.UUID   `gorm:"type:uuid;not null;uniqueIndex:idx_post_rev_ver" json:"post_id"`
	EditorID    uuid.UUID   `gorm:"type:uuid;not null" json:"editor_id"`
	Title       string      `gorm:"not null;size:255" json:"title"`
	ContentJSON ContentJSON `gorm:"type:jsonb;column:content_json" json:"content_json,omitempty"`
	ContentHTML string      `gorm:"type:text" json:"content_html,omitempty"`
	Excerpt     string      `gorm:"size:500" json:"excerpt,omitempty"`
	Version     int         `gorm:"not null;uniqueIndex:idx_post_rev_ver" json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for PostRevision
func (PostRevision) TableName() string {
	return "blog_post_revisions"
}

// BeforeCreate hook for PostRevision
func (r *PostRevision) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
