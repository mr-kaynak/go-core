package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	cryptoutil "github.com/mr-kaynak/go-core/internal/core/crypto"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/test"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

type authRepoStub struct {
	createFn                 func(user *domain.User) error
	updateFn                 func(user *domain.User) error
	getByIDFn                func(id uuid.UUID) (*domain.User, error)
	getByEmailFn             func(email string) (*domain.User, error)
	existsByEmailFn          func(email string) (bool, error)
	existsByUsernameFn       func(username string) (bool, error)
	loadRolesFn              func(user *domain.User) error
	getRoleByNameFn          func(name string) (*domain.Role, error)
	createRoleFn             func(role *domain.Role) error
	assignRoleFn             func(userID, roleID uuid.UUID) error
	createRefreshTokenFn     func(token *domain.RefreshToken) error
	getRefreshTokenFn        func(token string) (*domain.RefreshToken, error)
	revokeRefreshTokenFn     func(token string) error
	revokeAllRefreshTokenFn  func(userID uuid.UUID) error
	cleanExpiredRefreshFn    func() error
	getByUsernameFn          func(username string) (*domain.User, error)
	getAllFn                 func(offset, limit int) ([]*domain.User, error)
	countFn                  func() (int64, error)
	deleteFn                 func(id uuid.UUID) error
	createPermissionFn       func(permission *domain.Permission) error
	updatePermissionFn       func(permission *domain.Permission) error
	deletePermissionFn       func(id uuid.UUID) error
	getPermissionByIDFn      func(id uuid.UUID) (*domain.Permission, error)
	getAllPermissionsFn      func() ([]*domain.Permission, error)
	assignPermissionToRoleFn func(roleID, permissionID uuid.UUID) error
	removePermissionFn       func(roleID, permissionID uuid.UUID) error
	getRolePermissionsFn     func(roleID uuid.UUID) ([]*domain.Permission, error)
	updateRoleFn             func(role *domain.Role) error
	deleteRoleFn             func(id uuid.UUID) error
	getRoleByIDFn            func(id uuid.UUID) (*domain.Role, error)
	getAllRolesFn            func() ([]*domain.Role, error)
	removeRoleFn             func(userID, roleID uuid.UUID) error
	getUserRolesFn           func(userID uuid.UUID) ([]*domain.Role, error)
}

var _ repository.UserRepository = (*authRepoStub)(nil)

func (s *authRepoStub) WithTx(_ *gorm.DB) repository.UserRepository { return s }

func (s *authRepoStub) Create(user *domain.User) error {
	if s.createFn != nil {
		return s.createFn(user)
	}
	return nil
}

func (s *authRepoStub) Update(user *domain.User) error {
	if s.updateFn != nil {
		return s.updateFn(user)
	}
	return nil
}

func (s *authRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}

