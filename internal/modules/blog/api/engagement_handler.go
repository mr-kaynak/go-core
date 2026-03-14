package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// swag annotation type references
var _ *domain.PostStats

// EngagementHandler handles blog engagement HTTP requests
type EngagementHandler struct {
	engagementSvc *service.EngagementService
}

// NewEngagementHandler creates a new EngagementHandler
func NewEngagementHandler(engagementSvc *service.EngagementService) *EngagementHandler {
	return &EngagementHandler{engagementSvc: engagementSvc}
}

// RegisterRoutes registers engagement routes
func (h *EngagementHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler) {
	posts := blog.Group("/posts")

	// Auth required
	posts.Post("/:id/like", authMw, h.ToggleLike)
	posts.Get("/:id/like", authMw, h.IsLiked)

	// Public
	posts.Post("/:id/view", h.RecordView)
	posts.Post("/:id/share", h.RecordShare)
	posts.Get("/:id/stats", h.GetStats)
}

// ToggleLike toggles a like on a blog post.
// @Summary      Toggle post like
// @Description  Toggles a like on a blog post for the authenticated user
// @Tags         Blog Engagement
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  service.ToggleLikeResponse
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/like [post]
func (h *EngagementHandler) ToggleLike(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	resp, err := h.engagementSvc.ToggleLike(c.Context(), postID, *userID)
	if err != nil {
		return err
	}

	return c.JSON(resp)
}

// IsLiked checks if the current user liked a blog post.
// @Summary      Check like status
// @Description  Checks whether the authenticated user has liked a blog post
// @Tags         Blog Engagement
// @Produce      json
// @Security     Bearer
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string]bool
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/like [get]
func (h *EngagementHandler) IsLiked(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	liked, err := h.engagementSvc.IsLiked(postID, *userID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"liked": liked})
}

// RecordViewRequest holds the request for recording a view
type RecordViewRequest struct {
	Referrer string `json:"referrer"`
}

// RecordView records a view on a blog post.
// @Summary      Record post view
// @Description  Records a view on a blog post with deduplication
// @Tags         Blog Engagement
// @Accept       json
// @Param        id    path  string            true   "Post ID (UUID)"
// @Param        request  body  RecordViewRequest  false  "View metadata"
// @Success      204  "No Content"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/view [post]
func (h *EngagementHandler) RecordView(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req RecordViewRequest
	_ = c.Bind().Body(&req)

	userID := getUserIDFromCtx(c)
	ip := c.IP()
	userAgent := c.Get("User-Agent")

	if err := h.engagementSvc.RecordView(c.Context(), postID, userID, ip, userAgent, req.Referrer); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// RecordShareRequest holds the request for recording a share
type RecordShareRequest struct {
	Platform string `json:"platform" validate:"required,max=50"`
}

// RecordShare records a share action on a blog post.
// @Summary      Record post share
// @Description  Records a share action on a blog post
// @Tags         Blog Engagement
// @Accept       json
// @Param        id    path  string              true  "Post ID (UUID)"
// @Param        request  body  RecordShareRequest  true  "Share data"
// @Success      204  "No Content"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/share [post]
func (h *EngagementHandler) RecordShare(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req RecordShareRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := getUserIDFromCtx(c)
	ip := c.IP()

	if err := h.engagementSvc.RecordShare(c.Context(), postID, userID, req.Platform, ip); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GetStats returns engagement stats for a blog post.
// @Summary      Get post engagement stats
// @Description  Returns aggregated engagement statistics for a blog post
// @Tags         Blog Engagement
// @Produce      json
// @Param        id  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  domain.PostStats
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{id}/stats [get]
func (h *EngagementHandler) GetStats(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	stats, err := h.engagementSvc.GetStats(postID)
	if err != nil {
		return err
	}

	return c.JSON(stats)
}

// fiber:context-methods migrated
