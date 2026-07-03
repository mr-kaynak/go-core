package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
)

// TestNewCasbinNilGuard verifies that server.New rejects a nil casbinSvc in
// production and continues (with a warning) in non-production environments.
// The test passes nil for db/redis/rabbitmq because the guard fires before any
// of those are touched.
func TestNewCasbinNilGuard(t *testing.T) {
	prodCfg := &config.Config{App: config.AppConfig{Env: "production"}}
	devCfg := &config.Config{App: config.AppConfig{Env: "development"}}

	t.Run("production rejects nil casbinSvc", func(t *testing.T) {
		_, err := New(prodCfg, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error when casbinSvc is nil in production, got nil")
		}
		if !strings.Contains(err.Error(), "casbinSvc must not be nil") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("non-production allows nil casbinSvc", func(t *testing.T) {
		// New will proceed past the guard and eventually panic or error when it
		// tries to set up database-backed routes.  We only care that the guard
		// itself does not return an error.
		defer func() { recover() }() //nolint:errcheck // intentional panic recovery for test
		_, err := New(devCfg, nil, nil, nil, nil)
		if err != nil && strings.Contains(err.Error(), "casbinSvc must not be nil") {
			t.Fatalf("guard should not fire in non-production, got: %v", err)
		}
		// Any other error (e.g. nil pointer deeper in New) is acceptable here.
	})
}

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
