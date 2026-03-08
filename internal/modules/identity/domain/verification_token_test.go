package domain

import (
	"regexp"
	"testing"
	"time"
)

func TestVerificationTokenIsValid(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		used      bool
		expiresAt time.Time
		valid     bool
	}{
		{
			name:      "unused and not expired",
			used:      false,
			expiresAt: now.Add(time.Hour),
			valid:     true,
		},
		{
			name:      "used token",
			used:      true,
			expiresAt: now.Add(time.Hour),
			valid:     false,
		},
		{
			name:      "expired token",
			used:      false,
			expiresAt: now.Add(-time.Minute),
			valid:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &VerificationToken{
				Used:      tt.used,
				ExpiresAt: tt.expiresAt,
			}
			if got := token.IsValid(); got != tt.valid {
				t.Fatalf("expected valid=%v, got %v", tt.valid, got)
			}
		})
	}
}

func TestVerificationTokenIsExpired(t *testing.T) {
	token := &VerificationToken{ExpiresAt: time.Now().Add(-time.Second)}
	if !token.IsExpired() {
		t.Fatalf("expected token to be expired")
	}

	token.ExpiresAt = time.Now().Add(time.Second)
	if token.IsExpired() {
		t.Fatalf("expected token to be not expired")
	}
}

func TestVerificationTokenMarkAsUsed(t *testing.T) {
	token := &VerificationToken{
		Used:   false,
		UsedAt: nil,
	}
	token.MarkAsUsed()

	if !token.Used {
		t.Fatalf("expected token to be marked used")
	}
	if token.UsedAt == nil {
		t.Fatalf("expected used timestamp to be set")
	}
}

func TestGenerateSecureTokenFormat(t *testing.T) {
	token, err := GenerateSecureToken()
	if err != nil {
		t.Fatalf("expected token generation success, got %v", err)
	}
	// 32 random bytes => 64 hex chars
	if len(token) != 64 {
		t.Fatalf("expected 64-char token, got %d", len(token))
	}
	hexRe := regexp.MustCompile(`^[0-9a-f]+$`)
	if !hexRe.MatchString(token) {
		t.Fatalf("expected lowercase hex token, got %q", token)
	}
}

func TestGenerateShortCodeFormat(t *testing.T) {
	code, err := GenerateShortCode()
	if err != nil {
		t.Fatalf("expected short code generation success, got %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-digit short code, got %q", code)
	}
	digitsRe := regexp.MustCompile(`^[0-9]{6}$`)
	if !digitsRe.MatchString(code) {
		t.Fatalf("expected numeric 6-digit code, got %q", code)
	}
}
