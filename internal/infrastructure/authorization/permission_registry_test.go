package authorization

import (
	"strings"
	"testing"
)

func validOrdersView() PermissionDef {
	return PermissionDef{
		Name:    "orders.view",
		Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
		Action:  ActionRead,
	}
}

func TestNewPermissionRegistry_SeededWithCorePermissions(t *testing.T) {
	r := NewPermissionRegistry()

	def, ok := r.Lookup("users.view")
	if !ok {
		t.Fatal("core permission users.view must be pre-seeded")
	}
	if def.Action != ActionRead {
		t.Fatalf("users.view action = %q, want read", def.Action)
	}
	if len(def.Objects) != 1 || def.Objects[0] != string(ResourceUser) {
		t.Fatalf("users.view objects = %v, want [%s]", def.Objects, ResourceUser)
	}
}

func TestPermissionRegistry_InstancesAreIsolated(t *testing.T) {
	a := NewPermissionRegistry()
	b := NewPermissionRegistry()

	if err := a.Register(validOrdersView()); err != nil {
		t.Fatalf("register on a: %v", err)
	}
	if _, ok := b.Lookup("orders.view"); ok {
		t.Fatal("registration on instance a must not leak into instance b")
	}
}

func TestPermissionRegistry_RegisterValidCustomPermission(t *testing.T) {
	r := NewPermissionRegistry()
	if err := r.Register(validOrdersView()); err != nil {
		t.Fatalf("valid registration failed: %v", err)
	}
	def, ok := r.Lookup("orders.view")
	if !ok || len(def.Objects) != 2 {
		t.Fatalf("lookup after register: ok=%v def=%+v", ok, def)
	}
}

func TestPermissionRegistry_RejectsDuplicateName(t *testing.T) {
	r := NewPermissionRegistry()
	p := validOrdersView()
	p.Name = "users.view" // collides with core
	if err := r.Register(p); err == nil {
		t.Fatal("duplicate name (vs core) must be rejected")
	}
}

func TestPermissionRegistry_RejectsDuplicateObjectActionPair(t *testing.T) {
	r := NewPermissionRegistry()
	p := validOrdersView()
	p.Objects = []string{string(ResourceUser)} // (users/*, read) already owned by users.view
	if err := r.Register(p); err == nil {
		t.Fatal("duplicate (object, action) pair must be rejected")
	}
}

func TestPermissionRegistry_RejectsInvalidDefs(t *testing.T) {
	cases := map[string]PermissionDef{
		"empty name":            {Name: "", Objects: []string{"/api/v1/x"}, Action: ActionRead},
		"nil objects":           {Name: "x.view", Objects: nil, Action: ActionRead},
		"empty objects":         {Name: "x.view", Objects: []string{}, Action: ActionRead},
		"empty object element":  {Name: "x.view", Objects: []string{""}, Action: ActionRead},
		"dup object in one def": {Name: "x.view", Objects: []string{"/api/v1/x", "/api/v1/x"}, Action: ActionRead},
		"empty action":          {Name: "x.view", Objects: []string{"/api/v1/x"}, Action: ""},
		"unknown action":        {Name: "x.view", Objects: []string{"/api/v1/x"}, Action: "sometimes"},
	}
	for name, def := range cases {
		r := NewPermissionRegistry()
		if err := r.Register(def); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

func TestPermissionRegistry_BatchIsAtomic(t *testing.T) {
	r := NewPermissionRegistry()
	good := validOrdersView()
	bad := PermissionDef{Name: "orders.create", Objects: nil, Action: ActionCreate}

	err := r.Register(good, bad)
	if err == nil {
		t.Fatal("batch with one invalid def must fail")
	}
	if !strings.Contains(err.Error(), "orders.create") {
		t.Fatalf("error should name the offending permission, got: %v", err)
	}
	if _, ok := r.Lookup("orders.view"); ok {
		t.Fatal("atomicity violated: valid def from failed batch was committed")
	}
}
