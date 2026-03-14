package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MaxCommentDepth is the maximum allowed nesting depth for comment replies.
const MaxCommentDepth = 3

// CommentStatus represents the status of a blog comment
type CommentStatus string

const (
	CommentStatusPending  CommentStatus = "pending"
	CommentStatusApproved CommentStatus = "approved"
	CommentStatusRejected CommentStatus = "rejected"
)

// Comment represents a blog comment with threading support
type Comment struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	PostID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"post_id"`
	AuthorID   *uuid.UUID     `gorm:"type:uuid;index" json:"author_id,omitempty"`
	ParentID   *uuid.UUID     `gorm:"type:uuid;index" json:"parent_id,omitempty"`
	Depth      int            `gorm:"default:0" json:"depth"`
	Content    string         `gorm:"type:text;not null" json:"content"`
	GuestName  string         `gorm:"size:100" json:"guest_name,omitempty"`
	GuestEmail string         `gorm:"size:255" json:"guest_email,omitempty"`
	Status     CommentStatus  `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	Children []Comment      `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Author   *CommentAuthor `gorm:"-" json:"author,omitempty"`
}

// TableName specifies the table name for Comment
func (Comment) TableName() string {
	return "blog_comments"
}

// BeforeCreate hook for Comment
func (c *Comment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// IsGuest checks if the comment was made by a guest
func (c *Comment) IsGuest() bool {
	return c.AuthorID == nil
}

// IsRoot checks if the comment is a root-level comment
func (c *Comment) IsRoot() bool {
	return c.ParentID == nil
}

// CommentResponse is the public API representation of a comment (excludes guest_email)
type CommentResponse struct {
	ID        uuid.UUID         `json:"id"`
	PostID    uuid.UUID         `json:"post_id"`
	AuthorID  *uuid.UUID        `json:"author_id,omitempty"`
	ParentID  *uuid.UUID        `json:"parent_id,omitempty"`
	Content   string            `json:"content"`
	GuestName string            `json:"guest_name,omitempty"`
	Status    CommentStatus     `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Children  []CommentResponse `json:"children,omitempty"`
	Author    *CommentAuthor    `json:"author,omitempty"`
}

// ToResponse converts a Comment to a CommentResponse (strips guest_email)
func (c *Comment) ToResponse() *CommentResponse {
	return c.toResponseWithDepth(0)
}

func (c *Comment) toResponseWithDepth(depth int) *CommentResponse {
	resp := &CommentResponse{
		ID:        c.ID,
		PostID:    c.PostID,
		AuthorID:  c.AuthorID,
		ParentID:  c.ParentID,
		Content:   c.Content,
		GuestName: c.GuestName,
		Status:    c.Status,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Author:    c.Author,
	}
	if len(c.Children) > 0 && depth < MaxCommentDepth {
		resp.Children = make([]CommentResponse, len(c.Children))
		for i := range c.Children {
			resp.Children[i] = *c.Children[i].toResponseWithDepth(depth + 1)
		}
	}
	return resp
}

// AdminCommentResponse is the admin API representation (includes guest_email for moderation)
type AdminCommentResponse struct {
	ID         uuid.UUID     `json:"id"`
	PostID     uuid.UUID     `json:"post_id"`
	AuthorID   *uuid.UUID    `json:"author_id,omitempty"`
	ParentID   *uuid.UUID    `json:"parent_id,omitempty"`
	Content    string        `json:"content"`
	GuestName  string        `json:"guest_name,omitempty"`
	GuestEmail string        `json:"guest_email,omitempty"`
	Status     CommentStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// ToAdminResponse converts a Comment to an AdminCommentResponse (explicit field control)
func (c *Comment) ToAdminResponse() *AdminCommentResponse {
	return &AdminCommentResponse{
		ID:         c.ID,
		PostID:     c.PostID,
		AuthorID:   c.AuthorID,
		ParentID:   c.ParentID,
		Content:    c.Content,
		GuestName:  c.GuestName,
		GuestEmail: c.GuestEmail,
		Status:     c.Status,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}

// CommentAuthor represents minimal author info for comment responses
type CommentAuthor struct {
	ID        *uuid.UUID `json:"id,omitempty"`
	Name      string     `json:"name"`
	AvatarURL string     `json:"avatar_url,omitempty"`
	IsGuest   bool       `json:"is_guest"`
}
