package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
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

// UserLookupFunc resolves minimal author info from a single user ID.
// Used for single-post endpoints (e.g. GetBySlug).
type UserLookupFunc func(ctx context.Context, userID uuid.UUID) (*domain.PostAuthor, error)

// UserBatchLookupFunc resolves author info for multiple user IDs in one call.
// Used by list endpoints to avoid N+1 queries. Returns a map keyed by user ID;
// missing IDs are absent from the map (not an error).
type UserBatchLookupFunc func(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]*domain.PostAuthor, error)

// PostHandler handles blog post HTTP requests
type PostHandler struct {
	postSvc            *service.PostService
	engagementSvc      *service.EngagementService
	postsPerPage       int
	userLookup         UserLookupFunc
	userBatchLookup    UserBatchLookupFunc
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

// SetUserLookup sets the function used to resolve author info for single-post endpoints.
func (h *PostHandler) SetUserLookup(fn UserLookupFunc) {
	h.userLookup = fn
}

// SetUserBatchLookup sets the function used to resolve author info for list endpoints.
// When set, list handlers use a single batch query instead of one query per post.
func (h *PostHandler) SetUserBatchLookup(fn UserBatchLookupFunc) {
	h.userBatchLookup = fn
}

// enrichPostResponse populates the Author field on a PostResponse.
// Used for single-post endpoints; falls back to userLookup.
func (h *PostHandler) enrichPostResponse(ctx context.Context, resp *domain.PostResponse, authorID uuid.UUID) {
	if h.userLookup != nil {
		author, err := h.userLookup(ctx, authorID)
		if err == nil && author != nil {
			resp.Author = author
		}
	}
}

// enrichPostResponsesFromMap populates Author fields on a slice of PostResponses
// using a pre-fetched author map. Missing authors are left as nil (graceful degradation).
func enrichPostResponsesFromMap(responses []*domain.PostResponse, authorIDs []uuid.UUID, authors map[uuid.UUID]*domain.PostAuthor) {
	for i, resp := range responses {
		if author, ok := authors[authorIDs[i]]; ok {
			resp.Author = author
		}
	}
}

// batchEnrichPostResponses collects distinct author IDs, fetches them with a
// single batch query, and populates Author on every response in the slice.
func (h *PostHandler) batchEnrichPostResponses(ctx context.Context, responses []*domain.PostResponse, authorIDs []uuid.UUID) {
	if h.userBatchLookup == nil || len(responses) == 0 {
		return
	}
	// Collect distinct IDs.
	seen := make(map[uuid.UUID]struct{}, len(authorIDs))
	distinct := make([]uuid.UUID, 0, len(authorIDs))
	for _, id := range authorIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			distinct = append(distinct, id)
		}
	}
	authors, err := h.userBatchLookup(ctx, distinct)
	if err != nil || authors == nil {
		return
	}
	enrichPostResponsesFromMap(responses, authorIDs, authors)
}

