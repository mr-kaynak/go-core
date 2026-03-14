package api

import (
	"github.com/gofiber/fiber/v3"
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
func (h *TwoFactorHandler) audit(c fiber.Ctx, userID uuid.UUID, action string) {
	if h.auditService != nil {
		h.auditService.LogAction(&userID, action, "user", userID.String(), c.IP(), c.UserAgent(), nil)
	}
}

// TwoFactorCodeRequest represents a 2FA code verification request
type TwoFactorCodeRequest struct {
	Code string `json:"code"`
}

// Enable2FAResponse is the response for 2FA setup initiation.
type Enable2FAResponse struct {
	Message     string   `json:"message"`
	OTPURL      string   `json:"otp_url"`
	BackupCodes []string `json:"backup_codes"`
}

// RegisterRoutes registers 2FA routes.
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
func (h *TwoFactorHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	middlewares := []any{authMw}
	if authzMw != nil {
		middlewares = append(middlewares, authzMw)
	}
	twoFA := router.Group("/auth/2fa", middlewares...)

	twoFA.Post("/enable", h.Enable)
	twoFA.Post("/verify", h.Verify)
	twoFA.Post("/disable", h.Disable)
}

// Enable initiates the 2FA setup by generating a TOTP secret and returning the otpauth URL
// @Summary Enable two-factor authentication
// @Description Initiate 2FA setup by generating a TOTP secret and QR code URL
// @Tags 2FA
// @Security Bearer
// @Produce json
// @Success 200 {object} Enable2FAResponse "OTP URL and backup codes"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 409 {object} errors.ProblemDetail "2FA already enabled"
// @Router /auth/2fa/enable [post]
func (h *TwoFactorHandler) Enable(c fiber.Ctx) error {
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
	c fiber.Ctx,
	action func(userID uuid.UUID, code string) error,
	successMsg, auditAction string,
) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	var req TwoFactorCodeRequest

	if err := c.Bind().Body(&req); err != nil {
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
// @Summary Verify 2FA setup
// @Description Verify a TOTP code to complete 2FA setup
// @Tags 2FA
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body TwoFactorCodeRequest true "TOTP verification code"
// @Success 200 {object} MessageResponse "2FA enabled successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid code"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /auth/2fa/verify [post]
func (h *TwoFactorHandler) Verify(c fiber.Ctx) error {
	return h.handle2FAAction(c, h.authService.Verify2FA,
		"Two-factor authentication has been enabled successfully", service.Action2FAEnable)
}

// Disable disables 2FA after verifying a valid TOTP code
// @Summary Disable two-factor authentication
// @Description Disable 2FA after verifying a valid TOTP code
// @Tags 2FA
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body TwoFactorCodeRequest true "TOTP verification code"
// @Success 200 {object} MessageResponse "2FA disabled successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid code"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /auth/2fa/disable [post]
func (h *TwoFactorHandler) Disable(c fiber.Ctx) error {
	return h.handle2FAAction(c, h.authService.Disable2FA,
		"Two-factor authentication has been disabled", service.Action2FADisable)
}
