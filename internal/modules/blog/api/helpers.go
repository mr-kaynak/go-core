package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/api/helpers"
)

// getUserIDFromCtx extracts user ID from fiber context (returns nil if not authenticated)
func getUserIDFromCtx(c fiber.Ctx) *uuid.UUID {
	return helpers.GetUserIDFromCtx(c)
}

// requireUserID extracts user ID from fiber context and returns nil if not present
func requireUserID(c fiber.Ctx) *uuid.UUID {
	return helpers.GetUserIDFromCtx(c)
}

// isAdmin checks if the authenticated user has admin or system_admin role
func isAdmin(c fiber.Ctx) bool {
	return helpers.IsAdmin(c)
}

// splitComma splits a comma-separated string into a slice
func splitComma(s string) []string {
	return helpers.SplitComma(s)
}
