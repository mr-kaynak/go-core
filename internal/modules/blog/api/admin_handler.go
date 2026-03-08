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

// AdminHandler handles blog admin HTTP requests
type AdminHandler struct {
	postSvc       *service.PostService
	commentSvc    *service.CommentService
	engagementSvc *service.EngagementService
	settingsSvc   *service.SettingsService
	postRepo      repository.PostRepository
	postsPerPage  int
}

// NewAdminHandler creates a new AdminHandler
func NewAdminHandler(
	postSvc *service.PostService,
	commentSvc *service.CommentService,
	engagementSvc *service.EngagementService,
	settingsSvc *service.SettingsService,
	postRepo repository.PostRepository,
	postsPerPage int,
) *AdminHandler {
	return &AdminHandler{
		postSvc:       postSvc,
		commentSvc:    commentSvc,
		engagementSvc: engagementSvc,
		settingsSvc:   settingsSvc,
		postRepo:      postRepo,
		postsPerPage:  postsPerPage,
	}
}

// RegisterRoutes registers admin blog routes under the admin group
func (h *AdminHandler) RegisterRoutes(admin fiber.Router) {
	blog := admin.Group("/blog")

	blog.Get("/posts", h.ListAll)
	blog.Get("/comments/pending", h.PendingComments)
	blog.Post("/comments/:id/approve", h.ApproveComment)
	blog.Post("/comments/:id/reject", h.RejectComment)
	blog.Get("/stats", h.DashboardStats)
	blog.Get("/settings", h.GetSettings)
	blog.Put("/settings", h.UpdateSettings)
}

// ListAll returns a paginated list of all blog posts for admin.
// @Summary      List all posts (admin)
// @Description  Returns a paginated list of all blog posts with optional filtering (admin only)
// @Tags         Blog Admin
// @Produce      json
// @Security     Bearer
// @Param        page       query  int     false  "Page number"          default(1)
// @Param        limit      query  int     false  "Items per page"       default(20)
// @Param        sort_by    query  string  false  "Sort field"           default(created_at)
// @Param        order      query  string  false  "Sort order"           default(desc)
// @Param        search     query  string  false  "Search query"
// @Param        status     query  string  false  "Filter by status (draft, published, archived)"
// @Param        author_id  query  string  false  "Filter by author ID (UUID)"
// @Success      200  {object}  apiresponse.PaginatedResponse[domain.PostResponse]
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /admin/blog/posts [get]
func (h *AdminHandler) ListAll(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", h.postsPerPage)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = h.postsPerPage
	}

	status := c.Query("status")
	if status != "" {
		switch status {
		case string(domain.PostStatusDraft), string(domain.PostStatusPublished), string(domain.PostStatusArchived):
			// valid
		default:
			return errors.NewBadRequest("Invalid status filter: must be draft, published, or archived")
		}
	}

	sortBy := c.Query("sort_by", "created_at")
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
		Status: status,
	}

	if authorID := c.Query("author_id"); authorID != "" {
		id, err := uuid.Parse(authorID)
		if err == nil {
			filter.AuthorID = &id
		}
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

// PendingComments returns a paginated list of pending comments.
// @Summary      List pending comments (admin)
// @Description  Returns a paginated list of comments awaiting moderation (admin only)
// @Tags         Blog Admin
// @Produce      json
// @Security     Bearer
// @Param        page   query  int  false  "Page number"     default(1)
// @Param        limit  query  int  false  "Items per page"  default(20)
// @Success      200  {object}  apiresponse.PaginatedResponse[domain.Comment]
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /admin/blog/comments/pending [get]
func (h *AdminHandler) PendingComments(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	comments, total, err := h.commentSvc.ListPending(offset, limit)
	if err != nil {
		return err
	}

	responses := make([]*domain.AdminCommentResponse, len(comments))
	for i, c := range comments {
		responses[i] = c.ToAdminResponse()
	}

	return c.JSON(apiresponse.NewPaginatedResponse(responses, page, limit, total))
}

// ApproveComment approves a pending comment.
// @Summary      Approve a comment (admin)
// @Description  Approves a pending comment for publication (admin only)
// @Tags         Blog Admin
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Comment ID (UUID)"
// @Success      200  {object}  map[string]string  "{ message: string }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /admin/blog/comments/{id}/approve [post]
func (h *AdminHandler) ApproveComment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	if _, err := h.commentSvc.Approve(c.UserContext(), id); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "Comment approved successfully"})
}

