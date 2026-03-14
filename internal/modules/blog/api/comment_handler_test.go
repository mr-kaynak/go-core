package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

// ---------------------------------------------------------------------------
// CommentHandler.GetThreaded tests
// ---------------------------------------------------------------------------

func TestCommentHandler_GetThreaded_InvalidPostID_ReturnsBadRequest(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	app := newTestApp()
	app.Get("/posts/:postId/comments", h.GetThreaded)

	resp := doReq(t, app, http.MethodGet, "/posts/not-a-uuid/comments", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CommentHandler.Create tests
// ---------------------------------------------------------------------------

func TestCommentHandler_Create_InvalidPostID_ReturnsBadRequest(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	app := newTestApp()
	app.Post("/posts/:postId/comments", h.Create)

	resp := doReq(t, app, http.MethodPost, "/posts/not-a-uuid/comments", `{"content":"test"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCommentHandler_Create_InvalidBody_ReturnsBadRequest(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	app := newTestApp()
	postID := uuid.New()
	app.Post("/posts/:postId/comments", h.Create)

	resp := doReq(t, app, http.MethodPost, "/posts/"+postID.String()+"/comments", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCommentHandler_Create_MissingContent_ReturnsBadRequest(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	app := newTestApp()
	postID := uuid.New()
	app.Post("/posts/:postId/comments", h.Create)

	resp := doReq(t, app, http.MethodPost, "/posts/"+postID.String()+"/comments", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CommentHandler.Delete tests
// ---------------------------------------------------------------------------

func TestCommentHandler_Delete_InvalidID_ReturnsBadRequest(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)
	userID := uuid.New()

	app := newTestApp()
	app.Delete("/comments/:id", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{"admin"})
		return c.Next()
	}, h.Delete)

	resp := doReq(t, app, http.MethodDelete, "/comments/not-a-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCommentHandler_Delete_NoAuth_ReturnsUnauthorized(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	app := newTestApp()
	// No auth middleware — userID nil
	app.Delete("/comments/:id", h.Delete)

	resp := doReq(t, app, http.MethodDelete, "/comments/"+uuid.New().String(), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CommentHandler.SetUserLookup tests
// ---------------------------------------------------------------------------

func TestCommentHandler_SetUserLookup(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	called := false
	h.SetUserLookup(func(ctx context.Context, userID uuid.UUID) (*domain.PostAuthor, error) {
		called = true
		return &domain.PostAuthor{ID: userID, Name: "Test"}, nil
	})

	if h.userLookup == nil {
		t.Fatal("expected userLookup to be set")
	}
	// We set it, that's the test — it shouldn't panic
	_ = called
}

// ---------------------------------------------------------------------------
// CommentHandler.enrichCommentAuthor tests
// ---------------------------------------------------------------------------

func TestCommentHandler_enrichCommentAuthor_Guest(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	resp := &domain.CommentResponse{
		ID:        uuid.New(),
		PostID:    uuid.New(),
		AuthorID:  nil,
		GuestName: "Guest User",
	}

	h.enrichCommentAuthor(context.Background(), resp)

	if resp.Author == nil {
		t.Fatal("expected author to be set for guest")
	}
	author, ok := resp.Author.(*domain.CommentAuthor)
	if !ok {
		t.Fatal("expected Author to be *domain.CommentAuthor")
	}
	if !author.IsGuest {
		t.Fatal("expected IsGuest to be true")
	}
	if author.Name != "Guest User" {
		t.Fatalf("expected name 'Guest User', got %q", author.Name)
	}
}

func TestCommentHandler_enrichCommentAuthor_WithUserLookup(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	authorID := uuid.New()
	h.SetUserLookup(func(ctx context.Context, userID uuid.UUID) (*domain.PostAuthor, error) {
		return &domain.PostAuthor{ID: userID, Name: "Test Author"}, nil
	})

	resp := &domain.CommentResponse{
		ID:       uuid.New(),
		PostID:   uuid.New(),
		AuthorID: &authorID,
	}

	h.enrichCommentAuthor(context.Background(), resp)

	if resp.Author == nil {
		t.Fatal("expected author to be set")
	}
	author, ok := resp.Author.(*domain.CommentAuthor)
	if !ok {
		t.Fatal("expected Author to be *domain.CommentAuthor")
	}
	if author.IsGuest {
		t.Fatal("expected IsGuest to be false")
	}
	if author.Name != "Test Author" {
		t.Fatalf("expected name 'Test Author', got %q", author.Name)
	}
}

func TestCommentHandler_enrichCommentAuthor_RecursesChildren(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	resp := &domain.CommentResponse{
		ID:        uuid.New(),
		PostID:    uuid.New(),
		AuthorID:  nil,
		GuestName: "Parent Guest",
		Children: []domain.CommentResponse{
			{
				ID:        uuid.New(),
				PostID:    uuid.New(),
				AuthorID:  nil,
				GuestName: "Child Guest",
			},
		},
	}

	h.enrichCommentAuthor(context.Background(), resp)

	if resp.Author == nil {
		t.Fatal("expected parent author to be set")
	}
	if len(resp.Children) != 1 {
		t.Fatal("expected 1 child")
	}
	if resp.Children[0].Author == nil {
		t.Fatal("expected child author to be set")
	}
}

// ---------------------------------------------------------------------------
// CommentHandler.RegisterRoutes tests
// ---------------------------------------------------------------------------

func TestCommentHandler_RegisterRoutes(t *testing.T) {
	commentRepo := &commentRepoStub{}
	postRepo := &postRepoStub{}
	cfg := newAdminTestCfg()
	commentSvc := newCommentSvc(cfg, commentRepo, postRepo)
	h := NewCommentHandler(commentSvc)

	app := newTestApp()
	blog := app.Group("/blog")
	authMw := func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	}
	h.RegisterRoutes(blog, authMw)

	// Verify route is registered by checking a non-UUID postId returns 400 (bad request)
	// rather than 404 (not found). A 400 means the route IS registered but the ID is invalid.
	resp := doReq(t, app, http.MethodGet, "/blog/posts/not-a-uuid/comments", "")
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("expected route to be registered, got 404")
	}
}
