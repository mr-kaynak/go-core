package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// PermissionHandler handles permission-related HTTP requests
type PermissionHandler struct {
	permRepo repository.PermissionRepository
	logger   *logger.Logger
}

// NewPermissionHandler creates a new permission handler
func NewPermissionHandler(permRepo repository.PermissionRepository) *PermissionHandler {
	return &PermissionHandler{
		permRepo: permRepo,
		logger:   logger.Get().WithFields(logger.Fields{"handler": "permission"}),
	}
}

// RegisterRoutes registers all permission routes (role-based permission management)
func (h *PermissionHandler) RegisterRoutes(app *fiber.App, authMw fiber.Handler) {
	// All permission endpoints require authentication and admin/system_admin role
	roles := app.Group("/api/v1/roles", authMw)

	// Permission management within role endpoints
	adminOnly := roles.Group("", auth.RequireRoles("admin", "system_admin"))
	adminOnly.Get("/:id/permissions", h.GetRolePermissions)
	adminOnly.Post("/:id/permissions", h.AddPermissionToRole)
	adminOnly.Delete("/:id/permissions/:permission_id", h.RemovePermissionFromRole)

	// Global permission endpoints
	perms := app.Group("/api/v1/permissions", authMw)
	perms.Get("/", h.ListPermissions)
	perms.Get("/:id", h.GetPermission)

	// Admin-only permission CRUD
	adminPerms := perms.Group("", auth.RequireRoles("admin", "system_admin"))
	adminPerms.Post("/", h.CreatePermission)
	adminPerms.Put("/:id", h.UpdatePermission)
	adminPerms.Delete("/:id", h.DeletePermission)
}

