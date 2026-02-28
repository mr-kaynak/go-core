package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

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

func (h *EngagementHandler) ToggleLike(c *fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	resp, err := h.engagementSvc.ToggleLike(postID, *userID)
	if err != nil {
		return err
	}

	return c.JSON(resp)
}

func (h *EngagementHandler) IsLiked(c *fiber.Ctx) error {
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

func (h *EngagementHandler) RecordView(c *fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req RecordViewRequest
	_ = c.BodyParser(&req)

	userID := getUserIDFromCtx(c)
	ip := c.IP()
	userAgent := c.Get("User-Agent")

	if err := h.engagementSvc.RecordView(postID, userID, ip, userAgent, req.Referrer); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// RecordShareRequest holds the request for recording a share
type RecordShareRequest struct {
	Platform string `json:"platform" validate:"required,max=50"`
}

func (h *EngagementHandler) RecordShare(c *fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req RecordShareRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := getUserIDFromCtx(c)
	ip := c.IP()

	if err := h.engagementSvc.RecordShare(postID, userID, req.Platform, ip); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *EngagementHandler) GetStats(c *fiber.Ctx) error {
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
