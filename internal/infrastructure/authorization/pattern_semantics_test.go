package authorization

import "testing"

// TestPatternSemantics_ModuleRecipe pins the Casbin matcher behavior the
// module Permission recipe depends on (real model + keyMatch2, no mocks).
// The documented recipe is: one permission carries BOTH the collection path
// and the item pattern — Objects: ["/api/v1/orders", "/api/v1/orders/*"] —
// because keyMatch2's "/api/v1/orders/*" does NOT match the bare collection
// path. If casbin's matching ever changes, this test fails before any module
// documentation goes stale.
func TestPatternSemantics_ModuleRecipe(t *testing.T) {
	svc, err := NewTestCasbinService()
	if err != nil {
		t.Fatalf("test casbin service: %v", err)
	}

	const sub = "role:editor"
	if err := svc.AddPolicy(sub, DomainDefault, "/api/v1/orders", ActionRead, "allow"); err != nil {
		t.Fatalf("add collection policy: %v", err)
	}
	if err := svc.AddPolicy(sub, DomainDefault, "/api/v1/orders/*", ActionRead, "allow"); err != nil {
		t.Fatalf("add item policy: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"collection path", "/api/v1/orders", true},
		{"item path", "/api/v1/orders/2f9c9c33-0000-0000-0000-000000000001", true},
		{"nested item path", "/api/v1/orders/123/lines", true},
		{"collection with trailing slash", "/api/v1/orders/", true},
		{"sibling path is denied", "/api/v1/ordersextra", false},
		{"parent path is denied", "/api/v1", false},
		{"unrelated resource is denied", "/api/v1/invoices", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Enforce(sub, DomainDefault, tc.path, ActionRead)
			if err != nil {
				t.Fatalf("enforce: %v", err)
			}
			if got != tc.allowed {
				t.Fatalf("Enforce(%q) = %v, want %v", tc.path, got, tc.allowed)
			}
		})
	}

	// The premise of the two-pattern recipe: the item pattern ALONE must not
	// cover the collection path. If this ever starts passing, the recipe (and
	// the plan's Objects documentation) must be revisited.
	if err := svc.RemovePolicy(sub, DomainDefault, "/api/v1/orders", ActionRead, "allow"); err != nil {
		t.Fatalf("remove collection policy: %v", err)
	}
	got, err := svc.Enforce(sub, DomainDefault, "/api/v1/orders", ActionRead)
	if err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if got {
		t.Fatal("item pattern /api/v1/orders/* unexpectedly matches the bare collection path — two-pattern recipe premise broken")
	}
}
