package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"gorm.io/gorm"
)

// ToggleLikeResponse holds the response for a like toggle
type ToggleLikeResponse struct {
	Liked     bool `json:"liked"`
	LikeCount int  `json:"like_count"`
}

// EngagementService handles blog engagement business logic
type EngagementService struct {
	cfg            *config.Config
	engagementRepo repository.EngagementRepository
	postRepo       repository.PostRepository
	sseSvc         *notificationService.SSEService
	redisClient    *cache.RedisClient
	logger         *logger.Logger
}

// NewEngagementService creates a new EngagementService
func NewEngagementService(cfg *config.Config, engagementRepo repository.EngagementRepository, postRepo repository.PostRepository) *EngagementService {
	return &EngagementService{
		cfg:            cfg,
		engagementRepo: engagementRepo,
		postRepo:       postRepo,
		logger:         logger.Get().WithFields(logger.Fields{"service": "blog_engagement"}),
	}
}

// SetSSEService sets the optional SSE service
func (s *EngagementService) SetSSEService(svc *notificationService.SSEService) {
	s.sseSvc = svc
}

// SetRedisClient sets the optional Redis client for view cooldown
func (s *EngagementService) SetRedisClient(rc *cache.RedisClient) {
	s.redisClient = rc
}

// ToggleLike toggles a like on a post
func (s *EngagementService) ToggleLike(postID, userID uuid.UUID) (*ToggleLikeResponse, error) {
	liked, err := s.engagementRepo.IsLiked(postID, userID)
	if err != nil {
		return nil, errors.NewInternalError("Failed to check like status")
	}

	if liked {
		// Unlike
		if err := s.engagementRepo.DeleteLike(postID, userID); err != nil {
			return nil, errors.NewInternalError("Failed to unlike post")
		}
		_ = s.engagementRepo.IncrementStat(postID, "like_count", -1)
		metrics.GetMetrics().RecordBlogLikeToggled("unlike")
	} else {
		// Like
		like := &domain.PostLike{PostID: postID, UserID: userID}
		if err := s.engagementRepo.CreateLike(like); err != nil {
			return nil, errors.NewInternalError("Failed to like post")
		}
		_ = s.engagementRepo.IncrementStat(postID, "like_count", 1)
		metrics.GetMetrics().RecordBlogLikeToggled("like")
	}

	// Get updated stats
	stats, err := s.engagementRepo.GetStats(postID)
	likeCount := 0
	if err == nil {
		likeCount = stats.LikeCount
	}

	// Broadcast SSE event
	if s.sseSvc != nil {
		event := domain.NewSSEBlogLikeEvent(domain.SSEBlogLikeData{
			PostID:    postID,
			UserID:    userID,
			LikeCount: likeCount,
			Liked:     !liked,
		})
		_ = s.sseSvc.BroadcastToChannel(context.Background(), "blog:engagement", event)
	}

	return &ToggleLikeResponse{Liked: !liked, LikeCount: likeCount}, nil
}

// RecordView records a post view with dedup cooldown
func (s *EngagementService) RecordView(postID uuid.UUID, userID *uuid.UUID, ip, userAgent, referrer string) error {
	cooldown := s.cfg.Blog.ViewCooldown
	if cooldown == 0 {
		cooldown = 30 * time.Minute
	}

	// Check cooldown via Redis if available
	if s.redisClient != nil {
		identifier := ip
		if userID != nil {
			identifier = userID.String()
		}
		cacheKey := fmt.Sprintf("blog:view:%s:%s", postID.String(), identifier)

		existing, err := s.redisClient.Get(context.Background(), cacheKey)
		if err == nil && existing != "" {
			return nil // Already viewed recently
		}
		// Set cooldown
		_ = s.redisClient.Set(context.Background(), cacheKey, "1", cooldown)
	} else {
		// Fallback to DB check
		since := time.Now().Add(-cooldown)
		if userID != nil {
			recent, err := s.engagementRepo.HasRecentUserView(postID, *userID, since)
			if err == nil && recent {
				return nil
			}
		} else {
			recent, err := s.engagementRepo.HasRecentView(postID, ip, since)
			if err == nil && recent {
				return nil
			}
		}
	}

	view := &domain.PostView{
		PostID:    postID,
		UserID:    userID,
		IPAddress: ip,
		UserAgent: userAgent,
		Referrer:  referrer,
		ViewedAt:  time.Now(),
	}

	if err := s.engagementRepo.CreateView(view); err != nil {
		s.logger.Error("Failed to record view", "error", err)
		return errors.NewInternalError("Failed to record view")
	}

	_ = s.engagementRepo.IncrementStat(postID, "view_count", 1)
	metrics.GetMetrics().RecordBlogViewRecorded()

	return nil
}

// RecordShare records a post share
func (s *EngagementService) RecordShare(postID uuid.UUID, userID *uuid.UUID, platform, ip string) error {
	share := &domain.PostShare{
		PostID:    postID,
		UserID:    userID,
		Platform:  platform,
		IPAddress: ip,
	}

	if err := s.engagementRepo.CreateShare(share); err != nil {
		return errors.NewInternalError("Failed to record share")
	}

	_ = s.engagementRepo.IncrementStat(postID, "share_count", 1)
	metrics.GetMetrics().RecordBlogShareRecorded(platform)

	return nil
}

// GetStats returns engagement stats for a post
func (s *EngagementService) GetStats(postID uuid.UUID) (*domain.PostStats, error) {
	stats, err := s.engagementRepo.GetStats(postID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return &domain.PostStats{PostID: postID}, nil
		}
		return nil, errors.NewInternalError("Failed to get stats")
	}
	return stats, nil
}

// GetBatchStats returns stats for multiple posts
func (s *EngagementService) GetBatchStats(postIDs []uuid.UUID) ([]*domain.PostStats, error) {
	return s.engagementRepo.GetBatchStats(postIDs)
}

// IsLiked checks if a user has liked a post
func (s *EngagementService) IsLiked(postID, userID uuid.UUID) (bool, error) {
	return s.engagementRepo.IsLiked(postID, userID)
}

// GetTrending returns trending posts
func (s *EngagementService) GetTrending(limit int) ([]*domain.Post, error) {
	weights := s.cfg.Blog.TrendingWeights
	return s.engagementRepo.GetTrending(repository.TrendingQuery{
		Limit:         limit,
		Days:          7,
		ViewWeight:    weights.View,
		LikeWeight:    weights.Like,
		CommentWeight: weights.Comment,
		ShareWeight:   weights.Share,
	})
}

// GetPopular returns all-time popular posts
func (s *EngagementService) GetPopular(limit int) ([]*domain.Post, error) {
	return s.engagementRepo.GetPopular(limit)
}
