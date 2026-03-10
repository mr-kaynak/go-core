package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

var allowedSortFields = map[string]bool{
	"created_at":   true,
	"updated_at":   true,
	"published_at": true,
	"title":        true,
}

func validateSortParams(sortBy, order string) error {
	if sortBy != "" && !allowedSortFields[sortBy] {
		return errors.NewBadRequest("Invalid sort_by field")
	}
	if order != "" && order != "asc" && order != "desc" {
		return errors.NewBadRequest("Invalid order: must be asc or desc")
	}
	return nil
}

// UserLookupFunc resolves minimal author info from a user ID.
type UserLookupFunc func(ctx context.Context, userID uuid.UUID) (*domain.PostAuthor, error)

// PostHandler handles blog post HTTP requests
type PostHandler struct {
	postSvc       *service.PostService
	engagementSvc *service.EngagementService
	postsPerPage  int
	userLookup    UserLookupFunc
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

// SetUserLookup sets the function used to resolve author info for posts.
func (h *PostHandler) SetUserLookup(fn UserLookupFunc) {
	h.userLookup = fn
}

// enrichPostResponse populates the Author field on a PostResponse.
func (h *PostHandler) enrichPostResponse(ctx context.Context, resp *domain.PostResponse, authorID uuid.UUID) {
	if h.userLookup != nil {
		author, err := h.userLookup(ctx, authorID)
		if err == nil && author != nil {
			resp.Author = author
		}
	}
}

// RegisterRoutes registers post routes
func (h *PostHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler) {
	posts := blog.Group("/posts")

	// Public routes - static paths first
	posts.Get("/", h.ListPublished)
	posts.Get("/trending", h.GetTrending)
	posts.Get("/popular", h.GetPopular)

	// Protected routes - BEFORE catch-all /:slug
	protected := posts.Group("", authMw)
	protected.Post("/draft", h.CreateDraft)
	protected.Post("/", h.Create)
	protected.Put("/:id", h.Update)
	protected.Post("/:id/publish", h.Publish)
	protected.Post("/:id/archive", h.Archive)
	protected.Delete("/:id", h.SoftDelete)
	protected.Get("/:id/edit", h.GetForEdit)
	protected.Get("/:id/revisions", h.ListRevisions)
	protected.Get("/:id/revisions/:rid", h.GetRevision)

	// Public catch-all - MUST be last
	posts.Get("/:slug", h.GetBySlug)
}

// ListPublished returns a paginated list of published blog posts.
// @Summary      List published posts
// @Description  Returns a paginated list of published blog posts with optional filtering
// @Tags         Blog Posts
// @Produce      json
// @Param        page        query  int     false  "Page number"         default(1)
// @Param        limit       query  int     false  "Items per page"      default(20)
// @Param        sort_by     query  string  false  "Sort field"          default(published_at)
// @Param        order       query  string  false  "Sort order"          default(desc)
// @Param        search      query  string  false  "Search query"
// @Param        category_id query  string  false  "Filter by category ID (UUID)"
// @Param        tags        query  string  false  "Filter by tag slugs (comma-separated)"
// @Success      200  {object}  apiresponse.PaginatedResponse[domain.PostResponse]
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts [get]
func (h *PostHandler) ListPublished(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", h.postsPerPage)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = h.postsPerPage
	}

	sortBy := c.Query("sort_by", "published_at")
	order := c.Query("order", "desc")
	if err := validateSortParams(sortBy, order); err != nil {
		return err
	}

	filter := repository.PostListFilter{
		Offset: (page - 1) * limit,
		Limit:  limit,
		SortBy: sortBy,
		Order:  order,
		Search: c.Query("search"),
		Status: string(domain.PostStatusPublished),
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
		h.enrichPostResponse(c.UserContext(), responses[i], p.AuthorID)
	}

	return c.JSON(apiresponse.NewPaginatedResponse(responses, page, limit, total))
}

// GetTrending returns trending blog posts.
// @Summary      Get trending posts
// @Description  Returns a list of trending blog posts based on recent engagement
// @Tags         Blog Posts
// @Produce      json
// @Param        limit  query  int  false  "Number of posts"  default(10)
// @Success      200  {object}  map[string][]domain.PostResponse
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/trending [get]
func (h *PostHandler) GetTrending(c *fiber.Ctx) error {
	return h.getEngagementPosts(c, h.engagementSvc.GetTrending)
}

// GetPopular returns popular blog posts.
// @Summary      Get popular posts
// @Description  Returns a list of most popular blog posts based on total engagement
// @Tags         Blog Posts
// @Produce      json
// @Param        limit  query  int  false  "Number of posts"  default(10)
// @Success      200  {object}  map[string][]domain.PostResponse
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/popular [get]
func (h *PostHandler) GetPopular(c *fiber.Ctx) error {
	return h.getEngagementPosts(c, h.engagementSvc.GetPopular)
}

func (h *PostHandler) getEngagementPosts(
	c *fiber.Ctx,
	fetch func(int) ([]*domain.Post, error),
) error {
	limit := c.QueryInt("limit", 10)
	if limit < 1 || limit > 50 {
		limit = 10
	}

	if h.engagementSvc == nil {
		return c.JSON(fiber.Map{"items": []interface{}{}})
	}

	posts, err := fetch(limit)
	if err != nil {
		return err
	}

	responses := make([]*domain.PostResponse, len(posts))
	for i, p := range posts {
		responses[i] = toPostResponse(p)
		h.enrichPostResponse(c.UserContext(), responses[i], p.AuthorID)
	}
	return c.JSON(fiber.Map{"items": responses})
}

