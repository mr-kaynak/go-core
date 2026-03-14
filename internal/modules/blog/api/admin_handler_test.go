package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Comment repository stub for admin tests
// ---------------------------------------------------------------------------

type commentRepoStub struct {
	listPendingFn func(offset, limit int) ([]*domain.Comment, int64, error)
	getByIDFn     func(id uuid.UUID) (*domain.Comment, error)
	updateFn      func(comment *domain.Comment) error
}

var _ repository.CommentRepository = (*commentRepoStub)(nil)

func (s *commentRepoStub) WithTx(_ *gorm.DB) repository.CommentRepository { return s }
func (s *commentRepoStub) Create(_ *domain.Comment) error                 { return nil }
func (s *commentRepoStub) Update(c *domain.Comment) error {
	if s.updateFn != nil {
		return s.updateFn(c)
	}
	return nil
}
func (s *commentRepoStub) Delete(_ uuid.UUID) error { return nil }
func (s *commentRepoStub) GetByID(id uuid.UUID) (*domain.Comment, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, gorm.ErrRecordNotFound
}
func (s *commentRepoStub) GetThreaded(_ uuid.UUID) ([]*domain.Comment, error) {
	return nil, nil
}
func (s *commentRepoStub) CountByPost(_ uuid.UUID) (int64, error) { return 0, nil }
func (s *commentRepoStub) ListPending(offset, limit int) ([]*domain.Comment, int64, error) {
	if s.listPendingFn != nil {
		return s.listPendingFn(offset, limit)
	}
	return []*domain.Comment{}, 0, nil
}

// ---------------------------------------------------------------------------
// Settings repository stub for admin tests
// ---------------------------------------------------------------------------

type settingsRepoStub struct {
	getFn    func() (*domain.BlogSettings, error)
	upsertFn func(settings *domain.BlogSettings) error
}

var _ repository.SettingsRepository = (*settingsRepoStub)(nil)

func (s *settingsRepoStub) WithTx(_ *gorm.DB) repository.SettingsRepository { return s }
func (s *settingsRepoStub) Get() (*domain.BlogSettings, error) {
	if s.getFn != nil {
		return s.getFn()
	}
	return &domain.BlogSettings{
		AutoApproveComments: true,
		PostsPerPage:        20,
		ViewCooldownMinutes: 30,
		FeedItemLimit:       50,
		ReadTimeWPM:         200,
	}, nil
}
func (s *settingsRepoStub) Upsert(settings *domain.BlogSettings) error {
	if s.upsertFn != nil {
		return s.upsertFn(settings)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers for admin tests
// ---------------------------------------------------------------------------

func newAdminTestCfg() *config.Config {
	return &config.Config{
		Blog: config.BlogConfig{
			SiteURL:             "https://example.com",
			AutoApproveComments: true,
			PostsPerPage:        20,
			FeedItemLimit:       50,
			ReadTimeWPM:         200,
		},
	}
}

func newAdminHandler(
	postRepo repository.PostRepository,
	commentRepo repository.CommentRepository,
	settingsRepo repository.SettingsRepository,
) *AdminHandler {
	cfg := newAdminTestCfg()
	postSvc := newPostService(postRepo)
	commentSvc := service.NewCommentService(cfg, commentRepo, postRepo)
	settingsSvc := service.NewSettingsService(cfg, settingsRepo)
	return NewAdminHandler(postSvc, commentSvc, nil, settingsSvc, postRepo, 10)
}

func adminAuthMw() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := uuid.New()
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.ListAll tests
// ---------------------------------------------------------------------------

func TestAdminHandler_ListAll_ReturnsOK(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/posts", h.ListAll)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_ListAll_PaginationNormalized(t *testing.T) {
	var gotOffset, gotLimit int
	postRepo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			gotOffset = filter.Offset
			gotLimit = filter.Limit
			return []*domain.Post{}, 0, nil
		},
	}
	h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/posts", h.ListAll)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts?page=-1&limit=999", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if gotOffset != 0 {
		t.Fatalf("expected offset 0, got %d", gotOffset)
	}
	// limit out of range => falls back to postsPerPage=10
	if gotLimit != 10 {
		t.Fatalf("expected limit 10, got %d", gotLimit)
	}
}