func (s *authRepoStub) GetByID(id uuid.UUID) (*domain.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

func (s *authRepoStub) GetByEmail(email string) (*domain.User, error) {
	if s.getByEmailFn != nil {
		return s.getByEmailFn(email)
	}
	return nil, nil
}

func (s *authRepoStub) GetByUsername(username string) (*domain.User, error) {
	if s.getByUsernameFn != nil {
		return s.getByUsernameFn(username)
	}
	return nil, nil
}

func (s *authRepoStub) GetAll(offset, limit int) ([]*domain.User, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return nil, nil
}

func (s *authRepoStub) ListFiltered(_ domain.UserListFilter) ([]*domain.User, int64, error) {
	return nil, 0, nil
}

func (s *authRepoStub) Count() (int64, error) {
	if s.countFn != nil {
		return s.countFn()
	}
	return 0, nil
}

func (s *authRepoStub) ExistsByEmail(email string) (bool, error) {
	if s.existsByEmailFn != nil {
		return s.existsByEmailFn(email)
	}
	return false, nil
}

func (s *authRepoStub) ExistsByUsername(username string) (bool, error) {
	if s.existsByUsernameFn != nil {
		return s.existsByUsernameFn(username)
	}
	return false, nil
}

func (s *authRepoStub) LoadRoles(user *domain.User) error {
	if s.loadRolesFn != nil {
		return s.loadRolesFn(user)
	}
	return nil
}

func (s *authRepoStub) CreateRole(role *domain.Role) error {
	if s.createRoleFn != nil {
		return s.createRoleFn(role)
	}
	return nil
}

func (s *authRepoStub) UpdateRole(role *domain.Role) error {
	if s.updateRoleFn != nil {
		return s.updateRoleFn(role)
	}
	return nil
}

func (s *authRepoStub) DeleteRole(id uuid.UUID) error {
	if s.deleteRoleFn != nil {
		return s.deleteRoleFn(id)
	}
	return nil
}

func (s *authRepoStub) GetRoleByID(id uuid.UUID) (*domain.Role, error) {
	if s.getRoleByIDFn != nil {
		return s.getRoleByIDFn(id)
	}
	return nil, nil
}

func (s *authRepoStub) GetRoleByName(name string) (*domain.Role, error) {
	if s.getRoleByNameFn != nil {
		return s.getRoleByNameFn(name)
	}
	return nil, nil
}

func (s *authRepoStub) GetAllRoles() ([]*domain.Role, error) {
	if s.getAllRolesFn != nil {
		return s.getAllRolesFn()
	}
	return nil, nil
}

func (s *authRepoStub) AssignRole(userID, roleID uuid.UUID) error {
	if s.assignRoleFn != nil {
		return s.assignRoleFn(userID, roleID)
	}
	return nil
}

func (s *authRepoStub) RemoveRole(userID, roleID uuid.UUID) error {
	if s.removeRoleFn != nil {
		return s.removeRoleFn(userID, roleID)
	}
	return nil
}

func (s *authRepoStub) GetUserRoles(userID uuid.UUID) ([]*domain.Role, error) {
	if s.getUserRolesFn != nil {
		return s.getUserRolesFn(userID)
	}
	return nil, nil
}

func (s *authRepoStub) CreatePermission(permission *domain.Permission) error {
	if s.createPermissionFn != nil {
		return s.createPermissionFn(permission)
	}
	return nil
}

func (s *authRepoStub) UpdatePermission(permission *domain.Permission) error {
	if s.updatePermissionFn != nil {
		return s.updatePermissionFn(permission)
	}
	return nil
}

func (s *authRepoStub) DeletePermission(id uuid.UUID) error {
	if s.deletePermissionFn != nil {
		return s.deletePermissionFn(id)
	}
	return nil
}

func (s *authRepoStub) GetPermissionByID(id uuid.UUID) (*domain.Permission, error) {
	if s.getPermissionByIDFn != nil {
		return s.getPermissionByIDFn(id)
	}
	return nil, nil
}

func (s *authRepoStub) GetAllPermissions() ([]*domain.Permission, error) {
	if s.getAllPermissionsFn != nil {
		return s.getAllPermissionsFn()
	}
	return nil, nil
}

func (s *authRepoStub) AssignPermissionToRole(roleID, permissionID uuid.UUID) error {
	if s.assignPermissionToRoleFn != nil {
		return s.assignPermissionToRoleFn(roleID, permissionID)
	}
	return nil
}

func (s *authRepoStub) RemovePermissionFromRole(roleID, permissionID uuid.UUID) error {
	if s.removePermissionFn != nil {
		return s.removePermissionFn(roleID, permissionID)
	}
	return nil
}

func (s *authRepoStub) GetRolePermissions(roleID uuid.UUID) ([]*domain.Permission, error) {
	if s.getRolePermissionsFn != nil {
		return s.getRolePermissionsFn(roleID)
	}
	return nil, nil
}

func (s *authRepoStub) CreateRefreshToken(token *domain.RefreshToken) error {
	if s.createRefreshTokenFn != nil {
		return s.createRefreshTokenFn(token)
	}
	return nil
}

func (s *authRepoStub) GetRefreshToken(token string) (*domain.RefreshToken, error) {
	if s.getRefreshTokenFn != nil {
		return s.getRefreshTokenFn(token)
	}
	return nil, nil
}

func (s *authRepoStub) RevokeRefreshToken(token string) error {
	if s.revokeRefreshTokenFn != nil {
		return s.revokeRefreshTokenFn(token)
	}
	return nil
}

func (s *authRepoStub) RevokeAllUserRefreshTokens(userID uuid.UUID) error {
	if s.revokeAllRefreshTokenFn != nil {
		return s.revokeAllRefreshTokenFn(userID)
	}
	return nil
}

func (s *authRepoStub) GetActiveRefreshTokensByUser(userID uuid.UUID) ([]*domain.RefreshToken, error) {
	return nil, nil
}

func (s *authRepoStub) RevokeRefreshTokenByID(id uuid.UUID) error { return nil }

func (s *authRepoStub) CleanExpiredRefreshTokens() error {
	if s.cleanExpiredRefreshFn != nil {
		return s.cleanExpiredRefreshFn()
	}
	return nil
}
func (s *authRepoStub) CountByStatus(status string) (int64, error)       { return 0, nil }
func (s *authRepoStub) CountCreatedAfter(after time.Time) (int64, error) { return 0, nil }
func (s *authRepoStub) GetAllActiveSessions(offset, limit int) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (s *authRepoStub) CountActiveSessions() (int64, error) { return 0, nil }

type verificationRepoStub struct {
	createFn           func(token *domain.VerificationToken) error
	findByTokenFn      func(token string) (*domain.VerificationToken, error)
	findByUserTypeFn   func(userID uuid.UUID, tokenType domain.TokenType) (*domain.VerificationToken, error)
	updateFn           func(token *domain.VerificationToken) error
	deleteFn           func(id uuid.UUID) error
	deleteExpiredFn    func() error
	deleteByUserTypeFn func(userID uuid.UUID, tokenType domain.TokenType) error
	countByUserTypeFn  func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error)
}

