package helpers

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
)

// GetUserIDFromCtx extracts user ID from fiber context (returns nil if not authenticated).
func GetUserIDFromCtx(c fiber.Ctx) *uuid.UUID {
	id := fiber.Locals[uuid.UUID](c, "userID")
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// IsAdmin checks if the authenticated user has admin or system_admin role.
func IsAdmin(c fiber.Ctx) bool {
	roles := fiber.Locals[[]string](c, "roles")
	if roles == nil {
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

// ParseUUIDParam parses the named path parameter (c.Params(param)) as a UUID.
// On failure it returns uuid.Nil and a *errors.ProblemDetail (via
// errors.NewBadRequest(message)) using the caller-supplied message verbatim,
// so callers keep full control over the wire-visible error text.
func ParseUUIDParam(c fiber.Ctx, param, message string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(param))
	if err != nil {
		return uuid.Nil, errors.NewBadRequest(message)
	}
	return id, nil
}

// ParsePagination parses the "page" and "limit" query parameters from the
// request, clamping limit via response.SanitizeLimit(limit, defaultLimit) and
// ensuring page is never less than 1. It returns the parsed page and limit
// alongside the computed offset ((page-1)*limit), mirroring the shape of the
// parsePagination helper duplicated across handlers (e.g.
// admin_handler.go).
func ParsePagination(c fiber.Ctx, defaultLimit int) (page, limit, offset int) {
	page = fiber.Query[int](c, "page", 1)
	limit = response.SanitizeLimit(fiber.Query[int](c, "limit", defaultLimit), defaultLimit)
	if page < 1 {
		page = 1
	}
	offset = (page - 1) * limit
	return page, limit, offset
}

// ValidateSort validates a sort field against an allow-list and a sort order
// against "asc"/"desc". Empty values are accepted as "no preference" (the
// caller is expected to apply its own default). It mirrors the behavior of
// validateSortParams in post_handler.go: an unknown sortBy or an order other
// than "asc"/"desc" returns a *errors.ProblemDetail (via errors.NewBadRequest)
// rather than silently falling back to a default. allowed keys are matched
// case-sensitively, consistent with the existing implementation.
func ValidateSort(sortBy, order string, allowed map[string]bool) (string, string, error) {
	if sortBy != "" && !allowed[sortBy] {
		return "", "", errors.NewBadRequest("Invalid sort_by field")
	}
	if order != "" && order != "asc" && order != "desc" {
		return "", "", errors.NewBadRequest("Invalid order: must be asc or desc")
	}
	return sortBy, order, nil
}
