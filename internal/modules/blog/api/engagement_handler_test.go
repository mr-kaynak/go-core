package api

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// ---------------------------------------------------------------------------
// Engagement repository stub
// ---------------------------------------------------------------------------

// engagementRepoStub is defined here for engagement handler tests.
// It satisfies repository.EngagementRepository.

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newEngagementHandler() *EngagementHandler {
	cfg := &config.Config{
		Blog: config.BlogConfig{SiteURL: "https://example.com"},
	}
	postRepo := &postRepoStub{}
	engRepo := &engagementRepoStubForHandler{}
	engSvc := service.NewEngagementService(nil, cfg, engRepo, postRepo)
	engSvc.SetMetrics(metrics.NoOpMetrics{})
	return NewEngagementHandler(engSvc)
}

// ---------------------------------------------------------------------------
// EngagementHandler.ToggleLike tests
// ---------------------------------------------------------------------------

func TestEngagementHandler_ToggleLike_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()
	userID := uuid.New()

	app := newTestApp()
	app.Post("/posts/:id/like", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"user"})
		return c.Next()
	}, h.ToggleLike)

	resp := doReq(t, app, http.MethodPost, "/posts/not-a-uuid/like", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEngagementHandler_ToggleLike_NoAuth_ReturnsUnauthorized(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Post("/posts/:id/like", h.ToggleLike)

	resp := doReq(t, app, http.MethodPost, "/posts/"+uuid.New().String()+"/like", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// EngagementHandler.IsLiked tests
// ---------------------------------------------------------------------------

func TestEngagementHandler_IsLiked_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:id/like", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"user"})
		return c.Next()
	}, h.IsLiked)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/like", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEngagementHandler_IsLiked_NoAuth_ReturnsUnauthorized(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Get("/posts/:id/like", h.IsLiked)

	resp := doReq(t, app, http.MethodGet, "/posts/"+uuid.New().String()+"/like", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// EngagementHandler.RecordView tests
// ---------------------------------------------------------------------------

func TestEngagementHandler_RecordView_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Post("/posts/:id/view", h.RecordView)

	resp := doReq(t, app, http.MethodPost, "/posts/not-a-uuid/view", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// EngagementHandler.RecordShare tests
// ---------------------------------------------------------------------------

func TestEngagementHandler_RecordShare_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Post("/posts/:id/share", h.RecordShare)

	resp := doReq(t, app, http.MethodPost, "/posts/not-a-uuid/share", `{"platform":"twitter"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEngagementHandler_RecordShare_InvalidBody_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Post("/posts/:id/share", h.RecordShare)

	resp := doReq(t, app, http.MethodPost, "/posts/"+uuid.New().String()+"/share", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEngagementHandler_RecordShare_MissingPlatform_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Post("/posts/:id/share", h.RecordShare)

	resp := doReq(t, app, http.MethodPost, "/posts/"+uuid.New().String()+"/share", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// EngagementHandler.GetStats tests
// ---------------------------------------------------------------------------

func TestEngagementHandler_GetStats_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	app.Get("/posts/:id/stats", h.GetStats)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/stats", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// EngagementHandler.RegisterRoutes tests
// ---------------------------------------------------------------------------

func TestEngagementHandler_RegisterRoutes(t *testing.T) {
	h := newEngagementHandler()

	app := newTestApp()
	blog := app.Group("/blog")
	authMw := func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	}
	h.RegisterRoutes(blog, authMw)

	// Verify public route is registered
	resp := doReq(t, app, http.MethodGet, "/blog/posts/"+uuid.New().String()+"/stats", "")
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected route to be registered, got 404")
	}
}
