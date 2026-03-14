package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Storage service stub
// ---------------------------------------------------------------------------

type storageStub struct {
	getObjectFn  func(ctx context.Context, key string) (io.ReadCloser, error)
	statObjectFn func(ctx context.Context, key string) (*storage.ObjectInfo, error)
	getUploadURL func(ctx context.Context, key, contentType string) (string, error)
	deleteFn     func(ctx context.Context, key string) error
}

var _ storage.StorageService = (*storageStub)(nil)

func (s *storageStub) Upload(_ context.Context, _ string, _ io.Reader, _ int64, _ string) (*storage.FileInfo, error) {
	return &storage.FileInfo{}, nil
}
func (s *storageStub) Delete(ctx context.Context, key string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, key)
	}
	return nil
}
func (s *storageStub) GetURL(_ context.Context, _ string) (string, error) {
	return "https://example.com/file", nil
}
func (s *storageStub) GetUploadURL(ctx context.Context, key, contentType string) (string, error) {
	if s.getUploadURL != nil {
		return s.getUploadURL(ctx, key, contentType)
	}
	return "https://example.com/upload", nil
}
func (s *storageStub) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.getObjectFn != nil {
		return s.getObjectFn(ctx, key)
	}
	return io.NopCloser(strings.NewReader("file contents")), nil
}
func (s *storageStub) StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	if s.statObjectFn != nil {
		return s.statObjectFn(ctx, key)
	}
	return &storage.ObjectInfo{
		ETag:        "\"abc123\"",
		Size:        13, // matches "file contents" length from GetObject
		ContentType: "image/jpeg",
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newMediaTestCfg() *config.Config {
	return &config.Config{
		Blog: config.BlogConfig{
			SiteURL:      "https://example.com",
			MaxMediaSize: 10 * 1024 * 1024, // 10MB
		},
	}
}

func newMediaHandler(postRepo *postRepoStub, storageSvc storage.StorageService) *MediaHandler {
	cfg := newMediaTestCfg()
	mediaSvc := service.NewMediaService(postRepo, storageSvc, cfg)
	return NewMediaHandler(mediaSvc, storageSvc)
}

func mediaAuthMw(userID uuid.UUID) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}
}

func optionalAuthMw() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// ---------------------------------------------------------------------------
// MediaHandler.ListByPost tests
// ---------------------------------------------------------------------------

