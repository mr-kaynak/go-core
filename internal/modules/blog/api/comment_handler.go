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

// GetThreaded returns threaded comments for a blog post.
// @Summary      Get post comments
// @Description  Returns threaded comments for a blog post
// @Tags         Blog Comments
// @Produce      json
// @Param        postId  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string][]domain.CommentResponse
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{postId}/comments [get]
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

	return c.JSON(fiber.Map{"comments": responses})
}

// Create creates a new comment on a blog post.
// @Summary      Create a comment
// @Description  Creates a new comment on a blog post (guests allowed)
// @Tags         Blog Comments
// @Accept       json
// @Produce      json
// @Param        postId  path  string                        true  "Post ID (UUID)"
// @Param        request  body  service.CreateCommentRequest  true  "Comment data"
// @Success      201  {object}  map[string]interface{}  "{ message: string, comment: CommentResponse }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{postId}/comments [post]
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

	comment, err := h.commentSvc.Create(c.UserContext(), postID, &req, authorID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Comment created successfully",
		"comment": comment.ToResponse(),
	})
}

// Delete deletes a comment.
// @Summary      Delete a comment
// @Description  Deletes a comment (owner or admin only)
// @Tags         Blog Comments
// @Security     Bearer
// @Param        id  path  string  true  "Comment ID (UUID)"
// @Success      204  "No Content"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/comments/{id} [delete]
func (h *CommentHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	userID := getUserIDFromCtx(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.commentSvc.Delete(c.UserContext(), id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}
