package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Post repository stub
// ---------------------------------------------------------------------------

type postRepoStub struct {
	listFilteredFn func(filter repository.PostListFilter) ([]*domain.Post, int64, error)
}

var _ repository.PostRepository = (*postRepoStub)(nil)

func (s *postRepoStub) WithTx(_ *gorm.DB) repository.PostRepository       { return s }
func (s *postRepoStub) Create(_ *domain.Post) error                       { return nil }
func (s *postRepoStub) Update(_ *domain.Post) error                       { return nil }
func (s *postRepoStub) Delete(_ uuid.UUID) error                          { return nil }
func (s *postRepoStub) GetByID(_ uuid.UUID) (*domain.Post, error)         { return nil, nil }
func (s *postRepoStub) GetBySlug(_ string) (*domain.Post, error)          { return nil, nil }
func (s *postRepoStub) GetBySlugPublished(_ string) (*domain.Post, error) { return nil, nil }
func (s *postRepoStub) CountByStatus(_ string) (int64, error)             { return 0, nil }
func (s *postRepoStub) ExistsBySlug(_ string) (bool, error)               { return false, nil }
func (s *postRepoStub) ExistsBySlugExcluding(_ string, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (s *postRepoStub) CreateRevision(_ *domain.PostRevision) error { return nil }
func (s *postRepoStub) ListRevisions(_ uuid.UUID) ([]*domain.PostRevision, error) {
	return nil, nil
}
func (s *postRepoStub) GetRevision(_ uuid.UUID) (*domain.PostRevision, error) { return nil, nil }
func (s *postRepoStub) GetLatestRevisionVersion(_ uuid.UUID) (int, error)     { return 0, nil }
func (s *postRepoStub) CreateMedia(_ *domain.PostMedia) error                 { return nil }
func (s *postRepoStub) DeleteMedia(_ uuid.UUID) error                         { return nil }
func (s *postRepoStub) GetMediaByID(_ uuid.UUID) (*domain.PostMedia, error)   { return nil, nil }
func (s *postRepoStub) ListMediaByPost(_ uuid.UUID) ([]*domain.PostMedia, error) {
	return nil, nil
}
func (s *postRepoStub) ReplaceTags(_ uuid.UUID, _ []uuid.UUID) error { return nil }
func (s *postRepoStub) ListFiltered(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
	if s.listFilteredFn != nil {
		return s.listFilteredFn(filter)
	}
	return []*domain.Post{}, 0, nil
}

// ---------------------------------------------------------------------------
// Category repository stub
// ---------------------------------------------------------------------------

type categoryRepoStub struct {
	getTreeFn func() ([]*domain.Category, error)
}

var _ repository.CategoryRepository = (*categoryRepoStub)(nil)

func (s *categoryRepoStub) WithTx(_ *gorm.DB) repository.CategoryRepository { return s }
func (s *categoryRepoStub) Create(_ *domain.Category) error                 { return nil }
func (s *categoryRepoStub) Update(_ *domain.Category) error                 { return nil }
func (s *categoryRepoStub) Delete(_ uuid.UUID) error                        { return nil }
func (s *categoryRepoStub) GetByID(_ uuid.UUID) (*domain.Category, error)   { return nil, nil }
func (s *categoryRepoStub) GetBySlug(_ string) (*domain.Category, error)    { return nil, nil }
func (s *categoryRepoStub) GetAll() ([]*domain.Category, error)             { return nil, nil }
func (s *categoryRepoStub) Count() (int64, error)                           { return 0, nil }
func (s *categoryRepoStub) ExistsBySlug(_ string) (bool, error)             { return false, nil }
func (s *categoryRepoStub) ExistsBySlugExcluding(_ string, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (s *categoryRepoStub) HasChildren(_ uuid.UUID) (bool, error) { return false, nil }
func (s *categoryRepoStub) HasPosts(_ uuid.UUID) (bool, error)    { return false, nil }
func (s *categoryRepoStub) GetTree() ([]*domain.Category, error) {
	if s.getTreeFn != nil {
		return s.getTreeFn()
	}
	return []*domain.Category{}, nil
}

// ---------------------------------------------------------------------------
// Tag repository stub
// ---------------------------------------------------------------------------

type tagRepoStub struct {
	getAllFn func(offset, limit int) ([]*domain.Tag, int64, error)
}

var _ repository.TagRepository = (*tagRepoStub)(nil)

func (s *tagRepoStub) WithTx(_ *gorm.DB) repository.TagRepository { return s }
func (s *tagRepoStub) Create(_ *domain.Tag) error                 { return nil }
func (s *tagRepoStub) Update(_ *domain.Tag) error                 { return nil }
func (s *tagRepoStub) Delete(_ uuid.UUID) error                   { return nil }
func (s *tagRepoStub) GetByID(_ uuid.UUID) (*domain.Tag, error)   { return nil, nil }
func (s *tagRepoStub) GetBySlug(_ string) (*domain.Tag, error)    { return nil, nil }
func (s *tagRepoStub) ExistsBySlug(_ string) (bool, error)        { return false, nil }
func (s *tagRepoStub) GetPopular(_ int) ([]*domain.Tag, error)    { return []*domain.Tag{}, nil }
func (s *tagRepoStub) GetOrCreateByNames(_ []string, _ func(string) string) ([]*domain.Tag, error) {
	return []*domain.Tag{}, nil
}
func (s *tagRepoStub) GetAll(offset, limit int) ([]*domain.Tag, int64, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return []*domain.Tag{}, 0, nil
}

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

func newTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
}

func doReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// PostHandler tests
// ---------------------------------------------------------------------------

func newPostService(repo repository.PostRepository) *service.PostService {
	slugSvc := service.NewSlugService()
	contentSvc := service.NewContentService()
	readTimeSvc := service.NewReadTimeService(200)
	return service.NewPostService(nil, repo, nil, nil, contentSvc, slugSvc, readTimeSvc)
}

func TestPostHandler_ListPublished_ReturnsOK(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPostHandler_ListPublished_PaginationNormalized(t *testing.T) {
	var gotOffset, gotLimit int
	repo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			gotOffset = filter.Offset
			gotLimit = filter.Limit
			return []*domain.Post{}, 0, nil
		},
	}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts?page=-5&limit=999", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// page is normalized to 1, so offset should be 0
	if gotOffset != 0 {
		t.Fatalf("expected offset 0, got %d", gotOffset)
	}
	// limit out of range => falls back to postsPerPage=10
	if gotLimit != 10 {
		t.Fatalf("expected limit 10, got %d", gotLimit)
	}
}

