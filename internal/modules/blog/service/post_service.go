package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"gorm.io/gorm"
)

// CreatePostRequest holds the request data for creating a blog post
type CreatePostRequest struct {
	Title           string          `json:"title" validate:"required,min=1,max=255"`
	ContentJSON     json.RawMessage `json:"content_json" validate:"required"`
	Excerpt         string          `json:"excerpt" validate:"max=500"`
	CoverImageURL   string          `json:"cover_image_url" validate:"omitempty,max=512"`
	MetaTitle       string          `json:"meta_title" validate:"omitempty,max=255"`
	MetaDescription string          `json:"meta_description" validate:"omitempty,max=500"`
	CategoryID      *string         `json:"category_id" validate:"omitempty,uuid"`
	TagNames        []string        `json:"tag_names"`
	IsFeatured      bool            `json:"is_featured"`
}

// UpdatePostRequest holds the request data for updating a blog post
type UpdatePostRequest struct {
	Title           *string         `json:"title" validate:"omitempty,min=1,max=255"`
	ContentJSON     json.RawMessage `json:"content_json"`
	Excerpt         *string         `json:"excerpt" validate:"omitempty,max=500"`
	CoverImageURL   *string         `json:"cover_image_url" validate:"omitempty,max=512"`
	MetaTitle       *string         `json:"meta_title" validate:"omitempty,max=255"`
	MetaDescription *string         `json:"meta_description" validate:"omitempty,max=500"`
	CategoryID      *string         `json:"category_id" validate:"omitempty,uuid"`
	TagNames        []string        `json:"tag_names"`
	IsFeatured      *bool           `json:"is_featured"`
}

// PostService handles blog post business logic
type PostService struct {
	db             *gorm.DB
	postRepo       repository.PostRepository
	categoryRepo   repository.CategoryRepository
	tagRepo        repository.TagRepository
	contentSvc     *ContentService
	slugSvc        *SlugService
	readTimeSvc    *ReadTimeService
	engagementRepo repository.EngagementRepository
	sseSvc         *notificationService.SSEService
	metrics        metrics.MetricsRecorder
	logger         *logger.Logger
}

// NewPostService creates a new PostService
func NewPostService(
	db *gorm.DB,
	postRepo repository.PostRepository,
	categoryRepo repository.CategoryRepository,
	tagRepo repository.TagRepository,
	contentSvc *ContentService,
	slugSvc *SlugService,
	readTimeSvc *ReadTimeService,
) *PostService {
	return &PostService{
		db:           db,
		postRepo:     postRepo,
		categoryRepo: categoryRepo,
		tagRepo:      tagRepo,
		contentSvc:   contentSvc,
		slugSvc:      slugSvc,
		readTimeSvc:  readTimeSvc,
		logger:       logger.Get().WithFields(logger.Fields{"service": "blog_post"}),
	}
}

// SetSSEService sets the optional SSE service for broadcasting events
func (s *PostService) SetSSEService(svc *notificationService.SSEService) {
	s.sseSvc = svc
}

// SetMetrics sets the optional metrics recorder.
func (s *PostService) SetMetrics(m metrics.MetricsRecorder) {
	s.metrics = m
}

func (s *PostService) getMetrics() metrics.MetricsRecorder {
	if s.metrics != nil {
		return s.metrics
	}
	if m := metrics.GetMetrics(); m != nil {
		return m
	}
	return metrics.NoOpMetrics{}
}

// SetEngagementRepo sets the optional engagement repository
func (s *PostService) SetEngagementRepo(repo repository.EngagementRepository) {
	s.engagementRepo = repo
}

