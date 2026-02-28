package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// AdminHandler handles blog admin HTTP requests
type AdminHandler struct {
	postSvc       *service.PostService
	commentSvc    *service.CommentService
	engagementSvc *service.EngagementService
	postRepo      repository.PostRepository
	postsPerPage  int
}

// NewAdminHandler creates a new AdminHandler
func NewAdminHandler(postSvc *service.PostService, commentSvc *service.CommentService, engagementSvc *service.EngagementService, postRepo repository.PostRepository, postsPerPage int) *AdminHandler {
	return &AdminHandler{
		postSvc:       postSvc,
		commentSvc:    commentSvc,
		engagementSvc: engagementSvc,
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
}

func (h *AdminHandler) ListAll(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", h.postsPerPage)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = h.postsPerPage
	}

	filter := repository.PostListFilter{
		Offset: (page - 1) * limit,
		Limit:  limit,
		SortBy: c.Query("sort_by", "created_at"),
		Order:  c.Query("order", "desc"),
		Search: c.Query("search"),
		Status: c.Query("status"),
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

	return c.JSON(apiresponse.NewPaginatedResponse(comments, page, limit, total))
}

func (h *AdminHandler) ApproveComment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	comment, err := h.commentSvc.Approve(id)
	if err != nil {
		return err
	}

	return c.JSON(comment)
}

func (h *AdminHandler) RejectComment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	comment, err := h.commentSvc.Reject(id)
	if err != nil {
		return err
	}

	return c.JSON(comment)
}

// DashboardStatsResponse holds blog dashboard stats
type DashboardStatsResponse struct {
	TotalPosts      int64 `json:"total_posts"`
	PublishedPosts   int64 `json:"published_posts"`
	DraftPosts       int64 `json:"draft_posts"`
	PendingComments  int64 `json:"pending_comments"`
}

func (h *AdminHandler) DashboardStats(c *fiber.Ctx) error {
	totalAll, _ := h.postRepo.CountByStatus("")
	totalPublished, _ := h.postRepo.CountByStatus(string(domain.PostStatusPublished))
	totalDraft, _ := h.postRepo.CountByStatus(string(domain.PostStatusDraft))
	_, totalPending, _ := h.commentSvc.ListPending(0, 1)

	return c.JSON(DashboardStatsResponse{
		TotalPosts:      totalAll,
		PublishedPosts:  totalPublished,
		DraftPosts:      totalDraft,
		PendingComments: totalPending,
	})
}
