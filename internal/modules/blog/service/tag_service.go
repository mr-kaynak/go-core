package service

import (
	"context"
	stderrors "errors"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"gorm.io/gorm"
)

// CreateTagRequest holds the request data for creating a tag
type CreateTagRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
}

// TagService handles blog tag business logic
type TagService struct {
	tagRepo repository.TagRepository
	slugSvc *SlugService
	logger  *logger.Logger
}

// NewTagService creates a new TagService
func NewTagService(tagRepo repository.TagRepository, slugSvc *SlugService) *TagService {
	return &TagService{
		tagRepo: tagRepo,
		slugSvc: slugSvc,
		logger:  logger.Get().WithFields(logger.Fields{"service": "blog_tag"}),
	}
}

// Create creates a new tag
func (s *TagService) Create(ctx context.Context, req *CreateTagRequest) (*domain.Tag, error) {
	slug := s.slugSvc.Generate(req.Name)
	exists, err := s.tagRepo.ExistsBySlug(ctx, slug)
	if err != nil {
		return nil, errors.NewInternalError("Failed to check slug")
	}
	if exists {
		return nil, errors.NewConflict("Tag with this name already exists")
	}

	tag := &domain.Tag{
		Name: req.Name,
		Slug: slug,
	}

	if err := s.tagRepo.Create(ctx, tag); err != nil {
		s.logger.Error("Failed to create tag", "error", err)
		return nil, errors.NewInternalError("Failed to create tag")
	}

	s.logger.Info("Tag created", "tag_id", tag.ID)
	return tag, nil
}

// Delete deletes a tag
func (s *TagService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.tagRepo.Delete(ctx, id); err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.NewNotFound("Tag", id.String())
		}
		return errors.NewInternalError("Failed to delete tag")
	}
	return nil
}

// List lists all tags with pagination
func (s *TagService) List(ctx context.Context, offset, limit int) ([]*domain.Tag, int64, error) {
	return s.tagRepo.GetAll(ctx, offset, limit)
}

// GetPopular returns the most popular tags
func (s *TagService) GetPopular(ctx context.Context, limit int) ([]*domain.Tag, error) {
	return s.tagRepo.GetPopular(ctx, limit)
}

// GetOrCreateByNames gets or creates tags by their names
func (s *TagService) GetOrCreateByNames(ctx context.Context, names []string) ([]*domain.Tag, error) {
	return s.tagRepo.GetOrCreateByNames(ctx, names, s.slugSvc.Generate)
}