// Create creates a new blog post
func (s *PostService) Create(ctx context.Context, req *CreatePostRequest, authorID uuid.UUID) (*domain.Post, error) {
	if err := validateURLScheme(req.CoverImageURL); err != nil {
		return nil, errors.NewBadRequest(err.Error())
	}

	// Validate content
	if err := s.contentSvc.ValidateContent(req.ContentJSON); err != nil {
		return nil, errors.New(errors.CodeBlogInvalidContent, http.StatusBadRequest, "Invalid Content", err.Error())
	}

	// Process content
	contentHTML, err := s.contentSvc.SerializeToHTML(req.ContentJSON)
	if err != nil {
		return nil, errors.New(errors.CodeBlogInvalidContent, http.StatusBadRequest, "Invalid Content", err.Error())
	}
	contentHTML = s.contentSvc.SanitizeHTML(contentHTML)

	plainText, err := s.contentSvc.ExtractPlainText(req.ContentJSON)
	if err != nil {
		s.logger.Error("Failed to extract plain text", "error", err)
		plainText = ""
	}

	categoryID, err := s.parseCategoryID(req.CategoryID)
	if err != nil {
		return nil, err
	}

	// Calculate read time
	readTime := s.readTimeSvc.Calculate(plainText)

	baseSlug := s.slugSvc.Generate(req.Title)

	post := &domain.Post{
		Title:           req.Title,
		Excerpt:         req.Excerpt,
		ContentJSON:     domain.ContentJSON(req.ContentJSON),
		ContentHTML:     contentHTML,
		ContentPlain:    plainText,
		CoverImageURL:   req.CoverImageURL,
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		Status:          domain.PostStatusDraft,
		AuthorID:        authorID,
		CategoryID:      categoryID,
		ReadTime:        readTime,
		IsFeatured:      req.IsFeatured,
	}

	// Retry loop: handles slug unique constraint violations
	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		post.Slug = s.resolveUniqueSlug(baseSlug)

		txErr := s.db.Transaction(func(tx *gorm.DB) error {
			txPostRepo := s.postRepo.WithTx(tx)

			if err := txPostRepo.Create(post); err != nil {
				return fmt.Errorf("create post: %w", err)
			}

			// Create initial stats row
			if s.engagementRepo != nil {
				stats := &domain.PostStats{PostID: post.ID, UpdatedAt: time.Now()}
				if err := tx.Create(stats).Error; err != nil {
					return fmt.Errorf("create post stats: %w", err)
				}
			}

			// Create initial revision
			version, err := txPostRepo.GetLatestRevisionVersion(post.ID)
			if err != nil {
				return fmt.Errorf("get revision version: %w", err)
			}
			revision := &domain.PostRevision{
				PostID:      post.ID,
				EditorID:    authorID,
				Title:       post.Title,
				ContentJSON: post.ContentJSON,
				ContentHTML: post.ContentHTML,
				Excerpt:     post.Excerpt,
				Version:     version + 1,
			}
			if err := txPostRepo.CreateRevision(revision); err != nil {
				return fmt.Errorf("create revision: %w", err)
			}

			// Handle tags
			if len(req.TagNames) > 0 {
				txTagRepo := s.tagRepo.WithTx(tx)
				tags, err := txTagRepo.GetOrCreateByNames(req.TagNames, s.slugSvc.Generate)
				if err != nil {
					return fmt.Errorf("create tags: %w", err)
				}
				tagIDs := make([]uuid.UUID, len(tags))
				for i, t := range tags {
					tagIDs[i] = t.ID
				}
				if err := txPostRepo.ReplaceTags(post.ID, tagIDs); err != nil {
					return fmt.Errorf("associate tags: %w", err)
				}
			}

			return nil
		})

		if txErr == nil {
			break
		}

		// Check if it's a unique constraint violation on slug — retry with new suffix
		if isUniqueViolation(txErr) && attempt < maxRetries {
			post.ID = uuid.Nil // reset ID so BeforeCreate generates a new one
			baseSlug = fmt.Sprintf("%s-%s", s.slugSvc.Generate(req.Title), uuid.New().String()[:8])
			s.logger.Warn("Slug collision, retrying", "attempt", attempt+1, "slug", baseSlug)
			continue
		}

		s.logger.Error("Failed to create post", "error", txErr)
		return nil, errors.NewInternalError("Failed to create post")
	}

	s.getMetrics().RecordBlogPostCreated(string(domain.PostStatusDraft))
	s.logger.Info("Post created", "post_id", post.ID, "slug", post.Slug)
	return post, nil
}

