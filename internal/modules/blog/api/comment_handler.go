package api

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// CommentHandler handles blog comment HTTP requests
type CommentHandler struct {
	commentSvc *service.CommentService
	userLookup UserLookupFunc
}

// NewCommentHandler creates a new CommentHandler
func NewCommentHandler(commentSvc *service.CommentService) *CommentHandler {
	return &CommentHandler{commentSvc: commentSvc}
}

// SetUserLookup sets the function used to resolve author info for comments.
func (h *CommentHandler) SetUserLookup(fn UserLookupFunc) {
	h.userLookup = fn
}

// enrichCommentAuthor populates the Author field on a CommentResponse tree.
func (h *CommentHandler) enrichCommentAuthor(ctx context.Context, resp *domain.CommentResponse) {
	if h.userLookup != nil && resp.AuthorID != nil {
		author, err := h.userLookup(ctx, *resp.AuthorID)
		if err == nil && author != nil {
			resp.Author = &domain.CommentAuthor{
				ID:        resp.AuthorID,
				Name:      author.Name,
				AvatarURL: author.AvatarURL,
				IsGuest:   false,
			}
		}
	} else if resp.AuthorID == nil {
		resp.Author = &domain.CommentAuthor{
			Name:    resp.GuestName,
			IsGuest: true,
		}
	}
	for i := range resp.Children {
		h.enrichCommentAuthor(ctx, &resp.Children[i])
	}
}

// RegisterRoutes registers comment routes
// RegisterRoutes registers comment routes.
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
func (h *CommentHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	// Public: get comments and create (guests allowed)
	blog.Get("/posts/:postId/comments", h.GetThreaded)
	blog.Post("/posts/:postId/comments", h.Create)

	// Protected: delete own comment
	if authzMw != nil {
		blog.Delete("/comments/:id", authMw, authzMw, h.Delete)
	} else {
		blog.Delete("/comments/:id", authMw, h.Delete)
	}
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
func (h *CommentHandler) GetThreaded(c fiber.Ctx) error {
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
		h.enrichCommentAuthor(c, responses[i])
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
func (h *CommentHandler) Create(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("postId"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	var req service.CreateCommentRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	// Get authenticated user ID (may be nil for guests)
	authorID := getUserIDFromCtx(c)

	comment, err := h.commentSvc.Create(c, postID, &req, authorID)
	if err != nil {
		return err
	}

	resp := comment.ToResponse()
	h.enrichCommentAuthor(c, resp)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Comment created successfully",
		"comment": resp,
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
func (h *CommentHandler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid comment ID format")
	}

	userID := getUserIDFromCtx(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.commentSvc.Delete(c, id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// fiber:context-methods migrated
