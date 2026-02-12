package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// TwoFactorHandler handles two-factor authentication HTTP requests
type TwoFactorHandler struct {
	authService *service.AuthService
}

// NewTwoFactorHandler creates a new two-factor handler
func NewTwoFactorHandler(authService *service.AuthService) *TwoFactorHandler {
	return &TwoFactorHandler{
		authService: authService,
	}
}

// RegisterRoutes registers 2FA routes under /auth/2fa (all require authentication)
func (h *TwoFactorHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler) {
	twoFA := router.Group("/auth/2fa", authMw)

	twoFA.Post("/enable", h.Enable)
	twoFA.Post("/verify", h.Verify)
	twoFA.Post("/disable", h.Disable)
}

// Enable initiates the 2FA setup by generating a TOTP secret and returning the otpauth URL
func (h *TwoFactorHandler) Enable(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	otpURL, err := h.authService.Enable2FA(claims.UserID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Two-factor authentication setup initiated. Scan the QR code with your authenticator app.",
		"otp_url": otpURL,
	})
}

// Verify verifies a TOTP code to complete the 2FA setup
func (h *TwoFactorHandler) Verify(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	var req struct {
		Code string `json:"code"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if req.Code == "" {
		return errors.NewBadRequest("Two-factor code is required")
	}

	if err := h.authService.Verify2FA(claims.UserID, req.Code); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Two-factor authentication has been enabled successfully",
	})
}

// Disable disables 2FA after verifying a valid TOTP code
func (h *TwoFactorHandler) Disable(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	var req struct {
		Code string `json:"code"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if req.Code == "" {
		return errors.NewBadRequest("Two-factor code is required")
	}

	if err := h.authService.Disable2FA(claims.UserID, req.Code); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Two-factor authentication has been disabled",
	})
}
