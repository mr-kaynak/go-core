package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MediaType represents the type of uploaded media
type MediaType string

const (
	MediaTypeImage MediaType = "image"
	MediaTypeVideo MediaType = "video"
	MediaTypeFile  MediaType = "file"
)

// PostMedia represents a media file associated with a blog post
type PostMedia struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	PostID      uuid.UUID `gorm:"type:uuid;not null;index" json:"post_id"`
	UploaderID  uuid.UUID `gorm:"type:uuid;not null" json:"uploader_id"`
	S3Key       string    `gorm:"not null;size:512" json:"s3_key"`
	Filename    string    `gorm:"not null;size:255" json:"filename"`
	MediaType   MediaType `gorm:"type:varchar(20);not null" json:"media_type"`
	ContentType string    `gorm:"size:100" json:"content_type"`
	FileSize    int64     `gorm:"not null" json:"file_size"`
	URL         string    `gorm:"-" json:"url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName specifies the table name for PostMedia
func (PostMedia) TableName() string {
	return "blog_post_media"
}

// BeforeCreate hook for PostMedia
func (m *PostMedia) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
