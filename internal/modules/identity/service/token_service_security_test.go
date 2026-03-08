package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/test"
)

func mustTokenTestUser(t *testing.T) *domain.User {
	t.Helper()

	u := &domain.User{
		ID:       uuid.New(),
		Email:    "token-test@example.com",
		Username: "token_test_user",
		Status:   domain.UserStatusActive,
		Verified: true,
		Roles: []domain.Role{
			{
				Name: "admin",
				Permissions: []domain.Permission{
					{Name: "users.read"},
				},
			},
		},
	}
	if err := u.SetPassword("StrongPass123!"); err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	return u
}

func assertUnauthorizedProblem(t *testing.T, err error, expectedDetail string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected unauthorized error, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected problem detail, got %T", err)
	}
	if pd.Status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, pd.Status)
	}
	if pd.Detail != expectedDetail {
		t.Fatalf("expected detail %q, got %q", expectedDetail, pd.Detail)
	}
}

func TestTokenServiceValidateAccessToken_RejectsBlacklistedToken(t *testing.T) {
	cfg := test.TestConfig()
	user := mustTokenTestUser(t)
	svc := NewTokenService(cfg)

	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	svc.SetBlacklist(&blacklistStub{
		isBlacklistedFn: func(ctx context.Context, tokenHash string) (bool, error) {
			if tokenHash != hashToken(token) {
				t.Fatalf("expected hashed token to be checked")
			}
			return true, nil
		},
		isUserBlacklistedFn: func(ctx context.Context, userID string) (bool, error) {
			return false, nil
		},
	})

	_, err = svc.ValidateAccessToken(token)
	assertUnauthorizedProblem(t, err, "Token has been revoked")
}

func TestTokenServiceValidateAccessToken_RejectsUserBlacklisted(t *testing.T) {
	cfg := test.TestConfig()
	user := mustTokenTestUser(t)
	svc := NewTokenService(cfg)

	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	svc.SetBlacklist(&blacklistStub{
		isBlacklistedFn: func(ctx context.Context, tokenHash string) (bool, error) {
			return false, nil
		},
		isUserBlacklistedFn: func(ctx context.Context, userID string) (bool, error) {
			if userID != user.ID.String() {
				t.Fatalf("unexpected user id checked: %s", userID)
			}
			return true, nil
		},
	})

	_, err = svc.ValidateAccessToken(token)
	assertUnauthorizedProblem(t, err, "All user tokens have been revoked")
}

func TestTokenServiceValidateRefreshToken_RejectsRevokedStoredToken(t *testing.T) {
	cfg := test.TestConfig()
	user := mustTokenTestUser(t)
	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(token string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{
				Token:   token,
				Revoked: true,
			}, nil
		},
	}
	svc := NewTokenService(cfg, repo)

	refresh, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.ValidateRefreshToken(refresh)
	assertUnauthorizedProblem(t, err, "Refresh token has been revoked")
}

func TestTokenServiceBlacklistAccessToken_DelegatesHashedToken(t *testing.T) {
	cfg := test.TestConfig()
	user := mustTokenTestUser(t)
	svc := NewTokenService(cfg)

	token, _, err := svc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	called := false
	expiry := 15 * time.Minute
	svc.SetBlacklist(&blacklistStub{
		blacklistFn: func(ctx context.Context, tokenHash string, gotExpiry time.Duration) error {
			called = true
			if tokenHash != hashToken(token) {
				t.Fatalf("expected hashed token delegation")
			}
			if gotExpiry != expiry {
				t.Fatalf("expected expiry %v, got %v", expiry, gotExpiry)
			}
			return nil
		},
	})

	if err := svc.BlacklistAccessToken(context.Background(), token, expiry); err != nil {
		t.Fatalf("expected blacklist call success, got %v", err)
	}
	if !called {
		t.Fatalf("expected blacklist to be invoked")
	}
}

func TestTokenServiceBlacklistAccessToken_NoBlacklistConfiguredIsNoop(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)

	if err := svc.BlacklistAccessToken(context.Background(), "some-token", time.Minute); err != nil {
		t.Fatalf("expected noop success when blacklist is nil, got %v", err)
	}
}

func TestTokenServiceBlacklistAllUserTokens_UnsupportedBlacklistIsNoop(t *testing.T) {
	cfg := test.TestConfig()
	svc := NewTokenService(cfg)
	// Implements checker only; does not implement user blacklister extension.
	svc.SetBlacklist(&blacklistStub{})

	if err := svc.BlacklistAllUserTokens(context.Background(), uuid.NewString(), time.Minute); err != nil {
		t.Fatalf("expected noop success when user blacklister extension missing, got %v", err)
	}
}

func TestTokenServiceRevokeRefreshToken_UsesHashedToken(t *testing.T) {
	cfg := test.TestConfig()
	user := mustTokenTestUser(t)
	var revokedTokenHash string

	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		revokeRefreshTokenFn: func(token string) error {
			revokedTokenHash = token
			return nil
		},
	}
	svc := NewTokenService(cfg, repo)
	refresh, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	if err := svc.RevokeRefreshToken(refresh); err != nil {
		t.Fatalf("expected revoke success, got %v", err)
	}
	if revokedTokenHash != hashToken(refresh) {
		t.Fatalf("expected hashed refresh token to be revoked")
	}
}

func TestTokenServiceRevokeAllUserTokens_DelegatesToRepository(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()
	called := false

	repo := &authRepoStub{
		revokeAllRefreshTokenFn: func(id uuid.UUID) error {
			called = true
			if id != userID {
				t.Fatalf("unexpected user id revoke: %s", id)
			}
			return nil
		},
	}
	svc := NewTokenService(cfg, repo)

	if err := svc.RevokeAllUserTokens(userID); err != nil {
		t.Fatalf("expected revoke-all success, got %v", err)
	}
	if !called {
		t.Fatalf("expected revoke-all delegation")
	}
}

func TestTokenServiceValidateRefreshToken_NotFoundInStore(t *testing.T) {
	cfg := test.TestConfig()
	user := mustTokenTestUser(t)
	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(token string) (*domain.RefreshToken, error) {
			return nil, stderrors.New("not found")
		},
	}
	svc := NewTokenService(cfg, repo)

	refresh, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.ValidateRefreshToken(refresh)
	assertUnauthorizedProblem(t, err, "Refresh token not found")
}