func TestMediaHandler_ListByPost_InvalidPostID_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:postId/media", mediaAuthMw(userID), h.ListByPost)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/media", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ListByPost_ReturnsOK(t *testing.T) {
	postID := uuid.New()
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:postId/media", mediaAuthMw(userID), h.ListByPost)

	resp := doReq(t, app, http.MethodGet, "/posts/"+postID.String()+"/media", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// MediaHandler.Delete tests
// ---------------------------------------------------------------------------

func TestMediaHandler_Delete_InvalidID_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Delete("/media/:id", mediaAuthMw(userID), h.Delete)

	resp := doReq(t, app, http.MethodDelete, "/media/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_Delete_NoAuth_ReturnsUnauthorized(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})

	app := newTestApp()
	// No auth middleware — userID will be nil
	app.Delete("/media/:id", h.Delete)

	resp := doReq(t, app, http.MethodDelete, "/media/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// MediaHandler.GeneratePresignedUpload tests
// ---------------------------------------------------------------------------

func TestMediaHandler_GeneratePresignedUpload_InvalidBody_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Post("/media/presign", mediaAuthMw(userID), h.GeneratePresignedUpload)

	resp := doReq(t, app, http.MethodPost, "/media/presign", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_GeneratePresignedUpload_MissingFields_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Post("/media/presign", mediaAuthMw(userID), h.GeneratePresignedUpload)

	// Missing required fields
	resp := doReq(t, app, http.MethodPost, "/media/presign", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_GeneratePresignedUpload_NoAuth_ReturnsUnauthorized(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})

	app := newTestApp()
	app.Post("/media/presign", h.GeneratePresignedUpload)

	postID := uuid.New()
	body := `{"post_id":"` + postID.String() + `","filename":"test.jpg","content_type":"image/jpeg"}`
	resp := doReq(t, app, http.MethodPost, "/media/presign", body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_GeneratePresignedUpload_PostNotFound_Returns404(t *testing.T) {
	postRepoWithNotFound := &postRepoStubWithGetByID{
		postRepoStub: postRepoStub{},
		getByIDFn: func(id uuid.UUID) (*domain.Post, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	cfg := newMediaTestCfg()
	mediaSvc := service.NewMediaService(postRepoWithNotFound, &storageStub{}, cfg)
	h := NewMediaHandler(mediaSvc, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Post("/media/presign", mediaAuthMw(userID), h.GeneratePresignedUpload)

	postID := uuid.New()
	body := `{"post_id":"` + postID.String() + `","filename":"test.jpg","content_type":"image/jpeg"}`
	resp := doReq(t, app, http.MethodPost, "/media/presign", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// MediaHandler.Register tests
// ---------------------------------------------------------------------------

func TestMediaHandler_Register_InvalidBody_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Post("/media", mediaAuthMw(userID), h.Register)

	resp := doReq(t, app, http.MethodPost, "/media", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_Register_MissingFields_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	app.Post("/media", mediaAuthMw(userID), h.Register)

	resp := doReq(t, app, http.MethodPost, "/media", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_Register_NoAuth_ReturnsUnauthorized(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})

	app := newTestApp()
	app.Post("/media", h.Register)

	postID := uuid.New()
	body := `{"post_id":"` + postID.String() + `","s3_key":"blog/` + postID.String() + `/test.jpg","filename":"test.jpg","media_type":"image","content_type":"image/jpeg","file_size":1024}`
	resp := doReq(t, app, http.MethodPost, "/media", body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// MediaHandler.ServeFile tests
// ---------------------------------------------------------------------------

func TestMediaHandler_ServeFile_MissingKey_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	storageSvc := &storageStub{}
	h := newMediaHandler(postRepo, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ServeFile_InvalidKeyFormat_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	storageSvc := &storageStub{}
	h := newMediaHandler(postRepo, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/invalid-key", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ServeFile_WrongPrefix_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	storageSvc := &storageStub{}
	h := newMediaHandler(postRepo, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/notblog/"+uuid.New().String()+"/file.jpg", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ServeFile_InvalidPostIDInKey_ReturnsBadRequest(t *testing.T) {
	postRepo := &postRepoStub{}
	storageSvc := &storageStub{}
	h := newMediaHandler(postRepo, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/blog/not-uuid/file.jpg", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ServeFile_PostNotFound_Returns404(t *testing.T) {
	postRepo := &postRepoStub{}
	storageSvc := &storageStub{}
	cfg := newMediaTestCfg()
	// Create a postRepo that returns ErrRecordNotFound for GetByID
	postRepoWithNotFound := &postRepoStubWithGetByID{
		postRepoStub: *postRepo,
		getByIDFn: func(id uuid.UUID) (*domain.Post, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	mediaSvc := service.NewMediaService(postRepoWithNotFound, storageSvc, cfg)
	h := NewMediaHandler(mediaSvc, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	postID := uuid.New()
	resp := doReq(t, app, http.MethodGet, "/media/file/blog/"+postID.String()+"/test.jpg", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ServeFile_PublishedPost_ReturnsFile(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	postRepoWithGet := &postRepoStubWithGetByID{
		postRepoStub: postRepoStub{},
		getByIDFn: func(id uuid.UUID) (*domain.Post, error) {
			return &domain.Post{
				ID:       postID,
				AuthorID: authorID,
				Status:   domain.PostStatusPublished,
			}, nil
		},
	}

	storageSvc := &storageStub{}
	cfg := newMediaTestCfg()
	mediaSvc := service.NewMediaService(postRepoWithGet, storageSvc, cfg)
	h := NewMediaHandler(mediaSvc, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/blog/"+postID.String()+"/test.jpg", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check cache control header for published post
	cc := resp.Header.Get("Cache-Control")
	if cc != "public, max-age=86400" {
		t.Fatalf("expected public cache control, got %q", cc)
	}
}

func TestMediaHandler_ServeFile_DraftPost_NoAuth_ReturnsForbidden(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	postRepoWithGet := &postRepoStubWithGetByID{
		postRepoStub: postRepoStub{},
		getByIDFn: func(id uuid.UUID) (*domain.Post, error) {
			return &domain.Post{
				ID:       postID,
				AuthorID: authorID,
				Status:   domain.PostStatusDraft,
			}, nil
		},
	}

	storageSvc := &storageStub{}
	cfg := newMediaTestCfg()
	mediaSvc := service.NewMediaService(postRepoWithGet, storageSvc, cfg)
	h := NewMediaHandler(mediaSvc, storageSvc)

	app := newTestApp()
	// No auth middleware — user will be nil
	app.Get("/media/file/*", h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/blog/"+postID.String()+"/test.jpg", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_ServeFile_DraftPost_AuthorAccess_ReturnsOK(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	postRepoWithGet := &postRepoStubWithGetByID{
		postRepoStub: postRepoStub{},
		getByIDFn: func(id uuid.UUID) (*domain.Post, error) {
			return &domain.Post{
				ID:       postID,
				AuthorID: authorID,
				Status:   domain.PostStatusDraft,
			}, nil
		},
	}

	storageSvc := &storageStub{}
	cfg := newMediaTestCfg()
	mediaSvc := service.NewMediaService(postRepoWithGet, storageSvc, cfg)
	h := NewMediaHandler(mediaSvc, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", func(c *fiber.Ctx) error {
		c.Locals("userID", authorID)
		c.Locals("roles", []string{"user"})
		return c.Next()
	}, h.ServeFile)

	resp := doReq(t, app, http.MethodGet, "/media/file/blog/"+postID.String()+"/test.jpg", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check cache control for draft
	cc := resp.Header.Get("Cache-Control")
	if cc != "private, no-store" {
		t.Fatalf("expected private cache control, got %q", cc)
	}
}

func TestMediaHandler_ServeFile_ETagMatch_Returns304(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	postRepoWithGet := &postRepoStubWithGetByID{
		postRepoStub: postRepoStub{},
		getByIDFn: func(id uuid.UUID) (*domain.Post, error) {
			return &domain.Post{
				ID:       postID,
				AuthorID: authorID,
				Status:   domain.PostStatusPublished,
			}, nil
		},
	}

	storageSvc := &storageStub{
		statObjectFn: func(ctx context.Context, key string) (*storage.ObjectInfo, error) {
			return &storage.ObjectInfo{
				ETag:        "\"abc123\"",
				Size:        100,
				ContentType: "image/jpeg",
			}, nil
		},
	}
	cfg := newMediaTestCfg()
	mediaSvc := service.NewMediaService(postRepoWithGet, storageSvc, cfg)
	h := NewMediaHandler(mediaSvc, storageSvc)

	app := newTestApp()
	app.Get("/media/file/*", h.ServeFile)

	req, _ := http.NewRequest(http.MethodGet, "/media/file/blog/"+postID.String()+"/test.jpg", nil)
	req.Header.Set("If-None-Match", "\"abc123\"")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_RegisterRoutes(t *testing.T) {
	postRepo := &postRepoStub{}
	h := newMediaHandler(postRepo, &storageStub{})
	userID := uuid.New()

	app := newTestApp()
	blog := app.Group("/blog")
	h.RegisterRoutes(blog, mediaAuthMw(userID), optionalAuthMw())

	// Verify a media route is registered
	resp := doReq(t, app, http.MethodPost, "/blog/media/presign", `{}`)
	// Should not be 404
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected route to be registered, got 404")
	}
}

// ---------------------------------------------------------------------------
// cacheControlFor tests
// ---------------------------------------------------------------------------

func TestCacheControlFor_Published(t *testing.T) {
	result := cacheControlFor(domain.PostStatusPublished)
	if result != "public, max-age=86400" {
		t.Fatalf("expected public cache, got %q", result)
	}
}

func TestCacheControlFor_Draft(t *testing.T) {
	result := cacheControlFor(domain.PostStatusDraft)
	if result != "private, no-store" {
		t.Fatalf("expected private cache, got %q", result)
	}
}

func TestCacheControlFor_Archived(t *testing.T) {
	result := cacheControlFor(domain.PostStatusArchived)
	if result != "private, no-store" {
		t.Fatalf("expected private cache, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// postRepoStubWithGetByID wraps postRepoStub but overrides GetByID
// ---------------------------------------------------------------------------

type postRepoStubWithGetByID struct {
	postRepoStub
	getByIDFn func(id uuid.UUID) (*domain.Post, error)
}

func (s *postRepoStubWithGetByID) GetByID(id uuid.UUID) (*domain.Post, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
