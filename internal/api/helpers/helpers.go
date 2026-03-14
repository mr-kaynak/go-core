package helpers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// GetUserIDFromCtx extracts user ID from fiber context (returns nil if not authenticated).
func GetUserIDFromCtx(c *fiber.Ctx) *uuid.UUID {
	if id, ok := c.Locals("userID").(uuid.UUID); ok {
		return &id
	}
	return nil
}

// IsAdmin checks if the authenticated user has admin or system_admin role.
func IsAdmin(c *fiber.Ctx) bool {
	roles, ok := c.Locals("roles").([]string)
	if !ok {
		return false
	}
	for _, r := range roles {
		if r == "admin" || r == "system_admin" {
			return true
		}
	}
	return false
}

// SplitComma splits a comma-separated string into a trimmed, non-empty slice.
func SplitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