var _ repository.VerificationTokenRepository = (*verificationRepoStub)(nil)

func (s *verificationRepoStub) WithTx(_ *gorm.DB) repository.VerificationTokenRepository { return s }

func (s *verificationRepoStub) Create(token *domain.VerificationToken) error {
	if s.createFn != nil {
		return s.createFn(token)
	}
	return nil
}

func (s *verificationRepoStub) FindByToken(token string) (*domain.VerificationToken, error) {
	if s.findByTokenFn != nil {
		return s.findByTokenFn(token)
	}
	return nil, nil
}

func (s *verificationRepoStub) FindByUserAndType(
	userID uuid.UUID,
	tokenType domain.TokenType,
) (*domain.VerificationToken, error) {
	if s.findByUserTypeFn != nil {
		return s.findByUserTypeFn(userID, tokenType)
	}
	return nil, nil
}

func (s *verificationRepoStub) Update(token *domain.VerificationToken) error {
	if s.updateFn != nil {
		return s.updateFn(token)
	}
	return nil
}

func (s *verificationRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}

func (s *verificationRepoStub) DeleteExpiredTokens() error {
	if s.deleteExpiredFn != nil {
		return s.deleteExpiredFn()
	}
	return nil
}

func (s *verificationRepoStub) DeleteByUserAndType(userID uuid.UUID, tokenType domain.TokenType) error {
	if s.deleteByUserTypeFn != nil {
		return s.deleteByUserTypeFn(userID, tokenType)
	}
	return nil
}

func (s *verificationRepoStub) CountByUserAndType(
	userID uuid.UUID,
	tokenType domain.TokenType,
	since time.Time,
) (int64, error) {
	if s.countByUserTypeFn != nil {
		return s.countByUserTypeFn(userID, tokenType, since)
	}
	return 0, nil
}

type enhancedEmailStub struct {
	sendVerificationFn  func(to, username, token, languageCode string) error
	sendPasswordResetFn func(to, username, token, languageCode string) error
}

func (s *enhancedEmailStub) SendVerificationEmail(to, username, token, languageCode string) error {
	if s.sendVerificationFn != nil {
		return s.sendVerificationFn(to, username, token, languageCode)
	}
	return nil
}

func (s *enhancedEmailStub) SendPasswordResetEmail(to, username, token, languageCode string) error {
	if s.sendPasswordResetFn != nil {
		return s.sendPasswordResetFn(to, username, token, languageCode)
	}
	return nil
}

type blacklistStub struct {
	blacklistFn         func(ctx context.Context, tokenHash string, expiry time.Duration) error
	isBlacklistedFn     func(ctx context.Context, tokenHash string) (bool, error)
	isUserBlacklistedFn func(ctx context.Context, userID string) (bool, error)
}

func (s *blacklistStub) IsBlacklisted(ctx context.Context, tokenHash string) (bool, error) {
	if s.isBlacklistedFn != nil {
		return s.isBlacklistedFn(ctx, tokenHash)
	}
	return false, nil
}

func (s *blacklistStub) IsUserBlacklisted(ctx context.Context, userID string) (bool, error) {
	if s.isUserBlacklistedFn != nil {
		return s.isUserBlacklistedFn(ctx, userID)
	}
	return false, nil
}

