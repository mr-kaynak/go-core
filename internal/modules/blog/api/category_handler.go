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
var _ *domain.Category

// CategoryHandler handles blog category HTTP requests
type CategoryHandler struct {
	categorySvc *service.CategoryService
}

// NewCategoryHandler creates a new CategoryHandler
func NewCategoryHandler(categorySvc *service.CategoryService) *CategoryHandler {
	return &CategoryHandler{categorySvc: categorySvc}
}

// RegisterRoutes registers category routes.
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
func (h *CategoryHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	categories := blog.Group("/categories")

	// Public
	categories.Get("/", h.GetTree)

	// Protected — authMw (and optional authzMw) applied per-route to keep GET public
	if authzMw != nil {
		categories.Post("/", authMw, authzMw, h.Create)
		categories.Put("/:id", authMw, authzMw, h.Update)
		categories.Delete("/:id", authMw, authzMw, h.Delete)
	} else {
		categories.Post("/", authMw, h.Create)
		categories.Put("/:id", authMw, h.Update)
		categories.Delete("/:id", authMw, h.Delete)
	}
}

// GetTree returns the category tree.
// @Summary      Get category tree
// @Description  Returns all blog categories organized as a nested tree
// @Tags         Blog Categories
// @Produce      json
// @Success      200  {object}  map[string][]domain.Category
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/categories [get]
func (h *CategoryHandler) GetTree(c fiber.Ctx) error {
	tree, err := h.categorySvc.GetTree()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"categories": tree})
}

// Create creates a new blog category.
// @Summary      Create a category
// @Description  Creates a new blog category (admin only)
// @Tags         Blog Categories
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request  body  service.CreateCategoryRequest  true  "Category data"
// @Success      201  {object}  map[string]interface{}  "{ message: string, category: Category }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      409  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/categories [post]
func (h *CategoryHandler) Create(c fiber.Ctx) error {
	var req service.CreateCategoryRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	category, err := h.categorySvc.Create(&req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":  "Category created successfully",
		"category": category,
	})
}

// Update updates a blog category.
// @Summary      Update a category
// @Description  Updates an existing blog category (admin only)
// @Tags         Blog Categories
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        id    path  string                         true  "Category ID (UUID)"
// @Param        request  body  service.UpdateCategoryRequest  true  "Updated category data"
// @Success      200  {object}  map[string]interface{}  "{ message: string, category: Category }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/categories/{id} [put]
func (h *CategoryHandler) Update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid category ID format")
	}

	var req service.UpdateCategoryRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	category, err := h.categorySvc.Update(id, &req)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message":  "Category updated successfully",
		"category": category,
	})
}

// Delete deletes a blog category.
// @Summary      Delete a category
// @Description  Deletes a blog category (admin only)
// @Tags         Blog Categories
// @Security     Bearer
// @Param        id  path  string  true  "Category ID (UUID)"
// @Success      204  "No Content"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/categories/{id} [delete]
func (h *CategoryHandler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid category ID format")
	}

	if err := h.categorySvc.Delete(id); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}
