package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

// TrendingQuery holds parameters for trending posts query
type TrendingQuery struct {
	Limit       int
	Days        int
	ViewWeight  int
	LikeWeight  int
	CommentWeight int
	ShareWeight int
}

// EngagementRepository defines the interface for engagement data operations
type EngagementRepository interface {
	// Likes
	CreateLike(like *domain.PostLike) error
	DeleteLike(postID, userID uuid.UUID) error
	IsLiked(postID, userID uuid.UUID) (bool, error)

	// Views
	CreateView(view *domain.PostView) error
	HasRecentView(postID uuid.UUID, ip string, since time.Time) (bool, error)
	HasRecentUserView(postID, userID uuid.UUID, since time.Time) (bool, error)

	// Shares
	CreateShare(share *domain.PostShare) error

	// Stats
	GetStats(postID uuid.UUID) (*domain.PostStats, error)
	GetBatchStats(postIDs []uuid.UUID) ([]*domain.PostStats, error)
	UpsertStats(stats *domain.PostStats) error
	IncrementStat(postID uuid.UUID, field string, delta int) error

	// Trending & Popular
	GetTrending(query TrendingQuery) ([]*domain.Post, error)
	GetPopular(limit int) ([]*domain.Post, error)
}