// CreateDraft creates a minimal empty draft post for lazy draft creation.
// Used when the frontend editor needs a post_id before any content is written.
func (s *PostService) CreateDraft(ctx context.Context, authorID uuid.UUID) (*domain.Post, error) {
	post := &domain.Post{
		Title:    "",
		Slug:     fmt.Sprintf("draft-%s", uuid.New().String()),
		Status:   domain.PostStatusDraft,
		AuthorID: authorID,
	}

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		txPostRepo := s.postRepo.WithTx(tx)

		if err := txPostRepo.Create(post); err != nil {
			return fmt.Errorf("create draft: %w", err)
		}

		if s.engagementRepo != nil {
			stats := &domain.PostStats{PostID: post.ID, UpdatedAt: time.Now()}
			if err := tx.Create(stats).Error; err != nil {
				return fmt.Errorf("create post stats: %w", err)
			}
		}

		return nil
	})

	if txErr != nil {
		s.logger.Error("Failed to create draft", "error", txErr)
		return nil, errors.NewInternalError("Failed to create draft")
	}

	s.getMetrics().RecordBlogPostCreated(string(domain.PostStatusDraft))
	s.logger.Info("Draft created", "post_id", post.ID)
	return post, nil
}

// parseCategoryID parses and validates an optional category ID string.
func (s *PostService) parseCategoryID(rawID *string) (*uuid.UUID, error) {
	if rawID == nil {
		return nil, nil
	}
	cid, err := uuid.Parse(*rawID)
	if err != nil {
		return nil, errors.NewBadRequest("Invalid category ID format")
	}
	if _, err := s.categoryRepo.GetByID(cid); err != nil {
		return nil, errors.New(
			errors.CodeBlogCategoryNotFound, http.StatusNotFound,
			"Category Not Found", "Category not found",
		)
	}
	return &cid, nil
}

// resolveUniqueSlug finds a unique slug by appending numeric suffixes
func (s *PostService) resolveUniqueSlug(baseSlug string) string {
	for i := 0; i < 10; i++ {
		candidate := baseSlug
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", baseSlug, i)
		}
		exists, err := s.postRepo.ExistsBySlug(candidate)
		if err != nil {
			return fmt.Sprintf("%s-%s", baseSlug, uuid.New().String()[:8])
		}
		if !exists {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%s", baseSlug, uuid.New().String()[:8])
}

// validateURLScheme checks that a URL uses only http, https, or is relative.
func validateURLScheme(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	switch u.Scheme {
	case "", "http", "https":
		return nil
	default:
		return fmt.Errorf("URL scheme %q not allowed; use http or https", u.Scheme)
	}
}

// isUniqueViolation checks if an error is a PostgreSQL unique constraint violation
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "23505")
}

// Update updates an existing blog post
func (s *PostService) Update(
	ctx context.Context, id uuid.UUID, req *UpdatePostRequest, editorID uuid.UUID, isAdmin bool,
) (*domain.Post, error) {
	post, err := s.postRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}

	if !isAdmin && post.AuthorID != editorID {
		return nil, errors.New(errors.CodeBlogNotAuthor, http.StatusForbidden, "Forbidden", "You are not the author of this post")
	}

	if err := s.applyFieldUpdates(post, req); err != nil {
		return nil, err
	}

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		txPostRepo := s.postRepo.WithTx(tx)
		txTagRepo := s.tagRepo.WithTx(tx)

		if err := txPostRepo.Update(post); err != nil {
			return fmt.Errorf("update post: %w", err)
		}

		// Create revision
		version, err := txPostRepo.GetLatestRevisionVersion(post.ID)
		if err != nil {
			return fmt.Errorf("get revision version: %w", err)
		}
		revision := &domain.PostRevision{
			PostID:      post.ID,
			EditorID:    editorID,
			Title:       post.Title,
			ContentJSON: post.ContentJSON,
			ContentHTML: post.ContentHTML,
			Excerpt:     post.Excerpt,
			Version:     version + 1,
		}
		if err := txPostRepo.CreateRevision(revision); err != nil {
			return fmt.Errorf("create revision: %w", err)
		}

		// Handle tags
		if req.TagNames != nil {
			tags, err := txTagRepo.GetOrCreateByNames(req.TagNames, s.slugSvc.Generate)
			if err != nil {
				return fmt.Errorf("create tags: %w", err)
			}
			tagIDs := make([]uuid.UUID, len(tags))
			for i, t := range tags {
				tagIDs[i] = t.ID
			}
			if err := txPostRepo.ReplaceTags(post.ID, tagIDs); err != nil {
				return fmt.Errorf("replace tags: %w", err)
			}
		}

		return nil
	})

	if txErr != nil {
		s.logger.Error("Failed to update post", "error", txErr)
		return nil, errors.NewInternalError("Failed to update post")
	}

	// Broadcast SSE event if published
	if post.IsPublished() && s.sseSvc != nil {
		event := domain.NewSSEBlogPostEvent(domain.SSEEventTypeBlogPostUpdated, domain.SSEBlogPostData{
			PostID:   post.ID,
			Title:    post.Title,
			Slug:     post.Slug,
			AuthorID: post.AuthorID,
		})
		_ = s.sseSvc.BroadcastToChannel(ctx, "blog:posts", event)
	}

	s.logger.Info("Post updated", "post_id", post.ID)
	return post, nil
}

