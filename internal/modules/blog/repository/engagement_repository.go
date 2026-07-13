package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// TrendingQuery holds parameters for trending posts query
type TrendingQuery struct {
	Limit         int
	Days          int
	ViewWeight    int
	LikeWeight    int
	CommentWeight int
	ShareWeight   int
}

// EngagementRepository defines the interface for engagement data operations
type EngagementRepository interface {
	WithTx(tx *gorm.DB) EngagementRepository

	// Likes
	CreateLike(ctx context.Context, like *domain.PostLike) error
	DeleteLike(ctx context.Context, postID, userID uuid.UUID) error
	IsLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error)
	ToggleLike(ctx context.Context, postID, userID uuid.UUID) (liked bool, err error)

	// Views
	CreateView(ctx context.Context, view *domain.PostView) error
	HasRecentView(ctx context.Context, postID uuid.UUID, ip string, since time.Time) (bool, error)
	HasRecentUserView(ctx context.Context, postID, userID uuid.UUID, since time.Time) (bool, error)

	// Shares
	CreateShare(ctx context.Context, share *domain.PostShare) error

	// Stats
	GetStats(ctx context.Context, postID uuid.UUID) (*domain.PostStats, error)
	GetBatchStats(ctx context.Context, postIDs []uuid.UUID) ([]*domain.PostStats, error)
	UpsertStats(ctx context.Context, stats *domain.PostStats) error
	IncrementStat(ctx context.Context, postID uuid.UUID, field string, delta int) error

	// Trending & Popular
	GetTrending(ctx context.Context, query TrendingQuery) ([]*domain.TrendingPost, error)
	GetPopular(ctx context.Context, limit int) ([]*domain.Post, error)
}
