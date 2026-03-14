package api

import (
	"testing"

	"github.com/gofiber/fiber/v3"
)

// ---------------------------------------------------------------------------
// splitComma tests
// ---------------------------------------------------------------------------

func TestSplitComma_Empty(t *testing.T) {
	result := splitComma("")
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestSplitComma_SingleValue(t *testing.T) {
	result := splitComma("go")
	if len(result) != 1 || result[0] != "go" {
		t.Fatalf("expected [go], got %v", result)
	}
}

func TestSplitComma_MultipleValues(t *testing.T) {
	result := splitComma("go,rust,python")
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0] != "go" || result[1] != "rust" || result[2] != "python" {
		t.Fatalf("unexpected values: %v", result)
	}
}

func TestSplitComma_WithSpaces(t *testing.T) {
	result := splitComma("go , rust , python")
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0] != "go" || result[1] != "rust" || result[2] != "python" {
		t.Fatalf("unexpected values (spaces not trimmed): %v", result)
	}
}

func TestSplitComma_TrailingComma(t *testing.T) {
	result := splitComma("go,rust,")
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(result), result)
	}
}

// ---------------------------------------------------------------------------
// validateSortParams tests
// ---------------------------------------------------------------------------

func TestValidateSortParams_ValidFields(t *testing.T) {
	for _, field := range []string{"created_at", "updated_at", "published_at", "title"} {
		if err := validateSortParams(field, "asc"); err != nil {
			t.Fatalf("expected no error for field %q, got %v", field, err)
		}
	}
}

func TestValidateSortParams_ValidOrders(t *testing.T) {
	for _, order := range []string{"asc", "desc"} {
		if err := validateSortParams("created_at", order); err != nil {
			t.Fatalf("expected no error for order %q, got %v", order, err)
		}
	}
}

func TestValidateSortParams_EmptyIsValid(t *testing.T) {
	if err := validateSortParams("", ""); err != nil {
		t.Fatalf("expected no error for empty params, got %v", err)
	}
}

func TestValidateSortParams_InvalidField(t *testing.T) {
	if err := validateSortParams("invalid_field", "asc"); err == nil {
		t.Fatal("expected error for invalid sort field")
	}
}

func TestValidateSortParams_InvalidOrder(t *testing.T) {
	if err := validateSortParams("created_at", "random"); err == nil {
		t.Fatal("expected error for invalid order")
	}
}

// ---------------------------------------------------------------------------
// isAdmin helper tests
// ---------------------------------------------------------------------------

func TestIsAdmin_AdminRole(t *testing.T) {
	app := newTestApp()
	var result bool

	app.Get("/test", func(c fiber.Ctx) error {
		c.Locals("roles", []string{"admin"})
		result = isAdmin(c)
		return c.SendStatus(200)
	})

	doReq(t, app, "GET", "/test", "")
	if !result {
		t.Fatal("expected isAdmin to return true for admin role")
	}
}

func TestIsAdmin_SystemAdminRole(t *testing.T) {
	app := newTestApp()
	var result bool

	app.Get("/test", func(c fiber.Ctx) error {
		c.Locals("roles", []string{"system_admin"})
		result = isAdmin(c)
		return c.SendStatus(200)
	})

	doReq(t, app, "GET", "/test", "")
	if !result {
		t.Fatal("expected isAdmin to return true for system_admin role")
	}
}

func TestIsAdmin_UserRole(t *testing.T) {
	app := newTestApp()
	var result bool

	app.Get("/test", func(c fiber.Ctx) error {
		c.Locals("roles", []string{"user"})
		result = isAdmin(c)
		return c.SendStatus(200)
	})

	doReq(t, app, "GET", "/test", "")
	if result {
		t.Fatal("expected isAdmin to return false for user role")
	}
}

func TestIsAdmin_NoRoles(t *testing.T) {
	app := newTestApp()
	var result bool

	app.Get("/test", func(c fiber.Ctx) error {
		result = isAdmin(c)
		return c.SendStatus(200)
	})

	doReq(t, app, "GET", "/test", "")
	if result {
		t.Fatal("expected isAdmin to return false when no roles")
	}
}
