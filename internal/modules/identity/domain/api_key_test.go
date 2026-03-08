package domain

import (
	"testing"
	"time"
)

func TestAPIKeyIsValid(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		revoked   bool
		expiresAt *time.Time
		valid     bool
	}{
		{
			name:      "not revoked no expiry",
			revoked:   false,
			expiresAt: nil,
			valid:     true,
		},
		{
			name:      "revoked key",
			revoked:   true,
			expiresAt: nil,
			valid:     false,
		},
		{
			name:      "expired key",
			revoked:   false,
			expiresAt: ptrTime(now.Add(-time.Minute)),
			valid:     false,
		},
		{
			name:      "not expired key",
			revoked:   false,
			expiresAt: ptrTime(now.Add(time.Minute)),
			valid:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &APIKey{
				Revoked:   tt.revoked,
				ExpiresAt: tt.expiresAt,
			}
			if got := k.IsValid(); got != tt.valid {
				t.Fatalf("expected valid=%v, got %v", tt.valid, got)
			}
		})
	}
}

func TestHashAPIKeyDeterministic(t *testing.T) {
	const key = "gck_test_key_value"
	h1 := HashAPIKey(key)
	h2 := HashAPIKey(key)

	if h1 != h2 {
		t.Fatalf("expected deterministic hash")
	}
	if len(h1) != 64 {
		t.Fatalf("expected sha256 hex length 64, got %d", len(h1))
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
