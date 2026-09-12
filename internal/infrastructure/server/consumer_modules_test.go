package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/identity"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	identityDomain "github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
	"github.com/mr-kaynak/go-core/internal/test/sqliteschema"
	"gorm.io/gorm"
)

// ordersTestModule is the reference consumer module: one permission covering
// collection + item patterns, protected routes, and a protected sibling used
// to prove denial is authorization (403), not routing (404).
type ordersTestModule struct {
	registerErr error
}

func (m *ordersTestModule) Name() string { return "orders" }

func (m *ordersTestModule) Permissions() []modcontract.Permission {
	return []modcontract.Permission{{
		Name:    "orders.view",
		Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
		Action:  authorization.ActionRead,
	}, {
		Name:    "ordersextra.view",
		Objects: []string{"/api/v1/ordersextra", "/api/v1/ordersextra/*"},
		Action:  authorization.ActionRead,
	}}
}

func (m *ordersTestModule) Register(mctx *modcontract.ModuleContext) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	handler := func(c fiber.Ctx) error {
		p, ok := identity.FromContext(c)
		if !ok {
			return fiber.ErrUnauthorized
		}
		return c.JSON(fiber.Map{"user_id": p.UserID.String()})
	}
	orders := mctx.Router.Group("/orders", mctx.Auth, mctx.Authz)
	orders.Get("/", handler)
	orders.Get("/:id", handler)

	// Registered, protected sibling: a 403 here proves pattern denial;
	// an unregistered path's 404 would prove nothing.
	extra := mctx.Router.Group("/ordersextra", mctx.Auth, mctx.Authz)
	extra.Get("/", handler)
	return nil
}

type consumerHarness struct {
	srv      *AppServer
	permSvc  *identityService.PermissionService
	roleID   uuid.UUID
	permID   uuid.UUID
	password string
	email    string
}

func newConsumerHarness(t *testing.T, mod modcontract.Module) *consumerHarness {
	t.Helper()

	dsn := fmt.Sprintf("file:consumer_%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqliteschema.ApplyIdentity(gdb); err != nil {
		t.Fatalf("identity schema: %v", err)
	}
	db := &database.DB{DB: gdb}

	casbinSvc, err := authorization.NewTestCasbinService()
	if err != nil {
		t.Fatalf("casbin: %v", err)
	}
	registry := authorization.NewPermissionRegistry()

	cfg := newIntegrationConfig()
	cfg.JWT.Expiry = 15 * time.Minute
	cfg.JWT.RefreshExpiry = time.Hour
	srv, err := New(cfg, db, nil, nil, casbinSvc,
		WithConsumerModules(mod),
		WithPermissionRegistry(registry),
	)
	if err != nil {
		t.Fatalf("server.New with consumer module: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.StopNotifications(ctx)
		srv.StopSSE(ctx)
		_ = srv.Shutdown()
	})

	// Seed: active verified user with a custom role (NOT system_admin — the
	// wildcard would mask the pattern mapping under test).
	h := &consumerHarness{
		srv:      srv,
		email:    "editor@example.com",
		password: "orders-editor-pass-1",
		roleID:   uuid.New(),
		permID:   uuid.New(),
	}
	user := &identityDomain.User{Email: h.email, Username: "orderseditor", Status: identityDomain.UserStatusActive, Verified: true}
	if err := user.SetPassword(h.password); err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	userID := uuid.New()
	mustExec(t, gdb, `INSERT INTO users (id, email, username, password, status, verified, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`, userID.String(), h.email, user.Username, user.Password, string(user.Status), now, now)
	mustExec(t, gdb, `INSERT INTO roles (id, name, created_at, updated_at) VALUES (?, 'orders_editor', ?, ?)`, h.roleID.String(), now, now)
	mustExec(t, gdb, `INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`, userID.String(), h.roleID.String())
	mustExec(t, gdb, `INSERT INTO permissions (id, name, created_at, updated_at) VALUES (?, 'orders.view', ?, ?)`, h.permID.String(), now, now)

	// PermissionService on the SAME casbin enforcer + registry the server
	// enforces with: runtime grant/revoke must take effect without restart.
	h.permSvc = identityService.NewPermissionService(
		identityRepo.NewPermissionRepository(gdb),
		identityRepo.NewRoleRepository(gdb),
		casbinSvc,
		registry,
	)
	return h
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func (h *consumerHarness) login(t *testing.T) string {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, h.email, h.password)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.srv.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("login decode: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatal("login returned empty access token")
	}
	return out.AccessToken
}

