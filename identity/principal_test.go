package identity_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/identity"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	authmw "github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/test"
)

// newTestApp mirrors the auth middleware tests: a fiber app whose error
// handler renders ProblemDetail responses so auth failures surface as their
// real status codes instead of 500.
func newTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

// capture registers a protected route that records what FromContext returns
// inside the request. The returned pointers are read after app.Test, which
// runs the handler synchronously in this process.
func capture(app *fiber.App, path string, mw fiber.Handler) (**identity.Principal, *bool) {
	var got *identity.Principal
	var ok bool

	app.Get(path, mw, func(c fiber.Ctx) error {
		got, ok = identity.FromContext(c)
		return c.SendStatus(fiber.StatusOK)
	})

	return &got, &ok
}

func doRequest(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()

	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

// TestFromContext_JWTPath drives the real auth middleware with a real token
// minted by the token service. Locals are never written by the test, so a
// change to the middleware's local keys breaks this test.
func TestFromContext_JWTPath(t *testing.T) {
	cfg := test.TestConfig()
	tokenSvc := service.NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	user.Roles = []domain.Role{
		{
			Name: "admin",
			Permissions: []domain.Permission{
				{Name: "users:read"},
			},
		},
	}

	token, _, err := tokenSvc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to mint access token: %v", err)
	}

	mw := authmw.New(tokenSvc, nil, nil)
	app := newTestApp()
	got, ok := capture(app, "/private", mw.Handle)

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if resp := doRequest(t, app, req); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if !*ok {
		t.Fatalf("expected FromContext to report ok=true")
	}
	p := *got
	if p == nil {
		t.Fatalf("expected a principal, got nil")
	}
	if p.UserID != user.ID {
		t.Errorf("UserID = %s, want %s", p.UserID, user.ID)
	}
	if p.Username != user.Username {
		t.Errorf("Username = %q, want %q", p.Username, user.Username)
	}
	if p.Email != user.Email {
		t.Errorf("Email = %q, want %q", p.Email, user.Email)
	}
	if len(p.Roles) != 1 || p.Roles[0] != "admin" {
		t.Errorf("Roles = %v, want [admin]", p.Roles)
	}
	if len(p.Permissions) != 1 || p.Permissions[0] != "users:read" {
		t.Errorf("Permissions = %v, want [users:read]", p.Permissions)
	}
	if p.AuthMethod != "jwt" {
		t.Errorf("AuthMethod = %q, want %q", p.AuthMethod, "jwt")
	}
}

// TestFromContext_APIKeyPath drives the middleware's API-key branch through
// the real APIKeyService (real hash comparison, real validity check and real
// setClaimsLocals). Only the persistence layer is stubbed: the GORM API-key
// repository needs PostgreSQL-specific DDL, and CI has no database — the
// database-backed variant of this path is exercised by the server-level
// harness. The test still never writes locals itself.
//
// The key carries no roles, which is exactly the "authenticated but
// unauthorized" case: GetRoleNames returns an empty slice, GetPermissionNames
// returns nil, and the user repository is absent so username/email stay
// empty. All of that must still yield ok=true.
func TestFromContext_APIKeyPath(t *testing.T) {
	const rawKey = "gc_test_api_key_value"

	userID := uuid.New()
	repo := &apiKeyRepoStub{
		keys: map[string]*domain.APIKey{
			domain.HashAPIKey(rawKey): {
				ID:        uuid.New(),
				UserID:    userID,
				KeyHash:   domain.HashAPIKey(rawKey),
				KeyPrefix: "gc_test_",
				Name:      "test key",
			},
		},
	}

	cfg := test.TestConfig()
	mw := authmw.New(service.NewTokenService(cfg), service.NewAPIKeyService(repo, nil, nil), nil)

	app := newTestApp()
	got, ok := capture(app, "/private", mw.Handle)

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("X-API-Key", rawKey)
	if resp := doRequest(t, app, req); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if !*ok {
		t.Fatalf("expected FromContext to report ok=true")
	}
	p := *got
	if p == nil {
		t.Fatalf("expected a principal, got nil")
	}
	if p.UserID != userID {
		t.Errorf("UserID = %s, want %s", p.UserID, userID)
	}
	if p.AuthMethod != "api_key" {
		t.Errorf("AuthMethod = %q, want %q", p.AuthMethod, "api_key")
	}
	if p.Username != "" || p.Email != "" {
		t.Errorf("expected empty user info, got username=%q email=%q", p.Username, p.Email)
	}
	if len(p.Roles) != 0 {
		t.Errorf("Roles = %v, want empty", p.Roles)
	}
	if len(p.Permissions) != 0 {
		t.Errorf("Permissions = %v, want empty", p.Permissions)
	}
}

// The three cases below assert FromContext's own tolerance rather than
// middleware integration, so they set locals directly.

