package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	authService  *service.AuthService
	auditService *service.AuditService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// SetAuditService sets the optional audit service for logging security events.
func (h *AuthHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

const auditResourceUser = "user"

// RefreshTokenRequest represents a token refresh request
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// LogoutRequest represents a logout request
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// EmailRequest represents a request with just an email field
type EmailRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ResetPasswordRequest represents a password reset request
type ResetPasswordRequest struct {
	Token       string `json:"token" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,password"`
}

// audit is a nil-safe helper that logs an action if audit service is configured.
func (h *AuthHandler) audit(c *fiber.Ctx, userID *uuid.UUID, action, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		h.auditService.LogAction(userID, action, auditResourceUser, resourceID, c.IP(), c.Get("User-Agent"), meta)
	}
}

// RegisterRoutes registers auth routes
func (h *AuthHandler) RegisterRoutes(router fiber.Router, authMiddleware fiber.Handler) {
	auth := router.Group("/auth")

	// Public routes
	auth.Post("/register", h.Register)
	auth.Post("/login", h.Login)
	auth.Post("/refresh", h.RefreshToken)
	auth.Get("/verify-email", h.VerifyEmail)
	auth.Post("/resend-verification", h.ResendVerificationEmail)
	auth.Post("/request-password-reset", h.RequestPasswordReset)
	auth.Post("/reset-password", h.ResetPassword)
	auth.Get("/validate-reset-token", h.ValidatePasswordResetToken)

	// Protected routes
	auth.Post("/logout", authMiddleware, h.Logout)
}

// Register handles user registration
// @Summary Register a new user
// @Description Register a new user account with email verification
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body service.RegisterRequest true "Registration request"
// @Success 201 {object} fiber.Map "Registration successful"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 409 {object} errors.ProblemDetail "User already exists"
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req service.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	user, err := h.authService.Register(&req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Registration successful. Please verify your email.",
		"user":    user,
	})
}

// Login handles user login
// @Summary Login user
// @Description Authenticate user with email and password
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body service.LoginRequest true "Login credentials"
// @Success 200 {object} service.LoginResponse "Login successful"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Invalid credentials"
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req service.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	response, err := h.authService.Login(&req)
	if err != nil {
		h.audit(c, nil, service.ActionFailedLogin, "", map[string]interface{}{"email": req.Email})
		return err
	}

	h.audit(c, &response.User.ID, service.ActionLogin, response.User.ID.String(), nil)
	return c.JSON(response)
}

// RefreshToken handles token refresh
// @Summary Refresh access token
// @Description Get a new access token using a refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshTokenRequest true "Refresh token"
// @Success 200 {object} service.TokenPair "New token pair"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Invalid refresh token"
// @Router /auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *fiber.Ctx) error {
	var req RefreshTokenRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	tokenPair, err := h.authService.RefreshToken(req.RefreshToken)
	if err != nil {
		return err
	}

	return c.JSON(tokenPair)
}

// Logout handles user logout
// @Summary Logout user
// @Description Invalidate the current session and refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body LogoutRequest false "Optional refresh token to revoke"
// @Success 200 {object} fiber.Map "Logout successful"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /auth/logout [post]
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	// Get user ID from context (set by auth middleware)
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var req LogoutRequest

	// Parse refresh token if provided
	_ = c.BodyParser(&req)

	// Extract access token from Authorization header for blacklisting
	accessToken, _ := GetTokenFromHeader(c)

	if err := h.authService.Logout(userID, req.RefreshToken, accessToken); err != nil {
		_ = err // Log error but don't fail the logout — the user wants to logout anyway
	}

	h.audit(c, &userID, service.ActionLogout, userID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "Logout successful",
	})
}

// VerifyEmail handles email verification via GET request with token in query
// @Summary Verify email address
// @Description Verify user email with token from verification email
// @Tags Auth
// @Produce json
// @Param token query string true "Verification token"
// @Success 200 {object} fiber.Map "Email verified successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid or expired token"
// @Router /auth/verify-email [get]
func (h *AuthHandler) VerifyEmail(c *fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return errors.NewBadRequest("Verification token is required")
	}

	if err := h.authService.VerifyEmail(token); err != nil {
		return err
	}

	// You could redirect to a success page here
	return c.JSON(fiber.Map{
		"message": "Email verified successfully",
	})
}

// ResendVerificationEmail handles resending verification emails
// @Summary Resend verification email
// @Description Resend the email verification link
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body EmailRequest true "Email address"
// @Success 200 {object} fiber.Map "Verification email sent"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Router /auth/resend-verification [post]
func (h *AuthHandler) ResendVerificationEmail(c *fiber.Ctx) error {
	var req EmailRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.authService.ResendVerificationEmail(req.Email); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Verification email sent if the account exists",
	})
}

// RequestPasswordReset handles password reset requests
// @Summary Request password reset
// @Description Send a password reset email
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body EmailRequest true "Email address"
// @Success 200 {object} fiber.Map "Reset email sent"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Router /auth/request-password-reset [post]
func (h *AuthHandler) RequestPasswordReset(c *fiber.Ctx) error {
	var req EmailRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.authService.RequestPasswordReset(req.Email); err != nil {
		return err
	}

	// Always return success to prevent email enumeration
	return c.JSON(fiber.Map{
		"message": "If an account exists with this email, a password reset link has been sent",
	})
}

// ResetPassword handles password reset with token
// @Summary Reset password
// @Description Reset password using a reset token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ResetPasswordRequest true "Reset token and new password"
// @Success 200 {object} fiber.Map "Password reset successful"
// @Failure 400 {object} errors.ProblemDetail "Invalid or expired token"
// @Router /auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c *fiber.Ctx) error {
	var req ResetPasswordRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.authService.ResetPassword(req.Token, req.NewPassword); err != nil {
		return err
	}

	h.audit(c, nil, service.ActionPasswordChange, "", map[string]interface{}{"method": "reset"})
	return c.JSON(fiber.Map{
		"message": "Password has been reset successfully",
	})
}

// ValidatePasswordResetToken validates a password reset token
// @Summary Validate password reset token
// @Description Check if a password reset token is valid
// @Tags Auth
// @Produce json
// @Param token query string true "Password reset token"
// @Success 200 {object} fiber.Map "Token is valid"
// @Failure 400 {object} errors.ProblemDetail "Invalid or expired token"
// @Router /auth/validate-reset-token [get]
func (h *AuthHandler) ValidatePasswordResetToken(c *fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return errors.NewBadRequest("Password reset token is required")
	}

	if err := h.authService.ValidatePasswordResetToken(token); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Password reset token is valid",
		"valid":   true,
	})
}

// GetUserFromContext extracts the authenticated user from the context
func GetUserFromContext(c *fiber.Ctx) (*service.Claims, error) {
	claims, ok := c.Locals("claims").(*service.Claims)
	if !ok {
		return nil, errors.NewUnauthorized("User not authenticated")
	}
	return claims, nil
}

// GetTokenFromHeader extracts the JWT token from the Authorization header
func GetTokenFromHeader(c *fiber.Ctx) (string, error) {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return "", errors.NewUnauthorized("Authorization header missing")
	}

	// Check if the header starts with "Bearer "
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", errors.NewUnauthorized("Invalid authorization header format")
	}

	return parts[1], nil
}
