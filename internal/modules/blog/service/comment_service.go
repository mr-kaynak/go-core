package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"gorm.io/gorm"
)

// CreateCommentRequest holds the request data for creating a comment
type CreateCommentRequest struct {
	Content    string  `json:"content" validate:"required,min=1,max=5000"`
	ParentID   *string `json:"parent_id" validate:"omitempty,uuid"`
	GuestName  string  `json:"guest_name" validate:"omitempty,max=100"`
	GuestEmail string  `json:"guest_email" validate:"omitempty,email,max=255"`
}

// CommentService handles blog comment business logic
type CommentService struct {
	cfg            *config.Config
	commentRepo    repository.CommentRepository
	postRepo       repository.PostRepository
	engagementRepo repository.EngagementRepository
	settingsSvc    *SettingsService
	sseSvc         *notificationService.SSEService
	metrics        metrics.MetricsRecorder
	sanitizer      *bluemonday.Policy
	logger         *logger.Logger
}

// NewCommentService creates a new CommentService
func NewCommentService(cfg *config.Config, commentRepo repository.CommentRepository, postRepo repository.PostRepository) *CommentService {
	return &CommentService{
		cfg:         cfg,
		commentRepo: commentRepo,
		postRepo:    postRepo,
		sanitizer:   bluemonday.StrictPolicy(),
		logger:      logger.Get().WithFields(logger.Fields{"service": "blog_comment"}),
	}
}

// SetSSEService sets the optional SSE service
func (s *CommentService) SetSSEService(svc *notificationService.SSEService) {
	s.sseSvc = svc
}

// SetEngagementRepo sets the optional engagement repository
func (s *CommentService) SetEngagementRepo(repo repository.EngagementRepository) {
	s.engagementRepo = repo
}

// SetMetrics sets the optional metrics recorder.
func (s *CommentService) SetMetrics(m metrics.MetricsRecorder) {
	s.metrics = m
}

func (s *CommentService) getMetrics() metrics.MetricsRecorder {
	if s.metrics != nil {
		return s.metrics
	}
	if m := metrics.GetMetrics(); m != nil {
		return m
	}
	return metrics.NoOpMetrics{}
}

// SetSettingsService sets the optional settings service for runtime config
func (s *CommentService) SetSettingsService(svc *SettingsService) {
	s.settingsSvc = svc
}

// Create creates a new comment
func (s *CommentService) Create(
	ctx context.Context, postID uuid.UUID, req *CreateCommentRequest, authorID *uuid.UUID,
) (*domain.Comment, error) {
	// Verify post exists and is published
	post, err := s.postRepo.GetByID(postID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}

	if post.Status != domain.PostStatusPublished {
		return nil, errors.New(errors.CodeBlogInvalidStatus, http.StatusBadRequest,
			"Invalid Status", "Comments can only be added to published posts")
	}

	parentID, parentDepth, err := s.resolveParent(req.ParentID, postID)
	if err != nil {
		return nil, err
	}

	// Require guest name for unauthenticated users
	if authorID == nil && strings.TrimSpace(req.GuestName) == "" {
		return nil, errors.NewBadRequest("Guest name is required for unauthenticated comments")
	}

	status := s.resolveCommentStatus(ctx)

	// Sanitize comment content and guest name to prevent XSS
	sanitizedContent := s.sanitizer.Sanitize(req.Content)

	depth := 0
	if parentID != nil {
		depth = parentDepth + 1
	}

	comment := &domain.Comment{
		PostID:     postID,
		AuthorID:   authorID,
		ParentID:   parentID,
		Depth:      depth,
		Content:    sanitizedContent,
		GuestName:  s.sanitizer.Sanitize(req.GuestName),
		GuestEmail: req.GuestEmail,
		Status:     status,
	}

	if err := s.commentRepo.Create(comment); err != nil {
		s.logger.Error("Failed to create comment", "error", err)
		return nil, errors.NewInternalError("Failed to create comment")
	}

	// Increment comment count in stats
	if s.engagementRepo != nil && status == domain.CommentStatusApproved {
		_ = s.engagementRepo.IncrementStat(postID, "comment_count", 1)
	}

	// Record metric
	commentType := "authenticated"
	if authorID == nil {
		commentType = "guest"
	}
	s.getMetrics().RecordBlogCommentCreated(commentType)

	// Broadcast SSE event
	if s.sseSvc != nil {
		authorName := req.GuestName
		if authorName == "" {
			authorName = "User"
		}
		event := domain.NewSSEBlogCommentEvent(domain.SSEEventTypeBlogCommentNew, domain.SSEBlogCommentData{
			CommentID:  comment.ID,
			PostID:     postID,
			PostTitle:  post.Title,
			AuthorID:   authorID,
			AuthorName: authorName,
			Content:    sanitizedContent,
		})
		_ = s.sseSvc.BroadcastToChannel(ctx, "blog:comments", event)
	}

	s.logger.Info("Comment created", "comment_id", comment.ID, "post_id", postID)
	return comment, nil
}