// GetBySlug returns a single blog post by its slug.
// @Summary      Get post by slug
// @Description  Returns a single published blog post by its URL slug
// @Tags         Blog Posts
// @Produce      json
// @Param        slug  path  string  true  "Post slug"
// @Success      200  {object}  domain.PostResponse
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{slug} [get]
func (h *PostHandler) GetBySlug(c *fiber.Ctx) error {
	slug := c.Params("slug")
	post, err := h.postSvc.GetBySlug(slug)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c.UserContext(), resp, post.AuthorID)

	// Check if liked by current user
	if h.engagementSvc != nil {
		if userID := getUserIDFromCtx(c); userID != nil {
			liked, _ := h.engagementSvc.IsLiked(post.ID, *userID)
			resp.IsLiked = liked
		}
	}

	return c.JSON(resp)
}

// CreateDraft creates an empty draft post for the editor.
// @Summary      Create empty draft
// @Description  Creates a minimal empty draft post and returns its ID. Used by the editor for lazy draft creation.
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Success      201  {object}  domain.PostResponse
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/draft [post]
func (h *PostHandler) CreateDraft(c *fiber.Ctx) error {
	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.CreateDraft(c.UserContext(), *userID)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c.UserContext(), resp, post.AuthorID)
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// Create creates a new blog post.
// @Summary      Create a blog post
// @Description  Creates a new blog post as a draft
// @Tags         Blog Posts
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request  body  service.CreatePostRequest  true  "Post data"
// @Success      201  {object}  map[string]interface{}  "{ message: string, post: PostResponse }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts [post]
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

	post, err := h.postSvc.Create(c.UserContext(), &req, *userID)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c.UserContext(), resp, post.AuthorID)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Post created successfully",
		"post":    resp,
	})
}

// Update updates an existing blog post.
// @Summary      Update a blog post
// @Description  Updates an existing blog post (owner or admin only)
// @Tags         Blog Posts
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        id    path  string                     true  "Post ID (UUID)"
// @Param        request  body  service.UpdatePostRequest  true  "Updated post data"
// @Success      200  {object}  map[string]interface{}  "{ message: string, post: PostResponse }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id} [put]
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

	post, err := h.postSvc.Update(c.UserContext(), id, &req, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c.UserContext(), resp, post.AuthorID)
	return c.JSON(fiber.Map{
		"message": "Post updated successfully",
		"post":    resp,
	})
}

// Publish publishes a draft blog post.
// @Summary      Publish a blog post
// @Description  Changes a draft blog post status to published
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string]interface{}  "{ message: string, post: PostResponse }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/publish [post]
func (h *PostHandler) Publish(c *fiber.Ctx) error {
	return h.changePostStatus(c, h.postSvc.Publish, "Post published successfully")
}

// Archive archives a blog post.
// @Summary      Archive a blog post
// @Description  Changes a blog post status to archived
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string]interface{}  "{ message: string, post: PostResponse }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/archive [post]
func (h *PostHandler) Archive(c *fiber.Ctx) error {
	return h.changePostStatus(c, h.postSvc.Archive, "Post archived successfully")
}

type postStatusAction func(ctx context.Context, id uuid.UUID, userID uuid.UUID, isAdmin bool) (*domain.Post, error)

func (h *PostHandler) changePostStatus(c *fiber.Ctx, action postStatusAction, successMsg string) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := action(c.UserContext(), id, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c.UserContext(), resp, post.AuthorID)
	return c.JSON(fiber.Map{
		"message": successMsg,
		"post":    resp,
	})
}

// SoftDelete soft-deletes a blog post.
// @Summary      Delete a blog post
// @Description  Soft-deletes a blog post (owner or admin only)
// @Tags         Blog Posts
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      204  "No Content"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id} [delete]
func (h *PostHandler) SoftDelete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.postSvc.Delete(c.UserContext(), id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GetForEdit returns a blog post for editing.
// @Summary      Get post for editing
// @Description  Returns the full blog post data including content JSON for editing (owner or admin only)
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  domain.Post
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/edit [get]
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

// ListRevisions returns the revision history of a blog post.
// @Summary      List post revisions
// @Description  Returns the revision history of a blog post (owner or admin only)
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string][]domain.PostRevision
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/revisions [get]
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

	return c.JSON(fiber.Map{"revisions": revisions})
}

// GetRevision returns a specific revision of a blog post.
// @Summary      Get a specific post revision
// @Description  Returns a specific revision of a blog post (owner or admin only)
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id   path  string  true  "Post ID (UUID)"
// @Param        rid  path  string  true  "Revision ID (UUID)"
// @Success      200  {object}  domain.PostRevision
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/revisions/{rid} [get]
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

	revision, err := h.postSvc.GetRevision(id, rid)
	if err != nil {
		return err
	}

	return c.JSON(revision)
}

// toPostResponse converts a Post domain model to a public response
func toPostResponse(p *domain.Post) *domain.PostResponse {
	resp := &domain.PostResponse{
		ID:              p.ID,
		Title:           p.Title,
		Slug:            p.Slug,
		ContentHTML:     p.ContentHTML,
		Excerpt:         p.Excerpt,
		CoverImageURL:   p.CoverImageURL,
		MetaTitle:       p.MetaTitle,
		MetaDescription: p.MetaDescription,
		Status:          p.Status,
		PublishedAt:     p.PublishedAt,
		ReadTimeMinutes: p.ReadTime,
		IsFeatured:      p.IsFeatured,
		IsLiked:         p.IsLiked,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
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
		}
	}

	return resp
}
