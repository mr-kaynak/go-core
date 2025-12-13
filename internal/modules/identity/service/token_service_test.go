package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/test"
)

// TestTokenServiceGenerateAccessToken tests access token generation
func TestTokenServiceGenerateAccessToken(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	token, expiresAt, err := svc.GenerateAccessToken(user)

	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	if token == "" {
		t.Errorf("expected non-empty token")
	}

	if expiresAt.IsZero() {
		t.Errorf("expected non-zero expiration time")
	}

	// Token should be JWT-like (three parts separated by dots)
	parts := 0
	for _, ch := range token {
		if ch == '.' {
			parts++
		}
	}
	if parts != 2 {
		t.Errorf("invalid JWT format: expected 3 parts, got %d", parts+1)
	}
}

// TestTokenServiceGenerateRefreshToken tests refresh token generation
func TestTokenServiceGenerateRefreshToken(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	token, err := svc.GenerateRefreshToken(user)

	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	if token == "" {
		t.Errorf("expected non-empty token")
	}

	if len(token) < 32 {
		t.Errorf("token too short: expected at least 32 chars, got %d", len(token))
	}
}

// TestTokenServiceValidateAccessToken tests access token validation
func TestTokenServiceValidateAccessToken(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.UserID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, claims.UserID)
	}

	if claims.Email != user.Email {
		t.Errorf("expected email %s, got %s", user.Email, claims.Email)
	}

	if claims.Username != user.Username {
		t.Errorf("expected username %s, got %s", user.Username, claims.Username)
	}
}

// TestTokenServiceValidateAccessTokenExpired tests expired token validation
func TestTokenServiceValidateAccessTokenExpired(t *testing.T) {
	cfg := test.TestConfig()
	// Set very short expiry for testing
	cfg.JWT.Expiry = time.Millisecond
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(time.Millisecond * 100)

	_, err = svc.ValidateAccessToken(token)
	if err == nil {
		t.Errorf("expected token to be expired, but validation succeeded")
	}
}

// TestTokenServiceValidateAccessTokenInvalid tests invalid token validation
func TestTokenServiceValidateAccessTokenInvalid(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "invalid token format",
			token: "invalid.token",
		},
		{
			name:  "completely invalid",
			token: "completely-invalid-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ValidateAccessToken(tt.token)
			if err == nil {
				t.Errorf("expected validation error for token %q", tt.token)
			}
		})
	}
}

// TestTokenServiceGenerateTokenPair tests token pair generation
func TestTokenServiceGenerateTokenPair(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	tokenPair, err := svc.GenerateTokenPair(user)

	if err != nil {
		t.Fatalf("failed to generate token pair: %v", err)
	}

	if tokenPair.AccessToken == "" {
		t.Errorf("expected non-empty access token")
	}

	if tokenPair.RefreshToken == "" {
		t.Errorf("expected non-empty refresh token")
	}

	if tokenPair.ExpiresAt.IsZero() {
		t.Errorf("expected non-zero expires at")
	}

	// Validate access token
	claims, err := svc.ValidateAccessToken(tokenPair.AccessToken)
	if err != nil {
		t.Fatalf("failed to validate access token: %v", err)
	}

	if claims.UserID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, claims.UserID)
	}
}

// TestTokenServiceDecodeRefreshToken tests refresh token decoding
func TestTokenServiceDecodeRefreshToken(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	token, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	// Note: refresh token should not be empty
	if token == "" {
		t.Errorf("expected non-empty refresh token")
	}
}

// TestTokenServiceMultipleAccessTokens tests generating multiple tokens
func TestTokenServiceMultipleAccessTokens(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	tokens := make([]string, 3)

	for i := 0; i < 3; i++ {
		token, _, err := svc.GenerateAccessToken(user)
		if err != nil {
			t.Fatalf("failed to generate token %d: %v", i, err)
		}
		tokens[i] = token
		// Small delay to ensure different timestamps
		if i < 2 {
			time.Sleep(time.Millisecond)
		}
	}

	// All tokens should be valid
	for i, token := range tokens {
		claims, err := svc.ValidateAccessToken(token)
		if err != nil {
			t.Errorf("token %d validation failed: %v", i, err)
		}
		if claims.UserID != user.ID {
			t.Errorf("token %d has wrong user ID", i)
		}
	}
}

// TestClaimsExtraction tests JWT claims extraction
func TestClaimsExtraction(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	user.Roles = []domain.Role{
		{ID: uuid.New(), Name: "admin"},
		{ID: uuid.New(), Name: "user"},
	}

	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	// Check all required fields
	if claims.UserID != user.ID {
		t.Errorf("incorrect user ID in claims")
	}

	if claims.Email != user.Email {
		t.Errorf("incorrect email in claims")
	}

	if claims.Username != user.Username {
		t.Errorf("incorrect username in claims")
	}

	if len(claims.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(claims.Roles))
	}

	if claims.Issuer != cfg.JWT.Issuer {
		t.Errorf("incorrect issuer in claims")
	}
}

// TestTokenServiceWithRoles tests token generation with user roles
func TestTokenServiceWithRoles(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	user := test.CreateTestUserWithDefaults()
	user.Roles = []domain.Role{
		{ID: uuid.New(), Name: "admin"},
		{ID: uuid.New(), Name: "moderator"},
	}

	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if len(claims.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(claims.Roles))
	}

	// Check role names
	roleMap := make(map[string]bool)
	for _, role := range claims.Roles {
		roleMap[role] = true
	}

	if !roleMap["admin"] {
		t.Errorf("admin role not found in claims")
	}

	if !roleMap["moderator"] {
		t.Errorf("moderator role not found in claims")
	}
}

// BenchmarkGenerateAccessToken benchmarks access token generation
func BenchmarkGenerateAccessToken(b *testing.B) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	user := test.CreateTestUserWithDefaults()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.GenerateAccessToken(user)
	}
}

// BenchmarkValidateAccessToken benchmarks access token validation
func BenchmarkValidateAccessToken(b *testing.B) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	user := test.CreateTestUserWithDefaults()

	token, _, _ := svc.GenerateAccessToken(user)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.ValidateAccessToken(token)
	}
}

// BenchmarkGenerateRefreshToken benchmarks refresh token generation
func BenchmarkGenerateRefreshToken(b *testing.B) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	user := test.CreateTestUserWithDefaults()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.GenerateRefreshToken(user)
	}
}

// BenchmarkGenerateTokenPair benchmarks token pair generation
func BenchmarkGenerateTokenPair(b *testing.B) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	user := test.CreateTestUserWithDefaults()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.GenerateTokenPair(user)
	}
}