func (s *CommentService) resolveParent(parentIDStr *string, postID uuid.UUID) (*uuid.UUID, int, error) {
	if parentIDStr == nil {
		return nil, 0, nil
	}
	pid, err := uuid.Parse(*parentIDStr)
	if err != nil {
		return nil, 0, errors.NewBadRequest("Invalid parent ID format")
	}
	parent, err := s.commentRepo.GetByID(pid)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, errors.NewBadRequest("Parent comment not found")
		}
		return nil, 0, errors.NewInternalError("Failed to verify parent comment")
	}
	if parent.PostID != postID {
		return nil, 0, errors.NewBadRequest("Parent comment does not belong to this post")
	}
	if parent.Depth >= domain.MaxCommentDepth {
		return nil, 0, errors.NewBadRequest("Maximum comment nesting depth reached")
	}
	return &pid, parent.Depth, nil
}

func (s *CommentService) resolveCommentStatus(ctx context.Context) domain.CommentStatus {
	autoApprove := s.cfg.Blog.AutoApproveComments
	if s.settingsSvc != nil {
		autoApprove = s.settingsSvc.Get(ctx).AutoApproveComments
	}
	if autoApprove {
		return domain.CommentStatusApproved
	}
	return domain.CommentStatusPending
}

// Approve approves a pending comment
func (s *CommentService) Approve(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	comment, err := s.commentRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogCommentNotFound, http.StatusNotFound, "Comment Not Found", "Comment not found")
		}
		return nil, errors.NewInternalError("Failed to get comment")
	}

	if comment.Status != domain.CommentStatusPending {
		return nil, errors.New(errors.CodeBlogInvalidStatus, http.StatusBadRequest, "Invalid Status", "Only pending comments can be approved")
	}

	comment.Status = domain.CommentStatusApproved
	if err := s.commentRepo.Update(comment); err != nil {
		return nil, errors.NewInternalError("Failed to approve comment")
	}

	// Increment comment count
	if s.engagementRepo != nil {
		_ = s.engagementRepo.IncrementStat(comment.PostID, "comment_count", 1)
	}

	// Broadcast SSE
	if s.sseSvc != nil {
		event := domain.NewSSEBlogCommentEvent(domain.SSEEventTypeBlogCommentApproved, domain.SSEBlogCommentData{
			CommentID: comment.ID,
			PostID:    comment.PostID,
		})
		_ = s.sseSvc.BroadcastToChannel(ctx, "blog:comments", event)
	}

	s.logger.Info("Comment approved", "comment_id", id)
	return comment, nil
}

// Reject rejects a pending comment
func (s *CommentService) Reject(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	comment, err := s.commentRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogCommentNotFound, http.StatusNotFound, "Comment Not Found", "Comment not found")
		}
		return nil, errors.NewInternalError("Failed to get comment")
	}

	if comment.Status != domain.CommentStatusPending {
		return nil, errors.New(errors.CodeBlogInvalidStatus, http.StatusBadRequest, "Invalid Status", "Only pending comments can be rejected")
	}

	comment.Status = domain.CommentStatusRejected
	if err := s.commentRepo.Update(comment); err != nil {
		return nil, errors.NewInternalError("Failed to reject comment")
	}

	s.logger.Info("Comment rejected", "comment_id", id)
	return comment, nil
}

// Delete soft-deletes a comment
func (s *CommentService) Delete(ctx context.Context, id uuid.UUID, requesterID uuid.UUID, isAdmin bool) error {
	comment, err := s.commentRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(errors.CodeBlogCommentNotFound, http.StatusNotFound, "Comment Not Found", "Comment not found")
		}
		return errors.NewInternalError("Failed to get comment")
	}

	if !isAdmin && (comment.AuthorID == nil || *comment.AuthorID != requesterID) {
		return errors.NewForbidden("You are not the author of this comment")
	}

	if err := s.commentRepo.Delete(id); err != nil {
		return errors.NewInternalError("Failed to delete comment")
	}

	// Decrement comment count if was approved
	if s.engagementRepo != nil && comment.Status == domain.CommentStatusApproved {
		_ = s.engagementRepo.IncrementStat(comment.PostID, "comment_count", -1)
	}

	s.logger.Info("Comment deleted", "comment_id", id)
	return nil
}

// GetThreaded returns threaded comments for a published post
func (s *CommentService) GetThreaded(postID uuid.UUID) ([]*domain.Comment, error) {
	post, err := s.postRepo.GetByID(postID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}
	if post.Status != domain.PostStatusPublished {
		return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
	}
	return s.commentRepo.GetThreaded(postID)
}

// ListPending returns pending comments
func (s *CommentService) ListPending(offset, limit int) ([]*domain.Comment, int64, error) {
	return s.commentRepo.ListPending(offset, limit)
}
