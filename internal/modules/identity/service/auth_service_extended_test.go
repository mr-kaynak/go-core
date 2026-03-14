package service

import (
	"context"
	"errors"
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

// ---------------------------------------------------------------------------
// Stub implementations for optional dependencies
// ---------------------------------------------------------------------------

type eventPublisherStub struct {
	dispatchUserRegisteredFn     func(ctx context.Context, userID uuid.UUID, email, username, lang string) error
	dispatchEmailVerificationFn  func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error
	dispatchEmailPasswordResetFn func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error
	dispatchPasswordChangedFn    func(ctx context.Context, userID uuid.UUID, email, fullName, lang string) error
}

func (s *eventPublisherStub) DispatchUserRegistered(ctx context.Context, userID uuid.UUID, email, username, lang string) error {
	if s.dispatchUserRegisteredFn != nil {
		return s.dispatchUserRegisteredFn(ctx, userID, email, username, lang)
	}
	return nil
}

func (s *eventPublisherStub) DispatchEmailVerification(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
	if s.dispatchEmailVerificationFn != nil {
		return s.dispatchEmailVerificationFn(ctx, userID, email, username, token, lang)
	}
	return nil
}

func (s *eventPublisherStub) DispatchEmailPasswordReset(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
	if s.dispatchEmailPasswordResetFn != nil {
		return s.dispatchEmailPasswordResetFn(ctx, userID, email, username, token, lang)
	}
	return nil
}

func (s *eventPublisherStub) DispatchEmailPasswordChanged(ctx context.Context, userID uuid.UUID, email, fullName, lang string) error {
	if s.dispatchPasswordChangedFn != nil {
		return s.dispatchPasswordChangedFn(ctx, userID, email, fullName, lang)
	}
	return nil
}

type metricsStub struct {
	loginAttempts     int
	registrationCalls int
	lastLoginSuccess  bool
	lastLoginMethod   string
}

func (m *metricsStub) RecordLoginAttempt(success bool, method string) {
	m.loginAttempts++
	m.lastLoginSuccess = success
	m.lastLoginMethod = method
}

func (m *metricsStub) RecordUserRegistration() {
	m.registrationCalls++
}

func (m *metricsStub) RecordNotificationSent(string, bool)               {}
func (m *metricsStub) RecordBlogPostCreated(string)                      {}
func (m *metricsStub) RecordBlogPostPublished()                          {}
func (m *metricsStub) RecordBlogCommentCreated(string)                   {}
func (m *metricsStub) RecordBlogLikeToggled(string)                      {}
func (m *metricsStub) RecordBlogViewRecorded()                           {}
func (m *metricsStub) RecordBlogShareRecorded(string)                    {}
func (m *metricsStub) RecordDBQuery(string, string, bool, time.Duration) {}
func (m *metricsStub) UpdateDBConnections(int, int)                      {}
func (m *metricsStub) RecordCacheHit()                                   {}
func (m *metricsStub) RecordCacheMiss()                                  {}
func (m *metricsStub) RecordMQMessagePublished(string, string, bool)     {}
func (m *metricsStub) RecordMQMessageConsumed(string, bool)              {}
func (m *metricsStub) UpdateMQMetrics(int, int, bool)                    {}

type languageResolverStub struct {
	language string
	err      error
}

func (r *languageResolverStub) GetLanguageByUserID(uuid.UUID) (string, error) {
	return r.language, r.err
}

type prefCreatorStub struct {
	called   bool
	language string
	err      error
}

func (p *prefCreatorStub) CreateInitialPreferences(userID uuid.UUID, language string) error {
	p.called = true
	p.language = language
	return p.err
}

// ---------------------------------------------------------------------------
// Setter tests
// ---------------------------------------------------------------------------

func TestAuthService_SetEventPublisher(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	if svc.eventPublisher != nil {
		t.Fatalf("expected nil event publisher before set")
	}

	pub := &eventPublisherStub{}
	svc.SetEventPublisher(pub)

	if svc.eventPublisher == nil {
		t.Fatalf("expected event publisher to be set")
	}
}

func TestAuthService_SetMetrics(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	if svc.metrics != nil {
		t.Fatalf("expected nil metrics before set")
	}

	m := &metricsStub{}
	svc.SetMetrics(m)

	if svc.metrics == nil {
		t.Fatalf("expected metrics to be set")
	}

	// Verify getMetrics returns the injected recorder
	got := svc.getMetrics()
	if got == nil {
		t.Fatalf("expected injected metrics recorder to be returned by getMetrics()")
	}
}

func TestAuthService_SetLanguageResolver(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	if svc.languageResolver != nil {
		t.Fatalf("expected nil language resolver before set")
	}

	lr := &languageResolverStub{language: "tr"}
	svc.SetLanguageResolver(lr)

	if svc.languageResolver == nil {
		t.Fatalf("expected language resolver to be set")
	}
}

func TestAuthService_SetNotificationPreferenceCreator(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	if svc.prefCreator != nil {
		t.Fatalf("expected nil pref creator before set")
	}

	pc := &prefCreatorStub{}
	svc.SetNotificationPreferenceCreator(pc)

	if svc.prefCreator == nil {
		t.Fatalf("expected pref creator to be set")
	}
}

// ---------------------------------------------------------------------------
// resolveUserLanguage tests
// ---------------------------------------------------------------------------

func TestAuthService_ResolveUserLanguage_NilResolver(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	lang := svc.resolveUserLanguage(uuid.New())
	if lang != "en" {
		t.Fatalf("expected 'en' when resolver is nil, got %q", lang)
	}
}

func TestAuthService_ResolveUserLanguage_WithValidLanguage(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.SetLanguageResolver(&languageResolverStub{language: "tr"})

	lang := svc.resolveUserLanguage(uuid.New())
	if lang != "tr" {
		t.Fatalf("expected 'tr', got %q", lang)
	}
}

func TestAuthService_ResolveUserLanguage_ResolverError(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.SetLanguageResolver(&languageResolverStub{language: "", err: errors.New("db down")})

	lang := svc.resolveUserLanguage(uuid.New())
	if lang != "en" {
		t.Fatalf("expected 'en' on resolver error, got %q", lang)
	}
}

func TestAuthService_ResolveUserLanguage_EmptyLanguage(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.SetLanguageResolver(&languageResolverStub{language: ""})

	lang := svc.resolveUserLanguage(uuid.New())
	if lang != "en" {
		t.Fatalf("expected 'en' on empty language, got %q", lang)
	}
}

func TestAuthService_ResolveUserLanguage_InvalidLanguageCode(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.SetLanguageResolver(&languageResolverStub{language: "123"})

	lang := svc.resolveUserLanguage(uuid.New())
	if lang != "en" {
		t.Fatalf("expected 'en' on invalid language code, got %q", lang)
	}
}

// ---------------------------------------------------------------------------
// ForceDisable2FA tests
// ---------------------------------------------------------------------------

func TestAuthService_ForceDisable2FA_Success(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "admin@example.com", "admin", "StrongPass123!")
	user.TwoFactorEnabled = true
	user.TwoFactorSecret = "encrypted-secret"
	user.TwoFactorBackupCodes = "hash1,hash2"

	updateCalled := false
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			updateCalled = true
			if u.TwoFactorEnabled {
				t.Fatalf("expected TwoFactorEnabled to be false")
			}
			if u.TwoFactorSecret != "" {
				t.Fatalf("expected TwoFactorSecret to be cleared")
			}
			if u.TwoFactorBackupCodes != "" {
				t.Fatalf("expected TwoFactorBackupCodes to be cleared")
			}
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	if err := svc.ForceDisable2FA(user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !updateCalled {
		t.Fatalf("expected user update to be called")
	}
}

func TestAuthService_ForceDisable2FA_UserNotFound(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.ForceDisable2FA(uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestAuthService_ForceDisable2FA_NotEnabled(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "admin@example.com", "admin", "StrongPass123!")
	user.TwoFactorEnabled = false

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.ForceDisable2FA(user.ID)
	assertProblem(t, err, http.StatusBadRequest, "Two-factor authentication is not enabled")
}

func TestAuthService_ForceDisable2FA_UpdateFails(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "admin@example.com", "admin", "StrongPass123!")
	user.TwoFactorEnabled = true
	user.TwoFactorSecret = "encrypted"

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			return errors.New("db error")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.ForceDisable2FA(user.ID)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to disable two-factor authentication")
}

// ---------------------------------------------------------------------------
// Validate2FALogin tests
// ---------------------------------------------------------------------------

func TestAuthService_Validate2FALogin_Success(t *testing.T) {
	cfg := test.TestConfig()

	// Create a 2FA-enabled user
	user := mustAuthUser(t, "2fa@example.com", "twofa_user", "StrongPass123!")

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

	updateCalls := 0
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			u.Roles = []domain.Role{
				{
					Name:        "user",
					Permissions: []domain.Permission{{Name: "users.read"}},
				},
			}
			return nil
		},
		updateFn: func(u *domain.User) error {
			updateCalls++
			return nil
		},
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

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

	resp, err := svc.Validate2FALogin(twoFactorToken, code, "127.0.0.1", "TestAgent/1.0")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if resp.AccessToken == "" {
		t.Fatalf("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Fatalf("expected non-empty refresh token")
	}
	if resp.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero expiry")
	}
	if resp.User == nil {
		t.Fatalf("expected user in response")
	}
	if resp.User.Password != "" {
		t.Fatalf("expected password to be cleared in response")
	}
	if resp.RequiresTwoFactor {
		t.Fatalf("expected RequiresTwoFactor to be false after successful 2FA")
	}
	if resp.User.LastLogin == nil {
		t.Fatalf("expected LastLogin to be updated")
	}
}

