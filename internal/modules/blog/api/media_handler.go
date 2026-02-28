package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// MediaHandler handles blog media HTTP requests
type MediaHandler struct {
	mediaSvc *service.MediaService
}

// NewMediaHandler creates a new MediaHandler
func NewMediaHandler(mediaSvc *service.MediaService) *MediaHandler {
	return &MediaHandler{mediaSvc: mediaSvc}
}

// RegisterRoutes registers media routes
func (h *MediaHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler) {
	media := blog.Group("/media", authMw)
	media.Post("/presign", h.GeneratePresignedUpload)
	media.Post("/", h.Register)
	media.Delete("/:id", h.Delete)

	blog.Get("/posts/:postId/media", authMw, h.ListByPost)
}

// PresignRequest holds the request for generating a presigned upload URL
type PresignRequest struct {
	PostID      string `json:"post_id" validate:"required,uuid"`
	Filename    string `json:"filename" validate:"required,max=255"`
	ContentType string `json:"content_type" validate:"required,max=100"`
}

func (h *MediaHandler) GeneratePresignedUpload(c *fiber.Ctx) error {
	var req PresignRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	postID, _ := uuid.Parse(req.PostID)
	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	resp, err := h.mediaSvc.GeneratePresignedUpload(postID, req.Filename, req.ContentType, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.JSON(resp)
}

func (h *MediaHandler) Register(c *fiber.Ctx) error {
	var req service.RegisterMediaRequest
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

	media, err := h.mediaSvc.Register(&req, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(media)
}

func (h *MediaHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid media ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.mediaSvc.Delete(id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *MediaHandler) ListByPost(c *fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("postId"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	media, err := h.mediaSvc.ListByPost(postID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": media})
}
