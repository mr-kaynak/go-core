package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	authMiddleware "github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// CategoryHandler handles blog category HTTP requests
type CategoryHandler struct {
	categorySvc *service.CategoryService
}

// NewCategoryHandler creates a new CategoryHandler
func NewCategoryHandler(categorySvc *service.CategoryService) *CategoryHandler {
	return &CategoryHandler{categorySvc: categorySvc}
}

// RegisterRoutes registers category routes
func (h *CategoryHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler) {
	categories := blog.Group("/categories")

	// Public
	categories.Get("/", h.GetTree)

	// Admin only
	admin := categories.Group("", authMw, authMiddleware.RequireRoles("admin", "system_admin"))
	admin.Post("/", h.Create)
	admin.Put("/:id", h.Update)
	admin.Delete("/:id", h.Delete)
}

func (h *CategoryHandler) GetTree(c *fiber.Ctx) error {
	tree, err := h.categorySvc.GetTree()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": tree})
}

func (h *CategoryHandler) Create(c *fiber.Ctx) error {
	var req service.CreateCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	category, err := h.categorySvc.Create(&req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(category)
}

func (h *CategoryHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid category ID format")
	}

	var req service.UpdateCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	category, err := h.categorySvc.Update(id, &req)
	if err != nil {
		return err
	}

	return c.JSON(category)
}

func (h *CategoryHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid category ID format")
	}

	if err := h.categorySvc.Delete(id); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}
