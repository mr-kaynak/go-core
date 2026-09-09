package authorization

import (
	"fmt"
	"testing"
)

// TestPermissionMappingNoDuplicatePolicies ensures the core seed maps every
// permission name to a unique (Resource, Action) pair. Duplicate pairs would
// cause policy collision: removing one permission could silently revoke
// another. (The registry enforces this for runtime registrations; this guards
// the static seed itself.)
func TestPermissionMappingNoDuplicatePolicies(t *testing.T) {
	seen := make(map[string]string) // "resource|action" → permission name
	for name, m := range permissionToCasbin {
		key := fmt.Sprintf("%s|%s", m.Resource, m.Action)
		if prev, exists := seen[key]; exists {
			t.Errorf("policy collision: %q and %q both map to (%s, %s)", prev, name, m.Resource, m.Action)
		}
		seen[key] = name
	}
}

// TestRegistryLookupKnownAndUnknown verifies registry lookup for core-seeded
// and missing permission names (replaces the removed GetCasbinMapping global).
func TestRegistryLookupKnownAndUnknown(t *testing.T) {
	r := NewPermissionRegistry()

	def, ok := r.Lookup("users.view")
	if !ok {
		t.Fatal("expected mapping for users.view")
	}
	if len(def.Objects) != 1 || def.Objects[0] != string(ResourceUser) || def.Action != ActionRead {
		t.Fatalf("unexpected mapping: %+v", def)
	}

	if _, ok := r.Lookup("nonexistent.perm"); ok {
		t.Fatal("expected no mapping for nonexistent.perm")
	}
}

// TestRegistryAllReturnsDefensiveCopies ensures mutating returned defs cannot
// corrupt the registry.
func TestRegistryAllReturnsDefensiveCopies(t *testing.T) {
	r := NewPermissionRegistry()
	defs := r.All()
	if len(defs) == 0 {
		t.Fatal("core seed must not be empty")
	}
	defs[0].Objects[0] = "/mutated"

	fresh, _ := r.Lookup(defs[0].Name)
	if fresh.Objects[0] == "/mutated" {
		t.Fatal("All() leaked internal object slices — mutation corrupted the registry")
	}
}
