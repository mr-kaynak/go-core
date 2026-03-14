package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// AssignRoleToAPIKeyRequest represents a request to assign a role to an API key
type AssignRoleToAPIKeyRequest struct {
	RoleID uuid.UUID `json:"role_id" validate:"required"`
}

// ListAPIKeysResponse is the standardized paginated response for API keys.
type ListAPIKeysResponse struct {
	Items      []*domain.APIKey       `json:"items"`
	Pagination apiresponse.Pagination `json:"pagination"`
}

// CreateAPIKeyResponse is the response for API key creation.
type CreateAPIKeyResponse struct {
	Message string         `json:"message"`
	APIKey  *domain.APIKey `json:"api_key"`
	Key     string         `json:"key"`
}

// APIKeyRolesResponse is the response for listing API key roles.
type APIKeyRolesResponse struct {
	Roles []domain.Role `json:"roles"`
}

// APIKeyHandler handles API key related HTTP requests
type APIKeyHandler struct {
	apiKeyService *service.APIKeyService
	auditService  *service.AuditService
}

// NewAPIKeyHandler creates a new API key handler
func NewAPIKeyHandler(apiKeyService *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
	}
}

// SetAuditService sets the optional audit service for logging API key events.
func (h *APIKeyHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

// audit is a nil-safe helper that logs an action if audit service is configured.
func (h *APIKeyHandler) audit(c *fiber.Ctx, userID uuid.UUID, action, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		h.auditService.LogAction(&userID, action, "api_key", resourceID, c.IP(), c.Get("User-Agent"), meta)
	}
}

// RegisterRoutes registers API key routes (all require authentication)
func (h *APIKeyHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler) {
	apiKeys := router.Group("/api-keys", authMw)

	apiKeys.Post("/", h.CreateAPIKey)
	apiKeys.Get("/", h.ListAPIKeys)
	apiKeys.Delete("/:id", h.RevokeAPIKey)

	// Role management endpoints
	apiKeys.Get("/:id/roles", h.GetAPIKeyRoles)
	apiKeys.Post("/:id/roles", h.AssignRoleToAPIKey)
	apiKeys.Delete("/:id/roles/:role_id", h.RemoveRoleFromAPIKey)
}

// CreateAPIKey handles API key creation
// @Summary Create a new API key
// @Description Create a new API key for the authenticated user. Optionally assign roles via role_ids.
// @Tags API-Keys
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body service.CreateAPIKeyRequest true "API key creation request"
// @Success 201 {object} CreateAPIKeyResponse "API key created with raw key"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /api-keys [post]
func (h *APIKeyHandler) CreateAPIKey(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var req service.CreateAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	response, err := h.apiKeyService.Create(userID, &req)
	if err != nil {
		return err
	}

	h.audit(c, userID, service.ActionAPIKeyCreated, response.APIKey.ID.String(),
		map[string]interface{}{"key_name": req.Name})
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "API key created successfully. Store the key securely, it will not be shown again.",
		"api_key": response.APIKey,
		"key":     response.RawKey,
	})
}

// ListAPIKeys handles listing a user's API keys
// @Summary List user's API keys
// @Description Get all API keys for the authenticated user (includes assigned roles)
// @Tags API-Keys
// @Security Bearer
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} ListAPIKeysResponse "List of API keys"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /api-keys [get]
func (h *APIKeyHandler) ListAPIKeys(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	keys, total, err := h.apiKeyService.List(userID, offset, limit)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(keys, page, limit, total))
}

// RevokeAPIKey handles revoking an API key
// @Summary Revoke an API key
// @Description Revoke an API key by ID (owner only)
// @Tags API-Keys
// @Security Bearer
// @Produce json
// @Param id path string true "API key UUID"
// @Success 200 {object} MessageResponse "API key revoked"
// @Failure 400 {object} errors.ProblemDetail "Invalid API key ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "API key not found"
// @Router /api-keys/{id} [delete]
func (h *APIKeyHandler) RevokeAPIKey(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	keyID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid API key ID format")
	}

	if err := h.apiKeyService.Revoke(keyID, userID); err != nil {
		return err
	}

	h.audit(c, userID, service.ActionAPIKeyRevoked, keyID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "API key revoked successfully",
	})
}

// GetAPIKeyRoles godoc
// @Summary Get API key roles
// @Description Get all roles assigned to an API key (owner only)
// @Tags API-Keys
// @Security Bearer
// @Produce json
// @Param id path string true "API key UUID"
// @Success 200 {object} APIKeyRolesResponse "List of roles"
// @Failure 400 {object} errors.ProblemDetail "Invalid API key ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "API key not found"
// @Router /api-keys/{id}/roles [get]
func (h *APIKeyHandler) GetAPIKeyRoles(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	keyID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid API key ID format")
	}

	roles, err := h.apiKeyService.GetAPIKeyRoles(keyID, userID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"roles": roles,
	})
}

// AssignRoleToAPIKey godoc
// @Summary Assign role to API key
// @Description Assign a role to an API key (owner only)
// @Tags API-Keys
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "API key UUID"
// @Param request body AssignRoleToAPIKeyRequest true "Role to assign"
// @Success 200 {object} MessageResponse "Role assigned"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "API key or role not found"
// @Router /api-keys/{id}/roles [post]
func (h *APIKeyHandler) AssignRoleToAPIKey(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	keyID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid API key ID format")
	}

	var req AssignRoleToAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := validation.Struct(req); err != nil {
		return err
	}

	if err := h.apiKeyService.AssignRole(keyID, req.RoleID, userID); err != nil {
		return err
	}

	h.audit(c, userID, service.ActionAPIKeyRoleAssigned, keyID.String(),
		map[string]interface{}{"role_id": req.RoleID.String()})
	return c.JSON(fiber.Map{
		"message": "Role assigned to API key successfully",
	})
}

// RemoveRoleFromAPIKey godoc
// @Summary Remove role from API key
// @Description Remove a role from an API key (owner only)
// @Tags API-Keys
// @Security Bearer
// @Produce json
// @Param id path string true "API key UUID"
// @Param role_id path string true "Role UUID"
// @Success 204 "Role removed from API key"
// @Failure 400 {object} errors.ProblemDetail "Invalid ID format"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "API key not found"
// @Router /api-keys/{id}/roles/{role_id} [delete]
func (h *APIKeyHandler) RemoveRoleFromAPIKey(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	keyID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid API key ID format")
	}

	roleID, err := uuid.Parse(c.Params("role_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid role ID format")
	}

	if err := h.apiKeyService.RemoveRole(keyID, roleID, userID); err != nil {
		return err
	}

	h.audit(c, userID, service.ActionAPIKeyRoleRemoved, keyID.String(),
		map[string]interface{}{"role_id": roleID.String()})
	return c.SendStatus(fiber.StatusNoContent)
}
