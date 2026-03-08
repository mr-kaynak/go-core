package domain

import (
	"time"

	"github.com/google/uuid"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

// Blog SSE event types
const (
	SSEEventTypeBlogPostPublished   notificationDomain.SSEEventType = "blog:post:published"
	SSEEventTypeBlogPostUpdated     notificationDomain.SSEEventType = "blog:post:updated"
	SSEEventTypeBlogCommentNew      notificationDomain.SSEEventType = "blog:comment:new"
	SSEEventTypeBlogCommentApproved notificationDomain.SSEEventType = "blog:comment:approved"
	SSEEventTypeBlogPostLiked       notificationDomain.SSEEventType = "blog:post:liked"
	SSEEventTypeBlogPostEngagement  notificationDomain.SSEEventType = "blog:post:engagement"
	SSEEventTypeBlogDraftAutosave   notificationDomain.SSEEventType = "blog:draft:autosave"
	SSEEventTypeBlogAdminStats      notificationDomain.SSEEventType = "blog:admin:stats"
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
func NewSSEBlogPostEvent(eventType notificationDomain.SSEEventType, data SSEBlogPostData) *notificationDomain.SSEEvent {
	return &notificationDomain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewSSEBlogCommentEvent creates a new blog comment SSE event
func NewSSEBlogCommentEvent(eventType notificationDomain.SSEEventType, data SSEBlogCommentData) *notificationDomain.SSEEvent {
	return &notificationDomain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewSSEBlogLikeEvent creates a new blog like SSE event
func NewSSEBlogLikeEvent(data SSEBlogLikeData) *notificationDomain.SSEEvent {
	return &notificationDomain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeBlogPostLiked,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewSSEBlogEngagementEvent creates a new blog engagement SSE event
func NewSSEBlogEngagementEvent(data SSEBlogEngagementData) *notificationDomain.SSEEvent {
	return &notificationDomain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeBlogPostEngagement,
		Timestamp: time.Now(),
		Data:      data,
	}
}
