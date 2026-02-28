package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// CommentHandler handles blog comment HTTP requests
type CommentHandler struct {
	commentSvc *service.CommentService
}

// NewCommentHandler creates a new CommentHandler
func NewCommentHandler(commentSvc *service.CommentService) *CommentHandler {
	return &CommentHandler{commentSvc: commentSvc}
}

// RegisterRoutes registers comment routes
func (h *CommentHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler) {
	// Public: get comments and create (guests allowed)
	blog.Get("/posts/:postId/comments", h.GetThreaded)
	blog.Post("/posts/:postId/comments", h.Create)

	// Protected: delete own comment
	blog.Delete("/comments/:id", authMw, h.Delete)
}

func (h *CommentHandler) GetThreaded(c *fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("postId"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	comments, err := h.commentSvc.GetThreaded(postID)
	if err != nil {
		return err
	}

	// Convert to response DTOs to strip sensitive fields (guest_email)
	responses := make([]*domain.CommentResponse, len(comments))
	for i, comment := range comments {
		responses[i] = comment.ToResponse()
	}

	return c.JSON(fiber.Map{"items": responses})
}

func (h *CommentHandler) Create(c *fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("postId"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req service.CreateCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	// Get authenticated user ID (may be nil for guests)
	authorID := getUserIDFromCtx(c)

	comment, err := h.commentSvc.Create(postID, &req, authorID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(comment.ToResponse())
}

func (h *CommentHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	userID := getUserIDFromCtx(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.commentSvc.Delete(id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}