func (h *consumerHarness) get(t *testing.T, path, token string) int {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.srv.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestConsumerModule_EndToEnd is the K1b acceptance test: a consumer module
// registered through WithConsumerModules serves protected routes whose
// authorization is driven by the module's own registered permission, granted
// and revoked at runtime through the permission service — no core source
// edits, no restarts, no system_admin shortcuts.
func TestConsumerModule_EndToEnd(t *testing.T) {
	h := newConsumerHarness(t, &ordersTestModule{})
	token := h.login(t)
	ctx := context.Background()

	// Before any grant: authenticated but unauthorized.
	if code := h.get(t, "/api/v1/orders", token); code != http.StatusForbidden {
		t.Fatalf("pre-grant collection: got %d, want 403", code)
	}

	// Runtime grant through the permission API path.
	if err := h.permSvc.AddPermissionToRole(ctx, h.roleID, h.permID); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if code := h.get(t, "/api/v1/orders", token); code != http.StatusOK {
		t.Fatalf("post-grant collection: got %d, want 200", code)
	}
	if code := h.get(t, "/api/v1/orders/"+uuid.NewString(), token); code != http.StatusOK {
		t.Fatalf("post-grant item: got %d, want 200", code)
	}
	// Registered protected sibling must stay FORBIDDEN (not 404): the grant
	// covers exactly the declared patterns.
	if code := h.get(t, "/api/v1/ordersextra", token); code != http.StatusForbidden {
		t.Fatalf("sibling: got %d, want 403", code)
	}

	// Runtime revoke: enforcement drops without restart.
	if err := h.permSvc.RemovePermissionFromRole(ctx, h.roleID, h.permID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if code := h.get(t, "/api/v1/orders", token); code != http.StatusForbidden {
		t.Fatalf("post-revoke collection: got %d, want 403", code)
	}
}

// TestConsumerModule_RegisterErrorFailsNew: a failing module must fail server
// construction — a half-registered application never starts.
func TestConsumerModule_RegisterErrorFailsNew(t *testing.T) {
	cfg := newIntegrationConfig()
	db := newTestDB(t)
	casbinSvc, err := authorization.NewTestCasbinService()
	if err != nil {
		t.Fatalf("casbin: %v", err)
	}

	boom := errors.New("module exploded")
	_, err = New(cfg, db, nil, nil, casbinSvc,
		WithConsumerModules(&ordersTestModule{registerErr: boom}),
		WithPermissionRegistry(authorization.NewPermissionRegistry()),
	)
	if err == nil {
		t.Fatal("expected New to fail when a consumer module's Register fails")
	}
	if !strings.Contains(err.Error(), "orders") {
		t.Fatalf("error should name the failing module, got: %v", err)
	}
}

// TestConsumerModule_PermissionConflictFailsNew: module permissions clashing
// with core (or each other) abort construction via registry validation.
func TestConsumerModule_PermissionConflictFailsNew(t *testing.T) {
	cfg := newIntegrationConfig()
	db := newTestDB(t)
	casbinSvc, err := authorization.NewTestCasbinService()
	if err != nil {
		t.Fatalf("casbin: %v", err)
	}

	conflicting := &conflictingModule{}
	_, err = New(cfg, db, nil, nil, casbinSvc,
		WithConsumerModules(conflicting),
		WithPermissionRegistry(authorization.NewPermissionRegistry()),
	)
	if err == nil {
		t.Fatal("expected New to fail on a core-conflicting permission name")
	}
}

type conflictingModule struct{}

func (m *conflictingModule) Name() string { return "conflicting" }
func (m *conflictingModule) Permissions() []modcontract.Permission {
	return []modcontract.Permission{{
		Name:    "users.view", // collides with core
		Objects: []string{"/api/v1/whatever"},
		Action:  authorization.ActionRead,
	}}
}
func (m *conflictingModule) Register(_ *modcontract.ModuleContext) error { return nil }

// TestNew_DevelopmentWithoutDocsSpec_DoesNotPanic: a consumer application has
// no docs/openapi.json checkout; development-mode construction must skip the
// docs UI gracefully instead of panicking inside the scalar library.
func TestNew_DevelopmentWithoutDocsSpec_DoesNotPanic(t *testing.T) {
	cfg := newIntegrationConfig()
	cfg.App.Env = "development"
	db := newTestDB(t)

	srv, err := New(cfg, db, nil, nil, nil)
	if err != nil {
		t.Fatalf("New in development without docs/ must construct: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.StopNotifications(ctx)
		srv.StopSSE(ctx)
		_ = srv.Shutdown()
	})
}
