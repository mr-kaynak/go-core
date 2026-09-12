package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// ── fakes ────────────────────────────────────────────────────────────────

// fakeStore implements authorization.PolicyStore with failure injection.
type fakeStore struct {
	mu       sync.Mutex
	policies map[string]bool
	addErr   map[string]error
	remErr   map[string]error
}

func newFakeStore() *fakeStore {
	return &fakeStore{policies: map[string]bool{}, addErr: map[string]error{}, remErr: map[string]error{}}
}

func pkey(sub, dom, obj string, act authorization.Action) string {
	return fmt.Sprintf("%s|%s|%s|%s", sub, dom, obj, act)
}

func (f *fakeStore) AddPolicy(sub, dom, obj string, act authorization.Action, eft string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := pkey(sub, dom, obj, act)
	if err := f.addErr[k]; err != nil {
		return err
	}
	if f.policies[k] {
		return coreerrors.NewConflict("policy already exists")
	}
	f.policies[k] = true
	return nil
}

func (f *fakeStore) RemovePolicy(sub, dom, obj string, act authorization.Action, eft string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := pkey(sub, dom, obj, act)
	if err := f.remErr[k]; err != nil {
		return err
	}
	if !f.policies[k] {
		return coreerrors.NewNotFound("policy", "not found")
	}
	delete(f.policies, k)
	return nil
}

func (f *fakeStore) ListPolicies() ([][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, 0, len(f.policies))
	for k := range f.policies {
		var sub, dom, obj, act string
		parts := []rune(k)
		_ = parts
		// simple split on '|'
		fields := make([]string, 0, 4)
		start := 0
		for i, c := range k {
			if c == '|' {
				fields = append(fields, k[start:i])
				start = i + 1
			}
		}
		fields = append(fields, k[start:])
		sub, dom, obj, act = fields[0], fields[1], fields[2], fields[3]
		out = append(out, []string{sub, dom, obj, act, "allow"})
	}
	return out, nil
}

func (f *fakeStore) hasEditorReadPolicy(obj string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.policies[pkey("role:editor", authorization.DomainDefault, obj, authorization.ActionRead)]
}

// Stub repos: embed the interface, override only what the sync path uses.
type stubPermRepo struct {
	repository.PermissionRepository
	perm        *domain.Permission
	addErr      error
	removeErr   error
	addCalls    int
	assignments []authorization.RoleAssignment
}

func (s *stubPermRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Permission, error) {
	return s.perm, nil
}
func (s *stubPermRepo) AddPermissionToRole(_ context.Context, _, _ uuid.UUID) error {
	s.addCalls++
	return s.addErr
}
func (s *stubPermRepo) RemovePermissionFromRole(_ context.Context, _, _ uuid.UUID) error {
	return s.removeErr
}
func (s *stubPermRepo) GetAllRoleAssignments(_ context.Context) ([]authorization.RoleAssignment, error) {
	return s.assignments, nil
}

type stubRoleRepo struct {
	repository.RoleRepository
	role *domain.Role
}

func (s *stubRoleRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Role, error) {
	return s.role, nil
}

// ── fixtures ─────────────────────────────────────────────────────────────

func ordersRegistry(t *testing.T) *authorization.PermissionRegistry {
	t.Helper()
	r := authorization.NewPermissionRegistry()
	if err := r.Register(authorization.PermissionDef{
		Name:    "orders.view",
		Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
		Action:  authorization.ActionRead,
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func syncFixture(t *testing.T) (*PermissionService, *fakeStore, *stubPermRepo) {
	t.Helper()
	store := newFakeStore()
	permRepo := &stubPermRepo{perm: &domain.Permission{Name: "orders.view"}}
	roleRepo := &stubRoleRepo{role: &domain.Role{Name: "editor"}}
	svc := newPermissionServiceWithStore(permRepo, roleRepo, store, ordersRegistry(t))
	return svc, store, permRepo
}

const (
	dom = authorization.DomainDefault
)

// ── grant ────────────────────────────────────────────────────────────────

func TestGrant_WritesAllObjectsAndSucceeds(t *testing.T) {
	svc, store, _ := syncFixture(t)

	if err := svc.AddPermissionToRole(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !store.hasEditorReadPolicy("/api/v1/orders") ||
		!store.hasEditorReadPolicy("/api/v1/orders/*") {
		t.Fatal("grant must add policies for ALL objects of the permission")
	}
}

func TestGrant_SecondObjectFailure_RollsBackAndErrors(t *testing.T) {
	svc, store, _ := syncFixture(t)
	store.addErr[pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)] = errors.New("adapter down")

	err := svc.AddPermissionToRole(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("partial casbin failure must surface as an API error")
	}
	if store.hasEditorReadPolicy("/api/v1/orders") {
		t.Fatal("successfully-added first object must be rolled back best-effort")
	}
}

func TestGrant_RetryAfterPartialFailure_Converges(t *testing.T) {
	svc, store, permRepo := syncFixture(t)
	failKey := pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)
	store.addErr[failKey] = errors.New("adapter down")

	if err := svc.AddPermissionToRole(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("first attempt must fail")
	}

	// Adapter recovers; the DB row from attempt one may still exist (repo is
	// idempotent). Retry must repair Casbin without requiring a resync.
	delete(store.addErr, failKey)
	permRepo.addErr = nil // idempotent DB layer reports success on re-grant
	if err := svc.AddPermissionToRole(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("retry must converge, got: %v", err)
	}
	if !store.hasEditorReadPolicy("/api/v1/orders") ||
		!store.hasEditorReadPolicy("/api/v1/orders/*") {
		t.Fatal("after retry, all objects must be enforced")
	}
}

// ── revoke ───────────────────────────────────────────────────────────────

func seedGranted(store *fakeStore) {
	store.policies[pkey("role:editor", dom, "/api/v1/orders", authorization.ActionRead)] = true
	store.policies[pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)] = true
}

func TestRevoke_CasbinFirst_RemovesAllThenDB(t *testing.T) {
	svc, store, _ := syncFixture(t)
	seedGranted(store)

	if err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if store.hasEditorReadPolicy("/api/v1/orders") ||
		store.hasEditorReadPolicy("/api/v1/orders/*") {
		t.Fatal("revoke must remove all object policies")
	}
}

func TestRevoke_AlreadyAbsentObjectsAreSuccess(t *testing.T) {
	svc, store, _ := syncFixture(t)
	// Only one of the two policies exists (partially rolled-back earlier grant).
	store.policies[pkey("role:editor", dom, "/api/v1/orders", authorization.ActionRead)] = true

	if err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("already-absent object must not fail a revoke: %v", err)
	}
}

