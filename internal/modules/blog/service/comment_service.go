package service

import (
	"context"
	stderrors "errors"

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
	sseSvc         *notificationService.SSEService
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

// Create creates a new comment
func (s *CommentService) Create(postID uuid.UUID, req *CreateCommentRequest, authorID *uuid.UUID) (*domain.Comment, error) {
	// Verify post exists and is published
	post, err := s.postRepo.GetByID(postID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, 404, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}

	if post.Status != domain.PostStatusPublished {
		return nil, errors.New(errors.CodeBlogInvalidStatus, 400, "Invalid Status", "Comments can only be added to published posts")
	}

	var parentID *uuid.UUID
	if req.ParentID != nil {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			return nil, errors.NewBadRequest("Invalid parent ID format")
		}
		parentID = &pid
	}

	status := domain.CommentStatusPending
	if s.cfg.Blog.AutoApproveComments {
		status = domain.CommentStatusApproved
	}

	// Sanitize comment content and guest name to prevent XSS
	sanitizedContent := s.sanitizer.Sanitize(req.Content)

	comment := &domain.Comment{
		PostID:     postID,
		AuthorID:   authorID,
		ParentID:   parentID,
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
	metrics.GetMetrics().RecordBlogCommentCreated(commentType)

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
		_ = s.sseSvc.BroadcastToChannel(context.Background(), "blog:comments", event)
	}

	s.logger.Info("Comment created", "comment_id", comment.ID, "post_id", postID)
	return comment, nil
}

// Approve approves a pending comment
func (s *CommentService) Approve(id uuid.UUID) (*domain.Comment, error) {
	comment, err := s.commentRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogCommentNotFound, 404, "Comment Not Found", "Comment not found")
		}
		return nil, errors.NewInternalError("Failed to get comment")
	}

	if comment.Status != domain.CommentStatusPending {
		return nil, errors.New(errors.CodeBlogInvalidStatus, 400, "Invalid Status", "Only pending comments can be approved")
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
		_ = s.sseSvc.BroadcastToChannel(context.Background(), "blog:comments", event)
	}

	s.logger.Info("Comment approved", "comment_id", id)
	return comment, nil
}

// Reject rejects a pending comment
func (s *CommentService) Reject(id uuid.UUID) (*domain.Comment, error) {
	comment, err := s.commentRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogCommentNotFound, 404, "Comment Not Found", "Comment not found")
		}
		return nil, errors.NewInternalError("Failed to get comment")
	}

	if comment.Status != domain.CommentStatusPending {
		return nil, errors.New(errors.CodeBlogInvalidStatus, 400, "Invalid Status", "Only pending comments can be rejected")
	}

	comment.Status = domain.CommentStatusRejected
	if err := s.commentRepo.Update(comment); err != nil {
		return nil, errors.NewInternalError("Failed to reject comment")
	}

	s.logger.Info("Comment rejected", "comment_id", id)
	return comment, nil
}

// Delete soft-deletes a comment
func (s *CommentService) Delete(id uuid.UUID, requesterID uuid.UUID, isAdmin bool) error {
	comment, err := s.commentRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(errors.CodeBlogCommentNotFound, 404, "Comment Not Found", "Comment not found")
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

// GetThreaded returns threaded comments for a post
func (s *CommentService) GetThreaded(postID uuid.UUID) ([]*domain.Comment, error) {
	return s.commentRepo.GetThreaded(postID)
}

// ListPending returns pending comments
func (s *CommentService) ListPending(offset, limit int) ([]*domain.Comment, int64, error) {
	return s.commentRepo.ListPending(offset, limit)
}