func (s *blacklistStub) Blacklist(ctx context.Context, tokenHash string, expiry time.Duration) error {
	if s.blacklistFn != nil {
		return s.blacklistFn(ctx, tokenHash, expiry)
	}
	return nil
}

func (s *blacklistStub) BlacklistUser(ctx context.Context, userID string, expiry time.Duration) error {
	return nil
}

func (s *blacklistStub) ClearUserBlacklist(ctx context.Context, userID string) error {
	return nil
}

func mustAuthUser(t *testing.T, email, username, rawPassword string) *domain.User {
	t.Helper()

	u := &domain.User{
		ID:       uuid.New(),
		Email:    email,
		Username: username,
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	if err := u.SetPassword(rawPassword); err != nil {
		t.Fatalf("failed to set password: %v", err)
	}
	return u
}

func newAuthServiceWithStubs(
	cfg *config.Config,
	repo repository.UserRepository,
	vr repository.VerificationTokenRepository,
	enhanced interface {
		SendVerificationEmail(to, username, token string, languageCode string) error
		SendPasswordResetEmail(to, username, token string, languageCode string) error
	},
) *AuthService {
	tokenSvc := NewTokenService(cfg, repo)
	return NewAuthService(cfg, nil, repo, tokenSvc, vr, nil, enhanced)
}

func assertProblem(t *testing.T, err error, status int, detail string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got %T", err)
	}
	if pd.Status != status {
		t.Fatalf("expected status %d, got %d", status, pd.Status)
	}
	if detail != "" && pd.Detail != detail {
		t.Fatalf("expected detail %q, got %q", detail, pd.Detail)
	}
}

func TestAuthServiceRegister_SuccessNormalizesAndAssignsDefaultRole(t *testing.T) {
	cfg := test.TestConfig()
	var (
		createdUser    *domain.User
		assignedRoleID uuid.UUID
		sentEmail      bool
	)
	roleID := uuid.New()

	repo := &authRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(user *domain.User) error {
			createdUser = user
			user.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleIDArg uuid.UUID) error {
			assignedRoleID = roleIDArg
			return nil
		},
	}
	vr := &verificationRepoStub{
		createFn: func(token *domain.VerificationToken) error {
			token.RawToken = "verification-token"
			token.Token = domain.HashVerificationToken("verification-token")
			return nil
		},
	}
	emailSvc := &enhancedEmailStub{
		sendVerificationFn: func(to, username, token, languageCode string) error {
			sentEmail = true
			if token != "verification-token" {
				t.Fatalf("expected verification token to be propagated")
			}
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, emailSvc)

	user, err := svc.Register(&RegisterRequest{
		Email:    "STAFF@Example.COM",
		Username: "Admin_User",
		Password: "StrongPass123!",
	})

	if err != nil {
		t.Fatalf("expected register success, got %v", err)
	}
	if createdUser == nil || user == nil {
		t.Fatalf("expected created user to be returned")
	}
	if createdUser.Email != "staff@example.com" {
		t.Fatalf("email was not normalized: %q", createdUser.Email)
	}
	if createdUser.Username != "admin_user" {
		t.Fatalf("username was not normalized: %q", createdUser.Username)
	}
	if assignedRoleID != roleID {
		t.Fatalf("expected default role assignment")
	}
	if !sentEmail {
		t.Fatalf("expected verification email to be sent")
	}
	if user.Password != "" {
		t.Fatalf("expected password to be cleared in response")
	}
}

func TestAuthServiceRegister_EmailConflict(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		existsByEmailFn: func(email string) (bool, error) { return true, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	_, err := svc.Register(&RegisterRequest{
		Email:    "staff@example.com",
		Username: "staff",
		Password: "StrongPass123!",
	})
	assertProblem(t, err, http.StatusConflict, "Email already registered")
}

func TestAuthServiceRefreshToken_RejectsWhenRevokeFails(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Roles = []domain.Role{{Name: "user"}}

	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(tokenHash string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: tokenHash, Revoked: false}, nil
		},
		revokeRefreshTokenFn: func(token string) error { return stderrors.New("db down") },
		getByIDFn:            func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn:          func(user *domain.User) error { return nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	refresh, err := svc.tokenService.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	_, err = svc.RefreshToken(refresh)
	if err == nil {
		t.Fatalf("expected refresh to fail when revoke fails, got nil")
	}
}

func TestAuthServiceRefreshToken_InactiveUserRejected(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = false
	user.Status = domain.UserStatusPending

	repo := &authRepoStub{
		createRefreshTokenFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshTokenFn: func(tokenHash string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: tokenHash, Revoked: false}, nil
		},
		revokeRefreshTokenFn: func(token string) error { return nil },
		getByIDFn:            func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})
	refresh, _ := svc.tokenService.GenerateRefreshToken(user)

	_, err := svc.RefreshToken(refresh)
	assertProblem(t, err, http.StatusUnauthorized, "User account is not active")
}