func TestFromContext_NoLocals(t *testing.T) {
	app := newTestApp()
	got, ok := capture(app, "/open", func(c fiber.Ctx) error { return c.Next() })

	if resp := doRequest(t, app, httptest.NewRequest(http.MethodGet, "/open", nil)); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if *ok {
		t.Errorf("expected ok=false without locals")
	}
	if *got != nil {
		t.Errorf("expected nil principal, got %+v", *got)
	}
}

func TestFromContext_WrongUserIDType(t *testing.T) {
	app := newTestApp()
	got, ok := capture(app, "/open", func(c fiber.Ctx) error {
		c.Locals("userID", "not-a-uuid")
		c.Locals("username", "testuser")
		return c.Next()
	})

	if resp := doRequest(t, app, httptest.NewRequest(http.MethodGet, "/open", nil)); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if *ok {
		t.Errorf("expected ok=false for a wrongly typed userID")
	}
	if *got != nil {
		t.Errorf("expected nil principal, got %+v", *got)
	}
}

func TestFromContext_NilUUIDUserIDIsRejected(t *testing.T) {
	app := newTestApp()
	got, ok := capture(app, "/open", func(c fiber.Ctx) error {
		c.Locals("userID", uuid.Nil)
		c.Locals("username", "testuser")
		return c.Next()
	})

	if resp := doRequest(t, app, httptest.NewRequest(http.MethodGet, "/open", nil)); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if *ok {
		t.Errorf("expected ok=false for a zero-UUID userID: no authenticated principal is ever the zero UUID")
	}
	if *got != nil {
		t.Errorf("expected nil principal, got %+v", *got)
	}
}

func TestFromContext_EmptyRolesAndPermissions(t *testing.T) {
	userID := uuid.New()

	app := newTestApp()
	got, ok := capture(app, "/open", func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		c.Locals("roles", []string{})
		c.Locals("permissions", []string{})
		c.Locals("authMethod", "jwt")
		return c.Next()
	})

	if resp := doRequest(t, app, httptest.NewRequest(http.MethodGet, "/open", nil)); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if !*ok {
		t.Fatalf("expected ok=true: an authenticated user with no roles is legitimate")
	}
	p := *got
	if p.UserID != userID {
		t.Errorf("UserID = %s, want %s", p.UserID, userID)
	}
	if len(p.Roles) != 0 || len(p.Permissions) != 0 {
		t.Errorf("expected empty roles/permissions, got %v / %v", p.Roles, p.Permissions)
	}
	if p.Username != "" || p.Email != "" {
		t.Errorf("expected zero-valued strings, got username=%q email=%q", p.Username, p.Email)
	}
}

// apiKeyRepoStub serves API keys from an in-memory map keyed by key hash.
type apiKeyRepoStub struct {
	keys map[string]*domain.APIKey
}

var _ repository.APIKeyRepository = (*apiKeyRepoStub)(nil)

func (s *apiKeyRepoStub) GetByHashWithRoles(_ context.Context, keyHash string) (*domain.APIKey, error) {
	if key, ok := s.keys[keyHash]; ok {
		return key, nil
	}
	return nil, coreerrors.NewNotFound("API Key", keyHash)
}

func (s *apiKeyRepoStub) GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	return s.GetByHashWithRoles(ctx, keyHash)
}

func (s *apiKeyRepoStub) Create(_ context.Context, _ *domain.APIKey) error { return nil }

func (s *apiKeyRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domain.APIKey, error) {
	return nil, coreerrors.NewNotFound("API Key", id.String())
}

func (s *apiKeyRepoStub) GetByIDWithRoles(ctx context.Context, id uuid.UUID) (*domain.APIKey, error) {
	return s.GetByID(ctx, id)
}

func (s *apiKeyRepoStub) GetUserKeys(_ context.Context, _ uuid.UUID) ([]*domain.APIKey, error) {
	return nil, nil
}

func (s *apiKeyRepoStub) GetUserKeysPaginated(_ context.Context, _ uuid.UUID, _, _ int) ([]*domain.APIKey, int64, error) {
	return nil, 0, nil
}

func (s *apiKeyRepoStub) GetAll(_ context.Context, _, _ int) ([]*domain.APIKey, int64, error) {
	return nil, 0, nil
}

func (s *apiKeyRepoStub) Revoke(_ context.Context, _ uuid.UUID) error { return nil }

func (s *apiKeyRepoStub) UpdateLastUsed(_ context.Context, _ uuid.UUID) error { return nil }

func (s *apiKeyRepoStub) CleanupRevokedKeys(_ context.Context, _ time.Duration) error { return nil }

func (s *apiKeyRepoStub) AssignRole(_ context.Context, _, _ uuid.UUID) error { return nil }

func (s *apiKeyRepoStub) RemoveRole(_ context.Context, _, _ uuid.UUID) error { return nil }
