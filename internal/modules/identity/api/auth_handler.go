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
	authService *service.AuthService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// RegisterRoutes registers auth routes
func (h *AuthHandler) RegisterRoutes(router fiber.Router) {
	auth := router.Group("/auth")

	auth.Post("/register", h.Register)
	auth.Post("/login", h.Login)
	auth.Post("/refresh", h.RefreshToken)
	auth.Post("/logout", h.Logout)
	auth.Get("/verify-email", h.VerifyEmail)
	auth.Post("/resend-verification", h.ResendVerificationEmail)
	auth.Post("/request-password-reset", h.RequestPasswordReset)
	auth.Post("/reset-password", h.ResetPassword)
	auth.Get("/validate-reset-token", h.ValidatePasswordResetToken)
}

// Register handles user registration
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
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req service.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	response, err := h.authService.Login(&req)
	if err != nil {
		return err
	}

	return c.JSON(response)
}

// RefreshToken handles token refresh
func (h *AuthHandler) RefreshToken(c *fiber.Ctx) error {
	var req struct {
		RefreshToken string `json:"refresh_token" validate:"required"`
	}

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
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	// Get user ID from context (set by auth middleware)
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	// Parse refresh token if provided
	_ = c.BodyParser(&req)

	// Extract access token from Authorization header for blacklisting
	accessToken, _ := GetTokenFromHeader(c)

	if err := h.authService.Logout(userID, req.RefreshToken, accessToken); err != nil {
		_ = err // Log error but don't fail the logout — the user wants to logout anyway
	}

	return c.JSON(fiber.Map{
		"message": "Logout successful",
	})
}

// VerifyEmail handles email verification via GET request with token in query
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
func (h *AuthHandler) ResendVerificationEmail(c *fiber.Ctx) error {
	var req struct {
		Email string `json:"email" validate:"required,email"`
	}

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
func (h *AuthHandler) RequestPasswordReset(c *fiber.Ctx) error {
	var req struct {
		Email string `json:"email" validate:"required,email"`
	}

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
func (h *AuthHandler) ResetPassword(c *fiber.Ctx) error {
	var req struct {
		Token       string `json:"token" validate:"required"`
		NewPassword string `json:"new_password" validate:"required,password"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.authService.ResetPassword(req.Token, req.NewPassword); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Password has been reset successfully",
	})
}

// ValidatePasswordResetToken validates a password reset token
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
