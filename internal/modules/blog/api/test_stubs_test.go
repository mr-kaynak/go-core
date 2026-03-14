package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Engagement repository stub for handler tests
// ---------------------------------------------------------------------------

type engagementRepoStubForHandler struct {
	getStatsFn func(postID uuid.UUID) (*domain.PostStats, error)
	isLikedFn  func(postID, userID uuid.UUID) (bool, error)
}

var _ repository.EngagementRepository = (*engagementRepoStubForHandler)(nil)

func (s *engagementRepoStubForHandler) WithTx(_ *gorm.DB) repository.EngagementRepository { return s }
func (s *engagementRepoStubForHandler) CreateLike(_ *domain.PostLike) error                { return nil }
func (s *engagementRepoStubForHandler) DeleteLike(_, _ uuid.UUID) error                    { return nil }
func (s *engagementRepoStubForHandler) IsLiked(postID, userID uuid.UUID) (bool, error) {
	if s.isLikedFn != nil {
		return s.isLikedFn(postID, userID)
	}
	return false, nil
}
func (s *engagementRepoStubForHandler) ToggleLike(_, _ uuid.UUID) (bool, error) {
	return true, nil
}
func (s *engagementRepoStubForHandler) CreateView(_ *domain.PostView) error { return nil }
func (s *engagementRepoStubForHandler) HasRecentView(_ uuid.UUID, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (s *engagementRepoStubForHandler) HasRecentUserView(_ uuid.UUID, _ uuid.UUID, _ time.Time) (bool, error) {
	return false, nil
}
func (s *engagementRepoStubForHandler) CreateShare(_ *domain.PostShare) error { return nil }
func (s *engagementRepoStubForHandler) GetStats(postID uuid.UUID) (*domain.PostStats, error) {
	if s.getStatsFn != nil {
		return s.getStatsFn(postID)
	}
	return &domain.PostStats{PostID: postID}, nil
}
func (s *engagementRepoStubForHandler) GetBatchStats(_ []uuid.UUID) ([]*domain.PostStats, error) {
	return nil, nil
}
func (s *engagementRepoStubForHandler) UpsertStats(_ *domain.PostStats) error     { return nil }
func (s *engagementRepoStubForHandler) IncrementStat(_ uuid.UUID, _ string, _ int) error { return nil }
func (s *engagementRepoStubForHandler) GetTrending(_ repository.TrendingQuery) ([]*domain.Post, error) {
	return nil, nil
}
func (s *engagementRepoStubForHandler) GetPopular(_ int) ([]*domain.Post, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helper to create CommentService for tests
// ---------------------------------------------------------------------------

func newCommentSvc(cfg *config.Config, commentRepo repository.CommentRepository, postRepo repository.PostRepository) *service.CommentService {
	return service.NewCommentService(cfg, commentRepo, postRepo)
}