func TestAuthServiceLogout_PropagatesRevokeError(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	blacklistCalls := 0

	repo := &authRepoStub{
		revokeRefreshTokenFn: func(token string) error { return stderrors.New("revoke failed") },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.tokenService.SetBlacklist(&blacklistStub{
		blacklistFn: func(ctx context.Context, tokenHash string, expiry time.Duration) error {
			blacklistCalls++
			return stderrors.New("blacklist down")
		},
	})
	accessToken, _, _ := svc.tokenService.GenerateAccessToken(user)

	err := svc.Logout(user.ID, "some-refresh-token", accessToken)
	if err == nil {
		t.Fatal("expected logout to return error when refresh token revocation fails")
	}
	// Blacklist should still be attempted even though revoke failed
	if blacklistCalls != 1 {
		t.Fatalf("expected one blacklist call, got %d", blacklistCalls)
	}
}

func TestAuthServiceLogout_SucceedsWhenOnlyBlacklistFails(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")

	repo := &authRepoStub{
		revokeRefreshTokenFn: func(token string) error { return nil },
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})
	svc.tokenService.SetBlacklist(&blacklistStub{
		blacklistFn: func(ctx context.Context, tokenHash string, expiry time.Duration) error {
			return stderrors.New("blacklist down")
		},
	})
	accessToken, _, _ := svc.tokenService.GenerateAccessToken(user)

	if err := svc.Logout(user.ID, "some-refresh-token", accessToken); err != nil {
		t.Fatalf("expected logout to succeed when only blacklist fails, got %v", err)
	}
}

func TestAuthServiceVerifyEmail_Success(t *testing.T) {
	cfg := test.TestConfig()
	user := &domain.User{
		ID:       uuid.New(),
		Email:    "staff@example.com",
		Username: "staff",
		Status:   domain.UserStatusPending,
		Verified: false,
	}
	token := &domain.VerificationToken{
		UserID:    user.ID,
		Token:     "verify-token",
		Type:      domain.TokenTypeEmailVerification,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	updatedToken := false

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(user *domain.User) error {
			if !user.Verified || user.Status != domain.UserStatusActive {
				t.Fatalf("expected user to be verified and activated")
			}
			return nil
		},
	}
	vr := &verificationRepoStub{
		findByTokenFn: func(tok string) (*domain.VerificationToken, error) { return token, nil },
		updateFn: func(tok *domain.VerificationToken) error {
			updatedToken = tok.Used && tok.UsedAt != nil
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})

	if err := svc.VerifyEmail("verify-token"); err != nil {
		t.Fatalf("expected verify email success, got %v", err)
	}
	if !updatedToken {
		t.Fatalf("expected verification token to be marked as used")
	}
}

func TestAuthServiceResendVerificationEmail_RateLimited(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")
	user.Verified = false
	user.Status = domain.UserStatusPending

	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	vr := &verificationRepoStub{
		countByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
			return maxVerificationPerHour, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})

	err := svc.ResendVerificationEmail(user.Email)
	assertProblem(t, err, http.StatusTooManyRequests, "Too many verification email requests. Please try again later.")
}

func TestAuthServiceChangePassword_Success(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "OldPassword123!")
	updateCalls := 0

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(user *domain.User) error {
			updateCalls++
			if err := user.ComparePassword("NewPassword123!"); err != nil {
				t.Fatalf("expected password to be updated and hashed")
			}
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	if err := svc.ChangePassword(user.ID, "OldPassword123!", "NewPassword123!"); err != nil {
		t.Fatalf("expected change password success, got %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected one update call, got %d", updateCalls)
	}
}

func TestAuthServiceRequestPasswordReset_UserNotFoundIsNoop(t *testing.T) {
	cfg := test.TestConfig()
	repo := &authRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) {
			return nil, stderrors.New("not found")
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	if err := svc.RequestPasswordReset("missing@example.com"); err != nil {
		t.Fatalf("expected noop for missing user, got %v", err)
	}
}

