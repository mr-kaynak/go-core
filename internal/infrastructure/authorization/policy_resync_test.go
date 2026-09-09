package authorization

import (
	"errors"
	"fmt"
	"testing"

	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
)

// fakePolicyStore records policies as tuples and supports failure injection.
type fakePolicyStore struct {
	policies map[string]bool // key: sub|dom|obj|act|eft
	addErr   map[string]error
	remErr   map[string]error
	addCalls []string
	remCalls []string
}

func newFakePolicyStore() *fakePolicyStore {
	return &fakePolicyStore{
		policies: make(map[string]bool),
		addErr:   make(map[string]error),
		remErr:   make(map[string]error),
	}
}

func key(sub, dom, obj string, act Action, eft string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", sub, dom, obj, act, eft)
}

func (f *fakePolicyStore) seed(sub, dom, obj string, act Action, eft string) {
	f.policies[key(sub, dom, obj, act, eft)] = true
}

func (f *fakePolicyStore) AddPolicy(sub, dom, obj string, act Action, eft string) error {
	k := key(sub, dom, obj, act, eft)
	f.addCalls = append(f.addCalls, k)
	if err := f.addErr[k]; err != nil {
		return err
	}
	if f.policies[k] {
		return coreerrors.NewConflict("policy already exists")
	}
	f.policies[k] = true
	return nil
}

func (f *fakePolicyStore) RemovePolicy(sub, dom, obj string, act Action, eft string) error {
	k := key(sub, dom, obj, act, eft)
	f.remCalls = append(f.remCalls, k)
	if err := f.remErr[k]; err != nil {
		return err
	}
	if !f.policies[k] {
		return coreerrors.NewNotFound("policy", "policy not found")
	}
	delete(f.policies, k)
	return nil
}

func (f *fakePolicyStore) ListPolicies() ([][]string, error) {
	var out [][]string
	for k, present := range f.policies {
		if !present {
			continue
		}
		var sub, dom, obj, act, eft string
		fmt.Sscanf(k, "%s", &sub) // placeholder; replaced below
		_ = sub
		// parse by splitting on '|'
		parts := splitKey(k)
		sub, dom, obj, act, eft = parts[0], parts[1], parts[2], parts[3], parts[4]
		out = append(out, []string{sub, dom, obj, act, eft})
	}
	return out, nil
}

func splitKey(k string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			parts = append(parts, k[start:i])
			start = i + 1
		}
	}
	parts = append(parts, k[start:])
	return parts
}

func registryWithOrders(t *testing.T) *PermissionRegistry {
	t.Helper()
	r := NewPermissionRegistry()
	if err := r.Register(PermissionDef{
		Name:    "orders.view",
		Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
		Action:  ActionRead,
	}); err != nil {
		t.Fatalf("register orders.view: %v", err)
	}
	return r
}

func TestReservedDefaults_IncludeApiClientTuples(t *testing.T) {
	if !IsReservedDefaultPolicy("role:api_client", DomainDefault, string(ResourceNotification), ActionCreate) {
		t.Fatal("api_client notification-create must be a reserved default")
	}
	if !IsReservedDefaultPolicy("role:api_client", DomainDefault, string(ResourceTemplate), ActionRead) {
		t.Fatal("api_client template-read must be a reserved default")
	}
	if IsReservedDefaultPolicy("role:editor", DomainDefault, string(ResourceNotification), ActionCreate) {
		t.Fatal("a normal role policy must not be reserved")
	}
}

func TestResync_AddsMissingManagedPolicies(t *testing.T) {
	reg := registryWithOrders(t)
	store := newFakePolicyStore()

	added, removed, err := ResyncManagedPolicies(store, reg, []RoleAssignment{
		{RoleName: "editor", PermissionName: "orders.view"},
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if added != 2 || removed != 0 {
		t.Fatalf("added=%d removed=%d, want 2/0", added, removed)
	}
	if !store.policies[key("role:editor", DomainDefault, "/api/v1/orders", ActionRead, "allow")] {
		t.Fatal("collection policy missing after resync")
	}
	if !store.policies[key("role:editor", DomainDefault, "/api/v1/orders/*", ActionRead, "allow")] {
		t.Fatal("item policy missing after resync")
	}
}

func TestResync_RemovesStaleManagedPolicies_IncludingDeletedRoles(t *testing.T) {
	reg := registryWithOrders(t)
	store := newFakePolicyStore()
	// Stale: managed-shaped policy with no DB assignment (e.g. role was deleted).
	store.seed("role:ghost", DomainDefault, "/api/v1/orders", ActionRead, "allow")
	store.seed("role:ghost", DomainDefault, "/api/v1/orders/*", ActionRead, "allow")

	added, removed, err := ResyncManagedPolicies(store, reg, nil)
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if added != 0 || removed != 2 {
		t.Fatalf("added=%d removed=%d, want 0/2", added, removed)
	}
	if len(store.policies) != 0 {
		t.Fatalf("stale policies must be removed, still present: %v", store.policies)
	}
}

func TestResync_PreservesReservedDefaults_AndOutOfSubset(t *testing.T) {
	reg := registryWithOrders(t)
	store := newFakePolicyStore()
	// Reserved default whose (object, action) IS registry-mapped: must survive.
	store.seed("role:api_client", DomainDefault, string(ResourceNotification), ActionCreate, "allow")
	// Out-of-subset: object/action not derived from any registry mapping.
	store.seed("role:user", DomainDefault, string(ResourceUserSelf), ActionRead, "allow")
	// Out-of-subset: non-role subject.
	store.seed("someuser-uuid", DomainDefault, "/api/v1/orders", ActionRead, "allow")
	// Stale managed policy: must be removed.
	store.seed("role:ghost", DomainDefault, "/api/v1/orders", ActionRead, "allow")

	_, removed, err := ResyncManagedPolicies(store, reg, nil)
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d, want exactly 1 (the stale managed policy)", removed)
	}
	for _, kept := range []string{
		key("role:api_client", DomainDefault, string(ResourceNotification), ActionCreate, "allow"),
		key("role:user", DomainDefault, string(ResourceUserSelf), ActionRead, "allow"),
		key("someuser-uuid", DomainDefault, "/api/v1/orders", ActionRead, "allow"),
	} {
		if !store.policies[kept] {
			t.Fatalf("resync must preserve %s", kept)
		}
	}
}

func TestResync_TreatsConflictAndNotFoundAsSuccess(t *testing.T) {
	reg := registryWithOrders(t)
	store := newFakePolicyStore()
	// Already-present desired policy: AddPolicy will return Conflict; resync must not fail.
	store.seed("role:editor", DomainDefault, "/api/v1/orders", ActionRead, "allow")

	_, _, err := ResyncManagedPolicies(store, reg, []RoleAssignment{
		{RoleName: "editor", PermissionName: "orders.view"},
	})
	if err != nil {
		t.Fatalf("conflict on already-present policy must be treated as success: %v", err)
	}
}

func TestResync_PropagatesRealFailures(t *testing.T) {
	reg := registryWithOrders(t)
	store := newFakePolicyStore()
	boom := errors.New("adapter down")
	store.addErr[key("role:editor", DomainDefault, "/api/v1/orders/*", ActionRead, "allow")] = boom

	_, _, err := ResyncManagedPolicies(store, reg, []RoleAssignment{
		{RoleName: "editor", PermissionName: "orders.view"},
	})
	if err == nil {
		t.Fatal("real adapter failure must propagate")
	}
}
