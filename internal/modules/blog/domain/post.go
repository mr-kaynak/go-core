package domain

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PostStatus represents the status of a blog post
type PostStatus string

const (
	PostStatusDraft     PostStatus = "draft"
	PostStatusPublished PostStatus = "published"
	PostStatusArchived  PostStatus = "archived"
)

// ContentJSON is a custom type for storing Plate.js/Slate JSON content in PostgreSQL JSONB
type ContentJSON []byte

// Value implements the driver.Valuer interface for JSONB storage
func (c ContentJSON) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	return []byte(c), nil
}

// Scan implements the sql.Scanner interface for JSONB retrieval
func (c *ContentJSON) Scan(value interface{}) error {
	if value == nil {
		*c = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		*c = nil
		return nil
	}
	*c = make(ContentJSON, len(bytes))
	copy(*c, bytes)
	return nil
}

// MarshalJSON implements json.Marshaler
func (c ContentJSON) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}
	return []byte(c), nil
}

// UnmarshalJSON implements json.Unmarshaler
func (c *ContentJSON) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*c = nil
		return nil
	}
	*c = make(ContentJSON, len(data))
	copy(*c, data)
	return nil
}

// Post represents a blog post
type Post struct {
	ID              uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Title           string         `gorm:"not null;size:255" json:"title"`
	Slug            string         `gorm:"uniqueIndex;not null;size:255" json:"slug"`
	Excerpt         string         `gorm:"size:500" json:"excerpt,omitempty"`
	ContentJSON     ContentJSON    `gorm:"type:jsonb;column:content_json" json:"content_json,omitempty"`
	ContentHTML     string         `gorm:"type:text" json:"content_html,omitempty"`
	ContentPlain    string         `gorm:"type:text" json:"content_plain,omitempty"`
	CoverImageURL   string         `gorm:"size:512" json:"cover_image_url,omitempty"`
	MetaTitle       string         `gorm:"size:255" json:"meta_title,omitempty"`
	MetaDescription string         `gorm:"size:500" json:"meta_description,omitempty"`
	Status          PostStatus     `gorm:"type:varchar(20);default:'draft';index" json:"status"`
	AuthorID        uuid.UUID      `gorm:"type:uuid;not null;index" json:"author_id"`
	CategoryID      *uuid.UUID     `gorm:"type:uuid;index" json:"category_id,omitempty"`
	ReadTime        int            `gorm:"default:0" json:"read_time"`
	IsFeatured      bool           `gorm:"default:false" json:"is_featured"`
	PublishedAt     *time.Time     `gorm:"index" json:"published_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations (not persisted directly)
	Author   interface{} `gorm:"-" json:"author,omitempty"`
	Category *Category   `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Tags     []Tag       `gorm:"many2many:post_tags;" json:"tags"`
	Stats    *PostStats  `gorm:"foreignKey:PostID" json:"stats,omitempty"`

	// Runtime fields
	IsLiked bool `gorm:"-" json:"is_liked,omitempty"`
}

// TableName specifies the table name for Post
func (Post) TableName() string {
	return "blog_posts"
}

// BeforeCreate hook for Post
func (p *Post) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

// IsPublished checks if the post is published
func (p *Post) IsPublished() bool {
	return p.Status == PostStatusPublished
}

// IsDraft checks if the post is a draft
func (p *Post) IsDraft() bool {
	return p.Status == PostStatusDraft
}

// Allowed status transitions: draft→published, published→archived
var allowedTransitions = map[PostStatus]map[PostStatus]bool{
	PostStatusDraft:     {PostStatusPublished: true},
	PostStatusPublished: {PostStatusArchived: true},
}

// CanTransition checks if a status transition is valid.
func (p *Post) CanTransition(to PostStatus) bool {
	targets, ok := allowedTransitions[p.Status]
	if !ok {
		return false
	}
	return targets[to]
}

// PostAuthor represents minimal author info for API responses
type PostAuthor struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

// CategorySummary represents minimal category info for API responses
type CategorySummary struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// TagSummary represents minimal tag info for API responses
type TagSummary struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// StatsSummary represents post stats for API responses
type StatsSummary struct {
	LikeCount    int `json:"like_count"`
	ViewCount    int `json:"view_count"`
	ShareCount   int `json:"share_count"`
	CommentCount int `json:"comment_count"`
}

// PostResponse is the public API representation of a post
type PostResponse struct {
	ID              uuid.UUID        `json:"id"`
	Title           string           `json:"title"`
	Slug            string           `json:"slug"`
	ContentHTML     string           `json:"content_html,omitempty"`
	Excerpt         string           `json:"excerpt,omitempty"`
	CoverImageURL   string           `json:"cover_image_url,omitempty"`
	MetaTitle       string           `json:"meta_title,omitempty"`
	MetaDescription string           `json:"meta_description,omitempty"`
	Status          PostStatus       `json:"status"`
	PublishedAt     *time.Time       `json:"published_at,omitempty"`
	Author          *PostAuthor      `json:"author,omitempty"`
	Category        *CategorySummary `json:"category,omitempty"`
	Tags            []TagSummary     `json:"tags,omitempty"`
	Stats           *StatsSummary    `json:"stats,omitempty"`
	ReadTimeMinutes int              `json:"read_time_minutes"`
	IsLiked         bool             `json:"is_liked"`
	IsFeatured      bool             `json:"is_featured"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// ToJSON marshals ContentJSON for debugging/logging
func (c ContentJSON) ToJSON() json.RawMessage {
	if c == nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(c)
}