func TestAuthService_Validate2FALogin_EmptyTokenOrCode(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Validate2FALogin("", "123456", "", "")
	assertProblem(t, err, http.StatusBadRequest, "Two-factor token and code are required")

	_, err = svc.Validate2FALogin("some-token", "", "", "")
	assertProblem(t, err, http.StatusBadRequest, "Two-factor token and code are required")
}

func TestAuthService_Validate2FALogin_InvalidTwoFactorToken(t *testing.T) {
	cfg := test.TestConfig()
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Validate2FALogin("invalid-token", "123456", "", "")
	if err == nil {
		t.Fatalf("expected error for invalid 2FA token, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got %T", err)
	}
	if pd.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", pd.Status)
	}
}

func TestAuthService_Validate2FALogin_InvalidTOTPCode(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "2fa@example.com", "twofa_user", "StrongPass123!")

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
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	_, err = svc.Validate2FALogin(twoFactorToken, "000000", "", "")
	if err == nil {
		t.Fatalf("expected error for invalid TOTP code, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got %T", err)
	}
	if pd.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid TOTP, got %d", pd.Status)
	}
}

func TestAuthService_Validate2FALogin_UserNotFound(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(userID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	_, err = svc.Validate2FALogin(twoFactorToken, "123456", "", "")
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestAuthService_Validate2FALogin_LoadRolesFails(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "2fa@example.com", "twofa_user", "StrongPass123!")

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
			return errors.New("db unavailable")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}

	_, err = svc.Validate2FALogin(twoFactorToken, code, "", "")
	assertProblem(t, err, http.StatusInternalServerError, "Failed to load user roles")
}

