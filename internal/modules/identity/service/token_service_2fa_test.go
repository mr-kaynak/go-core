package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/crypto"
	"github.com/mr-kaynak/go-core/internal/test"
)

func TestTokenService_GenerateTwoFactorToken_Success(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	userID := uuid.New()

	token, err := svc.GenerateTwoFactorToken(userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token == "" {
		t.Fatalf("expected non-empty token")
	}

	// Token should be a valid JWT with 3 parts
	parts := 0
	for _, ch := range token {
		if ch == '.' {
			parts++
		}
	}
	if parts != 2 {
		t.Fatalf("expected JWT with 3 parts (2 dots), got %d dots", parts)
	}
}

func TestTokenService_ValidateTwoFactorToken_Success(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	userID := uuid.New()

	token, err := svc.GenerateTwoFactorToken(userID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	gotID, err := svc.ValidateTwoFactorToken(token)
	if err != nil {
		t.Fatalf("expected valid 2FA token, got error: %v", err)
	}
	if gotID != userID {
		t.Fatalf("expected user ID %s, got %s", userID, gotID)
	}
}

func TestTokenService_ValidateTwoFactorToken_Expired(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	userID := uuid.New()

	// Manually create an expired 2FA token
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-6 * time.Minute)),
		NotBefore: jwt.NewNumericDate(time.Now().Add(-6 * time.Minute)),
		Issuer:    cfg.JWT.Issuer,
		Subject:   userID.String(),
		Audience:  jwt.ClaimStrings{audienceTwoFactor},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signingKey := crypto.DeriveHMACKey([]byte(cfg.JWT.Secret), "2fa-token")
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("failed to sign expired 2FA token: %v", err)
	}

	_, err = svc.ValidateTwoFactorToken(tokenString)
	if err == nil {
		t.Fatalf("expected error for expired 2FA token, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got %T", err)
	}
	if pd.Detail != "Two-factor token has expired, please login again" {
		t.Fatalf("unexpected detail: %q", pd.Detail)
	}
}

func TestTokenService_ValidateTwoFactorToken_InvalidToken(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	_, err := svc.ValidateTwoFactorToken("completely-invalid-token")
	if err == nil {
		t.Fatalf("expected error for invalid 2FA token, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got %T", err)
	}
	if pd.Detail != "Invalid two-factor token" {
		t.Fatalf("unexpected detail: %q", pd.Detail)
	}
}

func TestTokenService_ValidateTwoFactorToken_WrongAudience(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	userID := uuid.New()

	// Create a token with the 2FA signing key but wrong audience
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		Issuer:    cfg.JWT.Issuer,
		Subject:   userID.String(),
		Audience:  jwt.ClaimStrings{"access"}, // wrong audience
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signingKey := crypto.DeriveHMACKey([]byte(cfg.JWT.Secret), "2fa-token")
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = svc.ValidateTwoFactorToken(tokenString)
	if err == nil {
		t.Fatalf("expected error for wrong audience, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got %T", err)
	}
	if pd.Detail != "Invalid two-factor token" {
		t.Fatalf("unexpected detail: %q", pd.Detail)
	}
}

func TestTokenService_ValidateTwoFactorToken_AccessTokenRejected(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	user := test.CreateTestUserWithDefaults()

	// Generate a regular access token and try to validate it as 2FA
	accessToken, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	_, err = svc.ValidateTwoFactorToken(accessToken)
	if err == nil {
		t.Fatalf("expected access token to be rejected as 2FA token")
	}
}

func TestTokenService_ValidateTwoFactorToken_RefreshTokenRejected(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	user := test.CreateTestUserWithDefaults()

	// Generate a refresh token and try to validate it as 2FA
	refreshToken, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.ValidateTwoFactorToken(refreshToken)
	if err == nil {
		t.Fatalf("expected refresh token to be rejected as 2FA token")
	}
}

func TestTokenService_TwoFactorSigningKey_IsDeterministic(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	key1 := svc.twoFactorSigningKey()
	key2 := svc.twoFactorSigningKey()

	if len(key1) == 0 {
		t.Fatalf("expected non-empty signing key")
	}
	if string(key1) != string(key2) {
		t.Fatalf("expected deterministic signing key derivation")
	}
}

func TestTokenService_TwoFactorSigningKey_DiffersFromAccessAndRefreshKeys(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	twoFactorKey := svc.twoFactorSigningKey()
	refreshKey := svc.refreshSigningKey()
	accessKey := []byte(cfg.JWT.Secret)

	if string(twoFactorKey) == string(refreshKey) {
		t.Fatalf("2FA signing key must differ from refresh signing key")
	}
	if string(twoFactorKey) == string(accessKey) {
		t.Fatalf("2FA signing key must differ from access signing key")
	}
}

func TestTokenService_GenerateTwoFactorToken_RoundTrip(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	// Generate and validate multiple times to ensure consistency
	for i := 0; i < 3; i++ {
		userID := uuid.New()
		token, err := svc.GenerateTwoFactorToken(userID)
		if err != nil {
			t.Fatalf("iteration %d: generate failed: %v", i, err)
		}

		gotID, err := svc.ValidateTwoFactorToken(token)
		if err != nil {
			t.Fatalf("iteration %d: validate failed: %v", i, err)
		}
		if gotID != userID {
			t.Fatalf("iteration %d: expected %s, got %s", i, userID, gotID)
		}
	}
}
