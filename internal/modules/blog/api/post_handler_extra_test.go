package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// ---------------------------------------------------------------------------
// PostHandler.GetPopular tests
// ---------------------------------------------------------------------------

func TestPostHandler_GetPopular_NoEngagementSvc_ReturnsOK(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	// engagementSvc intentionally not set

	app := newTestApp()
	app.Get("/posts/popular", h.GetPopular)

	resp := doReq(t, app, http.MethodGet, "/posts/popular", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.Create tests (stub-based)
// ---------------------------------------------------------------------------

func TestPostHandler_Create_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Post("/posts", h.Create)

	resp := doReq(t, app, http.MethodPost, "/posts", `{"title":"test","content_json":[]}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestPostHandler_Create_InvalidBody_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Post("/posts", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"user"})
		return c.Next()
	}, h.Create)

	resp := doReq(t, app, http.MethodPost, "/posts", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_Create_MissingTitle_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Post("/posts", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"user"})
		return c.Next()
	}, h.Create)

	resp := doReq(t, app, http.MethodPost, "/posts", `{"content_json":[]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.CreateDraft tests
// ---------------------------------------------------------------------------

func TestPostHandler_CreateDraft_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Post("/posts/draft", h.CreateDraft)

	resp := doReq(t, app, http.MethodPost, "/posts/draft", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.Publish tests
// ---------------------------------------------------------------------------

func TestPostHandler_Publish_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Post("/posts/:id/publish", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.Publish)

	resp := doReq(t, app, http.MethodPost, "/posts/not-a-uuid/publish", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_Publish_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Post("/posts/:id/publish", h.Publish)

	resp := doReq(t, app, http.MethodPost, "/posts/"+uuid.New().String()+"/publish", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.Archive tests
// ---------------------------------------------------------------------------

func TestPostHandler_Archive_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Post("/posts/:id/archive", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.Archive)

	resp := doReq(t, app, http.MethodPost, "/posts/not-a-uuid/archive", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_Archive_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Post("/posts/:id/archive", h.Archive)

	resp := doReq(t, app, http.MethodPost, "/posts/"+uuid.New().String()+"/archive", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.GetForEdit tests
// ---------------------------------------------------------------------------

func TestPostHandler_GetForEdit_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:id/edit", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.GetForEdit)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/edit", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_GetForEdit_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts/:id/edit", h.GetForEdit)

	resp := doReq(t, app, http.MethodGet, "/posts/"+uuid.New().String()+"/edit", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.ListRevisions tests
// ---------------------------------------------------------------------------

func TestPostHandler_ListRevisions_InvalidID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:id/revisions", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.ListRevisions)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/revisions", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_ListRevisions_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts/:id/revisions", h.ListRevisions)

	resp := doReq(t, app, http.MethodGet, "/posts/"+uuid.New().String()+"/revisions", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.GetRevision tests
// ---------------------------------------------------------------------------

func TestPostHandler_GetRevision_InvalidPostID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:id/revisions/:rid", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.GetRevision)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/revisions/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_GetRevision_InvalidRevisionID_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Get("/posts/:id/revisions/:rid", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.GetRevision)

	resp := doReq(t, app, http.MethodGet, "/posts/"+uuid.New().String()+"/revisions/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_GetRevision_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts/:id/revisions/:rid", h.GetRevision)

	resp := doReq(t, app, http.MethodGet, "/posts/"+uuid.New().String()+"/revisions/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.Update tests (additional)
// ---------------------------------------------------------------------------

func TestPostHandler_Update_InvalidBody_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)
	userID := uuid.New()

	app := newTestApp()
	app.Put("/posts/:id", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.Update)

	resp := doReq(t, app, http.MethodPut, "/posts/"+uuid.New().String(), "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_Update_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Put("/posts/:id", h.Update)

	resp := doReq(t, app, http.MethodPut, "/posts/"+uuid.New().String(), `{}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.SoftDelete tests (additional)
// ---------------------------------------------------------------------------

func TestPostHandler_SoftDelete_NoAuth_ReturnsUnauthorized(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Delete("/posts/:id", h.SoftDelete)

	resp := doReq(t, app, http.MethodDelete, "/posts/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.ListPublished sort/filter tests
// ---------------------------------------------------------------------------

func TestPostHandler_ListPublished_InvalidSortBy_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts?sort_by=invalid_field", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_ListPublished_InvalidOrder_ReturnsBadRequest(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts?order=random", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPostHandler_ListPublished_WithCategoryFilter(t *testing.T) {
	catID := uuid.New()
	var capturedCatID *uuid.UUID
	repo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			capturedCatID = filter.CategoryID
			return []*domain.Post{}, 0, nil
		},
	}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts?category_id="+catID.String(), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedCatID == nil || *capturedCatID != catID {
		t.Fatal("expected category_id to be captured")
	}
}

func TestPostHandler_ListPublished_WithTagsFilter(t *testing.T) {
	var capturedTagSlugs []string
	repo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			capturedTagSlugs = filter.TagSlugs
			return []*domain.Post{}, 0, nil
		},
	}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts?tags=go,rust", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if len(capturedTagSlugs) != 2 {
		t.Fatalf("expected 2 tag slugs, got %d", len(capturedTagSlugs))
	}
}

func TestPostHandler_ListPublished_WithSearchQuery(t *testing.T) {
	var capturedSearch string
	repo := &postRepoStub{
		listFilteredFn: func(filter repository.PostListFilter) ([]*domain.Post, int64, error) {
			capturedSearch = filter.Search
			return []*domain.Post{}, 0, nil
		},
	}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts", h.ListPublished)

	resp := doReq(t, app, http.MethodGet, "/posts?search=golang", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedSearch != "golang" {
		t.Fatalf("expected search 'golang', got %q", capturedSearch)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.SetEngagementService / SetUserLookup tests
// ---------------------------------------------------------------------------

func TestPostHandler_SetEngagementService(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	h.SetEngagementService(nil)
	if h.engagementSvc != nil {
		t.Fatal("expected nil engagement service")
	}
}

func TestPostHandler_SetUserLookup(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	h.SetUserLookup(func(ctx context.Context, userID uuid.UUID) (*domain.PostAuthor, error) {
		return nil, nil
	})
	if h.userLookup == nil {
		t.Fatal("expected userLookup to be set")
	}
}

// ---------------------------------------------------------------------------
// PostHandler.RegisterRoutes tests
// ---------------------------------------------------------------------------

func TestPostHandler_RegisterRoutes_AllRoutesRegistered(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	blog := app.Group("/blog")
	authMw := func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}
	h.RegisterRoutes(blog, authMw, nil)

	// Verify public route
	resp := doReq(t, app, http.MethodGet, "/blog/posts", "")
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected /blog/posts to be registered")
	}

	// Verify trending route
	resp = doReq(t, app, http.MethodGet, "/blog/posts/trending", "")
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected /blog/posts/trending to be registered")
	}

	// Verify popular route
	resp = doReq(t, app, http.MethodGet, "/blog/posts/popular", "")
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected /blog/posts/popular to be registered")
	}
}

// ---------------------------------------------------------------------------
// toPostResponse tests
// ---------------------------------------------------------------------------

func TestToPostResponse_BasicPost(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	post := &domain.Post{
		ID:       postID,
		Title:    "Test Post",
		Slug:     "test-post",
		AuthorID: authorID,
		Status:   domain.PostStatusPublished,
		ReadTime: 5,
	}

	resp := toPostResponse(post)

	if resp.ID != postID {
		t.Fatalf("expected ID %s, got %s", postID, resp.ID)
	}
	if resp.Title != "Test Post" {
		t.Fatalf("expected title 'Test Post', got %q", resp.Title)
	}
	if resp.Slug != "test-post" {
		t.Fatalf("expected slug 'test-post', got %q", resp.Slug)
	}
	if resp.ReadTimeMinutes != 5 {
		t.Fatalf("expected read time 5, got %d", resp.ReadTimeMinutes)
	}
}

func TestToPostResponse_WithCategory(t *testing.T) {
	catID := uuid.New()
	post := &domain.Post{
		ID:       uuid.New(),
		AuthorID: uuid.New(),
		Category: &domain.Category{
			ID:   catID,
			Name: "Tech",
			Slug: "tech",
		},
	}

	resp := toPostResponse(post)

	if resp.Category == nil {
		t.Fatal("expected category in response")
	}
	if resp.Category.ID != catID {
		t.Fatalf("expected category ID %s, got %s", catID, resp.Category.ID)
	}
}

func TestToPostResponse_WithTags(t *testing.T) {
	tag1ID := uuid.New()
	tag2ID := uuid.New()
	post := &domain.Post{
		ID:       uuid.New(),
		AuthorID: uuid.New(),
		Tags: []domain.Tag{
			{ID: tag1ID, Name: "Go", Slug: "go"},
			{ID: tag2ID, Name: "Rust", Slug: "rust"},
		},
	}

	resp := toPostResponse(post)

	if len(resp.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(resp.Tags))
	}
	if resp.Tags[0].Name != "Go" {
		t.Fatalf("expected first tag 'Go', got %q", resp.Tags[0].Name)
	}
}

func TestToPostResponse_WithStats(t *testing.T) {
	post := &domain.Post{
		ID:       uuid.New(),
		AuthorID: uuid.New(),
		Stats: &domain.PostStats{
			LikeCount:    10,
			ViewCount:    100,
			ShareCount:   5,
			CommentCount: 3,
		},
	}

	resp := toPostResponse(post)

	if resp.Stats == nil {
		t.Fatal("expected stats in response")
	}
	if resp.Stats.LikeCount != 10 {
		t.Fatalf("expected like count 10, got %d", resp.Stats.LikeCount)
	}
	if resp.Stats.ViewCount != 100 {
		t.Fatalf("expected view count 100, got %d", resp.Stats.ViewCount)
	}
}

func TestToPostResponse_NilCategoryAndTags(t *testing.T) {
	post := &domain.Post{
		ID:       uuid.New(),
		AuthorID: uuid.New(),
	}

	resp := toPostResponse(post)

	if resp.Category != nil {
		t.Fatal("expected nil category")
	}
	if resp.Tags != nil {
		t.Fatal("expected nil tags")
	}
	if resp.Stats != nil {
		t.Fatal("expected nil stats")
	}
}

// ---------------------------------------------------------------------------
// PostHandler.GetBySlug tests (with custom repo stub)
// ---------------------------------------------------------------------------

func TestPostHandler_GetBySlug_ReturnsPost(t *testing.T) {
	postID := uuid.New()
	authorID := uuid.New()
	repo := &postRepoStubWithGetBySlug{
		postRepoStub: postRepoStub{},
		getBySlugPublishedFn: func(slug string) (*domain.Post, error) {
			return &domain.Post{
				ID:       postID,
				Title:    "Test Post",
				Slug:     slug,
				AuthorID: authorID,
				Status:   domain.PostStatusPublished,
			}, nil
		},
	}
	slugSvc := service.NewSlugService()
	contentSvc := service.NewContentService()
	readTimeSvc := service.NewReadTimeService(200)
	postSvc := service.NewPostService(nil, repo, nil, nil, contentSvc, slugSvc, readTimeSvc)
	h := NewPostHandler(postSvc, 10)

	app := newTestApp()
	app.Get("/posts/:slug", h.GetBySlug)

	resp := doReq(t, app, http.MethodGet, "/posts/test-post", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PostHandler.enrichPostResponse tests
// ---------------------------------------------------------------------------

func TestPostHandler_enrichPostResponse_WithLookup(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	authorID := uuid.New()
	h.SetUserLookup(func(ctx context.Context, userID uuid.UUID) (*domain.PostAuthor, error) {
		return &domain.PostAuthor{ID: userID, Name: "Author Name"}, nil
	})

	resp := &domain.PostResponse{
		ID:   uuid.New(),
		Slug: "test",
	}

	h.enrichPostResponse(context.Background(), resp, authorID)

	if resp.Author == nil {
		t.Fatal("expected author to be set")
	}
	if resp.Author.Name != "Author Name" {
		t.Fatalf("expected author name 'Author Name', got %q", resp.Author.Name)
	}
}

func TestPostHandler_enrichPostResponse_NilLookup(t *testing.T) {
	repo := &postRepoStub{}
	postSvc := newPostService(repo)
	h := NewPostHandler(postSvc, 10)

	resp := &domain.PostResponse{
		ID:   uuid.New(),
		Slug: "test",
	}

	// Should not panic with nil lookup
	h.enrichPostResponse(context.Background(), resp, uuid.New())

	if resp.Author != nil {
		t.Fatal("expected nil author when no lookup set")
	}
}