func TestAuthService_Validate2FALogin_WithBackupCode(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "2fa@example.com", "twofa_user", "StrongPass123!")

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

	backupCode := "my-backup-code"
	backupHash := cryptoutil.HashSHA256Hex(backupCode)

	user.TwoFactorEnabled = true
	user.TwoFactorSecret = encSecret
	user.TwoFactorBackupCodes = backupHash

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			u.Roles = []domain.Role{{Name: "user"}}
			return nil
		},
		updateFn:             func(u *domain.User) error { return nil },
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	resp, err := svc.Validate2FALogin(twoFactorToken, backupCode, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("expected backup code login success, got %v", err)
	}
	if resp == nil || resp.AccessToken == "" {
		t.Fatalf("expected valid response with tokens")
	}
}

// ---------------------------------------------------------------------------
// RefreshToken success path
// ---------------------------------------------------------------------------

func TestAuthService_RefreshToken_SuccessWithSessionMeta(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Roles = []domain.Role{
		{
			Name:        "user",
			Permissions: []domain.Permission{{Name: "users.read"}},
		},
	}

	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(tokenHash string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: tokenHash, Revoked: false}, nil
		},
		revokeRefreshTokenFn: func(token string) error { return nil },
		getByIDFn:            func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			u.Roles = user.Roles
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	// Generate a valid refresh token
	refresh, err := svc.tokenService.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	// Refresh with session meta
	pair, err := svc.RefreshToken(refresh, SessionMeta{
		IPAddress: "10.0.0.1",
		UserAgent: "TestAgent/1.0",
	})
	if err != nil {
		t.Fatalf("expected refresh success, got %v", err)
	}
	if pair == nil {
		t.Fatalf("expected non-nil token pair")
	}
	if pair.AccessToken == "" {
		t.Fatalf("expected non-empty access token")
	}
	if pair.RefreshToken == "" {
		t.Fatalf("expected non-empty refresh token (rotated)")
	}
	if pair.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero expiry")
	}

	// Validate the new access token contains correct claims
	claims, err := svc.tokenService.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("expected new access token to be valid: %v", err)
	}
	if claims.UserID != user.ID {
		t.Fatalf("expected user ID %s, got %s", user.ID, claims.UserID)
	}
}

