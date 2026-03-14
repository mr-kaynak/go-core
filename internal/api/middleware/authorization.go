package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	authmw "github.com/mr-kaynak/go-core/internal/middleware/auth"
)

// AuthorizationMiddleware creates an authorization middleware using Casbin.
// It skips public endpoints, enforces role-based policies, and falls back
// to own-resource access when the primary check denies the request.
func AuthorizationMiddleware(casbinService *authorization.CasbinService) fiber.Handler {
	return func(c fiber.Ctx) error {
		path := c.Path()

		// Skip authorization for public endpoints
		if isPublicEndpoint(path) {
			return c.Next()
		}

		// Get user ID from context (set by auth middleware)
		userID := fiber.Locals[uuid.UUID](c, "userID")
		if userID == uuid.Nil {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Get user roles from context
		roles := fiber.Locals[[]string](c, "roles")

		// Domain is derived server-side; never trust client-supplied X-Domain header
		domain := authorization.DomainDefault
		method := c.Method()

		// Map HTTP method to action
		action := mapHTTPMethodToAction(method)

		// Check authorization
		allowed, err := casbinService.EnforceWithRoles(userID, roles, domain, path, action)
		if err != nil {
			return errors.NewInternalError("Authorization check failed")
		}

		if !allowed {
			// Fallback: allow users to read their own resources (GET/HEAD/OPTIONS only)
			if isUserOwnResource(path, userID.String()) && action == authorization.ActionRead {
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

// RequirePermission creates a middleware that checks specific permission
func RequirePermission(
	casbinService *authorization.CasbinService,
	resource authorization.Resource, action authorization.Action,
) fiber.Handler {
	return func(c fiber.Ctx) error {
		// Get user ID from context
		userID := fiber.Locals[uuid.UUID](c, "userID")
		if userID == uuid.Nil {
			return errors.NewUnauthorized("User not authenticated")
		}

		// Domain is derived server-side; never trust client-supplied X-Domain header
		domain := authorization.DomainDefault

		// Check permission
		allowed, err := casbinService.EnforceUser(userID, domain, string(resource), action)
		if err != nil {
			return errors.NewInternalError("Permission check failed")
		}

		if !allowed {
			return errors.NewForbidden("Insufficient permissions")
		}

		return c.Next()
	}
}

// AdminRoles defines roles that bypass ownership checks. Extend this list
// when new admin-level roles are introduced.
var AdminRoles = []string{"admin", "system_admin"}

// RequireOwnership creates a middleware that checks resource ownership
func RequireOwnership(casbinService *authorization.CasbinService) fiber.Handler {
	return func(c fiber.Ctx) error {
		// Get user ID from context
		userID := fiber.Locals[uuid.UUID](c, "userID")
		if userID == uuid.Nil {
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
			roles := fiber.Locals[[]string](c, "roles")
			if !hasAdminRole(roles) {
				return errors.NewForbidden("Insufficient permissions")
			}
		}

		return c.Next()
	}
}

// hasAdminRole checks if any of the user's roles is in AdminRoles.
func hasAdminRole(userRoles []string) bool {
	for _, role := range userRoles {
		for _, admin := range AdminRoles {
			if role == admin {
				return true
			}
		}
	}
	return false
}

// mapHTTPMethodToAction maps HTTP methods to Casbin actions.
// OPTIONS and HEAD are treated as read; unknown methods are denied via ActionManage
// which requires explicit admin-level policy.
func mapHTTPMethodToAction(method string) authorization.Action {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS":
		return authorization.ActionRead
	case "POST":
		return authorization.ActionCreate
	case "PUT", "PATCH":
		return authorization.ActionUpdate
	case "DELETE":
		return authorization.ActionDelete
	default:
		return authorization.ActionManage
	}
}

// isPublicEndpoint checks if an endpoint is public using the single
// source of truth defined in the auth middleware package.
func isPublicEndpoint(path string) bool {
	for _, endpoint := range authmw.PublicPaths {
		if path == endpoint {
			return true
		}
	}
	return false
}

// isUserOwnResource checks if the resource belongs to the user using exact path segment matching.
func isUserOwnResource(path, userID string) bool {
	// Strip query string if present (c.Path() normally doesn't include it, but be defensive)
	if idx := strings.IndexByte(path, '?'); idx != -1 {
		path = path[:idx]
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")

	for i, seg := range segments {
		if seg == "users" && i+1 < len(segments) {
			next := segments[i+1]
			if next == "me" || next == userID {
				return true
			}
		}
		if seg == "profile" && i > 0 {
			// Only match if a preceding segment is the userID
			for _, prev := range segments[:i] {
				if prev == userID {
					return true
				}
			}
		}
	}

	return false
}
