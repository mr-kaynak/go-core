package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// swag annotation type references
var _ *domain.Role

// RoleHandler handles role-related HTTP requests
type RoleHandler struct {
	roleService  *service.RoleService
	auditService *service.AuditService
}

// ListRolesResponse is the standardized paginated response for roles.
type ListRolesResponse struct {
	Items      []domain.Role          `json:"items"`
	Pagination apiresponse.Pagination `json:"pagination"`
}

// NewRoleHandler creates a new role handler
func NewRoleHandler(roleService *service.RoleService) *RoleHandler {
	return &RoleHandler{
		roleService: roleService,
	}
}

// SetAuditService sets the optional audit service for logging security events.
func (h *RoleHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

func (h *RoleHandler) audit(c *fiber.Ctx, action, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID, _ := c.Locals("userID").(uuid.UUID)
		h.auditService.LogAction(&userID, action, "role", resourceID, c.IP(), c.Get("User-Agent"), meta)
	}
}

// RegisterRoutes registers all role routes on the given router (expected to be /api/v1).
func (h *RoleHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler) {
	roles := router.Group("/roles", authMw)

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
// @Summary Create a new role
// @Description Create a new role (admin only)
// @Tags Roles
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body service.CreateRoleRequest true "Role creation request"
// @Success 201 {object} domain.Role "Role created"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles [post]
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

	h.audit(c, service.ActionRoleCreate, role.ID.String(), map[string]interface{}{"name": req.Name})
	return c.Status(fiber.StatusCreated).JSON(role)
}

// ListRoles lists all roles with pagination
// @Summary List all roles
// @Description Get paginated list of roles
// @Tags Roles
// @Security Bearer
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} ListRolesResponse "List of roles"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /roles [get]
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

	return c.JSON(apiresponse.NewPaginatedResponse(roles, page, limit, total))
}

// GetRole gets a specific role by ID
// @Summary Get a role by ID
// @Description Get role details by UUID
// @Tags Roles
// @Security Bearer
// @Produce json
// @Param id path string true "Role UUID"
// @Success 200 {object} domain.Role "Role details"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 404 {object} errors.ProblemDetail "Role not found"
// @Router /roles/{id} [get]
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
// @Summary Update a role
// @Description Update role details (admin only)
// @Tags Roles
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Role UUID"
// @Param request body service.UpdateRoleRequest true "Role update request"
// @Success 200 {object} domain.Role "Updated role"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "Role not found"
// @Router /roles/{id} [put]
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

	h.audit(c, service.ActionRoleUpdate, roleID.String(), nil)
	return c.JSON(role)
}

// DeleteRole deletes a role
// @Summary Delete a role
// @Description Delete a role by ID (admin only)
// @Tags Roles
// @Security Bearer
// @Produce json
// @Param id path string true "Role UUID"
// @Success 200 {object} MessageResponse "Role deleted"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles/{id} [delete]
func (h *RoleHandler) DeleteRole(c *fiber.Ctx) error {
	roleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid role ID format")
	}

	if err := h.roleService.DeleteRole(roleID); err != nil {
		return err
	}

	h.audit(c, service.ActionRoleDelete, roleID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "Role deleted successfully",
	})
}

// SetRoleHierarchy sets role inheritance (child_role inherits from parent_role)
// @Summary Set role hierarchy
// @Description Set role inheritance (child inherits from parent)
// @Tags Roles
// @Security Bearer
// @Produce json
// @Param id path string true "Child role UUID"
// @Param parent_id path string true "Parent role UUID"
// @Success 200 {object} MessageResponse "Hierarchy set"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles/{id}/inherit/{parent_id} [post]
func (h *RoleHandler) SetRoleHierarchy(c *fiber.Ctx) error {
	return h.modifyRoleHierarchy(
		c, h.roleService.SetRoleHierarchy,
		service.ActionRoleHierarchySet, "Role hierarchy set successfully",
	)
}

// RemoveRoleHierarchy removes role inheritance
// @Summary Remove role hierarchy
// @Description Remove role inheritance relationship
// @Tags Roles
// @Security Bearer
// @Produce json
// @Param id path string true "Child role UUID"
// @Param parent_id path string true "Parent role UUID"
// @Success 200 {object} MessageResponse "Hierarchy removed"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles/{id}/inherit/{parent_id} [delete]
func (h *RoleHandler) RemoveRoleHierarchy(c *fiber.Ctx) error {
	return h.modifyRoleHierarchy(
		c, h.roleService.RemoveRoleHierarchy,
		service.ActionRoleHierarchyRemove, "Role hierarchy removed successfully",
	)
}

func (h *RoleHandler) modifyRoleHierarchy(
	c *fiber.Ctx,
	action func(uuid.UUID, uuid.UUID) error,
	auditAction, message string,
) error {
	childRoleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid child role ID format")
	}

	parentRoleID, err := uuid.Parse(c.Params("parent_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid parent role ID format")
	}

	if err := action(childRoleID, parentRoleID); err != nil {
		return err
	}

	h.audit(c, auditAction, childRoleID.String(),
		map[string]interface{}{"parent_id": parentRoleID.String()})
	return c.JSON(fiber.Map{
		"message": message,
	})
}