func TestAuthService_RefreshToken_UserNotFound(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")

	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(tokenHash string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: tokenHash, Revoked: false}, nil
		},
		revokeRefreshTokenFn: func(token string) error { return nil },
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	refresh, err := svc.tokenService.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.RefreshToken(refresh)
	assertProblem(t, err, http.StatusUnauthorized, "Invalid refresh token")
}

func TestAuthService_RefreshToken_LoadRolesFails(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")

	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(tokenHash string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: tokenHash, Revoked: false}, nil
		},
		revokeRefreshTokenFn: func(token string) error { return nil },
		getByIDFn:            func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error {
			return errors.New("db unavailable")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	refresh, err := svc.tokenService.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.RefreshToken(refresh)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to load user roles")
}

// ---------------------------------------------------------------------------
// ValidatePasswordResetToken success path
// ---------------------------------------------------------------------------

func TestAuthService_ValidatePasswordResetToken_Success(t *testing.T) {
	cfg := test.TestConfig()
	vr := &verificationRepoStub{
		findByTokenFn: func(token string) (*domain.VerificationToken, error) {
			return &domain.VerificationToken{
				Token:     token,
				Type:      domain.TokenTypePasswordReset,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	if err := svc.ValidatePasswordResetToken("valid-reset-token"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestAuthService_ValidatePasswordResetToken_Expired(t *testing.T) {
	cfg := test.TestConfig()
	vr := &verificationRepoStub{
		findByTokenFn: func(token string) (*domain.VerificationToken, error) {
			return &domain.VerificationToken{
				Token:     token,
				Type:      domain.TokenTypePasswordReset,
				ExpiresAt: time.Now().Add(-time.Minute),
			}, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	err := svc.ValidatePasswordResetToken("expired-token")
	assertProblem(t, err, http.StatusBadRequest, "Password reset token has expired")
}

func TestAuthService_ValidatePasswordResetToken_AlreadyUsed(t *testing.T) {
	cfg := test.TestConfig()
	now := time.Now()
	vr := &verificationRepoStub{
		findByTokenFn: func(token string) (*domain.VerificationToken, error) {
			return &domain.VerificationToken{
				Token:     token,
				Type:      domain.TokenTypePasswordReset,
				ExpiresAt: time.Now().Add(time.Hour),
				Used:      true,
				UsedAt:    &now,
			}, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	err := svc.ValidatePasswordResetToken("used-token")
	assertProblem(t, err, http.StatusBadRequest, "Password reset token has already been used")
}

// ---------------------------------------------------------------------------
// assignDefaultRole — role not found, creates new role
// ---------------------------------------------------------------------------

func TestAuthService_AssignDefaultRole_CreatesRoleWhenNotFound(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "new@example.com", "newuser", "StrongPass123!")

	roleCreated := false
	roleAssigned := false

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = user.ID
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return nil, errors.New("not found")
		},
		createRoleFn: func(role *domain.Role) error {
			roleCreated = true
			role.ID = uuid.New()
			return nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error {
			roleAssigned = true
			return nil
		},
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-token"
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "new@example.com",
		Username: "newuser",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected register success, got %v", err)
	}
	if !roleCreated {
		t.Fatalf("expected default role to be created when not found")
	}
	if !roleAssigned {
		t.Fatalf("expected role to be assigned")
	}
}

func TestAuthService_AssignDefaultRole_CreateRoleFails(t *testing.T) {
	cfg := test.TestConfig()

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return nil, errors.New("not found")
		},
		createRoleFn: func(role *domain.Role) error {
			return errors.New("db error creating role")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "new@example.com",
		Username: "newuser",
		Password: "StrongPass123!",
	})
	assertProblem(t, err, http.StatusInternalServerError, "Failed to assign default role")
}

// ---------------------------------------------------------------------------
// Event publisher integration (sendVerificationEmail, sendPasswordResetEmailNotification,
// sendPasswordChangedEmail via event publisher)
// ---------------------------------------------------------------------------

func TestAuthService_Register_DispatchesUserRegisteredEvent(t *testing.T) {
	cfg := test.TestConfig()
	dispatched := false
	roleID := uuid.New()

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-token"
			return nil
		},
	}
	pub := &eventPublisherStub{
		dispatchUserRegisteredFn: func(ctx context.Context, userID uuid.UUID, email, username, lang string) error {
			dispatched = true
			return nil
		},
		dispatchEmailVerificationFn: func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})
	svc.SetEventPublisher(pub)

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "event@example.com",
		Username: "eventuser",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !dispatched {
		t.Fatalf("expected user registered event to be dispatched")
	}
}

func TestAuthService_Register_EventPublisherFailureDoesNotBreakRegistration(t *testing.T) {
	cfg := test.TestConfig()
	roleID := uuid.New()

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-token"
			return nil
		},
	}
	pub := &eventPublisherStub{
		dispatchUserRegisteredFn: func(ctx context.Context, userID uuid.UUID, email, username, lang string) error {
			return errors.New("rabbitmq down")
		},
		dispatchEmailVerificationFn: func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
			return errors.New("rabbitmq down")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})
	svc.SetEventPublisher(pub)

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "event@example.com",
		Username: "eventuser",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected registration to succeed despite event publisher failure, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Password reset via event publisher path
