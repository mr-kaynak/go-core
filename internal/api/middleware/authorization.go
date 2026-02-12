package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
)

// AuthorizationMiddleware creates an authorization middleware using Casbin
func AuthorizationMiddleware(casbinService *authorization.CasbinService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get user ID from context (set by auth middleware)
		userID, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Get user roles from context
		roles, _ := c.Locals("roles").([]string)

		// Get request details
		domain := c.Get("X-Domain", authorization.DomainDefault)
		path := c.Path()
		method := c.Method()

		// Map HTTP method to action
		action := mapHTTPMethodToAction(method)

		// Check authorization
		allowed, err := casbinService.EnforceWithRoles(userID, roles, domain, path, action)
		if err != nil {
			return errors.NewInternal("Authorization check failed")
		}

		if !allowed {
			return errors.NewForbidden("Insufficient permissions")
		}

		return c.Next()
	}
}

// RequireRole creates a middleware that requires specific roles
func RequireRole(casbinService *authorization.CasbinService, requiredRoles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get user roles from context
		userRoles, ok := c.Locals("roles").([]string)
		if !ok {
			return errors.NewUnauthorized("User roles not found")
		}

		// Check if user has at least one required role
		hasRole := false
		for _, requiredRole := range requiredRoles {
			for _, userRole := range userRoles {
				if userRole == requiredRole {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}

		if !hasRole {
			return errors.NewForbidden("Required role not found")
		}

		return c.Next()
	}
}

// RequirePermission creates a middleware that checks specific permission
func RequirePermission(
	casbinService *authorization.CasbinService,
	resource authorization.Resource, action authorization.Action,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get user ID from context
		userID, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Get domain from header or use default
		domain := c.Get("X-Domain", authorization.DomainDefault)

		// Check permission
		allowed, err := casbinService.EnforceUser(userID, domain, string(resource), action)
		if err != nil {
			return errors.NewInternal("Permission check failed")
		}

		if !allowed {
			return errors.NewForbidden("Permission denied")
		}

		return c.Next()
	}
}

// RequireOwnership creates a middleware that checks resource ownership
func RequireOwnership(casbinService *authorization.CasbinService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get user ID from context
		userID, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Get resource owner ID from params or body
		resourceOwnerID := c.Params("user_id")
		if resourceOwnerID == "" {
			// Try to get from path for /users/me endpoint
			if strings.Contains(c.Path(), "/me") {
				resourceOwnerID = userID.String()
			}
		}

		// Check if user is the owner or has admin role
		if resourceOwnerID != userID.String() {
			// Check if user has admin privileges
			roles, _ := c.Locals("roles").([]string)
			isAdmin := false
			for _, role := range roles {
				if role == "admin" || role == "system_admin" {
					isAdmin = true
					break
				}
			}

			if !isAdmin {
				return errors.NewForbidden("Access to resource denied")
			}
		}

		return c.Next()
	}
}

// DynamicAuthorization creates a middleware with dynamic permission checking
func DynamicAuthorization(casbinService *authorization.CasbinService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get user ID from context
		userID, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			// Check for API key authentication
			apiKey := c.Get("X-API-Key")
			if apiKey != "" {
				// API key authorization logic
				domain := authorization.DomainDefault
				path := c.Path()
				method := c.Method()
				action := mapHTTPMethodToAction(method)

				// Check API client permissions
				allowed, err := casbinService.Enforce("role:api_client", domain, path, action)
				if err != nil || !allowed {
					return errors.NewForbidden("API key insufficient permissions")
				}

				return c.Next()
			}

			return errors.NewUnauthorized("Authentication required")
		}

		// Get user roles
		roles, _ := c.Locals("roles").([]string)

		// Get request details
		domain := c.Get("X-Domain", authorization.DomainDefault)
		path := c.Path()
		method := c.Method()

		// Skip authorization for public endpoints
		if isPublicEndpoint(path) {
			return c.Next()
		}

		// Map HTTP method to action
		action := mapHTTPMethodToAction(method)

		// Check authorization with roles
		allowed, err := casbinService.EnforceWithRoles(userID, roles, domain, path, action)
		if err != nil {
			return errors.NewInternal("Authorization check failed")
		}

		if !allowed {
			// Check for user's own resource access
			if isUserOwnResource(path, userID.String()) {
				allowed, err = casbinService.EnforceUser(userID, domain, path, authorization.ActionRead)
				if err == nil && allowed {
					return c.Next()
				}
			}

			return errors.NewForbidden("Insufficient permissions")
		}

		return c.Next()
	}
}

// mapHTTPMethodToAction maps HTTP methods to Casbin actions
func mapHTTPMethodToAction(method string) authorization.Action {
	switch strings.ToUpper(method) {
	case "GET":
		return authorization.ActionRead
	case "POST":
		return authorization.ActionCreate
	case "PUT", "PATCH":
		return authorization.ActionUpdate
	case "DELETE":
		return authorization.ActionDelete
	default:
		return authorization.ActionRead
	}
}

// isPublicEndpoint checks if an endpoint is public
func isPublicEndpoint(path string) bool {
	publicEndpoints := []string{
		"/api/health",
		"/api/metrics",
		"/api/auth/login",
		"/api/auth/register",
		"/api/auth/refresh",
		"/api/auth/verify-email",
		"/api/auth/request-password-reset",
		"/api/auth/reset-password",
		"/api/auth/validate-reset-token",
	}

	for _, endpoint := range publicEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	return false
}

// isUserOwnResource checks if the resource belongs to the user
func isUserOwnResource(path, userID string) bool {
	// Check patterns like /api/users/{id} or /api/users/me
	if strings.Contains(path, "/users/me") {
		return true
	}

	if strings.Contains(path, "/users/"+userID) {
		return true
	}

	if strings.Contains(path, "/profile") && strings.Contains(path, userID) {
		return true
	}

	return false
}

// AdminOnly creates a middleware that only allows admin access
func AdminOnly(casbinService *authorization.CasbinService) fiber.Handler {
	return RequireRole(casbinService, "admin", "system_admin")
}

// ManagerOnly creates a middleware that allows manager and above access
func ManagerOnly(casbinService *authorization.CasbinService) fiber.Handler {
	return RequireRole(casbinService, "manager", "admin", "system_admin")
}