// RegisterRoutes registers post routes.
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
//
// Route ordering contract:
//  1. Static public routes (/trending, /popular) — matched first.
//  2. Parameterized protected routes (/:id/...) — require auth.
//  3. /:slug catch-all — MUST be last. Fiber matches routes in
//     registration order, so any route registered after /:slug that
//     could collide with a slug-shaped path will never be reached.
//     If you add new GET sub-resources, register them ABOVE /:slug.
func (h *PostHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	posts := blog.Group("/posts")

	// --- Public routes (static paths first) ---
	posts.Get("/", h.ListPublished)
	posts.Get("/trending", h.GetTrending)
	posts.Get("/popular", h.GetPopular)

	// --- Protected routes ---
	// authMw (and optional authzMw) applied per-route to avoid
	// prefix-match leaking into public routes.
	var extraMws []any
	if authzMw != nil {
		extraMws = []any{authzMw}
	}
	posts.Post("/draft", authMw, append(extraMws, h.CreateDraft)...)
	posts.Post("/", authMw, append(extraMws, h.Create)...)
	posts.Put("/:id", authMw, append(extraMws, h.Update)...)
	posts.Post("/:id/publish", authMw, append(extraMws, h.Publish)...)
	posts.Post("/:id/archive", authMw, append(extraMws, h.Archive)...)
	posts.Post("/:id/revert-to-draft", authMw, append(extraMws, h.RevertToDraft)...)
	posts.Delete("/:id", authMw, append(extraMws, h.SoftDelete)...)
	posts.Get("/:id/edit", authMw, append(extraMws, h.GetForEdit)...)
	posts.Get("/:id/revisions", authMw, append(extraMws, h.ListRevisions)...)
	posts.Get("/:id/revisions/:rid", authMw, append(extraMws, h.GetRevision)...)

	// --- Public catch-all: MUST remain last (see doc above) ---
	posts.Get("/:slug", h.GetBySlug)
}

// ListPublished returns a paginated list of published blog posts.
// Supports both offset-based (page/limit) and cursor-based (cursor/limit) pagination.
// When the "cursor" query parameter is present, cursor-based pagination is used.
// Cursor pagination is only supported with sort_by=published_at (or the default). Combining
// a cursor with any other sort field returns 400 Bad Request.
// @Summary      List published posts
// @Description  Returns a paginated list of published blog posts with optional filtering.
// @Description  Two pagination modes are supported:
// @Description  - Offset mode (default): use page + limit parameters. Returns apiresponse.PaginatedResponse.
// @Description  - Cursor mode: provide cursor parameter (requires sort_by=published_at). Returns apiresponse.CursorPaginatedResponse. Using a cursor with any other sort field returns 400.
// @Tags         Blog Posts
// @Produce      json
// @Param        page        query  int     false  "Page number (offset mode)"       default(1)
// @Param        limit       query  int     false  "Items per page"                  default(20)
// @Param        cursor      query  string  false  "Pagination cursor (cursor mode, requires sort_by=published_at)"
// @Param        sort_by     query  string  false  "Sort field"                      default(published_at)
// @Param        order       query  string  false  "Sort order"                      default(desc)
// @Param        search      query  string  false  "Search query"
// @Param        category_id query  string  false  "Filter by category ID (UUID)"
// @Param        tags        query  string  false  "Filter by tag slugs (comma-separated)"
// @Success      200  {object}  apiresponse.PaginatedResponse[domain.PostResponse]       "Offset mode: paginated list with total count"
// @Success      200  {object}  apiresponse.CursorPaginatedResponse[domain.PostResponse] "Cursor mode: list with next_cursor and has_more"
// @Failure      400  {object}  errors.ProblemDetail  "Invalid parameters or cursor used with unsupported sort"
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts [get]
func (h *PostHandler) ListPublished(c fiber.Ctx) error {
	limit := apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", h.postsPerPage), h.postsPerPage)

	sortBy := c.Query("sort_by", "published_at")
	order := c.Query("order", "desc")
	if err := validateSortParams(sortBy, order); err != nil {
		return err
	}

	search := c.Query("search")
	if len(search) > 200 {
		return errors.NewBadRequest("Search query too long (max 200 characters)")
	}

	filter := service.PostListFilter{
		Limit:  limit,
		SortBy: sortBy,
		Order:  order,
		Search: search,
		Status: string(domain.PostStatusPublished),
	}

	if catID := c.Query("category_id"); catID != "" {
		id, err := uuid.Parse(catID)
		if err != nil {
			return errors.NewBadRequest("Invalid category_id: must be a valid UUID")
		}
		filter.CategoryID = &id
	}
	if tagSlugs := c.Query("tags"); tagSlugs != "" {
		filter.TagSlugs = splitComma(tagSlugs)
	}

	// Cursor-based pagination mode
	if cursor := c.Query("cursor"); cursor != "" {
		if sortBy != "" && sortBy != "published_at" {
			return errors.NewBadRequest("cursor pagination supports only sort_by=published_at")
		}
		cursorTime, cursorID, err := decodeCursor(cursor)
		if err != nil {
			return errors.NewBadRequest("Invalid cursor format")
		}
		filter.CursorPublishedAt = &cursorTime
		filter.CursorID = &cursorID
		filter.Limit = limit + 1 // fetch one extra to determine has_more

		posts, _, err := h.postSvc.List(filter)
		if err != nil {
			return err
		}

		authorIDs := make([]uuid.UUID, len(posts))
		responses := make([]*domain.PostResponse, len(posts))
		for i, p := range posts {
			responses[i] = toPostResponse(p)
			authorIDs[i] = p.AuthorID
		}
		h.batchEnrichPostResponses(c, responses, authorIDs)

		return c.JSON(apiresponse.NewCursorPaginatedResponse(responses, limit, func(r *domain.PostResponse) string {
			pa := r.CreatedAt
			if r.PublishedAt != nil {
				pa = *r.PublishedAt
			}
			return encodeCursor(pa, r.ID)
		}))
	}

	// Offset-based pagination mode (default)
	page := fiber.Query[int](c, "page", 1)
	if page < 1 {
		page = 1
	}
	filter.Offset = (page - 1) * limit

	posts, total, err := h.postSvc.List(filter)
	if err != nil {
		return err
	}

	authorIDs := make([]uuid.UUID, len(posts))
	responses := make([]*domain.PostResponse, len(posts))
	for i, p := range posts {
		responses[i] = toPostResponse(p)
		authorIDs[i] = p.AuthorID
	}
	h.batchEnrichPostResponses(c, responses, authorIDs)

	return c.JSON(apiresponse.NewPaginatedResponse(responses, page, limit, total))
}