// ---------------------------------------------------------------------------

func TestAuthService_RequestPasswordReset_UsesEventPublisher(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	emailDispatched := false

	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "reset-token"
			return nil
		},
	}
	pub := &eventPublisherStub{
		dispatchEmailPasswordResetFn: func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
			emailDispatched = true
			if token != "reset-token" {
				t.Fatalf("expected raw token propagated, got %q", token)
			}
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})
	svc.SetEventPublisher(pub)

	if err := svc.RequestPasswordReset(context.Background(), user.Email); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !emailDispatched {
		t.Fatalf("expected password reset email dispatched via event publisher")
	}
}

// ---------------------------------------------------------------------------
// Password change via event publisher path
// ---------------------------------------------------------------------------

func TestAuthService_ChangePassword_UsesEventPublisherForNotification(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "OldPassword123!")
	passwordChangedDispatched := false

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	pub := &eventPublisherStub{
		dispatchPasswordChangedFn: func(ctx context.Context, userID uuid.UUID, email, fullName, lang string) error {
			passwordChangedDispatched = true
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.SetEventPublisher(pub)

	if err := svc.ChangePassword(context.Background(), user.ID, "OldPassword123!", "NewPassword123!"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !passwordChangedDispatched {
		t.Fatalf("expected password changed event to be dispatched")
	}
}

// ---------------------------------------------------------------------------
// NotificationPreferenceCreator on Register
// ---------------------------------------------------------------------------

func TestAuthService_Register_CallsPrefCreator(t *testing.T) {
	cfg := test.TestConfig()
	roleID := uuid.New()
	pc := &prefCreatorStub{}

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-token"
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})
	svc.SetNotificationPreferenceCreator(pc)

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "prefs@example.com",
		Username: "prefsuser",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !pc.called {
		t.Fatalf("expected pref creator to be called")
	}
}

func TestAuthService_Register_PrefCreatorFailureDoesNotBreakRegistration(t *testing.T) {
	cfg := test.TestConfig()
	roleID := uuid.New()
	pc := &prefCreatorStub{err: errors.New("pref creation failed")}

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-token"
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})
	svc.SetNotificationPreferenceCreator(pc)

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "prefs@example.com",
		Username: "prefsuser",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected registration to succeed despite pref creator failure, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Metrics integration
// ---------------------------------------------------------------------------

func TestAuthService_Login_RecordsMetrics(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "StrongPass123!", 0)
	m := &metricsStub{}

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) { return user, nil },
		loadRolesFunc:  func(u *domain.User) error { return nil },
		updateFunc:     func(u *domain.User) error { return nil },
	}
	svc := newAuthServiceForLoginTest(t, repo)
	svc.SetMetrics(m)

	_, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected login success, got %v", err)
	}
	if m.loginAttempts != 1 {
		t.Fatalf("expected 1 login attempt recorded, got %d", m.loginAttempts)
	}
	if !m.lastLoginSuccess {
		t.Fatalf("expected successful login recorded")
	}
	if m.lastLoginMethod != "credentials" {
		t.Fatalf("expected method 'credentials', got %q", m.lastLoginMethod)
	}
}

func TestAuthService_Login_2FAPath_RecordsMetrics(t *testing.T) {
	cfg := test.TestConfig()
	user := mustCreateUserWithPassword(t, "2fa@example.com", "twofauser", "StrongPass123!", 0)

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

	m := &metricsStub{}

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) { return user, nil },
		updateFunc:     func(u *domain.User) error { return nil },
	}
	svc := newAuthServiceForLoginTest(t, repo)
	svc.SetMetrics(m)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected login to return 2FA challenge, got error: %v", err)
	}
	if !resp.RequiresTwoFactor {
		t.Fatalf("expected RequiresTwoFactor to be true")
	}
	if resp.TwoFactorToken == "" {
		t.Fatalf("expected non-empty 2FA token")
	}
	if m.loginAttempts != 1 {
		t.Fatalf("expected 1 login attempt recorded, got %d", m.loginAttempts)
	}
	if m.lastLoginMethod != "credentials_2fa_pending" {
		t.Fatalf("expected method 'credentials_2fa_pending', got %q", m.lastLoginMethod)
	}
}

