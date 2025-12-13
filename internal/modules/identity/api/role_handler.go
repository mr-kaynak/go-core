package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// RoleHandler handles role-related HTTP requests
type RoleHandler struct {
	roleService *service.RoleService
}

// NewRoleHandler creates a new role handler
func NewRoleHandler(roleService *service.RoleService) *RoleHandler {
	return &RoleHandler{
		roleService: roleService,
	}
}

// RegisterRoutes registers all role routes (all protected with authentication)
func (h *RoleHandler) RegisterRoutes(app *fiber.App, authMw fiber.Handler) {
	// All role endpoints require authentication (role info in JWT is sufficient)
	roles := app.Group("/api/v1/roles", authMw)

	// GET endpoints (list and get role details) - any authenticated user
	roles.Get("/", h.ListRoles)
	roles.Get("/:id", h.GetRole)

	// POST/PUT/DELETE endpoints - require admin or system_admin role
	adminOnly := roles.Group("", auth.RequireRoles("admin", "system_admin"))
	adminOnly.Post("/", h.CreateRole)
	adminOnly.Put("/:id", h.UpdateRole)
	adminOnly.Delete("/:id", h.DeleteRole)
	adminOnly.Post("/:id/inherit/:parent_id", h.SetRoleHierarchy)
	adminOnly.Delete("/:id/inherit/:parent_id", h.RemoveRoleHierarchy)
}

// CreateRole creates a new role
func (h *RoleHandler) CreateRole(c *fiber.Ctx) error {
	var req service.CreateRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	role, err := h.roleService.CreateRole(&req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(role)
}

// ListRoles lists all roles with pagination
func (h *RoleHandler) ListRoles(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit

	roles, total, err := h.roleService.ListRoles(offset, limit)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"roles": roles,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetRole gets a specific role by ID
func (h *RoleHandler) GetRole(c *fiber.Ctx) error {
	roleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid role ID format")
	}

	role, err := h.roleService.GetRoleByID(roleID)
	if err != nil {
		return err
	}

	return c.JSON(role)
}

// UpdateRole updates a role
func (h *RoleHandler) UpdateRole(c *fiber.Ctx) error {
	roleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid role ID format")
	}

	var req service.UpdateRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	role, err := h.roleService.UpdateRole(roleID, &req)
	if err != nil {
		return err
	}

	return c.JSON(role)
}

// DeleteRole deletes a role
func (h *RoleHandler) DeleteRole(c *fiber.Ctx) error {
	roleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid role ID format")
	}

	if err := h.roleService.DeleteRole(roleID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Role deleted successfully",
	})
}

// SetRoleHierarchy sets role inheritance (child_role inherits from parent_role)
func (h *RoleHandler) SetRoleHierarchy(c *fiber.Ctx) error {
	childRoleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid child role ID format")
	}

	parentRoleID, err := uuid.Parse(c.Params("parent_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid parent role ID format")
	}

	if err := h.roleService.SetRoleHierarchy(childRoleID, parentRoleID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Role hierarchy set successfully",
	})
}

// RemoveRoleHierarchy removes role inheritance
func (h *RoleHandler) RemoveRoleHierarchy(c *fiber.Ctx) error {
	childRoleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid child role ID format")
	}

	parentRoleID, err := uuid.Parse(c.Params("parent_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid parent role ID format")
	}

	if err := h.roleService.RemoveRoleHierarchy(childRoleID, parentRoleID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Role hierarchy removed successfully",
	})
}
