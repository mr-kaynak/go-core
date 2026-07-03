package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"gorm.io/gorm"
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

// newTestDB builds an in-memory SQLite database.DB suitable for integration
// tests that exercise server.New without a real PostgreSQL instance.
func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return &database.DB{DB: gdb}
}

// newIntegrationConfig returns a minimal development config that satisfies
// all server.New code paths without requiring real infrastructure.
func newIntegrationConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Name:      "go-core-test",
			Env:       "test",
			Version:   "0.0.1",
			Debug:     false,
			BodyLimit: 4 * 1024 * 1024,
		},
		JWT: config.JWTConfig{
			Secret:        "test-secret-key-minimum-32-chars-ok",
			RefreshSecret: "test-refresh-secret-32-chars-ok!",
			Issuer:        "go-core-test",
		},
		Security: config.SecurityConfig{
			BCryptCost:    4,
			EncryptionKey: "test-encryption-key-min-32-chars!!",
		},
		RateLimit: config.RateLimitConfig{
			PerMinute: 60,
		},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
		Email: config.EmailConfig{
			SMTPHost:  "localhost",
			SMTPPort:  1025,
			FromEmail: "noreply@example.com",
		},
		Metrics: config.MetricsConfig{
			Port: 9091,
		},
		Blog: config.BlogConfig{
			PostsPerPage: 20,
			ReadTimeWPM:  200,
		},
	}
}

// TestNewIntegration_SecurityHeaders exercises the full server.New wiring path
// (real middleware stack, real route registration) using an in-memory SQLite
// database in place of PostgreSQL.  It hits GET /api/v1/ — the public API
// status endpoint registered after the middleware chain — and verifies:
//   - HTTP 200 response
//   - Cache-Control: no-store header (set by cache middleware)
//   - X-Request-Id header (set by requestid middleware)
//   - X-Content-Type-Options: nosniff (set by helmet security middleware)
//
// It also hits GET /livez (registered before middleware, used by k8s probes)
// and verifies it returns 200 without requiring any DB access.
//
// Together these two sub-tests catch regressions in server.go's middleware
// order and route registration that the existing unit tests (which build their
// own local Fiber app) cannot detect.
func TestNewIntegration_SecurityHeaders(t *testing.T) {
	cfg := newIntegrationConfig()
	db := newTestDB(t)

	srv, err := New(cfg, db, nil, nil, nil)
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.StopNotifications(ctx)
		srv.StopSSE(ctx)
		_ = srv.Shutdown()
	})

	// /api/v1/ goes through the full middleware stack and returns JSON status.
	t.Run("api_status_has_security_headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/", nil)
		resp, err := srv.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatalf("request to /api/v1/ failed: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from /api/v1/, got %d", resp.StatusCode)
		}

		// Cache-Control — set by the first middleware in setupMiddleware.
		cc := resp.Header.Get("Cache-Control")
		if !strings.Contains(cc, "no-store") {
			t.Errorf("expected Cache-Control to contain 'no-store', got %q", cc)
		}

		// X-Request-Id — set by the requestid middleware wired in setupMiddleware.
		if resp.Header.Get("X-Request-Id") == "" {
			t.Errorf("expected X-Request-Id header to be present, got empty string")
		}

		// X-Content-Type-Options — set by the helmet security middleware.
		xcto := resp.Header.Get("X-Content-Type-Options")
		if xcto != "nosniff" {
			t.Errorf("expected X-Content-Type-Options: nosniff, got %q", xcto)
		}
	})

	// /livez is registered before middleware (k8s liveness probe) and must
	// always return 200 regardless of middleware state.
	t.Run("livez_returns_200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/livez", nil)
		resp, err := srv.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatalf("request to /livez failed: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from /livez, got %d", resp.StatusCode)
		}
	})
}
