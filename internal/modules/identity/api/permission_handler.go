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

// CreatePermissionRequest represents a permission creation request
type CreatePermissionRequest struct {
	Name        string `json:"name" validate:"required,min=3"`
	Description string `json:"description" validate:"max=255"`
	Category    string `json:"category" validate:"required,min=2"`
}

// UpdatePermissionRequest represents a permission update request
type UpdatePermissionRequest struct {
	Name        string `json:"name" validate:"omitempty,min=3"`
	Description string `json:"description" validate:"omitempty,max=255"`
	Category    string `json:"category" validate:"omitempty,min=2"`
}

// AddPermissionToRoleRequest represents a request to add a permission to a role
type AddPermissionToRoleRequest struct {
	PermissionID uuid.UUID `json:"permission_id" validate:"required"`
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
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param category query string false "Filter by category"
// @Success 200 {object} fiber.Map "List of permissions"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /permissions [get]
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

// GetPermission godoc
// @Summary Get a permission by ID
// @Description Get permission details by UUID
// @Tags Permissions
// @Security Bearer
// @Produce json
// @Param id path string true "Permission UUID"
// @Success 200 {object} fiber.Map "Permission details"
// @Failure 400 {object} errors.ProblemDetail "Invalid permission ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 404 {object} errors.ProblemDetail "Permission not found"
// @Router /permissions/{id} [get]
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

// CreatePermission godoc
// @Summary Create a new permission
// @Description Create a new permission (admin only)
// @Tags Permissions
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body CreatePermissionRequest true "Permission creation request"
// @Success 201 {object} fiber.Map "Permission created"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /permissions [post]
func (h *PermissionHandler) CreatePermission(c *fiber.Ctx) error {
	var req CreatePermissionRequest

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

// UpdatePermission godoc
// @Summary Update a permission
// @Description Update permission details (admin only)
// @Tags Permissions
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Permission UUID"
// @Param request body UpdatePermissionRequest true "Permission update request"
// @Success 200 {object} fiber.Map "Updated permission"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "Permission not found"
// @Router /permissions/{id} [put]
func (h *PermissionHandler) UpdatePermission(c *fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	var req UpdatePermissionRequest

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

// DeletePermission godoc
// @Summary Delete a permission
// @Description Delete a permission (admin only)
// @Tags Permissions
// @Security Bearer
// @Param id path string true "Permission UUID"
// @Success 204 "Permission deleted"
// @Failure 400 {object} errors.ProblemDetail "Invalid permission ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /permissions/{id} [delete]
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

// GetRolePermissions godoc
// @Summary Get role permissions
// @Description Get all permissions assigned to a role
// @Tags Permissions
// @Security Bearer
// @Produce json
// @Param id path string true "Role UUID"
// @Success 200 {array} fiber.Map "List of permissions"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /roles/{id}/permissions [get]
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

	for i := range perms {
		responses[i] = &struct {
			ID          uuid.UUID
			Name        string
			Description string
			Category    string
		}{
			ID:          perms[i].ID,
			Name:        perms[i].Name,
			Description: perms[i].Description,
			Category:    perms[i].Category,
		}
	}

	return c.JSON(responses)
}

// AddPermissionToRole godoc
// @Summary Add permission to role
// @Description Add a permission to a role (admin only)
// @Tags Permissions
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Role UUID"
// @Param request body AddPermissionToRoleRequest true "Permission to add"
// @Success 201 "Permission added to role"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "Permission not found"
// @Router /roles/{id}/permissions [post]
func (h *PermissionHandler) AddPermissionToRole(c *fiber.Ctx) error {
	roleID := c.Params("id")
	id, err := uuid.Parse(roleID)
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	var req AddPermissionToRoleRequest

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

// RemovePermissionFromRole godoc
// @Summary Remove permission from role
// @Description Remove a permission from a role (admin only)
// @Tags Permissions
// @Security Bearer
// @Param id path string true "Role UUID"
// @Param permission_id path string true "Permission UUID"
// @Success 204 "Permission removed from role"
// @Failure 400 {object} errors.ProblemDetail "Invalid ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles/{id}/permissions/{permission_id} [delete]
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
