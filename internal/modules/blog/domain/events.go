package domain

import (
	"time"

	"github.com/google/uuid"
)

// SSEEventType identifies the kind of an SSE event emitted by the blog module.
// It mirrors the notification module's event type on the wire (plain string)
// without importing that module, keeping blog isolated.
type SSEEventType string

// SSEEvent is the blog module's neutral representation of a Server-Sent Event.
// It is converted to the notification module's event by an adapter at the
// composition root, preserving the exact JSON/wire shape.
type SSEEvent struct {
	ID        string       `json:"id"`
	Type      SSEEventType `json:"type"`
	Data      interface{}  `json:"data"`
	Timestamp time.Time    `json:"timestamp"`
}

// Blog SSE event types
const (
	SSEEventTypeBlogPostPublished   SSEEventType = "blog:post:published"
	SSEEventTypeBlogPostUpdated     SSEEventType = "blog:post:updated"
	SSEEventTypeBlogCommentNew      SSEEventType = "blog:comment:new"
	SSEEventTypeBlogCommentApproved SSEEventType = "blog:comment:approved"
	SSEEventTypeBlogPostLiked       SSEEventType = "blog:post:liked"
	SSEEventTypeBlogPostEngagement  SSEEventType = "blog:post:engagement"
	SSEEventTypeBlogDraftAutosave   SSEEventType = "blog:draft:autosave"
	SSEEventTypeBlogAdminStats      SSEEventType = "blog:admin:stats"
)

// SSEBlogPostData represents post event data for SSE
type SSEBlogPostData struct {
	PostID      uuid.UUID  `json:"post_id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	AuthorID    uuid.UUID  `json:"author_id"`
	AuthorName  string     `json:"author_name"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// SSEBlogCommentData represents comment event data for SSE
type SSEBlogCommentData struct {
	CommentID  uuid.UUID  `json:"comment_id"`
	PostID     uuid.UUID  `json:"post_id"`
	PostTitle  string     `json:"post_title"`
	AuthorID   *uuid.UUID `json:"author_id,omitempty"`
	AuthorName string     `json:"author_name"`
	Content    string     `json:"content"`
}

// SSEBlogLikeData represents like event data for SSE
type SSEBlogLikeData struct {
	PostID    uuid.UUID `json:"post_id"`
	UserID    uuid.UUID `json:"user_id"`
	LikeCount int       `json:"like_count"`
	Liked     bool      `json:"liked"`
}

// SSEBlogEngagementData represents engagement stats update for SSE
type SSEBlogEngagementData struct {
	PostID       uuid.UUID `json:"post_id"`
	LikeCount    int       `json:"like_count"`
	ViewCount    int       `json:"view_count"`
	ShareCount   int       `json:"share_count"`
	CommentCount int       `json:"comment_count"`
}

// NewSSEBlogPostEvent creates a new blog post SSE event
func NewSSEBlogPostEvent(eventType SSEEventType, data SSEBlogPostData) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewSSEBlogCommentEvent creates a new blog comment SSE event
func NewSSEBlogCommentEvent(eventType SSEEventType, data SSEBlogCommentData) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewSSEBlogLikeEvent creates a new blog like SSE event
func NewSSEBlogLikeEvent(data SSEBlogLikeData) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeBlogPostLiked,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewSSEBlogEngagementEvent creates a new blog engagement SSE event
func NewSSEBlogEngagementEvent(data SSEBlogEngagementData) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeBlogPostEngagement,
		Timestamp: time.Now(),
		Data:      data,
	}
}
