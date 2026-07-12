package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// PermissionHandler handles permission-related HTTP requests
type PermissionHandler struct {
	permService  *service.PermissionService
	auditService *service.AuditService
	logger       *logger.Logger
}

// NewPermissionHandler creates a new permission handler
func NewPermissionHandler(permService *service.PermissionService) *PermissionHandler {
	return &PermissionHandler{
		permService: permService,
		logger:      logger.Get().WithFields(logger.Fields{"handler": "permission"}),
	}
}

// SetAuditService sets the optional audit service for logging security events.
func (h *PermissionHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

func (h *PermissionHandler) audit(c fiber.Ctx, action, resource, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID := fiber.Locals[uuid.UUID](c, "userID")
		h.auditService.LogAction(c.Context(), &userID, action, resource, resourceID, c.IP(), c.UserAgent(), meta)
	}
}

// PermissionResponse represents a permission in API responses.
type PermissionResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func permissionToResponse(p *domain.Permission) *PermissionResponse {
	return &PermissionResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Category:    p.Category,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
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

// ListPermissionsResponse is the standardized paginated response for permissions.
type ListPermissionsResponse struct {
	Items      []domain.Permission    `json:"items"`
	Pagination apiresponse.Pagination `json:"pagination"`
}

// RegisterRoutes registers all permission routes (role-based permission management).
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
func (h *PermissionHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	middlewares := []any{authMw}
	if authzMw != nil {
		middlewares = append(middlewares, authzMw)
	}

	// Permission management within role endpoints
	roles := router.Group("/roles", middlewares...)
	roles.Get("/:id/permissions", h.GetRolePermissions)
	roles.Post("/:id/permissions", h.AddPermissionToRole)
	roles.Delete("/:id/permissions/:permission_id", h.RemovePermissionFromRole)

	// Global permission endpoints
	perms := router.Group("/permissions", middlewares...)
	perms.Get("/", h.ListPermissions)
	perms.Get("/:id", h.GetPermission)
	perms.Post("/", h.CreatePermission)
	perms.Put("/:id", h.UpdatePermission)
	perms.Delete("/:id", h.DeletePermission)
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
// @Success 200 {object} ListPermissionsResponse "List of permissions"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /permissions [get]
func (h *PermissionHandler) ListPermissions(c fiber.Ctx) error {
	page := fiber.Query[int](c, "page", 1)
	limit := fiber.Query[int](c, "limit", 10)
	category := c.Query("category", "")

	if page < 1 {
		page = 1
	}
	limit = apiresponse.SanitizeLimit(limit, 10)

	offset := (page - 1) * limit

	permissions, total, err := h.permService.ListPermissions(c.Context(), category, offset, limit)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(permissions, page, limit, total))
}

// GetPermission godoc
// @Summary Get a permission by ID
// @Description Get permission details by UUID
// @Tags Permissions
// @Security Bearer
// @Produce json
// @Param id path string true "Permission UUID"
// @Success 200 {object} PermissionResponse "Permission details"
// @Failure 400 {object} errors.ProblemDetail "Invalid permission ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 404 {object} errors.ProblemDetail "Permission not found"
// @Router /permissions/{id} [get]
func (h *PermissionHandler) GetPermission(c fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	perm, err := h.permService.GetPermission(c.Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(permissionToResponse(perm))
}

// CreatePermission godoc
// @Summary Create a new permission
// @Description Create a new permission (admin only)
// @Tags Permissions
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body CreatePermissionRequest true "Permission creation request"
// @Success 201 {object} MessageResponse "Permission created"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /permissions [post]
func (h *PermissionHandler) CreatePermission(c fiber.Ctx) error {
	var req CreatePermissionRequest

	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := validation.Struct(req); err != nil {
		return err
	}

	permission, err := h.permService.CreatePermission(c.Context(), req.Name, req.Description, req.Category)
	if err != nil {
		return err
	}

	h.audit(c, service.ActionPermissionCreate, "permission", permission.ID.String(),
		map[string]interface{}{"name": req.Name, "category": req.Category})

	return c.Status(fiber.StatusCreated).JSON(permissionToResponse(permission))
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
// @Success 200 {object} PermissionResponse "Updated permission"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "Permission not found"
// @Router /permissions/{id} [put]
func (h *PermissionHandler) UpdatePermission(c fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	var req UpdatePermissionRequest

	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := validation.Struct(req); err != nil {
		return err
	}

	perm, err := h.permService.UpdatePermission(c.Context(), id, req.Name, req.Description, req.Category)
	if err != nil {
		return err
	}

	h.audit(c, service.ActionPermissionUpdate, "permission", id.String(), nil)
	h.logger.Info("Permission updated", "id", id)
	return c.JSON(permissionToResponse(perm))
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
func (h *PermissionHandler) DeletePermission(c fiber.Ctx) error {
	permID := c.Params("id")
	id, err := uuid.Parse(permID)
	if err != nil {
		return errors.NewBadRequest("Invalid permission ID")
	}

	if err := h.permService.DeletePermission(c.Context(), id); err != nil {
		return err
	}

	h.audit(c, service.ActionPermissionDelete, "permission", id.String(), nil)
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
// @Success 200 {array} PermissionResponse "List of permissions"
// @Failure 400 {object} errors.ProblemDetail "Invalid role ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /roles/{id}/permissions [get]
func (h *PermissionHandler) GetRolePermissions(c fiber.Ctx) error {
	roleID := c.Params("id")
	id, err := uuid.Parse(roleID)
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	perms, err := h.permService.GetRolePermissions(c.Context(), id)
	if err != nil {
		return err
	}

	responses := make([]*PermissionResponse, len(perms))
	for i := range perms {
		responses[i] = permissionToResponse(&perms[i])
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
func (h *PermissionHandler) AddPermissionToRole(c fiber.Ctx) error {
	roleID := c.Params("id")
	id, err := uuid.Parse(roleID)
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	var req AddPermissionToRoleRequest

	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := validation.Struct(req); err != nil {
		return err
	}

	if err := h.permService.AddPermissionToRole(c.Context(), id, req.PermissionID); err != nil {
		return err
	}

	h.audit(c, service.ActionPermissionAddToRole, "role", id.String(), map[string]interface{}{"permission_id": req.PermissionID.String()})
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
func (h *PermissionHandler) RemovePermissionFromRole(c fiber.Ctx) error {
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

	if err := h.permService.RemovePermissionFromRole(c.Context(), id, permissionID); err != nil {
		return err
	}

	h.audit(c, service.ActionPermissionRemoveFromRole, "role", id.String(), map[string]interface{}{"permission_id": permissionID.String()})
	h.logger.Info("Permission removed from role", "role_id", id, "permission_id", permissionID)
	return c.SendStatus(fiber.StatusNoContent)
}
