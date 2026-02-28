package service

import (
	"context"
	stderrors "errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"gorm.io/gorm"
)

// PresignedUploadResponse holds the presigned upload URL info
type PresignedUploadResponse struct {
	UploadURL string `json:"upload_url"`
	S3Key     string `json:"s3_key"`
}

// RegisterMediaRequest holds the request data for registering an uploaded media
type RegisterMediaRequest struct {
	PostID      string `json:"post_id" validate:"required,uuid"`
	S3Key       string `json:"s3_key" validate:"required,max=512"`
	Filename    string `json:"filename" validate:"required,max=255"`
	MediaType   string `json:"media_type" validate:"required,oneof=image video file"`
	ContentType string `json:"content_type" validate:"required,max=100"`
	FileSize    int64  `json:"file_size" validate:"required,min=1"`
}

// MediaService handles blog media operations
type MediaService struct {
	postRepo   repository.PostRepository
	storageSvc storage.StorageService
	cfg        *config.Config
	logger     *logger.Logger
}

// NewMediaService creates a new MediaService
func NewMediaService(postRepo repository.PostRepository, storageSvc storage.StorageService, cfg *config.Config) *MediaService {
	return &MediaService{
		postRepo:   postRepo,
		storageSvc: storageSvc,
		cfg:        cfg,
		logger:     logger.Get().WithFields(logger.Fields{"service": "blog_media"}),
	}
}

// GeneratePresignedUpload generates a presigned upload URL
func (s *MediaService) GeneratePresignedUpload(postID uuid.UUID, filename, contentType string, uploaderID uuid.UUID, isAdmin bool) (*PresignedUploadResponse, error) {
	// Verify post exists and check ownership
	post, err := s.postRepo.GetByID(postID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, 404, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to verify post")
	}

	if !isAdmin && post.AuthorID != uploaderID {
		return nil, errors.NewForbidden("You are not the author of this post")
	}

	s3Key := fmt.Sprintf("blog/%s/%s_%s", postID.String(), uuid.New().String()[:8], filename)

	uploadURL, err := s.storageSvc.GetURL(context.Background(), s3Key)
	if err != nil {
		s.logger.Error("Failed to generate presigned URL", "error", err)
		return nil, errors.NewInternalError("Failed to generate upload URL")
	}

	return &PresignedUploadResponse{
		UploadURL: uploadURL,
		S3Key:     s3Key,
	}, nil
}

// Register registers an uploaded media file in the database
func (s *MediaService) Register(req *RegisterMediaRequest, uploaderID uuid.UUID, isAdmin bool) (*domain.PostMedia, error) {
	postID, _ := uuid.Parse(req.PostID)

	// Verify post ownership
	post, err := s.postRepo.GetByID(postID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, 404, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to verify post")
	}
	if !isAdmin && post.AuthorID != uploaderID {
		return nil, errors.NewForbidden("You are not the author of this post")
	}

	if req.FileSize > s.cfg.Blog.MaxMediaSize {
		return nil, errors.New(errors.CodeBlogMediaLimitExceeded, 400, "File Too Large",
			fmt.Sprintf("File size exceeds maximum allowed size of %d bytes", s.cfg.Blog.MaxMediaSize))
	}

	media := &domain.PostMedia{
		PostID:      postID,
		UploaderID:  uploaderID,
		S3Key:       req.S3Key,
		Filename:    req.Filename,
		MediaType:   domain.MediaType(req.MediaType),
		ContentType: req.ContentType,
		FileSize:    req.FileSize,
	}

	if err := s.postRepo.CreateMedia(media); err != nil {
		s.logger.Error("Failed to register media", "error", err)
		return nil, errors.NewInternalError("Failed to register media")
	}

	// Set URL for response
	url, err := s.storageSvc.GetURL(context.Background(), req.S3Key)
	if err == nil {
		media.URL = url
	}

	s.logger.Info("Media registered", "media_id", media.ID, "post_id", postID)
	return media, nil
}

// Delete deletes a media file from storage and database
func (s *MediaService) Delete(mediaID uuid.UUID, requesterID uuid.UUID, isAdmin bool) error {
	media, err := s.postRepo.GetMediaByID(mediaID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(errors.CodeBlogMediaNotFound, 404, "Media Not Found", "Media not found")
		}
		return errors.NewInternalError("Failed to get media")
	}

	if !isAdmin && media.UploaderID != requesterID {
		return errors.NewForbidden("You are not the uploader of this media")
	}

	// Delete from storage
	if err := s.storageSvc.Delete(context.Background(), media.S3Key); err != nil {
		s.logger.Error("Failed to delete media from storage", "s3_key", media.S3Key, "error", err)
	}

	// Delete from database
	if err := s.postRepo.DeleteMedia(mediaID); err != nil {
		return errors.NewInternalError("Failed to delete media")
	}

	s.logger.Info("Media deleted", "media_id", mediaID)
	return nil
}

// ListByPost lists all media for a post
func (s *MediaService) ListByPost(postID uuid.UUID) ([]*domain.PostMedia, error) {
	media, err := s.postRepo.ListMediaByPost(postID)
	if err != nil {
		return nil, errors.NewInternalError("Failed to list media")
	}

	// Populate URLs
	for _, m := range media {
		url, err := s.storageSvc.GetURL(context.Background(), m.S3Key)
		if err == nil {
			m.URL = url
		}
	}

	return media, nil
}
