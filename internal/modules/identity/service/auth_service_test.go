package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/test"
)

func TestAuthServiceRegister_UsernameConflict(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return true, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Register(&RegisterRequest{
		Email:    "staff@example.com",
		Username: "staff",
		Password: "StrongPass123!",
	})
	assertProblem(t, err, http.StatusConflict, "Username already taken")
}

func TestAuthServiceVerifyEmail_ExpiredToken(t *testing.T) {
	cfg := test.TestConfig()
	token := &domain.VerificationToken{
		UserID:    uuid.New(),
		Type:      domain.TokenTypeEmailVerification,
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	vr := &verificationRepoStub{
		findByTokenFn: func(input string) (*domain.VerificationToken, error) {
			return token, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	err := svc.VerifyEmail("expired-token")
	assertProblem(t, err, http.StatusBadRequest, "Verification token has expired")
}

func TestAuthServiceVerifyEmail_AlreadyUsedToken(t *testing.T) {
	cfg := test.TestConfig()
	token := &domain.VerificationToken{
		UserID:    uuid.New(),
		Type:      domain.TokenTypeEmailVerification,
		Used:      true,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	vr := &verificationRepoStub{
		findByTokenFn: func(input string) (*domain.VerificationToken, error) {
			return token, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	err := svc.VerifyEmail("used-token")
	assertProblem(t, err, http.StatusBadRequest, "Verification token has already been used")
}

func TestAuthServiceResendVerificationEmail_UserNotFoundReturnsNil(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	if err := svc.ResendVerificationEmail("missing@example.com"); err != nil {
		t.Fatalf("expected nil error for unknown email, got %v", err)
	}
}

func TestAuthServiceResendVerificationEmail_AlreadyVerifiedConflict(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) {
			return &domain.User{ID: uuid.New(), Email: email, Verified: true}, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.ResendVerificationEmail("verified@example.com")
	assertProblem(t, err, http.StatusConflict, "Email already verified")
}

func TestAuthServiceRequestPasswordReset_ExistingEmailCreatesToken(t *testing.T) {
	cfg := test.TestConfig()
	user := &domain.User{ID: uuid.New(), Email: "staff@example.com", Username: "staff"}
	created := false
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		createFn: func(token *domain.VerificationToken) error {
			created = true
			token.Token = "reset-token"
			return nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error {
			return nil
		},
	}
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) {
			return user, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})

	if err := svc.RequestPasswordReset(user.Email); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !created {
		t.Fatalf("expected reset token to be created")
	}
}

func TestAuthServiceResetPassword_InvalidToken(t *testing.T) {
	cfg := test.TestConfig()
	vr := &verificationRepoStub{
		findByTokenFn: func(token string) (*domain.VerificationToken, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	err := svc.ResetPassword("bad-token", "StrongPass123!")
	assertProblem(t, err, http.StatusBadRequest, "Invalid or expired password reset token")
}

func TestAuthServiceRefreshToken_RevokedTokenRejected(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(tokenHash string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: tokenHash, Revoked: true}, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	refreshToken, err := svc.tokenService.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.RefreshToken(refreshToken)
	assertProblem(t, err, http.StatusUnauthorized, "Refresh token has been revoked")
}