func TestAdminHandler_ListAll_InvalidStatus_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/posts", h.ListAll)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts?status=invalid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_ListAll_ValidStatusFilters(t *testing.T) {
	for _, status := range []string{"draft", "published", "archived"} {
		t.Run(status, func(t *testing.T) {
			var capturedStatus string
			postRepo := &postRepoStub{
				listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
					capturedStatus = filter.Status
					return []*domain.Post{}, 0, nil
				},
			}
			h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

			app := newTestApp()
			app.Get("/admin/blog/posts", h.ListAll)

			resp := doReq(t, app, http.MethodGet, "/admin/blog/posts?status="+status, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			if capturedStatus != status {
				t.Fatalf("expected status %q, got %q", status, capturedStatus)
			}
		})
	}
}

func TestAdminHandler_ListAll_InvalidSortBy_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/posts", h.ListAll)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts?sort_by=invalid_field", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_ListAll_InvalidOrder_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/posts", h.ListAll)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts?order=invalid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_ListAll_WithAuthorID(t *testing.T) {
	authorID := uuid.New()
	var capturedAuthorID *uuid.UUID
	postRepo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			capturedAuthorID = filter.AuthorID
			return []*domain.Post{}, 0, nil
		},
	}
	h := newAdminHandler(postRepo, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/posts", h.ListAll)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts?author_id="+authorID.String(), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedAuthorID == nil || *capturedAuthorID != authorID {
		t.Fatalf("expected author_id to be captured")
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.PendingComments tests
// ---------------------------------------------------------------------------

func TestAdminHandler_PendingComments_ReturnsOK(t *testing.T) {
	commentRepo := &commentRepoStub{
		listPendingFn: func(offset, limit int) ([]*domain.Comment, int64, error) {
			return []*domain.Comment{}, 0, nil
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/comments/pending", h.PendingComments)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/comments/pending", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_PendingComments_PaginationNormalized(t *testing.T) {
	var gotOffset, gotLimit int
	commentRepo := &commentRepoStub{
		listPendingFn: func(offset, limit int) ([]*domain.Comment, int64, error) {
			gotOffset = offset
			gotLimit = limit
			return []*domain.Comment{}, 0, nil
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/comments/pending", h.PendingComments)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/comments/pending?page=-3&limit=999", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if gotOffset != 0 {
		t.Fatalf("expected offset 0, got %d", gotOffset)
	}
	if gotLimit != 20 {
		t.Fatalf("expected limit 20, got %d", gotLimit)
	}
}

func TestAdminHandler_PendingComments_WithResults(t *testing.T) {
	now := time.Now()
	commentRepo := &commentRepoStub{
		listPendingFn: func(offset, limit int) ([]*domain.Comment, int64, error) {
			return []*domain.Comment{
				{
					ID:        uuid.New(),
					PostID:    uuid.New(),
					Content:   "Test comment",
					Status:    domain.CommentStatusPending,
					CreatedAt: now,
				},
			}, 1, nil
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/comments/pending", h.PendingComments)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/comments/pending", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.ApproveComment tests
// ---------------------------------------------------------------------------

func TestAdminHandler_ApproveComment_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Post("/admin/blog/comments/:id/approve", h.ApproveComment)

	resp := doReq(t, app, http.MethodPost, "/admin/blog/comments/not-a-uuid/approve", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_ApproveComment_NotFound_Returns404(t *testing.T) {
	commentRepo := &commentRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Comment, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Post("/admin/blog/comments/:id/approve", h.ApproveComment)

	resp := doReq(t, app, http.MethodPost, "/admin/blog/comments/"+uuid.New().String()+"/approve", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_ApproveComment_Success(t *testing.T) {
	commentID := uuid.New()
	commentRepo := &commentRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Comment, error) {
			return &domain.Comment{
				ID:     commentID,
				PostID: uuid.New(),
				Status: domain.CommentStatusPending,
			}, nil
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Post("/admin/blog/comments/:id/approve", h.ApproveComment)

	resp := doReq(t, app, http.MethodPost, "/admin/blog/comments/"+commentID.String()+"/approve", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.RejectComment tests
// ---------------------------------------------------------------------------

func TestAdminHandler_RejectComment_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Post("/admin/blog/comments/:id/reject", h.RejectComment)

	resp := doReq(t, app, http.MethodPost, "/admin/blog/comments/not-a-uuid/reject", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_RejectComment_NotFound_Returns404(t *testing.T) {
	commentRepo := &commentRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Comment, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Post("/admin/blog/comments/:id/reject", h.RejectComment)

	resp := doReq(t, app, http.MethodPost, "/admin/blog/comments/"+uuid.New().String()+"/reject", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_RejectComment_Success(t *testing.T) {
	commentID := uuid.New()
	commentRepo := &commentRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Comment, error) {
			return &domain.Comment{
				ID:     commentID,
				PostID: uuid.New(),
				Status: domain.CommentStatusPending,
			}, nil
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Post("/admin/blog/comments/:id/reject", h.RejectComment)

	resp := doReq(t, app, http.MethodPost, "/admin/blog/comments/"+commentID.String()+"/reject", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.DashboardStats tests
// ---------------------------------------------------------------------------

func TestAdminHandler_DashboardStats_ReturnsOK(t *testing.T) {
	commentRepo := &commentRepoStub{
		listPendingFn: func(offset, limit int) ([]*domain.Comment, int64, error) {
			return []*domain.Comment{}, 0, nil
		},
	}
	h := newAdminHandler(&postRepoStub{}, commentRepo, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/stats", h.DashboardStats)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.GetSettings tests
// ---------------------------------------------------------------------------

func TestAdminHandler_GetSettings_ReturnsOK(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Get("/admin/blog/settings", h.GetSettings)

	resp := doReq(t, app, http.MethodGet, "/admin/blog/settings", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.UpdateSettings tests
// ---------------------------------------------------------------------------

func TestAdminHandler_UpdateSettings_InvalidBody_ReturnsBadRequest(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Put("/admin/blog/settings", h.UpdateSettings)

	resp := doReq(t, app, http.MethodPut, "/admin/blog/settings", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_UpdateSettings_ValidBody_ReturnsOK(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Put("/admin/blog/settings", h.UpdateSettings)

	resp := doReq(t, app, http.MethodPut, "/admin/blog/settings", `{"posts_per_page": 10}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminHandler_UpdateSettings_ValidationError(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	app.Put("/admin/blog/settings", h.UpdateSettings)

	// posts_per_page max is 100
	resp := doReq(t, app, http.MethodPut, "/admin/blog/settings", `{"posts_per_page": 999}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// AdminHandler.RegisterRoutes tests
// ---------------------------------------------------------------------------

func TestAdminHandler_RegisterRoutes(t *testing.T) {
	h := newAdminHandler(&postRepoStub{}, &commentRepoStub{}, &settingsRepoStub{})

	app := newTestApp()
	admin := app.Group("/admin")
	h.RegisterRoutes(admin)

	// Just verify routes are registered and respond (vs 404)
	resp := doReq(t, app, http.MethodGet, "/admin/blog/posts", "")
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected route to be registered, got 404")
	}
}

// ---------------------------------------------------------------------------
// Helper: add userContext for admin approve/reject
// ---------------------------------------------------------------------------

func newAdminAppWithContext(handler fiber.Handler) *fiber.App {
	app := newTestApp()
	app.Use(func(c *fiber.Ctx) error {
		c.SetUserContext(context.Background())
		return c.Next()
	})
	return app
}
