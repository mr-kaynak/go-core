package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// PostHandler handles blog post HTTP requests
type PostHandler struct {
	postSvc       *service.PostService
	engagementSvc *service.EngagementService
	postsPerPage  int
}

// NewPostHandler creates a new PostHandler
func NewPostHandler(postSvc *service.PostService, postsPerPage int) *PostHandler {
	return &PostHandler{
		postSvc:      postSvc,
		postsPerPage: postsPerPage,
	}
}

// SetEngagementService sets the optional engagement service
func (h *PostHandler) SetEngagementService(svc *service.EngagementService) {
	h.engagementSvc = svc
}

// RegisterRoutes registers post routes
func (h *PostHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler) {
	posts := blog.Group("/posts")

	// Public routes
	posts.Get("/", h.ListPublished)
	posts.Get("/trending", h.GetTrending)
	posts.Get("/popular", h.GetPopular)
	posts.Get("/:slug", h.GetBySlug)

	// Protected routes
	protected := posts.Group("", authMw)
	protected.Post("/", h.Create)
	protected.Put("/:id", h.Update)
	protected.Post("/:id/publish", h.Publish)
	protected.Post("/:id/archive", h.Archive)
	protected.Delete("/:id", h.SoftDelete)
	protected.Get("/:id/edit", h.GetForEdit)
	protected.Get("/:id/revisions", h.ListRevisions)
	protected.Get("/:id/revisions/:rid", h.GetRevision)
}

func (h *PostHandler) ListPublished(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", h.postsPerPage)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = h.postsPerPage
	}

	filter := repository.PostListFilter{
		Offset:   (page - 1) * limit,
		Limit:    limit,
		SortBy:   c.Query("sort_by", "published_at"),
		Order:    c.Query("order", "desc"),
		Search:   c.Query("search"),
		Status:   string(domain.PostStatusPublished),
	}

	if catID := c.Query("category_id"); catID != "" {
		id, err := uuid.Parse(catID)
		if err == nil {
			filter.CategoryID = &id
		}
	}
	if tagSlugs := c.Query("tags"); tagSlugs != "" {
		filter.TagSlugs = splitComma(tagSlugs)
	}

	posts, total, err := h.postSvc.List(filter)
	if err != nil {
		return err
	}

	responses := make([]*domain.PostResponse, len(posts))
	for i, p := range posts {
		responses[i] = toPostResponse(p)
	}

	return c.JSON(apiresponse.NewPaginatedResponse(responses, page, limit, total))
}

func (h *PostHandler) GetTrending(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 10)
	if limit < 1 || limit > 50 {
		limit = 10
	}

	if h.engagementSvc == nil {
		return c.JSON(fiber.Map{"items": []interface{}{}})
	}

	posts, err := h.engagementSvc.GetTrending(limit)
	if err != nil {
		return err
	}

	responses := make([]*domain.PostResponse, len(posts))
	for i, p := range posts {
		responses[i] = toPostResponse(p)
	}
	return c.JSON(fiber.Map{"items": responses})
}

func (h *PostHandler) GetPopular(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 10)
	if limit < 1 || limit > 50 {
		limit = 10
	}

	if h.engagementSvc == nil {
		return c.JSON(fiber.Map{"items": []interface{}{}})
	}

	posts, err := h.engagementSvc.GetPopular(limit)
	if err != nil {
		return err
	}

	responses := make([]*domain.PostResponse, len(posts))
	for i, p := range posts {
		responses[i] = toPostResponse(p)
	}
	return c.JSON(fiber.Map{"items": responses})
}

func (h *PostHandler) GetBySlug(c *fiber.Ctx) error {
	slug := c.Params("slug")
	post, err := h.postSvc.GetBySlug(slug)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)

	// Check if liked by current user
	if h.engagementSvc != nil {
		if userID := getUserIDFromCtx(c); userID != nil {
			liked, _ := h.engagementSvc.IsLiked(post.ID, *userID)
			resp.IsLiked = liked
		}
	}

	return c.JSON(resp)
}

func (h *PostHandler) Create(c *fiber.Ctx) error {
	var req service.CreatePostRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.Create(&req, *userID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(toPostResponse(post))
}

func (h *PostHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req service.UpdatePostRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.Update(id, &req, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.JSON(toPostResponse(post))
}

func (h *PostHandler) Publish(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.Publish(id, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.JSON(toPostResponse(post))
}

func (h *PostHandler) Archive(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.Archive(id, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.JSON(toPostResponse(post))
}

func (h *PostHandler) SoftDelete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.postSvc.Delete(id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *PostHandler) GetForEdit(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.GetForEdit(id, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.JSON(post)
}

func (h *PostHandler) ListRevisions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	// Verify post ownership before exposing revision history
	if _, err := h.postSvc.GetForEdit(id, *userID, isAdmin(c)); err != nil {
		return err
	}

	revisions, err := h.postSvc.ListRevisions(id)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": revisions})
}

func (h *PostHandler) GetRevision(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	rid, err := uuid.Parse(c.Params("rid"))
	if err != nil {
		return errors.NewBadRequest("Invalid revision ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	// Verify post ownership before exposing revision content
	if _, err := h.postSvc.GetForEdit(id, *userID, isAdmin(c)); err != nil {
		return err
	}

	revision, err := h.postSvc.GetRevision(rid)
	if err != nil {
		return err
	}

	return c.JSON(revision)
}

// toPostResponse converts a Post domain model to a public response
func toPostResponse(p *domain.Post) *domain.PostResponse {
	resp := &domain.PostResponse{
		ID:            p.ID,
		Title:         p.Title,
		Slug:          p.Slug,
		ContentHTML:   p.ContentHTML,
		Excerpt:       p.Excerpt,
		CoverImageURL: p.CoverImageURL,
		Status:        p.Status,
		PublishedAt:   p.PublishedAt,
		IsFeatured:    p.IsFeatured,
		IsLiked:       p.IsLiked,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}

	if p.Category != nil {
		resp.Category = &domain.CategorySummary{
			ID:   p.Category.ID,
			Name: p.Category.Name,
			Slug: p.Category.Slug,
		}
	}

	if len(p.Tags) > 0 {
		tags := make([]domain.TagSummary, len(p.Tags))
		for i, t := range p.Tags {
			tags[i] = domain.TagSummary{ID: t.ID, Name: t.Name, Slug: t.Slug}
		}
		resp.Tags = tags
	}

	if p.Stats != nil {
		resp.Stats = &domain.StatsSummary{
			LikeCount:    p.Stats.LikeCount,
			ViewCount:    p.Stats.ViewCount,
			ShareCount:   p.Stats.ShareCount,
			CommentCount: p.Stats.CommentCount,
			ReadTime:     p.ReadTime / 60, // seconds to minutes
		}
	}

	return resp
}