func TestRevoke_PartialCasbinFailure_RestoresAndErrors(t *testing.T) {
	svc, store, _ := syncFixture(t)
	seedGranted(store)
	store.remErr[pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)] = errors.New("adapter down")

	err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("partial casbin removal failure must surface as an API error")
	}
	if !store.hasEditorReadPolicy("/api/v1/orders") {
		t.Fatal("removed policy must be restored best-effort so enforcement matches DB intent")
	}
}

func TestRevoke_DBFailureAfterCasbin_ReturnsError(t *testing.T) {
	svc, store, permRepo := syncFixture(t)
	seedGranted(store)
	permRepo.removeErr = errors.New("db down")

	err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("DB failure after casbin removal must surface as an API error")
	}
}

func TestRevoke_DBFailureRetry_Converges(t *testing.T) {
	svc, store, permRepo := syncFixture(t)
	seedGranted(store)
	permRepo.removeErr = errors.New("db down")

	if err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("first attempt must fail")
	}
	permRepo.removeErr = nil
	// Casbin policies are already gone; retry must treat that as success and
	// finish the DB removal.
	if err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("retry must converge, got: %v", err)
	}
}

// ── resync ───────────────────────────────────────────────────────────────

func TestServiceResync_RemovesStaleUsingDBAssignments(t *testing.T) {
	svc, store, permRepo := syncFixture(t)
	// Casbin has a grant the DB does not know about.
	seedGranted(store)
	permRepo.assignments = nil

	added, removed, err := svc.ResyncPolicies(context.Background())
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if added != 0 || removed != 2 {
		t.Fatalf("added=%d removed=%d, want 0/2", added, removed)
	}
}

func TestGrantAndResync_AreSerialized(t *testing.T) {
	svc, store, permRepo := syncFixture(t)
	permRepo.assignments = []authorization.RoleAssignment{{RoleName: "editor", PermissionName: "orders.view"}}

	// Run grant and resync concurrently under -race; the shared mutex must
	// serialize them so the final state is the fully-granted set.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = svc.AddPermissionToRole(context.Background(), uuid.New(), uuid.New()) }()
	go func() { defer wg.Done(); _, _, _ = svc.ResyncPolicies(context.Background()) }()
	wg.Wait()

	if !store.hasEditorReadPolicy("/api/v1/orders") ||
		!store.hasEditorReadPolicy("/api/v1/orders/*") {
		t.Fatal("after concurrent grant+resync with a granting DB state, all policies must be present")
	}
}

func TestGrant_PreexistingPolicySurvivesFailedGrantRollback(t *testing.T) {
	svc, store, _ := syncFixture(t)
	// The collection policy ALREADY exists (e.g. from an earlier partial
	// grant or resync); the item policy write will fail.
	store.policies[pkey("role:editor", dom, "/api/v1/orders", authorization.ActionRead)] = true
	store.addErr[pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)] = errors.New("adapter down")

	if err := svc.AddPermissionToRole(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("grant must fail")
	}
	// Rollback must reverse only what THIS call changed — the pre-existing
	// policy was a no-op for this call and must remain.
	if !store.hasEditorReadPolicy("/api/v1/orders") {
		t.Fatal("rollback removed a pre-existing policy it did not create")
	}
}

func TestRevoke_PreexistinglyAbsentPolicyNotRestoredOnFailedRevoke(t *testing.T) {
	svc, store, _ := syncFixture(t)
	// Only the item policy exists; the collection policy is already absent.
	store.policies[pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)] = true
	store.remErr[pkey("role:editor", dom, "/api/v1/orders/*", authorization.ActionRead)] = errors.New("adapter down")

	if err := svc.RemovePermissionFromRole(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("revoke must fail")
	}
	// The collection policy was absent BEFORE this call (its removal was a
	// no-op); rollback must not create it.
	if store.hasEditorReadPolicy("/api/v1/orders") {
		t.Fatal("rollback created a policy that never existed before the call")
	}
}
