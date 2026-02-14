package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// PermissionHandler handles permission-related HTTP requests
type PermissionHandler struct {
	permRepo     repository.PermissionRepository
	roleRepo     repository.RoleRepository
	casbinService *authorization.CasbinService
	auditService *service.AuditService
	logger       *logger.Logger
}

// NewPermissionHandler creates a new permission handler
func NewPermissionHandler(
	permRepo repository.PermissionRepository,
	roleRepo repository.RoleRepository,
	casbinService *authorization.CasbinService,
) *PermissionHandler {
	return &PermissionHandler{
		permRepo:      permRepo,
		roleRepo:      roleRepo,
		casbinService: casbinService,
		logger:        logger.Get().WithFields(logger.Fields{"handler": "permission"}),
	}
}

// SetAuditService sets the optional audit service for logging security events.
func (h *PermissionHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

func (h *PermissionHandler) audit(c *fiber.Ctx, action, resource, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID, _ := c.Locals("userID").(uuid.UUID)
		h.auditService.LogAction(&userID, action, resource, resourceID, c.IP(), c.Get("User-Agent"), meta)
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
// @Success 200 {object} ListPermissionsResponse "List of permissions"
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

	var permissions []domain.Permission
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

	return c.JSON(apiresponse.NewPaginatedResponse(permissions, page, limit, total))
}

// GetPermission godoc
// @Summary Get a permission by ID
// @Description Get permission details by UUID
// @Tags Permissions
// @Security Bearer
// @Produce json
// @Param id path string true "Permission UUID"
// @Success 200 {object} domain.PermissionResponse "Permission details"
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
// @Success 201 {object} MessageResponse "Permission created"
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

	// Check for duplicate permission name
	existing, err := h.permRepo.GetByName(req.Name)
	if err != nil {
		// If the error is NOT a "not found" error, it's a real DB error
		pd := errors.GetProblemDetail(err)
		if pd == nil || pd.Code != errors.CodeNotFound {
			h.logger.Error("Failed to check existing permission", "name", req.Name, "error", err)
			return errors.NewInternalError("Failed to create permission")
		}
	}
	if existing != nil {
		return errors.NewConflict("Permission with name '" + req.Name + "' already exists")
	}

	// Create domain object
	permission := &domain.Permission{
		ID:          uuid.New(),
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
	}

	// Save to database
	if err := h.permRepo.Create(permission); err != nil {
		h.logger.Error("Failed to create permission", "name", req.Name, "error", err)
		return errors.NewInternalError("Failed to create permission")
	}

	// Audit log
	h.audit(c, service.ActionPermissionCreate, "permission", permission.ID.String(), map[string]interface{}{"name": req.Name, "category": req.Category})

	return c.Status(fiber.StatusCreated).JSON(permission.ToResponse())
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
// @Success 200 {object} domain.PermissionResponse "Updated permission"
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

	h.audit(c, service.ActionPermissionUpdate, "permission", id.String(), nil)
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
// @Success 200 {array} domain.PermissionResponse "List of permissions"
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

	// Sync to Casbin
	h.syncPermissionToCasbin(id, req.PermissionID, true)

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

	// Sync removal to Casbin
	h.syncPermissionToCasbin(id, permissionID, false)

	h.audit(c, service.ActionPermissionRemoveFromRole, "role", id.String(), map[string]interface{}{"permission_id": permissionID.String()})
	h.logger.Info("Permission removed from role", "role_id", id, "permission_id", permissionID)
	return c.SendStatus(fiber.StatusNoContent)
}

// syncPermissionToCasbin adds or removes a Casbin policy for a role-permission pair.
// It is best-effort: failures are logged but do not block the HTTP response.
func (h *PermissionHandler) syncPermissionToCasbin(roleID, permissionID uuid.UUID, add bool) {
	if h.casbinService == nil || h.roleRepo == nil {
		return
	}

	role, err := h.roleRepo.GetByID(roleID)
	if err != nil {
		h.logger.Error("Casbin sync: failed to fetch role", "role_id", roleID, "error", err)
		return
	}

	perm, err := h.permRepo.GetByID(permissionID)
	if err != nil {
		h.logger.Error("Casbin sync: failed to fetch permission", "permission_id", permissionID, "error", err)
		return
	}

	mapping, ok := authorization.GetCasbinMapping(perm.Name)
	if !ok {
		h.logger.Warn("Casbin sync: no mapping for permission", "permission", perm.Name)
		return
	}

	subject := "role:" + role.Name
	resource := string(mapping.Resource)

	if add {
		if err := h.casbinService.AddPolicy(subject, authorization.DomainDefault, resource, mapping.Action, "allow"); err != nil {
			h.logger.Error("Casbin sync: failed to add policy", "role", role.Name, "permission", perm.Name, "error", err)
		}
	} else {
		if err := h.casbinService.RemovePolicy(subject, authorization.DomainDefault, resource, mapping.Action, "allow"); err != nil {
			h.logger.Error("Casbin sync: failed to remove policy", "role", role.Name, "permission", perm.Name, "error", err)
		}
	}
}