// ListPermissions godoc
// @Summary List all permissions
// @Description Get a list of all permissions with pagination and optional filtering by category
// @Tags Permissions
// @Security Bearer
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param category query string false "Filter by category"
// @Success 200 {object} map[string]interface{} "List of permissions"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /api/v1/permissions [get]
func (h *PermissionHandler) ListPermissions(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)
	category := c.Query("category", "")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit

	var permissions interface{}
	var total int64

	if category != "" {
		perms, err := h.permRepo.GetByCategory(category)
		if err != nil {
			h.logger.Error("Failed to fetch permissions by category", "category", category, "error", err)
			return errors.NewInternalError("Failed to fetch permissions")
		}
		permissions = perms
		total = int64(len(perms))
	} else {
		perms, err := h.permRepo.GetAll(offset, limit)
		if err != nil {
			h.logger.Error("Failed to fetch permissions", "error", err)
			return errors.NewInternalError("Failed to fetch permissions")
		}
		count, err := h.permRepo.Count()
		if err != nil {
			h.logger.Error("Failed to count permissions", "error", err)
			return errors.NewInternalError("Failed to fetch permissions count")
		}
		permissions = perms
		total = count
	}

	return c.JSON(fiber.Map{
		"data":  permissions,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetPermission retrieves a permission by ID
func (h *PermissionHandler) GetPermission(c *fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	perm, err := h.permRepo.GetByID(id)
	if err != nil {
		h.logger.Error("Failed to fetch permission", "id", id, "error", err)
		return err
	}

	return c.JSON(perm.ToResponse())
}

// CreatePermission creates a new permission
func (h *PermissionHandler) CreatePermission(c *fiber.Ctx) error {
	var req struct {
		Name        string `json:"name" validate:"required,min=3"`
		Description string `json:"description" validate:"max=255"`
		Category    string `json:"category" validate:"required,min=2"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := validation.Struct(req); err != nil {
		return err
	}

	// Permission creation requires proper service layer implementation
	// This is a placeholder that maintains the API contract
	h.logger.Info("Creating permission", "name", req.Name, "category", req.Category)

	// In production, this would call:
	// permission, err := h.permissionService.CreatePermission(req)
	// if err != nil { return err }

	// Return created response
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"name":        req.Name,
		"category":    req.Category,
		"description": req.Description,
		"message":     "Permission creation placeholder - implement service layer",
	})
}

// UpdatePermission updates a permission
func (h *PermissionHandler) UpdatePermission(c *fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	var req struct {
		Name        string `json:"name" validate:"omitempty,min=3"`
		Description string `json:"description" validate:"omitempty,max=255"`
		Category    string `json:"category" validate:"omitempty,min=2"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	perm, err := h.permRepo.GetByID(id)
	if err != nil {
		return err
	}

	if req.Name != "" {
		perm.Name = req.Name
	}
	if req.Description != "" {
		perm.Description = req.Description
	}
	if req.Category != "" {
		perm.Category = req.Category
	}

	if err := h.permRepo.Update(perm); err != nil {
		h.logger.Error("Failed to update permission", "id", id, "error", err)
		return errors.NewInternalError("Failed to update permission")
	}

	h.logger.Info("Permission updated", "id", id)
	return c.JSON(perm.ToResponse())
}

// DeletePermission deletes a permission
func (h *PermissionHandler) DeletePermission(c *fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	if err := h.permRepo.Delete(id); err != nil {
		h.logger.Error("Failed to delete permission", "id", id, "error", err)
		return errors.NewInternalError("Failed to delete permission")
	}

	h.logger.Info("Permission deleted", "id", id)
	return c.SendStatus(fiber.StatusNoContent)
}

// GetRolePermissions retrieves all permissions assigned to a role
func (h *PermissionHandler) GetRolePermissions(c *fiber.Ctx) error {
	roleID := c.Params("id")
	id, err := uuid.Parse(roleID)
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	perms, err := h.permRepo.GetRolePermissions(id)
	if err != nil {
		h.logger.Error("Failed to fetch role permissions", "role_id", id, "error", err)
		return errors.NewInternalError("Failed to fetch role permissions")
	}

	responses := make([]*struct {
		ID          uuid.UUID
		Name        string
		Description string
		Category    string
	}, len(perms))

	for i, perm := range perms {
		responses[i] = &struct {
			ID          uuid.UUID
			Name        string
			Description string
			Category    string
		}{
			ID:          perm.ID,
			Name:        perm.Name,
			Description: perm.Description,
			Category:    perm.Category,
		}
	}

	return c.JSON(responses)
}

// AddPermissionToRole adds a permission to a role
func (h *PermissionHandler) AddPermissionToRole(c *fiber.Ctx) error {
	roleID := c.Params("id")
	id, err := uuid.Parse(roleID)
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	var req struct {
		PermissionID uuid.UUID `json:"permission_id" validate:"required"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := validation.Struct(req); err != nil {
		return err
	}

	// Verify permission exists
	if _, err := h.permRepo.GetByID(req.PermissionID); err != nil {
		h.logger.Error("Permission not found", "permission_id", req.PermissionID, "error", err)
		return errors.NewNotFound("Permission", req.PermissionID.String())
	}

	if err := h.permRepo.AddPermissionToRole(id, req.PermissionID); err != nil {
		h.logger.Error("Failed to add permission to role", "role_id", id, "permission_id", req.PermissionID, "error", err)
		return errors.NewInternalError("Failed to add permission to role")
	}

	h.logger.Info("Permission added to role", "role_id", id, "permission_id", req.PermissionID)
	return c.SendStatus(fiber.StatusCreated)
}

// RemovePermissionFromRole removes a permission from a role
func (h *PermissionHandler) RemovePermissionFromRole(c *fiber.Ctx) error {
	roleID := c.Params("id")
	permID := c.Params("permission_id")

	id, err := uuid.Parse(roleID)
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	permissionID, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	if err := h.permRepo.RemovePermissionFromRole(id, permissionID); err != nil {
		h.logger.Error("Failed to remove permission from role", "role_id", id, "permission_id", permissionID, "error", err)
		return errors.NewInternalError("Failed to remove permission from role")
	}

	h.logger.Info("Permission removed from role", "role_id", id, "permission_id", permissionID)
	return c.SendStatus(fiber.StatusNoContent)
}