func (s *PostService) applyContentUpdate(post *domain.Post, contentJSON []byte) error {
	if err := s.contentSvc.ValidateContent(contentJSON); err != nil {
		return errors.New(errors.CodeBlogInvalidContent, http.StatusBadRequest, "Invalid Content", err.Error())
	}
	contentHTML, err := s.contentSvc.SerializeToHTML(contentJSON)
	if err != nil {
		return errors.New(errors.CodeBlogInvalidContent, http.StatusBadRequest, "Invalid Content", err.Error())
	}
	contentHTML = s.contentSvc.SanitizeHTML(contentHTML)
	plainText, _ := s.contentSvc.ExtractPlainText(contentJSON)

	post.ContentJSON = domain.ContentJSON(contentJSON)
	post.ContentHTML = contentHTML
	post.ContentPlain = plainText
	post.ReadTime = s.readTimeSvc.Calculate(plainText)
	return nil
}

func (s *PostService) applyFieldUpdates(post *domain.Post, req *UpdatePostRequest) error {
	if req.Title != nil {
		post.Title = *req.Title
		newSlug := s.slugSvc.Generate(*req.Title)
		exists, err := s.postRepo.ExistsBySlugExcluding(newSlug, post.ID)
		if err != nil {
			return errors.NewInternalError("Failed to check slug")
		}
		if exists {
			newSlug = fmt.Sprintf("%s-%s", newSlug, uuid.New().String()[:8])
		}
		post.Slug = newSlug
	}
	if req.ContentJSON != nil {
		if err := s.applyContentUpdate(post, req.ContentJSON); err != nil {
			return err
		}
	}
	if req.CategoryID != nil {
		cid, err := s.parseCategoryID(req.CategoryID)
		if err != nil {
			return err
		}
		post.CategoryID = cid
	}
	if req.Excerpt != nil {
		post.Excerpt = *req.Excerpt
	}
	if req.CoverImageURL != nil {
		if err := validateURLScheme(*req.CoverImageURL); err != nil {
			return errors.NewBadRequest(err.Error())
		}
		post.CoverImageURL = *req.CoverImageURL
	}
	if req.IsFeatured != nil {
		post.IsFeatured = *req.IsFeatured
	}
	if req.MetaTitle != nil {
		post.MetaTitle = *req.MetaTitle
	}
	if req.MetaDescription != nil {
		post.MetaDescription = *req.MetaDescription
	}
	return nil
}

// Publish publishes a draft post
func (s *PostService) Publish(ctx context.Context, id uuid.UUID, publisherID uuid.UUID, isAdmin bool) (*domain.Post, error) {
	post, err := s.postRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}

	if !isAdmin && post.AuthorID != publisherID {
		return nil, errors.New(errors.CodeBlogNotAuthor, http.StatusForbidden, "Forbidden", "You are not the author of this post")
	}

	if !post.CanTransition(domain.PostStatusPublished) {
		return nil, errors.New(errors.CodeBlogInvalidStatus, http.StatusConflict,
			"Invalid Transition", fmt.Sprintf("Cannot publish a post with status %q", post.Status))
	}

	// Validate post has required content before publishing
	if strings.TrimSpace(post.Title) == "" {
		return nil, errors.NewBadRequest("Cannot publish a post with an empty title")
	}
	if strings.TrimSpace(post.ContentHTML) == "" {
		return nil, errors.NewBadRequest("Cannot publish a post with empty content")
	}

	now := time.Now()
	post.Status = domain.PostStatusPublished
	post.PublishedAt = &now

	if err := s.postRepo.Update(post); err != nil {
		return nil, errors.NewInternalError("Failed to publish post")
	}

	s.getMetrics().RecordBlogPostPublished()

	// Broadcast SSE event
	if s.sseSvc != nil {
		event := domain.NewSSEBlogPostEvent(domain.SSEEventTypeBlogPostPublished, domain.SSEBlogPostData{
			PostID:      post.ID,
			Title:       post.Title,
			Slug:        post.Slug,
			AuthorID:    post.AuthorID,
			PublishedAt: post.PublishedAt,
		})
		_ = s.sseSvc.BroadcastToChannel(ctx, "blog:posts", event)
	}

	s.logger.Info("Post published", "post_id", post.ID)
	return post, nil
}

