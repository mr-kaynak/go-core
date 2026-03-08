package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestMetadataValue_NilReturnsEmptyObject(t *testing.T) {
	var m Metadata
	v, err := m.Value()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	b, ok := v.([]byte)
	if !ok {
		t.Fatalf("expected []byte from json marshal, got %T", v)
	}
	if string(b) != "{}" {
		t.Fatalf("expected empty object JSON, got %s", string(b))
	}
}

func TestMetadataScanBehaviors(t *testing.T) {
	t.Run("nil initializes empty map", func(t *testing.T) {
		var m Metadata
		if err := (&m).Scan(nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(m) != 0 {
			t.Fatalf("expected empty map")
		}
	})

	t.Run("non-bytes returns error", func(t *testing.T) {
		var m Metadata
		if err := (&m).Scan("not-bytes"); err == nil {
			t.Fatal("expected error for non-bytes source type")
		}
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		var m Metadata
		if err := (&m).Scan([]byte(`{invalid`)); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("valid json is unmarshaled", func(t *testing.T) {
		var m Metadata
		if err := (&m).Scan([]byte(`{"tier":"gold","active":true}`)); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if m["tier"] != "gold" {
			t.Fatalf("expected tier=gold, got %v", m["tier"])
		}
		if m["active"] != true {
			t.Fatalf("expected active=true, got %v", m["active"])
		}
	})
}

func TestMetadataValue_RoundTripJSON(t *testing.T) {
	m := Metadata{
		"tier":  "pro",
		"quota": float64(10),
	}
	v, err := m.Value()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	b, ok := v.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", v)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("failed to unmarshal encoded metadata: %v", err)
	}
	if decoded["tier"] != "pro" {
		t.Fatalf("unexpected tier value: %v", decoded["tier"])
	}
}

func TestUserGetPermissions_DeduplicatesByID(t *testing.T) {
	permID := uuid.New()
	user := &User{
		Roles: []Role{
			{
				Name: "admin",
				Permissions: []Permission{
					{ID: permID, Name: "users.read"},
					{ID: uuid.New(), Name: "users.write"},
				},
			},
			{
				Name: "auditor",
				Permissions: []Permission{
					{ID: permID, Name: "users.read"},
				},
			},
		},
	}

	perms := user.GetPermissions()
	if len(perms) != 2 {
		t.Fatalf("expected 2 unique permissions, got %d", len(perms))
	}
}
