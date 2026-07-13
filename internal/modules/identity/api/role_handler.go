package api

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	helpers "github.com/mr-kaynak/go-core/internal/api/helpers"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
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

func (h *RoleHandler) audit(c fiber.Ctx, action, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID := fiber.Locals[uuid.UUID](c, "userID")
		h.auditService.LogAction(c.Context(), &userID, action, "role", resourceID, c.IP(), c.UserAgent(), meta)
	}
}

// RegisterRoutes registers all role routes on the given router (expected to be /api/v1).
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
func (h *RoleHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	middlewares := []any{authMw}
	if authzMw != nil {
		middlewares = append(middlewares, authzMw)
	}
	roles := router.Group("/roles", middlewares...)
	roles.Get("/", h.ListRoles)
	roles.Get("/:id", h.GetRole)
	roles.Post("/", h.CreateRole)
	roles.Put("/:id", h.UpdateRole)
	roles.Delete("/:id", h.DeleteRole)
	roles.Post("/:id/inherit/:parent_id", h.SetRoleHierarchy)
	roles.Delete("/:id/inherit/:parent_id", h.RemoveRoleHierarchy)
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
func (h *RoleHandler) CreateRole(c fiber.Ctx) error {
	var req service.CreateRoleRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	role, err := h.roleService.CreateRole(c.Context(), &req)
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
func (h *RoleHandler) ListRoles(c fiber.Ctx) error {
	page, limit, offset := helpers.ParsePagination(c, 10)

	roles, total, err := h.roleService.ListRoles(c.Context(), offset, limit)
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
func (h *RoleHandler) GetRole(c fiber.Ctx) error {
	roleID, err := helpers.ParseUUIDParam(c, "id", "Invalid role ID format")
	if err != nil {
		return err
	}

	role, err := h.roleService.GetRoleByID(c.Context(), roleID)
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
func (h *RoleHandler) UpdateRole(c fiber.Ctx) error {
	roleID, err := helpers.ParseUUIDParam(c, "id", "Invalid role ID format")
	if err != nil {
		return err
	}

	var req service.UpdateRoleRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	role, err := h.roleService.UpdateRole(c.Context(), roleID, &req)
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
// @Param id path string true "Role UUID"
// @Success 204 "Role deleted"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles/{id} [delete]
func (h *RoleHandler) DeleteRole(c fiber.Ctx) error {
	roleID, err := helpers.ParseUUIDParam(c, "id", "Invalid role ID format")
	if err != nil {
		return err
	}

	if err := h.roleService.DeleteRole(c.Context(), roleID); err != nil {
		return err
	}

	h.audit(c, service.ActionRoleDelete, roleID.String(), nil)
	return c.SendStatus(fiber.StatusNoContent)
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
func (h *RoleHandler) SetRoleHierarchy(c fiber.Ctx) error {
	childRoleID, err := helpers.ParseUUIDParam(c, "id", "Invalid child role ID format")
	if err != nil {
		return err
	}

	parentRoleID, err := helpers.ParseUUIDParam(c, "parent_id", "Invalid parent role ID format")
	if err != nil {
		return err
	}

	if err := h.roleService.SetRoleHierarchy(c.Context(), childRoleID, parentRoleID); err != nil {
		return err
	}

	h.audit(c, service.ActionRoleHierarchySet, childRoleID.String(),
		map[string]interface{}{"parent_id": parentRoleID.String()})
	return c.JSON(fiber.Map{
		"message": "Role hierarchy set successfully",
	})
}

// RemoveRoleHierarchy removes role inheritance
// @Summary Remove role hierarchy
// @Description Remove role inheritance relationship
// @Tags Roles
// @Security Bearer
// @Param id path string true "Child role UUID"
// @Param parent_id path string true "Parent role UUID"
// @Success 204 "Hierarchy removed"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /roles/{id}/inherit/{parent_id} [delete]
func (h *RoleHandler) RemoveRoleHierarchy(c fiber.Ctx) error {
	return h.modifyRoleHierarchy(c, h.roleService.RemoveRoleHierarchy, service.ActionRoleHierarchyRemove)
}

func (h *RoleHandler) modifyRoleHierarchy(
	c fiber.Ctx,
	action func(context.Context, uuid.UUID, uuid.UUID) error,
	auditAction string,
) error {
	childRoleID, err := helpers.ParseUUIDParam(c, "id", "Invalid child role ID format")
	if err != nil {
		return err
	}

	parentRoleID, err := helpers.ParseUUIDParam(c, "parent_id", "Invalid parent role ID format")
	if err != nil {
		return err
	}

	if err := action(c.Context(), childRoleID, parentRoleID); err != nil {
		return err
	}

	h.audit(c, auditAction, childRoleID.String(),
		map[string]interface{}{"parent_id": parentRoleID.String()})
	return c.SendStatus(fiber.StatusNoContent)
}