// Archive archives a published post
func (s *PostService) Archive(ctx context.Context, id uuid.UUID, requesterID uuid.UUID, isAdmin bool) (*domain.Post, error) {
	post, err := s.postRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}

	if !isAdmin && post.AuthorID != requesterID {
		return nil, errors.New(errors.CodeBlogNotAuthor, http.StatusForbidden, "Forbidden", "You are not the author of this post")
	}

	if !post.CanTransition(domain.PostStatusArchived) {
		return nil, errors.New(errors.CodeBlogInvalidStatus, http.StatusConflict,
			"Invalid Transition", fmt.Sprintf("Cannot archive a post with status %q", post.Status))
	}

	post.Status = domain.PostStatusArchived
	if err := s.postRepo.Update(post); err != nil {
		return nil, errors.NewInternalError("Failed to archive post")
	}

	s.logger.Info("Post archived", "post_id", post.ID)
	return post, nil
}

// Delete soft-deletes a post
func (s *PostService) Delete(ctx context.Context, id uuid.UUID, requesterID uuid.UUID, isAdmin bool) error {
	post, err := s.postRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return errors.NewInternalError("Failed to get post")
	}

	if !isAdmin && post.AuthorID != requesterID {
		return errors.New(errors.CodeBlogNotAuthor, http.StatusForbidden, "Forbidden", "You are not the author of this post")
	}

	if err := s.postRepo.Delete(id); err != nil {
		return errors.NewInternalError("Failed to delete post")
	}
	s.logger.Info("Post deleted", "post_id", id)
	return nil
}

// GetBySlug retrieves a published post by slug (public access)
func (s *PostService) GetBySlug(slug string) (*domain.Post, error) {
	post, err := s.postRepo.GetBySlugPublished(slug)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}
	return post, nil
}

// GetForEdit retrieves a post for editing (includes content_json)
func (s *PostService) GetForEdit(id uuid.UUID, requesterID uuid.UUID, isAdmin bool) (*domain.Post, error) {
	post, err := s.postRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogPostNotFound, http.StatusNotFound, "Post Not Found", "Post not found")
		}
		return nil, errors.NewInternalError("Failed to get post")
	}

	if !isAdmin && post.AuthorID != requesterID {
		return nil, errors.New(errors.CodeBlogNotAuthor, http.StatusForbidden, "Forbidden", "You are not the author of this post")
	}

	return post, nil
}

// validPostStatuses is the whitelist of allowed status filter values.
var validPostStatuses = map[string]bool{
	string(domain.PostStatusDraft):     true,
	string(domain.PostStatusPublished): true,
	string(domain.PostStatusArchived):  true,
}

// List lists posts with filtering and pagination
func (s *PostService) List(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
	if filter.Status != "" && !validPostStatuses[filter.Status] {
		return nil, 0, errors.NewBadRequest("Invalid status filter: must be draft, published, or archived")
	}
	return s.postRepo.ListFiltered(filter)
}

// ListRevisions lists revisions for a post
func (s *PostService) ListRevisions(postID uuid.UUID) ([]*domain.PostRevision, error) {
	return s.postRepo.ListRevisions(postID)
}

// GetRevision gets a specific revision, verifying it belongs to the given post
func (s *PostService) GetRevision(postID, revisionID uuid.UUID) (*domain.PostRevision, error) {
	rev, err := s.postRepo.GetRevision(revisionID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFound("Revision", revisionID.String())
		}
		return nil, errors.NewInternalError("Failed to get revision")
	}
	if rev.PostID != postID {
		return nil, errors.NewNotFound("Revision", revisionID.String())
	}
	return rev, nil
}
