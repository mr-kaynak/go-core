package auth

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// Middleware is the authentication middleware supporting JWT and API key auth
type Middleware struct {
	tokenService  *service.TokenService
	apiKeyService *service.APIKeyService
	userRepo      repository.UserRepository
	skipPaths     []string
}

// PublicPaths is the single source of truth for endpoints that bypass authentication
// and authorization. Both auth middleware and authorization middleware reference this list.
var PublicPaths = []string{
	"/api/v1/auth/register",
	"/api/v1/auth/login",
	"/api/v1/auth/2fa/validate",
	"/api/v1/auth/refresh",
	"/api/v1/auth/verify-email",
	"/api/v1/auth/resend-verification",
	"/api/v1/auth/request-password-reset",
	"/api/v1/auth/reset-password",
	"/api/v1/auth/validate-reset-token",
	"/livez",
	"/readyz",
	"/metrics",
}

// New creates a new auth middleware
func New(tokenService *service.TokenService, apiKeyService *service.APIKeyService, userRepo repository.UserRepository) *Middleware {
	return &Middleware{
		tokenService:  tokenService,
		apiKeyService: apiKeyService,
		userRepo:      userRepo,
		skipPaths:     PublicPaths,
	}
}

// OptionalHandle is like Handle but does not fail when no credentials are provided.
// If a token/API key is present it will be validated (invalid credentials still return 401).
// If no credentials are present the request continues unauthenticated.
func (m *Middleware) OptionalHandle(c fiber.Ctx) error {
	hasAuthHeader := c.Get("Authorization") != ""
	hasAPIKey := c.Get("X-API-Key") != ""

	if !hasAuthHeader && !hasAPIKey {
		return c.Next()
	}

	return m.Handle(c)
}

// Handle is the middleware handler function
func (m *Middleware) Handle(c fiber.Ctx) error {
	// Check if the path should be skipped
	path := c.Path()
	for _, skipPath := range m.skipPaths {
		if path == skipPath {
			return c.Next()
		}
	}

	// Determine which auth method the client is using
	hasAuthHeader := c.Get("Authorization") != ""
	hasAPIKey := c.Get("X-API-Key") != ""

	// If Authorization header is present, enforce JWT — do not fall through to API key
	if hasAuthHeader {
		token, err := m.getTokenFromHeader(c)
		if err != nil {
			return err
		}
		claims, err := m.tokenService.ValidateAccessToken(token)
		if err != nil {
			return err
		}
		setClaimsLocals(c, claims, "jwt")
		return c.Next()
	}

	// API key authentication
	if hasAPIKey {
		if m.apiKeyService == nil {
			return errors.NewUnauthorized("API key authentication not available")
		}

		key, err := m.apiKeyService.Validate(c.Get("X-API-Key"))
		if err != nil {
			return err
		}

		// Build Claims from API key roles
		claims := &service.Claims{
			UserID:      key.UserID,
			Roles:       key.GetRoleNames(),
			Permissions: key.GetPermissionNames(),
		}

		// Enrich claims with user info if repository is available
		if m.userRepo != nil {
			if user, err := m.userRepo.GetByID(key.UserID); err == nil {
				claims.Username = user.Username
				claims.Email = user.Email
			}
		}

		setClaimsLocals(c, claims, "api_key")
		c.Locals("apiKeyID", key.ID)
		return c.Next()
	}

	return errors.NewUnauthorized("User not authenticated")
}

// setClaimsLocals stores claims and auth method in fiber context locals
func setClaimsLocals(c fiber.Ctx, claims *service.Claims, authMethod string) {
	c.Locals("claims", claims)
	c.Locals("userID", claims.UserID)
	c.Locals("username", claims.Username)
	c.Locals("email", claims.Email)
	c.Locals("roles", claims.Roles)
	c.Locals("permissions", claims.Permissions)
	c.Locals("authMethod", authMethod)
}

// getTokenFromHeader extracts the JWT token from the Authorization header
func (m *Middleware) getTokenFromHeader(c fiber.Ctx) (string, error) {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return "", errors.NewUnauthorized("Invalid authorization header format")
	}

	// Check if the header starts with "Bearer "
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", errors.NewUnauthorized("Invalid authorization header format")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.NewUnauthorized("Invalid authorization header format")
	}

	return token, nil
}

// RequireRoles creates a middleware that requires specific roles
func RequireRoles(roles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
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
	return func(c fiber.Ctx) error {
		_, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}
		return c.Next()
	}
}

// RequirePermissions creates a middleware that requires specific permissions
func RequirePermissions(permissions ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
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

// GetAPIKeyID returns the API key ID from context if the request was authenticated via API key.
func GetAPIKeyID(c fiber.Ctx) (uuid.UUID, bool) {
	id, ok := c.Locals("apiKeyID").(uuid.UUID)
	return id, ok
}

// GetAuthMethod returns the authentication method used for the current request ("jwt" or "api_key").
func GetAuthMethod(c fiber.Ctx) string {
	if method, ok := c.Locals("authMethod").(string); ok {
		return method
	}
	return ""
}
