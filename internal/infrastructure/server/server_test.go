package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestCacheControlHeadersPresent(t *testing.T) {
	app := fiber.New()

	// Register only the cache-control middleware (same as in setupMiddleware)
	app.Use(func(c fiber.Ctx) error {
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Set("Pragma", "no-cache")
		return c.Next()
	})

	app.Get("/api/v1/test", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store, no-cache, must-revalidate" {
		t.Fatalf("expected Cache-Control header 'no-store, no-cache, must-revalidate', got %q", cc)
	}

	pragma := resp.Header.Get("Pragma")
	if pragma != "no-cache" {
		t.Fatalf("expected Pragma header 'no-cache', got %q", pragma)
	}
}