// ---------------------------------------------------------------------------
// Resend verification email with language resolver
// ---------------------------------------------------------------------------

func TestAuthService_ResendVerificationEmail_UsesLanguageResolver(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = false
	user.Status = domain.UserStatusPending

	var resolvedLanguage string
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "verification-token"
			return nil
		},
	}
	emailStub := &enhancedEmailStub{
		sendVerificationFn: func(to, username, token, languageCode string) error {
			resolvedLanguage = languageCode
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, emailStub)
	svc.SetLanguageResolver(&languageResolverStub{language: "de"})

	if err := svc.ResendVerificationEmail(context.Background(), user.Email); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resolvedLanguage != "de" {
		t.Fatalf("expected language 'de', got %q", resolvedLanguage)
	}
}

// ---------------------------------------------------------------------------
// Validate2FALogin with metrics and session cache
// ---------------------------------------------------------------------------

func TestAuthService_Validate2FALogin_CachesSessionAndRecordsMetrics(t *testing.T) {
	cfg := test.TestConfig()

	user := mustAuthUser(t, "2fa@example.com", "twofa_user", "StrongPass123!")

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
				{
					Name:        "user",
					Permissions: []domain.Permission{{Name: "users.read"}},
				},
			}
			return nil
		},
		updateFn:             func(u *domain.User) error { return nil },
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
	}

	m := &metricsStub{}
	cacheCalled := false
	sc := &fakeSessionCache{
		setPermissionsFunc: func(ctx context.Context, userID string, roles, permissions []string) error {
			cacheCalled = true
			return nil
		},
	}

	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.SetMetrics(m)
	svc.SetSessionCache(sc)

	twoFactorToken, err := svc.tokenService.GenerateTwoFactorToken(user.ID)
	if err != nil {
		t.Fatalf("failed to generate 2FA token: %v", err)
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}

	resp, err := svc.Validate2FALogin(twoFactorToken, code, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if m.loginAttempts != 1 {
		t.Fatalf("expected 1 login attempt, got %d", m.loginAttempts)
	}
	if m.lastLoginMethod != "credentials_2fa" {
		t.Fatalf("expected method 'credentials_2fa', got %q", m.lastLoginMethod)
	}
	if !cacheCalled {
		t.Fatalf("expected session cache to be invoked")
	}
}

// ---------------------------------------------------------------------------
// sendResendVerificationEmail — enhanced email direct path (no event publisher)
// ---------------------------------------------------------------------------

func TestAuthService_ResendVerificationEmail_EnhancedEmailFallback(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = false
	user.Status = domain.UserStatusPending

	var sentTo string
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "raw-verification-token"
			return nil
		},
	}
	emailStub := &enhancedEmailStub{
		sendVerificationFn: func(to, username, token, languageCode string) error {
			sentTo = to
			return nil
		},
	}
	// No event publisher set — should use enhanced email directly
	svc := newAuthServiceWithStubs(cfg, repo, vr, emailStub)

	if err := svc.ResendVerificationEmail(context.Background(), user.Email); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if sentTo != user.Email {
		t.Fatalf("expected enhanced email to be called for %s, got %q", user.Email, sentTo)
	}
}

func TestAuthService_ResendVerificationEmail_EnhancedEmailFailsReturnsError(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = false
	user.Status = domain.UserStatusPending

	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "raw-token"
			return nil
		},
	}
	emailStub := &enhancedEmailStub{
		sendVerificationFn: func(to, username, token, languageCode string) error {
			return errors.New("smtp failed")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, emailStub)

	err := svc.ResendVerificationEmail(context.Background(), user.Email)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to resend verification email")
}

func TestAuthService_ResendVerificationEmail_EventPublisherFallsBackToEnhancedEmail(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = false
	user.Status = domain.UserStatusPending

	enhancedCalled := false
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "raw-token"
			return nil
		},
	}
	emailStub := &enhancedEmailStub{
		sendVerificationFn: func(to, username, token, languageCode string) error {
			enhancedCalled = true
			return nil
		},
	}
	pub := &eventPublisherStub{
		dispatchEmailVerificationFn: func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
			return errors.New("rabbitmq down")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, emailStub)
	svc.SetEventPublisher(pub)

	if err := svc.ResendVerificationEmail(context.Background(), user.Email); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !enhancedCalled {
		t.Fatalf("expected enhanced email to be called as fallback")
	}
}

// ---------------------------------------------------------------------------
// sendPasswordChangedEmail — enhanced email direct path + fallback
// ---------------------------------------------------------------------------

