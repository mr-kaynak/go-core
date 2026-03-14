package api

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// SEOHandler tests
// ---------------------------------------------------------------------------

func newSEOHandler(postRepo *postRepoStubWithGetBySlug) *SEOHandler {
	cfg := &config.Config{
		Blog: config.BlogConfig{SiteURL: "https://example.com"},
	}
	seoSvc := service.NewSEOService(cfg)
	postSvc := newPostService(postRepo)
	return NewSEOHandler(seoSvc, postSvc)
}

func TestSEOHandler_GetMeta_PostNotFound_Returns404(t *testing.T) {
	postRepo := &postRepoStubWithGetBySlug{
		postRepoStub: postRepoStub{},
		getBySlugPublishedFn: func(slug string) (*domain.Post, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	h := newSEOHandler(postRepo)

	app := newTestApp()
	app.Get("/posts/:slug/meta", h.GetMeta)

	resp := doReq(t, app, http.MethodGet, "/posts/nonexistent-slug/meta", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestSEOHandler_GetMeta_ValidSlug_ReturnsOK(t *testing.T) {
	postID := uuid.New()
	postRepo := &postRepoStubWithGetBySlug{
		postRepoStub: postRepoStub{},
		getBySlugPublishedFn: func(slug string) (*domain.Post, error) {
			return &domain.Post{
				ID:       postID,
				Title:    "Test Post",
				Slug:     slug,
				AuthorID: uuid.New(),
				Status:   domain.PostStatusPublished,
			}, nil
		},
	}
	h := newSEOHandler(postRepo)

	app := newTestApp()
	app.Get("/posts/:slug/meta", h.GetMeta)

	resp := doReq(t, app, http.MethodGet, "/posts/test-post/meta", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSEOHandler_RegisterRoutes(t *testing.T) {
	postRepo := &postRepoStubWithGetBySlug{
		postRepoStub: postRepoStub{},
		getBySlugPublishedFn: func(slug string) (*domain.Post, error) {
			return &domain.Post{
				ID:       uuid.New(),
				Title:    "Test",
				Slug:     slug,
				AuthorID: uuid.New(),
				Status:   domain.PostStatusPublished,
			}, nil
		},
	}
	h := newSEOHandler(postRepo)

	app := newTestApp()
	blog := app.Group("/blog")
	h.RegisterRoutes(blog)

	// Route should be registered and return 200 for a valid post
	resp := doReq(t, app, http.MethodGet, "/blog/posts/any-slug/meta", "")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// postRepoStubWithGetBySlug wraps postRepoStub with GetBySlugPublished override
// ---------------------------------------------------------------------------

type postRepoStubWithGetBySlug struct {
	postRepoStub
	getBySlugPublishedFn func(slug string) (*domain.Post, error)
}

func (s *postRepoStubWithGetBySlug) GetBySlugPublished(slug string) (*domain.Post, error) {
	if s.getBySlugPublishedFn != nil {
		return s.getBySlugPublishedFn(slug)
	}
	return nil, nil
}