// RejectComment rejects a pending comment.
// @Summary      Reject a comment (admin)
// @Description  Rejects a pending comment (admin only)
// @Tags         Blog Admin
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Comment ID (UUID)"
// @Success      200  {object}  map[string]string  "{ message: string }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /admin/blog/comments/{id}/reject [post]
func (h *AdminHandler) RejectComment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	if _, err := h.commentSvc.Reject(c.UserContext(), id); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "Comment rejected successfully"})
}

// DashboardStatsResponse holds blog dashboard stats
type DashboardStatsResponse struct {
	TotalPosts      int64 `json:"total_posts"`
	PublishedPosts  int64 `json:"published_posts"`
	DraftPosts      int64 `json:"draft_posts"`
	PendingComments int64 `json:"pending_comments"`
}

// DashboardStats returns blog dashboard statistics.
// @Summary      Get blog dashboard stats (admin)
// @Description  Returns aggregated blog statistics for the admin dashboard
// @Tags         Blog Admin
// @Produce      json
// @Security     Bearer
// @Success      200  {object}  DashboardStatsResponse
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /admin/blog/stats [get]
func (h *AdminHandler) DashboardStats(c *fiber.Ctx) error {
	totalAll, err := h.postRepo.CountByStatus("")
	if err != nil {
		return errors.NewInternalError("Failed to get total post count")
	}
	totalPublished, err := h.postRepo.CountByStatus(string(domain.PostStatusPublished))
	if err != nil {
		return errors.NewInternalError("Failed to get published post count")
	}
	totalDraft, err := h.postRepo.CountByStatus(string(domain.PostStatusDraft))
	if err != nil {
		return errors.NewInternalError("Failed to get draft post count")
	}
	_, totalPending, err := h.commentSvc.ListPending(0, 1)
	if err != nil {
		return errors.NewInternalError("Failed to get pending comment count")
	}

	return c.JSON(DashboardStatsResponse{
		TotalPosts:      totalAll,
		PublishedPosts:  totalPublished,
		DraftPosts:      totalDraft,
		PendingComments: totalPending,
	})
}

// GetSettings returns the current blog settings.
// @Summary      Get blog settings (admin)
// @Description  Returns the current blog settings including runtime overrides
// @Tags         Blog Admin
// @Produce      json
// @Security     Bearer
// @Success      200  {object}  domain.BlogSettingsResponse
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Router       /admin/blog/settings [get]
func (h *AdminHandler) GetSettings(c *fiber.Ctx) error {
	settings := h.settingsSvc.Get(c.UserContext())
	return c.JSON(settings.ToResponse())
}

// UpdateSettings updates blog settings.
// @Summary      Update blog settings (admin)
// @Description  Partially updates blog settings (only provided fields are changed)
// @Tags         Blog Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        body  body  domain.UpdateBlogSettingsRequest  true  "Settings to update"
// @Success      200  {object}  domain.BlogSettingsResponse
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /admin/blog/settings [put]
func (h *AdminHandler) UpdateSettings(c *fiber.Ctx) error {
	var req domain.UpdateBlogSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	settings, err := h.settingsSvc.Update(c.UserContext(), &req)
	if err != nil {
		return errors.NewInternalError("Failed to update blog settings")
	}

	return c.JSON(settings.ToResponse())
}