func TestAuthServiceResetPassword_SuccessMarksTokenUsedAndCleansOldTokens(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "OldPassword123!")
	token := &domain.VerificationToken{
		UserID:    user.ID,
		Token:     "reset-token",
		Type:      domain.TokenTypePasswordReset,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	deleted := false

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(user *domain.User) error {
			if err := user.ComparePassword("NewPassword123!"); err != nil {
				t.Fatalf("expected password to be reset and hashed")
			}
			return nil
		},
	}
	vr := &verificationRepoStub{
		findByTokenFn: func(tokenString string) (*domain.VerificationToken, error) { return token, nil },
		updateFn:      func(tok *domain.VerificationToken) error { return nil },
		deleteByUserTypeFn: func(userID uuid.UUID, tokenType domain.TokenType) error {
			deleted = true
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, vr, &enhancedEmailStub{})

	if err := svc.ResetPassword("reset-token", "NewPassword123!"); err != nil {
		t.Fatalf("expected reset password success, got %v", err)
	}
	if !token.Used || token.UsedAt == nil {
		t.Fatalf("expected reset token to be marked as used")
	}
	if !deleted {
		t.Fatalf("expected old password reset tokens to be cleaned")
	}
}

func TestAuthServiceValidatePasswordResetToken_InvalidType(t *testing.T) {
	cfg := test.TestConfig()
	vr := &verificationRepoStub{
		findByTokenFn: func(token string) (*domain.VerificationToken, error) {
			return &domain.VerificationToken{
				Token:     token,
				Type:      domain.TokenTypeEmailVerification,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, &authRepoStub{}, vr, &enhancedEmailStub{})

	err := svc.ValidatePasswordResetToken("wrong-type-token")
	assertProblem(t, err, http.StatusBadRequest, "Invalid token type")
}

func TestAuthServiceTwoFactor_FullLifecycle(t *testing.T) {
	cfg := test.TestConfig()
	user := mustAuthUser(t, "staff@example.com", "staff", "StrongPass123!")

	repo := &authRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return user, nil
		},
		updateFn: func(u *domain.User) error {
			return nil
		},
	}
	svc := newAuthServiceWithStubs(cfg, repo, &verificationRepoStub{}, &enhancedEmailStub{})

	result, err := svc.Enable2FA(user.ID)
	if err != nil {
		t.Fatalf("expected enable 2fa success, got %v", err)
	}
	if !strings.HasPrefix(result.OTPAuthURL, "otpauth://") {
		t.Fatalf("expected otpauth url, got %q", result.OTPAuthURL)
	}
	if user.TwoFactorSecret == "" {
		t.Fatalf("expected generated 2fa secret")
	}
	if len(result.BackupCodes) != defaultBackupCodeCount {
		t.Fatalf("expected %d backup codes, got %d", defaultBackupCodeCount, len(result.BackupCodes))
	}

	// Decrypt the stored secret to generate a valid TOTP code for verification
	decryptedSecret, err := cryptoutil.Decrypt(user.TwoFactorSecret, cryptoutil.DeriveKey(cfg.Security.EncryptionKey))
	if err != nil {
		t.Fatalf("failed to decrypt stored 2fa secret: %v", err)
	}

	code, err := totp.GenerateCode(decryptedSecret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate totp code for test: %v", err)
	}
	if err := svc.Verify2FA(user.ID, code); err != nil {
		t.Fatalf("expected verify 2fa success, got %v", err)
	}
	if !user.TwoFactorEnabled {
		t.Fatalf("expected user 2fa to be enabled")
	}

	// Use a plaintext backup code (returned from Enable2FA) to validate
	usedCode := result.BackupCodes[0]
	if err := svc.Validate2FACode(user.ID, usedCode); err != nil {
		t.Fatalf("expected backup code validation success, got %v", err)
	}
	// Verify the hash of the used code is no longer stored
	usedHash := cryptoutil.HashSHA256Hex(usedCode)
	if strings.Contains(","+user.TwoFactorBackupCodes+",", ","+usedHash+",") {
		t.Fatalf("expected used backup code hash to be removed")
	}

	code, err = totp.GenerateCode(decryptedSecret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate totp code for disable: %v", err)
	}
	if err := svc.Disable2FA(user.ID, code); err != nil {
		t.Fatalf("expected disable 2fa success, got %v", err)
	}
	if user.TwoFactorEnabled || user.TwoFactorSecret != "" || user.TwoFactorBackupCodes != "" {
		t.Fatalf("expected 2fa state to be fully cleared")
	}
}
