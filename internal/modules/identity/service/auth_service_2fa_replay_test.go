package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	cryptoutil "github.com/mr-kaynak/go-core/internal/core/crypto"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/test"
	"github.com/pquerna/otp/totp"
)

// TestAuthService_Validate2FALogin_BlacklistsConsumedToken verifies that
// after a successful 2FA validation the consumed two_factor_token is
// blacklisted so it cannot be replayed within its TTL.
func TestAuthService_Validate2FALogin_BlacklistsConsumedToken(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "replay@example.com", "replay_user", "StrongPass123!")

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "go-core-test",
		AccountName: user.Email,
	})
	if err != nil {
		t.Fatalf("failed to generate TOTP key: %v", err)
	}

	encKey := cryptoutil.NormalizeKey(cfg.Security.EncryptionKey)
	encSecret, err := cryptoutil.Encrypt(key.Secret(), encKey)
	if err != nil {
		t.Fatalf("failed to encrypt 2FA secret: %v", err)
	}

	user.TwoFactorEnabled = true
	user.TwoFactorSecret = encSecret

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			u.Roles = []domain.Role{
				{Name: "user", Permissions: []domain.Permission{{Name: "users.read"}}},
			}
			return nil
		},
		updateFn:             func(u *domain.User) error { return nil },
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	// Track blacklist calls
	var blacklistedHash string
	bl := &blacklistStub{
		blacklistFn: func(ctx context.Context, tokenHash string, expiry time.Duration) error {
			blacklistedHash = tokenHash
			return nil
		},
	}
	svc.tokenService.SetBlacklist(bl)

	// Generate 2FA token
	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	// Generate valid TOTP code
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}

	// First call should succeed
	resp, err := svc.Validate2FALogin(twoFactorToken, code, "127.0.0.1", "TestAgent/1.0")
	if err != nil {
		t.Fatalf("expected first 2FA login to succeed, got %v", err)
	}
	if resp == nil || resp.AccessToken == "" {
		t.Fatalf("expected valid response with tokens")
	}

	// Verify the 2FA token was blacklisted
	expectedHash := cryptoutil.HashSHA256Hex(twoFactorToken)
	if blacklistedHash != expectedHash {
		t.Fatalf("expected 2FA token hash %q to be blacklisted, got %q", expectedHash, blacklistedHash)
	}
}

// TestAuthService_Validate2FALogin_ReplayRejected verifies that replaying
// a consumed two_factor_token is rejected when the blacklist reports
// the token hash as already used.
func TestAuthService_Validate2FALogin_ReplayRejected(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "replay2@example.com", "replay_user2", "StrongPass123!")

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "go-core-test",
		AccountName: user.Email,
	})
	if err != nil {
		t.Fatalf("failed to generate TOTP key: %v", err)
	}

	encKey := cryptoutil.NormalizeKey(cfg.Security.EncryptionKey)
	encSecret, err := cryptoutil.Encrypt(key.Secret(), encKey)
	if err != nil {
		t.Fatalf("failed to encrypt 2FA secret: %v", err)
	}

	user.TwoFactorEnabled = true
	user.TwoFactorSecret = encSecret

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			u.Roles = []domain.Role{
				{Name: "user", Permissions: []domain.Permission{{Name: "users.read"}}},
			}
			return nil
		},
		updateFn:             func(u *domain.User) error { return nil },
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	// Set up blacklist that tracks which hashes have been blacklisted
	blacklisted := make(map[string]bool)
	bl := &blacklistStub{
		blacklistFn: func(ctx context.Context, tokenHash string, expiry time.Duration) error {
			blacklisted[tokenHash] = true
			return nil
		},
		isBlacklistedFn: func(ctx context.Context, tokenHash string) (bool, error) {
			return blacklisted[tokenHash], nil
		},
	}
	svc.tokenService.SetBlacklist(bl)

	// Generate 2FA token
	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	// Generate valid TOTP code
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}

	// First call should succeed
	resp, err := svc.Validate2FALogin(twoFactorToken, code, "127.0.0.1", "TestAgent/1.0")
	if err != nil {
		t.Fatalf("expected first 2FA login to succeed, got %v", err)
	}
	if resp == nil || resp.AccessToken == "" {
		t.Fatalf("expected valid response with tokens on first call")
	}

	// Second call with the same 2FA token should fail (replay attack)
	code2, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate second TOTP code: %v", err)
	}

	_, err = svc.Validate2FALogin(twoFactorToken, code2, "127.0.0.1", "TestAgent/1.0")
	if err == nil {
		t.Fatalf("expected replay of consumed 2FA token to be rejected, got nil")
	}

	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail error, got %T", err)
	}
	if pd.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for replayed 2FA token, got %d", pd.Status)
	}
	if pd.Detail != "Two-factor token has already been used" {
		t.Fatalf("expected replay rejection detail, got %q", pd.Detail)
	}
}

// TestAuthService_Validate2FALogin_BlacklistUnavailableLogsWarning verifies
// that when the blacklist is nil (Redis unavailable) the 2FA login still
// succeeds gracefully -- the blacklist step is best-effort.
func TestAuthService_Validate2FALogin_BlacklistUnavailableLogsWarning(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "noredis@example.com", "noredis_user", "StrongPass123!")

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "go-core-test",
		AccountName: user.Email,
	})
	if err != nil {
		t.Fatalf("failed to generate TOTP key: %v", err)
	}

	encKey := cryptoutil.NormalizeKey(cfg.Security.EncryptionKey)
	encSecret, err := cryptoutil.Encrypt(key.Secret(), encKey)
	if err != nil {
		t.Fatalf("failed to encrypt 2FA secret: %v", err)
	}

	user.TwoFactorEnabled = true
	user.TwoFactorSecret = encSecret

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			u.Roles = []domain.Role{
				{Name: "user", Permissions: []domain.Permission{{Name: "users.read"}}},
			}
			return nil
		},
		updateFn:             func(u *domain.User) error { return nil },
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
	}
	// No blacklist set (simulates Redis being unavailable)
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}

	// Should still succeed when blacklist is nil
	resp, err := svc.Validate2FALogin(twoFactorToken, code, "127.0.0.1", "TestAgent/1.0")
	if err != nil {
		t.Fatalf("expected 2FA login to succeed without blacklist, got %v", err)
	}
	if resp == nil || resp.AccessToken == "" {
		t.Fatalf("expected valid response with tokens")
	}
}