func TestAuthService_ChangePassword_EventPublisherFallsBackToEnhancedEmail(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "OldPassword123!")
	enhancedCalled := false

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	pub := &eventPublisherStub{
		dispatchPasswordChangedFn: func(ctx context.Context, userID uuid.UUID, email, fullName, lang string) error {
			return errors.New("rabbitmq down")
		},
	}
	emailStub := &enhancedEmailStub{
		// The enhancedEmailStub.SendPasswordChangedEmail returns nil by default
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, emailStub)
	svc.SetEventPublisher(pub)

	// Override enhanced email to track call
	svc.enhancedEmailService = &enhancedEmailTracker{called: &enhancedCalled}

	if err := svc.ChangePassword(context.Background(), user.ID, "OldPassword123!", "NewPassword123!"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !enhancedCalled {
		t.Fatalf("expected enhanced email fallback to be called after event publisher failure")
	}
}

type enhancedEmailTracker struct {
	called *bool
}

func (e *enhancedEmailTracker) SendVerificationEmail(to, username, token, languageCode string) error {
	return nil
}

func (e *enhancedEmailTracker) SendPasswordResetEmail(to, username, token, languageCode string) error {
	return nil
}

func (e *enhancedEmailTracker) SendPasswordChangedEmail(to, fullName, languageCode string) error {
	*e.called = true
	return nil
}

// ---------------------------------------------------------------------------
// sendPasswordResetEmailNotification — event publisher fallback
// ---------------------------------------------------------------------------

func TestAuthService_RequestPasswordReset_EventPublisherFallsBackToEnhancedEmail(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	enhancedResetCalled := false

	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "reset-token"
			return nil
		},
	}
	emailStub := &enhancedEmailStub{
		sendPasswordResetFn: func(to, username, token, languageCode string) error {
			enhancedResetCalled = true
			return nil
		},
	}
	pub := &eventPublisherStub{
		dispatchEmailPasswordResetFn: func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
			return errors.New("rabbitmq down")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, emailStub)
	svc.SetEventPublisher(pub)

	if err := svc.RequestPasswordReset(context.Background(), user.Email); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !enhancedResetCalled {
		t.Fatalf("expected enhanced email fallback for password reset")
	}
}

// ---------------------------------------------------------------------------
// sendVerificationEmail — event publisher success path (register)
// ---------------------------------------------------------------------------

func TestAuthService_Register_VerificationEmailViaEventPublisher(t *testing.T) {
	cfg := test.TestConfig()
	roleID := uuid.New()
	emailDispatched := false

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-raw-token"
			return nil
		},
	}
	pub := &eventPublisherStub{
		dispatchUserRegisteredFn: func(ctx context.Context, userID uuid.UUID, email, username, lang string) error {
			return nil
		},
		dispatchEmailVerificationFn: func(ctx context.Context, userID uuid.UUID, email, username, token, lang string) error {
			emailDispatched = true
			if token != "test-raw-token" {
				t.Fatalf("expected raw token propagated, got %q", token)
			}
			return nil
		},
	}
	// Use nil enhanced email to prove event publisher was the path taken
	svc := newAuthServiceWithStubs(cfg, repo, vr, nil)
	svc.SetEventPublisher(pub)

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "eventverify@example.com",
		Username: "eventverify",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !emailDispatched {
		t.Fatalf("expected verification email dispatched via event publisher")
	}
}

// ---------------------------------------------------------------------------
// ChangePassword — no event publisher, no enhanced email (nil emailSvc path)
// ---------------------------------------------------------------------------