func TestPostHandler_ListPublished_StatusFilterIsPublished(t *testing.T) {
	var capturedStatus string
	repo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			capturedStatus = filter.Status
			return []*domain.Post{}, 0, nil
		},
	}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedStatus != string(domain.PostStatusPublished) {
		t.Fatalf("expected status %q, got %q", domain.PostStatusPublished, capturedStatus)
	}
}

func TestPostHandler_GetTrending_NoEngagementSvc_ReturnsOK(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	// engagementSvc intentionally not set

	app := newTestApp()
	app.Get("/posts/trending", h.GetTrending)

	resp := doReq(t, app, http.MethodGet, "/posts/trending", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPostHandler_Update_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Put("/posts/:id", h.Update)

	resp := doReq(t, app, http.MethodPut, "/posts/not-a-uuid", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_SoftDelete_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Delete("/posts/:id", h.SoftDelete)

	resp := doReq(t, app, http.MethodDelete, "/posts/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CategoryHandler tests
// ---------------------------------------------------------------------------

func newCategoryService(repo repository.CategoryRepository) *service.CategoryService {
	return service.NewCategoryService(repo, service.NewSlugService())
}

func TestCategoryHandler_GetTree_ReturnsOK(t *testing.T) {
	repo := &categoryRepoStub{}
	categorySvc := newCategoryService(repo)
	h := NewCategoryHandler(categorySvc)

	app := newTestApp()
	app.Get("/categories", h.GetTree)

	resp := doReq(t, app, http.MethodGet, "/categories", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCategoryHandler_GetTree_ReturnsItems(t *testing.T) {
	repo := &categoryRepoStub{
		getTreeFn: func() ([]*domain.Category, error) {
			return []*domain.Category{
				{Name: "Tech", Slug: "tech"},
			}, nil
		},
	}
	categorySvc := newCategoryService(repo)
	h := NewCategoryHandler(categorySvc)

	app := newTestApp()
	app.Get("/categories", h.GetTree)

	resp := doReq(t, app, http.MethodGet, "/categories", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCategoryHandler_Update_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &categoryRepoStub{}
	categorySvc := newCategoryService(repo)
	h := NewCategoryHandler(categorySvc)

	app := newTestApp()
	app.Put("/categories/:id", h.Update)

	resp := doReq(t, app, http.MethodPut, "/categories/not-a-uuid", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCategoryHandler_Delete_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &categoryRepoStub{}
	categorySvc := newCategoryService(repo)
	h := NewCategoryHandler(categorySvc)

	app := newTestApp()
	app.Delete("/categories/:id", h.Delete)

	resp := doReq(t, app, http.MethodDelete, "/categories/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// TagHandler tests
// ---------------------------------------------------------------------------

func newTagService(repo repository.TagRepository) *service.TagService {
	return service.NewTagService(repo, service.NewSlugService())
}

func TestTagHandler_List_ReturnsOK(t *testing.T) {
	repo := &tagRepoStub{}
	tagSvc := newTagService(repo)
	h := NewTagHandler(tagSvc)

	app := newTestApp()
	app.Get("/tags", h.List)

	resp := doReq(t, app, http.MethodGet, "/tags", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTagHandler_List_PaginationNormalized(t *testing.T) {
	var gotOffset, gotLimit int
	repo := &tagRepoStub{
		getAllFn: func(offset, limit int) ([]*domain.Tag, int64, error) {
			gotOffset = offset
			gotLimit = limit
			return []*domain.Tag{}, 0, nil
		},
	}
	tagSvc := newTagService(repo)
	h := NewTagHandler(tagSvc)

	app := newTestApp()
	app.Get("/tags", h.List)

	resp := doReq(t, app, http.MethodGet, "/tags?page=-1&limit=9999", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// page normalized to 1 => offset 0
	if gotOffset != 0 {
		t.Fatalf("expected offset 0, got %d", gotOffset)
	}
	// limit out of range => default 50
	if gotLimit != 50 {
		t.Fatalf("expected limit 50, got %d", gotLimit)
	}
}

func TestTagHandler_GetPopular_ReturnsOK(t *testing.T) {
	repo := &tagRepoStub{}
	tagSvc := newTagService(repo)
	h := NewTagHandler(tagSvc)

	app := newTestApp()
	app.Get("/tags/popular", h.GetPopular)

	resp := doReq(t, app, http.MethodGet, "/tags/popular", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// FeedHandler tests
// ---------------------------------------------------------------------------

func newFeedService(repo repository.PostRepository) *service.FeedService {
	cfg := &config.Config{
		App:  config.AppConfig{Name: "TestBlog"},
		Blog: config.BlogConfig{SiteURL: "https://example.com", FeedItemLimit: 20},
	}
	return service.NewFeedService(repo, cfg)
}

func TestFeedHandler_RSS_ReturnsOK_WithCorrectContentType(t *testing.T) {
	repo := &postRepoStub{}
	feedSvc := newFeedService(repo)
	h := NewFeedHandler(feedSvc)

	app := newTestApp()
	app.Get("/feed/rss", h.RSS)

	resp := doReq(t, app, http.MethodGet, "/feed/rss", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/rss+xml") {
		t.Fatalf("expected Content-Type application/rss+xml, got %q", ct)
	}
}

func TestFeedHandler_Atom_ReturnsOK_WithCorrectContentType(t *testing.T) {
	repo := &postRepoStub{}
	feedSvc := newFeedService(repo)
	h := NewFeedHandler(feedSvc)

	app := newTestApp()
	app.Get("/feed/atom", h.Atom)

	resp := doReq(t, app, http.MethodGet, "/feed/atom", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/atom+xml") {
		t.Fatalf("expected Content-Type application/atom+xml, got %q", ct)
	}
}

func TestFeedHandler_Sitemap_ReturnsOK_WithCorrectContentType(t *testing.T) {
	repo := &postRepoStub{}
	feedSvc := newFeedService(repo)
	h := NewFeedHandler(feedSvc)

	app := newTestApp()
	app.Get("/sitemap.xml", h.Sitemap)

	resp := doReq(t, app, http.MethodGet, "/sitemap.xml", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/xml") {
		t.Fatalf("expected Content-Type application/xml, got %q", ct)
	}
}

func TestFeedHandler_RSS_ContainsXMLDeclaration(t *testing.T) {
	repo := &postRepoStub{}
	feedSvc := newFeedService(repo)
	h := NewFeedHandler(feedSvc)

	app := newTestApp()
	app.Get("/feed/rss", h.RSS)

	resp := doReq(t, app, http.MethodGet, "/feed/rss", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "<?xml") {
		t.Fatalf("RSS response does not contain XML declaration, got: %q", body)
	}
}

func TestFeedHandler_Sitemap_ContainsBlogURL(t *testing.T) {
	repo := &postRepoStub{}
	feedSvc := newFeedService(repo)
	h := NewFeedHandler(feedSvc)

	app := newTestApp()
	app.Get("/sitemap.xml", h.Sitemap)

	resp := doReq(t, app, http.MethodGet, "/sitemap.xml", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var bodyBuilder strings.Builder
	readBuf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(readBuf)
		if n > 0 {
			bodyBuilder.Write(readBuf[:n])
		}
		if err != nil {
			break
		}
	}
	body := bodyBuilder.String()
	if !strings.Contains(body, "example.com/blog") {
		t.Fatalf("sitemap does not contain site URL, got: %q", body)
	}
}