// encodeCursor creates an opaque base64 cursor from published_at + id.
func encodeCursor(publishedAt time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%s|%s", publishedAt.Format(time.RFC3339Nano), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses the opaque cursor back into published_at and id.
func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return t, id, nil
}

// TrendingPostResponse extends PostResponse with a trending_score field.
type TrendingPostResponse struct {
	*domain.PostResponse
	TrendingScore float64 `json:"trending_score"`
}

// TrendingPostsResponse is the response envelope for the trending posts endpoint.
type TrendingPostsResponse struct {
	Items []*TrendingPostResponse `json:"items"`
}

// GetTrending returns trending blog posts.
// @Summary      Get trending posts
// @Description  Returns a list of trending blog posts based on recent engagement. Each item
// @Description  includes all standard post fields plus trending_score indicating relative rank.
// @Tags         Blog Posts
// @Produce      json
// @Param        limit  query  int  false  "Number of posts"  default(10)
// @Success      200  {object}  TrendingPostsResponse
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/trending [get]
func (h *PostHandler) GetTrending(c fiber.Ctx) error {
	limit := apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", 10), 10)

	if h.engagementSvc == nil {
		return c.JSON(TrendingPostsResponse{Items: []*TrendingPostResponse{}})
	}

	trendingPosts, err := h.engagementSvc.GetTrending(limit)
	if err != nil {
		return err
	}

	authorIDs := make([]uuid.UUID, len(trendingPosts))
	postResponses := make([]*domain.PostResponse, len(trendingPosts))
	responses := make([]*TrendingPostResponse, len(trendingPosts))
	for i, tp := range trendingPosts {
		postResponses[i] = toPostResponse(&tp.Post)
		responses[i] = &TrendingPostResponse{
			PostResponse:  postResponses[i],
			TrendingScore: tp.TrendingScore,
		}
		authorIDs[i] = tp.AuthorID
	}
	h.batchEnrichPostResponses(c, postResponses, authorIDs)
	return c.JSON(TrendingPostsResponse{Items: responses})
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
func (h *PostHandler) GetPopular(c fiber.Ctx) error {
	if h.engagementSvc == nil {
		return c.JSON(fiber.Map{"items": []interface{}{}})
	}
	return h.getEngagementPosts(c, h.engagementSvc.GetPopular)
}

func (h *PostHandler) getEngagementPosts(
	c fiber.Ctx,
	fetch func(int) ([]*domain.Post, error),
) error {
	limit := apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", 10), 10)

	if h.engagementSvc == nil {
		return c.JSON(fiber.Map{"items": []interface{}{}})
	}

	posts, err := fetch(limit)
	if err != nil {
		return err
	}

	authorIDs := make([]uuid.UUID, len(posts))
	responses := make([]*domain.PostResponse, len(posts))
	for i, p := range posts {
		responses[i] = toPostResponse(p)
		authorIDs[i] = p.AuthorID
	}
	h.batchEnrichPostResponses(c, responses, authorIDs)
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
func (h *PostHandler) GetBySlug(c fiber.Ctx) error {
	slug := c.Params("slug")
	post, err := h.postSvc.GetBySlug(slug)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c, resp, post.AuthorID)

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
func (h *PostHandler) CreateDraft(c fiber.Ctx) error {
	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.CreateDraft(c, *userID)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c, resp, post.AuthorID)
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
func (h *PostHandler) Create(c fiber.Ctx) error {
	var req service.CreatePostRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.Create(c, &req, *userID)
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c, resp, post.AuthorID)
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
func (h *PostHandler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req service.UpdatePostRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := h.postSvc.Update(c, id, &req, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c, resp, post.AuthorID)
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
func (h *PostHandler) Publish(c fiber.Ctx) error {
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
func (h *PostHandler) Archive(c fiber.Ctx) error {
	return h.changePostStatus(c, h.postSvc.Archive, "Post archived successfully")
}

type postStatusAction func(ctx context.Context, id uuid.UUID, userID uuid.UUID, isAdmin bool) (*domain.Post, error)

func (h *PostHandler) changePostStatus(c fiber.Ctx, action postStatusAction, successMsg string) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	post, err := action(c, id, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	resp := toPostResponse(post)
	h.enrichPostResponse(c, resp, post.AuthorID)
	return c.JSON(fiber.Map{
		"message": successMsg,
		"post":    resp,
	})
}

// RevertToDraft moves a published or archived post back to draft.
// @Summary      Revert post to draft
// @Description  Changes a published or archived blog post status back to draft
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string]interface{}  "{ message: string, post: PostResponse }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      409  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/revert-to-draft [post]
func (h *PostHandler) RevertToDraft(c fiber.Ctx) error {
	return h.changePostStatus(c, h.postSvc.RevertToDraft, "Post reverted to draft")
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
func (h *PostHandler) SoftDelete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.postSvc.Delete(c, id, *userID, isAdmin(c)); err != nil {
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
func (h *PostHandler) GetForEdit(c fiber.Ctx) error {
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
// @Description  Returns the paginated revision history of a blog post (owner or admin only)
// @Tags         Blog Posts
// @Produce      json
// @Security     Bearer
// @Param        id     path   string  true   "Post ID (UUID)"
// @Param        page   query  int     false  "Page number"     default(1)
// @Param        limit  query  int     false  "Items per page"  default(20)
// @Success      200  {object}  apiresponse.PaginatedResponse[domain.PostRevision]
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/revisions [get]
func (h *PostHandler) ListRevisions(c fiber.Ctx) error {
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

	page := fiber.Query[int](c, "page", 1)
	limit := apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", 20), 20)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	revisions, total, err := h.postSvc.ListRevisions(id, offset, limit)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(revisions, page, limit, total))
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
func (h *PostHandler) GetRevision(c fiber.Ctx) error {
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

// fiber:context-methods migrated
