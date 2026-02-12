package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// TwoFactorHandler handles two-factor authentication HTTP requests
type TwoFactorHandler struct {
	authService  *service.AuthService
	auditService *service.AuditService
}

// NewTwoFactorHandler creates a new two-factor handler
func NewTwoFactorHandler(authService *service.AuthService) *TwoFactorHandler {
	return &TwoFactorHandler{
		authService: authService,
	}
}

// SetAuditService sets the optional audit service for logging 2FA events.
func (h *TwoFactorHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

// audit is a nil-safe helper that logs an action if audit service is configured.
func (h *TwoFactorHandler) audit(c *fiber.Ctx, userID uuid.UUID, action string) {
	if h.auditService != nil {
		h.auditService.LogAction(&userID, action, "user", userID.String(), c.IP(), c.Get("User-Agent"), nil)
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

	result, err := h.authService.Enable2FA(claims.UserID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message":      "Two-factor authentication setup initiated. Scan the QR code with your authenticator app.",
		"otp_url":      result.OTPAuthURL,
		"backup_codes": result.BackupCodes,
	})
}

// handle2FAAction is a shared helper for Verify and Disable handlers
func (h *TwoFactorHandler) handle2FAAction(
	c *fiber.Ctx,
	action func(userID uuid.UUID, code string) error,
	successMsg, auditAction string,
) error {
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

	if err := action(claims.UserID, req.Code); err != nil {
		return err
	}

	h.audit(c, claims.UserID, auditAction)
	return c.JSON(fiber.Map{
		"message": successMsg,
	})
}

// Verify verifies a TOTP code to complete the 2FA setup
func (h *TwoFactorHandler) Verify(c *fiber.Ctx) error {
	return h.handle2FAAction(c, h.authService.Verify2FA,
		"Two-factor authentication has been enabled successfully", service.Action2FAEnable)
}

// Disable disables 2FA after verifying a valid TOTP code
func (h *TwoFactorHandler) Disable(c *fiber.Ctx) error {
	return h.handle2FAAction(c, h.authService.Disable2FA,
		"Two-factor authentication has been disabled", service.Action2FADisable)
}
