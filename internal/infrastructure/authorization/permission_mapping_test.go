package authorization

import (
	"fmt"
	"testing"
)

// TestPermissionMappingNoDuplicatePolicies ensures every permission name maps to
// a unique (Resource, Action) pair. Duplicate pairs would cause policy collision:
// removing one permission could silently revoke another.
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

// TestGetAllMappingsReturnsDefensiveCopy ensures callers cannot mutate the global registry.
func TestGetAllMappingsReturnsDefensiveCopy(t *testing.T) {
	m := GetAllMappings()
	originalLen := len(permissionToCasbin)

	// Mutate the returned map
	m["test.fake"] = PermissionMapping{Resource: "/fake", Action: "fake"}

	if len(permissionToCasbin) != originalLen {
		t.Fatal("GetAllMappings returned a reference to the global map — mutation leaked")
	}
}

// TestGetCasbinMappingKnownAndUnknown verifies lookup for existing and missing keys.
func TestGetCasbinMappingKnownAndUnknown(t *testing.T) {
	m, ok := GetCasbinMapping("users.view")
	if !ok {
		t.Fatal("expected mapping for users.view")
	}
	if m.Resource != ResourceUser || m.Action != ActionRead {
		t.Fatalf("unexpected mapping: %+v", m)
	}

	_, ok = GetCasbinMapping("nonexistent.perm")
	if ok {
		t.Fatal("expected no mapping for nonexistent.perm")
	}
}