func TestAuthService_ChangePassword_NoEmailSendersConfigured(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "OldPassword123!")

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	// No event publisher, no enhanced email, no basic email
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, nil)

	if err := svc.ChangePassword(context.Background(), user.ID, "OldPassword123!", "NewPassword123!"); err != nil {
		t.Fatalf("expected success even with no email senders, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Register with no enhanced email and no event publisher (nil both paths)
// ---------------------------------------------------------------------------

func TestAuthService_Register_NoEmailSendersConfigured(t *testing.T) {
	cfg := test.TestConfig()
	roleID := uuid.New()

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(u *domain.User) error {
			u.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "test-token"
			return nil
		},
	}
	// No event publisher, no enhanced email, no basic email
	svc := newAuthServiceWithStubs(cfg, repo, vr, nil)

	_, err := svc.Register(context.Background(), &RegisterRequest{
		Email:    "noemail@example.com",
		Username: "noemail",
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// RequestPasswordReset with no enhanced email (nil path for send)
// ---------------------------------------------------------------------------

func TestAuthService_RequestPasswordReset_NoEmailSendersConfigured(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")

	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return 0, nil
		},
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error { return nil },
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "reset-token"
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, nil)

	if err := svc.RequestPasswordReset(context.Background(), user.Email); err != nil {
		t.Fatalf("expected success even with no email senders, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Login with 2FA — generates two-factor token (covers login 2FA path more)
// ---------------------------------------------------------------------------

func TestAuthService_Login_2FA_GeneratesTokenAndReturnsChallenge(t *testing.T) {
	cfg := test.TestConfig()
	user := mustCreateUserWithPassword(t, "2fa@example.com", "twofauser", "StrongPass123!", 3)

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

	updateCalled := false
	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) { return user, nil },
		updateFunc: func(u *domain.User) error {
			updateCalled = true
			// FailedLoginAttempts should be reset to 0 since password was correct
			if u.FailedLoginAttempts != 0 {
				t.Fatalf("expected failed attempts reset, got %d", u.FailedLoginAttempts)
			}
			return nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "StrongPass123!",
	})
	if err != nil {
		t.Fatalf("expected 2FA challenge, got error: %v", err)
	}
	if !resp.RequiresTwoFactor {
		t.Fatalf("expected RequiresTwoFactor=true")
	}
	if resp.TwoFactorToken == "" {
		t.Fatalf("expected non-empty TwoFactorToken")
	}
	if resp.AccessToken != "" {
		t.Fatalf("expected empty AccessToken in 2FA challenge")
	}
	if resp.User.Password != "" {
		t.Fatalf("expected password cleared in response")
	}
	if !updateCalled {
		t.Fatalf("expected user update to be called")
	}
}

// ---------------------------------------------------------------------------
// Enable2FA — error paths
// ---------------------------------------------------------------------------

func TestAuthService_Enable2FA_UserNotFound(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Enable2FA(uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestAuthService_Enable2FA_AlreadyEnabled(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "2fa@example.com", "twofauser", "StrongPass123!")
	user.TwoFactorEnabled = true

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Enable2FA(user.ID)
	assertProblem(t, err, http.StatusConflict, "Two-factor authentication is already enabled")
}

// ---------------------------------------------------------------------------
// Verify2FA — error paths
// ---------------------------------------------------------------------------

func TestAuthService_Verify2FA_UserNotFound(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.Verify2FA(uuid.New(), "123456")
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestAuthService_Verify2FA_AlreadyEnabled(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "2fa@example.com", "twofauser", "StrongPass123!")
	user.TwoFactorEnabled = true

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.Verify2FA(user.ID, "123456")
	assertProblem(t, err, http.StatusConflict, "Two-factor authentication is already enabled")
}

func TestAuthService_Verify2FA_NoSecretSet(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "2fa@example.com", "twofauser", "StrongPass123!")
	user.TwoFactorSecret = "" // no secret set

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.Verify2FA(user.ID, "123456")
	assertProblem(t, err, http.StatusBadRequest, "Two-factor authentication has not been initiated. Please call enable first.")
}

func TestAuthService_Verify2FA_InvalidCode(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "2fa@example.com", "twofauser", "StrongPass123!")

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

	user.TwoFactorSecret = encSecret

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err = svc.Verify2FA(user.ID, "000000")
	assertProblem(t, err, http.StatusBadRequest, "Invalid two-factor code")
}

// ---------------------------------------------------------------------------
// Disable2FA — error paths
// ---------------------------------------------------------------------------

func TestAuthService_Disable2FA_UserNotFound(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.Disable2FA(uuid.New(), "123456")
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestAuthService_Disable2FA_NotEnabled(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "2fa@example.com", "twofauser", "StrongPass123!")
	user.TwoFactorEnabled = false

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err := svc.Disable2FA(user.ID, "123456")
	assertProblem(t, err, http.StatusBadRequest, "Two-factor authentication is not enabled")
}

func TestAuthService_Disable2FA_InvalidCode(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "2fa@example.com", "twofauser", "StrongPass123!")

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
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	err = svc.Disable2FA(user.ID, "000000")
	assertProblem(t, err, http.StatusBadRequest, "Invalid two-factor code")
}

// ---------------------------------------------------------------------------
// VerifyEmail — already verified path
// ---------------------------------------------------------------------------

func TestAuthService_VerifyEmail_AlreadyVerified(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = true

	token := &domain.VerificationToken{
		UserID:    user.ID,
		Token:     "valid-token",
		Type:      domain.TokenTypeEmailVerification,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		findByTokenFn: func(tok string) (*domain.VerificationToken, error) { return token, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})

	err := svc.VerifyEmail(context.Background(), "valid-token")
	assertProblem(t, err, http.StatusConflict, "Email already verified")
}
