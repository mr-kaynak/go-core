package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// APIKeyHandler handles API key related HTTP requests
type APIKeyHandler struct {
	apiKeyService *service.APIKeyService
}

// NewAPIKeyHandler creates a new API key handler
func NewAPIKeyHandler(apiKeyService *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
	}
}

// RegisterRoutes registers API key routes (all require authentication)
func (h *APIKeyHandler) RegisterRoutes(app *fiber.App, authMw fiber.Handler) {
	apiKeys := app.Group("/api/v1/api-keys", authMw)

	apiKeys.Post("/", h.CreateAPIKey)
	apiKeys.Get("/", h.ListAPIKeys)
	apiKeys.Delete("/:id", h.RevokeAPIKey)
}

// CreateAPIKey handles API key creation
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

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":    "API key created successfully. Store the key securely, it will not be shown again.",
		"api_key":    response.APIKey,
		"key":        response.RawKey,
	})
}

// ListAPIKeys handles listing a user's API keys
func (h *APIKeyHandler) ListAPIKeys(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	keys, err := h.apiKeyService.List(userID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"api_keys": keys,
	})
}

// RevokeAPIKey handles revoking an API key
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

	return c.JSON(fiber.Map{
		"message": "API key revoked successfully",
	})
}
