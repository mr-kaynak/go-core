package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// Middleware is the JWT authentication middleware
type Middleware struct {
	tokenService *service.TokenService
	skipPaths    []string
}

// New creates a new auth middleware
func New(tokenService *service.TokenService) *Middleware {
	return &Middleware{
		tokenService: tokenService,
		skipPaths: []string{
			"/api/v1/auth/register",
			"/api/v1/auth/login",
			"/api/v1/auth/refresh",
			"/api/v1/auth/verify-email",
			"/api/v1/auth/resend-verification",
			"/api/v1/auth/request-password-reset",
			"/api/v1/auth/reset-password",
			"/api/v1/auth/validate-reset-token",
			"/livez",
			"/readyz",
			"/metrics",
		},
	}
}

// Handle is the middleware handler function
func (m *Middleware) Handle(c *fiber.Ctx) error {
	// Check if the path should be skipped
	path := c.Path()
	for _, skipPath := range m.skipPaths {
		if strings.HasPrefix(path, skipPath) {
			return c.Next()
		}
	}

	// Get token from header
	token, err := m.getTokenFromHeader(c)
	if err != nil {
		return err
	}

	// Validate token
	claims, err := m.tokenService.ValidateAccessToken(token)
	if err != nil {
		return err
	}

	// Store claims in context
	c.Locals("claims", claims)
	c.Locals("userID", claims.UserID)
	c.Locals("username", claims.Username)
	c.Locals("email", claims.Email)
	c.Locals("roles", claims.Roles)
	c.Locals("permissions", claims.Permissions)

	return c.Next()
}

// getTokenFromHeader extracts the JWT token from the Authorization header
func (m *Middleware) getTokenFromHeader(c *fiber.Ctx) (string, error) {
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

// RequireRoles creates a middleware that requires specific roles
func RequireRoles(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Check if user has any of the required roles
		for _, requiredRole := range roles {
			for _, userRole := range claims.Roles {
				if userRole == requiredRole {
					return c.Next()
				}
			}
		}

		return errors.NewForbidden("Insufficient permissions")
	}
}

// RequireAuth is a middleware that requires authentication
func RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		_, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}
		return c.Next()
	}
}

// RequirePermissions creates a middleware that requires specific permissions
func RequirePermissions(permissions ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Check if user has any of the required permissions
		for _, requiredPerm := range permissions {
			for _, userPerm := range claims.Permissions {
				if userPerm == requiredPerm {
					return c.Next()
				}
			}
		}

		return errors.NewForbidden("Insufficient permissions")
	}
}
