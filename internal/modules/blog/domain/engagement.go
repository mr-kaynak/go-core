package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PostLike represents a user's like on a blog post
type PostLike struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	PostID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_post_like_unique" json:"post_id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_post_like_unique" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName specifies the table name for PostLike
func (PostLike) TableName() string {
	return "blog_post_likes"
}

// BeforeCreate hook for PostLike
func (l *PostLike) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// PostView represents a view on a blog post
type PostView struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	PostID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"post_id"`
	UserID    *uuid.UUID `gorm:"type:uuid" json:"user_id,omitempty"`
	IPAddress string     `gorm:"size:45" json:"ip_address"`
	UserAgent string     `gorm:"size:512" json:"user_agent"`
	Referrer  string     `gorm:"size:512" json:"referrer,omitempty"`
	ViewedAt  time.Time  `gorm:"not null;index" json:"viewed_at"`
}

// TableName specifies the table name for PostView
func (PostView) TableName() string {
	return "blog_post_views"
}

// BeforeCreate hook for PostView
func (v *PostView) BeforeCreate(tx *gorm.DB) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	if v.ViewedAt.IsZero() {
		v.ViewedAt = time.Now()
	}
	return nil
}

// PostShare represents a share action on a blog post
type PostShare struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	PostID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"post_id"`
	UserID    *uuid.UUID `gorm:"type:uuid" json:"user_id,omitempty"`
	Platform  string     `gorm:"size:50;not null" json:"platform"`
	IPAddress string     `gorm:"size:45" json:"ip_address"`
	CreatedAt time.Time  `json:"created_at"`
}

// TableName specifies the table name for PostShare
func (PostShare) TableName() string {
	return "blog_post_shares"
}

// BeforeCreate hook for PostShare
func (s *PostShare) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// PostStats holds aggregated engagement stats for a blog post
type PostStats struct {
	PostID       uuid.UUID `gorm:"type:uuid;primaryKey" json:"post_id"`
	LikeCount    int       `gorm:"default:0" json:"like_count"`
	ViewCount    int       `gorm:"default:0" json:"view_count"`
	ShareCount   int       `gorm:"default:0" json:"share_count"`
	CommentCount int       `gorm:"default:0" json:"comment_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName specifies the table name for PostStats
func (PostStats) TableName() string {
	return "blog_post_stats"
}

// TrendingPost represents a post with its trending score
type TrendingPost struct {
	Post
	TrendingScore float64 `gorm:"-" json:"trending_score"`
}
