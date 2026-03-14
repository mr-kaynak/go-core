package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

type ApiIntegrations struct {
	App      *fiber.App
	PostID   uuid.UUID
	PostSlug string
	AuthorID uuid.UUID
}

func setupFullIntegrationApp() ApiIntegrations {
	db, _ := service.SetupTestEnv()

	postRepo := repository.NewPostRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tagRepo := repository.NewTagRepository(db)
	engRepo := repository.NewEngagementRepository(db)
	commentRepo := repository.NewCommentRepository(db)

	contentSvc := service.NewContentService()
	slugSvc := service.NewSlugService()
	readTimeSvc := service.NewReadTimeService(200)

	cfg := &config.Config{
		Blog: config.BlogConfig{SiteURL: "https://example.com"},
	}

	postSvc := service.NewPostService(db, postRepo, catRepo, tagRepo, contentSvc, slugSvc, readTimeSvc)
	catSvc := service.NewCategoryService(catRepo, slugSvc)
	engSvc := service.NewEngagementService(db, cfg, engRepo, postRepo)
	engSvc.SetMetrics(metrics.NoOpMetrics{})
	commentSvc := service.NewCommentService(cfg, commentRepo, postRepo)
	seoSvc := service.NewSEOService(cfg)

	postSvc.SetEngagementRepo(engRepo)

	postH := NewPostHandler(postSvc, 10)
	catH := NewCategoryHandler(catSvc)
	engH := NewEngagementHandler(engSvc)
	commentH := NewCommentHandler(commentSvc)
	seoH := NewSEOHandler(seoSvc, postSvc)

	app := newTestApp()

	authorID := uuid.New()
	authMw := func(c fiber.Ctx) error {
		c.Locals("userID", authorID)
		c.Locals("roles", []string{"admin"})
		c.Locals("claims", &identityService.Claims{
			UserID: authorID,
			Roles:  []string{"admin"},
		})
		return c.Next()
	}

	blogGrp := app.Group("/blog")
	postH.RegisterRoutes(blogGrp, authMw)
	catH.RegisterRoutes(blogGrp, authMw)
	engH.RegisterRoutes(blogGrp, authMw)
	commentH.RegisterRoutes(blogGrp, authMw)
	seoH.RegisterRoutes(blogGrp)

	// Pre-create a post with unique slug to avoid conflicts across shared DB
	postID := uuid.New()
	post := &domain.Post{
		ID:       postID,
		Title:    "Seeded Post",
		Slug:     "seeded-post-" + postID.String()[:8],
		AuthorID: authorID,
		Status:   domain.PostStatusPublished,
	}
	postRepo.Create(post)

	return ApiIntegrations{
		App:      app,
		PostID:   post.ID,
		PostSlug: post.Slug,
		AuthorID: authorID,
	}
}

func doJSONReq(t *testing.T, app *fiber.App, method, url string, body interface{}) *http.Response {
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	req := httptest.NewRequest(method, url, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestAPI_Categories(t *testing.T) {
	apiInfo := setupFullIntegrationApp()
	app := apiInfo.App

	// Create
	reqBody := service.CreateCategoryRequest{
		Name: "Test Category",
	}
	resp := doJSONReq(t, app, http.MethodPost, "/blog/categories", reqBody)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Create Category expected 201, got %d", resp.StatusCode)
	}
	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	catMap := res["category"].(map[string]interface{})
	catID := catMap["id"].(string)

	// Update
	newName := "Updated Cat"
	updateReq := service.UpdateCategoryRequest{Name: &newName}
	resp = doJSONReq(t, app, http.MethodPut, "/blog/categories/"+catID, updateReq)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Update Category expected 200, got %d", resp.StatusCode)
	}

	// Get Tree
	resp = doJSONReq(t, app, http.MethodGet, "/blog/categories", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GetTree Category expected 200, got %d", resp.StatusCode)
	}

	// Delete
	resp = doJSONReq(t, app, http.MethodDelete, "/blog/categories/"+catID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete Category expected 204, got %d", resp.StatusCode)
	}
}

func TestAPI_Engagements(t *testing.T) {
	apiInfo := setupFullIntegrationApp()
	app := apiInfo.App
	postID := apiInfo.PostID.String()

	// Toggle Like
	resp := doJSONReq(t, app, http.MethodPost, "/blog/posts/"+postID+"/like", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("ToggleLike expected 200, got %d", resp.StatusCode)
	}

	// Is Liked
	resp = doJSONReq(t, app, http.MethodGet, "/blog/posts/"+postID+"/like", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("IsLiked expected 200, got %d", resp.StatusCode)
	}

	// Record View
	resp = doJSONReq(t, app, http.MethodPost, "/blog/posts/"+postID+"/view", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("RecordView expected 204, got %d", resp.StatusCode)
	}

	// Record Share
	shareReq := map[string]string{"platform": "twitter"}
	resp = doJSONReq(t, app, http.MethodPost, "/blog/posts/"+postID+"/share", shareReq)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("RecordShare expected 204, got %d", resp.StatusCode)
	}

	// Get Stats
	resp = doJSONReq(t, app, http.MethodGet, "/blog/posts/"+postID+"/stats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GetStats expected 200, got %d", resp.StatusCode)
	}
}

func TestAPI_Comments(t *testing.T) {
	apiInfo := setupFullIntegrationApp()
	app := apiInfo.App
	postID := apiInfo.PostID.String()

	// Create Comment (public route — no authMw, so guest fields required)
	commentReq := service.CreateCommentRequest{
		Content:    "Cool post!",
		GuestName:  "Test User",
		GuestEmail: "test@example.com",
	}
	resp := doJSONReq(t, app, http.MethodPost, "/blog/posts/"+postID+"/comments", commentReq)
	if resp.StatusCode != http.StatusCreated {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		t.Fatalf("Create Comment expected 201, got %d: %s", resp.StatusCode, buf.String())
	}

	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	if res["comment"] == nil {
		t.Fatalf("Expected comment object in response but it was nil")
	}
	commentMap := res["comment"].(map[string]interface{})
	if commentMap["id"] == nil {
		t.Fatalf("Expected comment id in response but it was nil")
	}
	commentID := commentMap["id"].(string)

	// Get Threaded
	resp = doJSONReq(t, app, http.MethodGet, "/blog/posts/"+postID+"/comments", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Get Threaded expected 200, got %d", resp.StatusCode)
	}

	// Delete
	resp = doJSONReq(t, app, http.MethodDelete, "/blog/comments/"+commentID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete Comment expected 204, got %d", resp.StatusCode)
	}
}

func TestAPI_SEO(t *testing.T) {
	apiInfo := setupFullIntegrationApp()
	app := apiInfo.App

	// Get Meta
	resp := doJSONReq(t, app, http.MethodGet, "/blog/posts/"+apiInfo.PostSlug+"/meta", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Get Meta expected 200, got %d", resp.StatusCode)
	}
}
