package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BlogSettings holds runtime-configurable blog settings (singleton row, id=1)
type BlogSettings struct {
	ID                  uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	AutoApproveComments bool           `gorm:"default:false" json:"auto_approve_comments"`
	PostsPerPage        int            `gorm:"default:20" json:"posts_per_page"`
	ViewCooldownMinutes int            `gorm:"default:30" json:"view_cooldown_minutes"`
	FeedItemLimit       int            `gorm:"default:50" json:"feed_item_limit"`
	ReadTimeWPM         int            `gorm:"default:200" json:"read_time_wpm"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for BlogSettings
func (BlogSettings) TableName() string {
	return "blog_settings"
}

// BeforeCreate hook for BlogSettings
func (s *BlogSettings) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// UpdateBlogSettingsRequest is the DTO for partial settings update (pointer fields for partial update)
type UpdateBlogSettingsRequest struct {
	AutoApproveComments *bool `json:"auto_approve_comments"`
	PostsPerPage        *int  `json:"posts_per_page" validate:"omitempty,min=1,max=100"`
	ViewCooldownMinutes *int  `json:"view_cooldown_minutes" validate:"omitempty,min=0,max=1440"`
	FeedItemLimit       *int  `json:"feed_item_limit" validate:"omitempty,min=1,max=500"`
	ReadTimeWPM         *int  `json:"read_time_wpm" validate:"omitempty,min=50,max=1000"`
}

// BlogSettingsResponse is the public API representation
type BlogSettingsResponse struct {
	AutoApproveComments bool `json:"auto_approve_comments"`
	PostsPerPage        int  `json:"posts_per_page"`
	ViewCooldownMinutes int  `json:"view_cooldown_minutes"`
	FeedItemLimit       int  `json:"feed_item_limit"`
	ReadTimeWPM         int  `json:"read_time_wpm"`
}

// ToResponse converts BlogSettings to BlogSettingsResponse
func (s *BlogSettings) ToResponse() *BlogSettingsResponse {
	return &BlogSettingsResponse{
		AutoApproveComments: s.AutoApproveComments,
		PostsPerPage:        s.PostsPerPage,
		ViewCooldownMinutes: s.ViewCooldownMinutes,
		FeedItemLimit:       s.FeedItemLimit,
		ReadTimeWPM:         s.ReadTimeWPM,
	}
}
