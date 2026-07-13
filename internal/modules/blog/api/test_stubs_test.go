package api

import (
	"context"
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
func (s *engagementRepoStubForHandler) CreateLike(_ context.Context, _ *domain.PostLike) error {
	return nil
}
func (s *engagementRepoStubForHandler) DeleteLike(_ context.Context, _, _ uuid.UUID) error {
	return nil
}
func (s *engagementRepoStubForHandler) IsLiked(_ context.Context, postID, userID uuid.UUID) (bool, error) {
	if s.isLikedFn != nil {
		return s.isLikedFn(postID, userID)
	}
	return false, nil
}
func (s *engagementRepoStubForHandler) ToggleLike(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}
func (s *engagementRepoStubForHandler) CreateView(_ context.Context, _ *domain.PostView) error {
	return nil
}
func (s *engagementRepoStubForHandler) HasRecentView(
	_ context.Context, _ uuid.UUID, _ string, _ time.Time,
) (bool, error) {
	return false, nil
}
func (s *engagementRepoStubForHandler) HasRecentUserView(
	_ context.Context, _ uuid.UUID, _ uuid.UUID, _ time.Time,
) (bool, error) {
	return false, nil
}
func (s *engagementRepoStubForHandler) CreateShare(_ context.Context, _ *domain.PostShare) error {
	return nil
}
func (s *engagementRepoStubForHandler) GetStats(_ context.Context, postID uuid.UUID) (*domain.PostStats, error) {
	if s.getStatsFn != nil {
		return s.getStatsFn(postID)
	}
	return &domain.PostStats{PostID: postID}, nil
}
func (s *engagementRepoStubForHandler) GetBatchStats(_ context.Context, _ []uuid.UUID) ([]*domain.PostStats, error) {
	return nil, nil
}
func (s *engagementRepoStubForHandler) UpsertStats(_ context.Context, _ *domain.PostStats) error {
	return nil
}
func (s *engagementRepoStubForHandler) IncrementStat(_ context.Context, _ uuid.UUID, _ string, _ int) error {
	return nil
}
func (s *engagementRepoStubForHandler) GetTrending(
	_ context.Context, _ repository.TrendingQuery,
) ([]*domain.TrendingPost, error) {
	return nil, nil
}
func (s *engagementRepoStubForHandler) GetPopular(_ context.Context, _ int) ([]*domain.Post, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helper to create CommentService for tests
// ---------------------------------------------------------------------------

func newCommentSvc(cfg *config.Config, commentRepo repository.CommentRepository, postRepo repository.PostRepository) *service.CommentService {
	// db is nil here; safe because these handler tests never set an engagementRepo,
	// so the transaction path is never reached.
	return service.NewCommentService(cfg, nil, commentRepo, postRepo)
}
