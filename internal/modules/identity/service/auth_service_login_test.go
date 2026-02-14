package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/test"
	"gorm.io/gorm"
)

type fakeUserRepository struct {
	getByEmailFunc func(email string) (*domain.User, error)
	loadRolesFunc  func(user *domain.User) error
	updateFunc     func(user *domain.User) error
}

var _ repository.UserRepository = (*fakeUserRepository)(nil)

func (f *fakeUserRepository) WithTx(_ *gorm.DB) repository.UserRepository { return f }
func (f *fakeUserRepository) Create(user *domain.User) error              { return nil }
func (f *fakeUserRepository) Update(user *domain.User) error {
	if f.updateFunc != nil {
		return f.updateFunc(user)
	}
	return nil
}
func (f *fakeUserRepository) Delete(id uuid.UUID) error { return nil }
func (f *fakeUserRepository) GetByID(id uuid.UUID) (*domain.User, error) {
	return nil, nil
}
func (f *fakeUserRepository) GetByEmail(email string) (*domain.User, error) {
	if f.getByEmailFunc != nil {
		return f.getByEmailFunc(email)
	}
	return nil, nil
}
func (f *fakeUserRepository) GetByUsername(username string) (*domain.User, error) {
	return nil, nil
}
func (f *fakeUserRepository) GetAll(offset, limit int) ([]*domain.User, error) {
	return nil, nil
}
func (f *fakeUserRepository) ListFiltered(_ repository.UserListFilter) ([]*domain.User, int64, error) {
	return nil, 0, nil
}
func (f *fakeUserRepository) Count() (int64, error) { return 0, nil }
func (f *fakeUserRepository) ExistsByEmail(email string) (bool, error) {
	return false, nil
}
func (f *fakeUserRepository) ExistsByUsername(username string) (bool, error) {
	return false, nil
}
func (f *fakeUserRepository) LoadRoles(user *domain.User) error {
	if f.loadRolesFunc != nil {
		return f.loadRolesFunc(user)
	}
	return nil
}
func (f *fakeUserRepository) CreateRole(role *domain.Role) error { return nil }
func (f *fakeUserRepository) UpdateRole(role *domain.Role) error { return nil }
func (f *fakeUserRepository) DeleteRole(id uuid.UUID) error      { return nil }
func (f *fakeUserRepository) GetRoleByID(id uuid.UUID) (*domain.Role, error) {
	return nil, nil
}
func (f *fakeUserRepository) GetRoleByName(name string) (*domain.Role, error) {
	return nil, nil
}
func (f *fakeUserRepository) GetAllRoles() ([]*domain.Role, error) { return nil, nil }
func (f *fakeUserRepository) AssignRole(userID, roleID uuid.UUID) error {
	return nil
}
func (f *fakeUserRepository) RemoveRole(userID, roleID uuid.UUID) error {
	return nil
}
func (f *fakeUserRepository) GetUserRoles(userID uuid.UUID) ([]*domain.Role, error) {
	return nil, nil
}
func (f *fakeUserRepository) CreatePermission(permission *domain.Permission) error {
	return nil
}
func (f *fakeUserRepository) UpdatePermission(permission *domain.Permission) error {
	return nil
}
func (f *fakeUserRepository) DeletePermission(id uuid.UUID) error { return nil }
func (f *fakeUserRepository) GetPermissionByID(id uuid.UUID) (*domain.Permission, error) {
	return nil, nil
}
func (f *fakeUserRepository) GetAllPermissions() ([]*domain.Permission, error) { return nil, nil }
func (f *fakeUserRepository) AssignPermissionToRole(roleID, permissionID uuid.UUID) error {
	return nil
}
func (f *fakeUserRepository) RemovePermissionFromRole(roleID, permissionID uuid.UUID) error {
	return nil
}
func (f *fakeUserRepository) GetRolePermissions(roleID uuid.UUID) ([]*domain.Permission, error) {
	return nil, nil
}
func (f *fakeUserRepository) CreateRefreshToken(token *domain.RefreshToken) error { return nil }
func (f *fakeUserRepository) GetRefreshToken(token string) (*domain.RefreshToken, error) {
	return nil, nil
}
func (f *fakeUserRepository) RevokeRefreshToken(token string) error { return nil }
func (f *fakeUserRepository) RevokeAllUserRefreshTokens(userID uuid.UUID) error {
	return nil
}
func (f *fakeUserRepository) GetActiveRefreshTokensByUser(userID uuid.UUID) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (f *fakeUserRepository) RevokeRefreshTokenByID(id uuid.UUID) error { return nil }
func (f *fakeUserRepository) CleanExpiredRefreshTokens() error           { return nil }
func (f *fakeUserRepository) CountByStatus(status string) (int64, error)         { return 0, nil }
func (f *fakeUserRepository) CountCreatedAfter(after time.Time) (int64, error) { return 0, nil }
func (f *fakeUserRepository) GetAllActiveSessions(offset, limit int) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (f *fakeUserRepository) CountActiveSessions() (int64, error) { return 0, nil }

type fakeVerificationTokenRepository struct{}

var _ repository.VerificationTokenRepository = (*fakeVerificationTokenRepository)(nil)

func (f *fakeVerificationTokenRepository) WithTx(_ *gorm.DB) repository.VerificationTokenRepository {
	return f
}
func (f *fakeVerificationTokenRepository) Create(token *domain.VerificationToken) error { return nil }
func (f *fakeVerificationTokenRepository) FindByToken(token string) (*domain.VerificationToken, error) {
	return nil, nil
}
func (f *fakeVerificationTokenRepository) FindByUserAndType(
	userID uuid.UUID,
	tokenType domain.TokenType,
) (*domain.VerificationToken, error) {
	return nil, nil
}
func (f *fakeVerificationTokenRepository) Update(token *domain.VerificationToken) error { return nil }
func (f *fakeVerificationTokenRepository) Delete(id uuid.UUID) error                    { return nil }
func (f *fakeVerificationTokenRepository) DeleteExpiredTokens() error                   { return nil }
func (f *fakeVerificationTokenRepository) DeleteByUserAndType(userID uuid.UUID, tokenType domain.TokenType) error {
	return nil
}
func (f *fakeVerificationTokenRepository) CountByUserAndType(
	userID uuid.UUID,
	tokenType domain.TokenType,
	since time.Time,
) (int64, error) {
	return 0, nil
}

type fakeSessionCache struct {
	setPermissionsFunc func(ctx context.Context, userID string, roles, permissions []string) error
}

func (f *fakeSessionCache) SetPermissions(ctx context.Context, userID string, roles, permissions []string) error {
	if f.setPermissionsFunc != nil {
		return f.setPermissionsFunc(ctx, userID, roles, permissions)
	}
	return nil
}

func mustCreateUserWithPassword(
	t *testing.T,
	email, username, rawPassword string,
	failedAttempts int,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ID:                  uuid.New(),
		Email:               email,
		Username:            username,
		Status:              domain.UserStatusActive,
		Verified:            true,
		FailedLoginAttempts: failedAttempts,
	}
	if err := user.SetPassword(rawPassword); err != nil {
		t.Fatalf("failed to set password: %v", err)
	}

	return user
}

func assertUnauthorizedError(t *testing.T, err error, expectedDetail string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected unauthorized error, got nil")
	}

	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail error, got %T", err)
	}
	if pd.Status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, pd.Status)
	}
	if pd.Detail != expectedDetail {
		t.Fatalf("expected detail %q, got %q", expectedDetail, pd.Detail)
	}
}

func assertInternalError(t *testing.T, err error, expectedDetail string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected internal error, got nil")
	}

	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail error, got %T", err)
	}
	if pd.Status != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, pd.Status)
	}
	if pd.Detail != expectedDetail {
		t.Fatalf("expected detail %q, got %q", expectedDetail, pd.Detail)
	}
}

func newAuthServiceForLoginTest(t *testing.T, userRepo repository.UserRepository) *AuthService {
	t.Helper()

	cfg := test.TestConfig()
	tokenSvc := NewTokenService(cfg, userRepo)

	return NewAuthService(
		cfg,
		nil,
		userRepo,
		tokenSvc,
		&fakeVerificationTokenRepository{},
		nil,
		nil,
	)
}

func TestAuthServiceLogin_InvalidPasswordIncrementsFailedAttempts(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 0)
	updateCalls := 0

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
		updateFunc: func(updated *domain.User) error {
			updateCalls++
			if updated.FailedLoginAttempts != 1 {
				t.Fatalf("expected failed attempts to be 1, got %d", updated.FailedLoginAttempts)
			}
			if updated.LockedUntil != nil {
				t.Fatalf("expected user to remain unlocked on first failure")
			}
			return nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "WrongPassword123!",
	})

	if resp != nil {
		t.Fatalf("expected nil response on invalid login")
	}
	assertUnauthorizedError(t, err, "Invalid credentials")
	if updateCalls != 1 {
		t.Fatalf("expected update to be called once, got %d", updateCalls)
	}
}

func TestAuthServiceLogin_FifthFailureLocksAccount(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 4)
	updateCalls := 0

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
		updateFunc: func(updated *domain.User) error {
			updateCalls++
			if updated.FailedLoginAttempts != 5 {
				t.Fatalf("expected failed attempts to be 5, got %d", updated.FailedLoginAttempts)
			}
			if updated.LockedUntil == nil {
				t.Fatalf("expected lock timestamp to be set")
			}
			if updated.Status != domain.UserStatusLocked {
				t.Fatalf("expected locked status, got %s", updated.Status)
			}
			return nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "WrongPassword123!",
	})

	if resp != nil {
		t.Fatalf("expected nil response on invalid login")
	}
	assertUnauthorizedError(t, err, "Invalid credentials")
	if updateCalls != 1 {
		t.Fatalf("expected update to be called once, got %d", updateCalls)
	}
}

func TestAuthServiceLogin_LockedAccountIsRejectedWithoutMutation(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 5)
	lockUntil := time.Now().Add(10 * time.Minute)
	user.LockedUntil = &lockUntil
	user.Status = domain.UserStatusLocked

	updateCalls := 0
	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
		updateFunc: func(updated *domain.User) error {
			updateCalls++
			return nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "CorrectPassword123!",
	})

	if resp != nil {
		t.Fatalf("expected nil response for locked account")
	}
	assertUnauthorizedError(
		t,
		err,
		"Your account has been temporarily locked due to too many failed login attempts. Please try again later.",
	)
	if updateCalls != 0 {
		t.Fatalf("expected no updates for locked account path, got %d", updateCalls)
	}
}

func TestAuthServiceLogin_SuccessResetsFailedAttemptsAndReturnsTokens(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 3)
	var (
		loadRolesCalls int
		updateCalls    int
	)

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
		loadRolesFunc: func(u *domain.User) error {
			loadRolesCalls++
			u.Roles = []domain.Role{
				{
					Name: "admin",
					Permissions: []domain.Permission{
						{Name: "users.read"},
						{Name: "users.write"},
					},
				},
			}
			return nil
		},
		updateFunc: func(updated *domain.User) error {
			updateCalls++
			if updated.FailedLoginAttempts != 0 {
				t.Fatalf("expected failed attempts reset to 0, got %d", updated.FailedLoginAttempts)
			}
			if updated.LastLogin == nil {
				t.Fatalf("expected last login to be set")
			}
			return nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "CorrectPassword123!",
	})

	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil login response")
	}
	if resp.User == nil {
		t.Fatalf("expected non-nil user in response")
	}
	if resp.User.Password != "" {
		t.Fatalf("expected password to be cleared in response")
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatalf("expected both access and refresh tokens")
	}
	if resp.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero access token expiry")
	}
	if loadRolesCalls != 1 {
		t.Fatalf("expected LoadRoles to be called once, got %d", loadRolesCalls)
	}
	if updateCalls != 1 {
		t.Fatalf("expected Update to be called once, got %d", updateCalls)
	}
}

func TestAuthServiceLogin_UserNotFoundReturnsUnauthorized(t *testing.T) {
	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return nil, errors.New("not found")
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    "missing@example.com",
		Password: "AnyPassword123!",
	})

	if resp != nil {
		t.Fatalf("expected nil response when user lookup fails")
	}
	assertUnauthorizedError(t, err, "Invalid credentials")
}

func TestAuthServiceLogin_InactiveUnverifiedUserRejected(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 0)
	user.Verified = false
	user.Status = domain.UserStatusPending

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "CorrectPassword123!",
	})

	if resp != nil {
		t.Fatalf("expected nil response for inactive user")
	}
	assertUnauthorizedError(t, err, "Please verify your email before logging in")
}

func TestAuthServiceLogin_LoadRolesFailureReturnsInternalError(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 0)
	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
		loadRolesFunc: func(user *domain.User) error {
			return errors.New("db unavailable")
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "CorrectPassword123!",
	})

	if resp != nil {
		t.Fatalf("expected nil response when roles cannot be loaded")
	}
	assertInternalError(t, err, "Failed to load user roles")
}

func TestAuthServiceLogin_SessionCacheFailureDoesNotBreakLogin(t *testing.T) {
	user := mustCreateUserWithPassword(t, "staff@example.com", "staff", "CorrectPassword123!", 0)
	cacheCalls := 0

	repo := &fakeUserRepository{
		getByEmailFunc: func(email string) (*domain.User, error) {
			return user, nil
		},
		loadRolesFunc: func(u *domain.User) error {
			u.Roles = []domain.Role{
				{
					Name: "admin",
					Permissions: []domain.Permission{
						{Name: "users.read"},
					},
				},
			}
			return nil
		},
		updateFunc: func(updated *domain.User) error {
			return nil
		},
	}
	svc := newAuthServiceForLoginTest(t, repo)
	svc.SetSessionCache(&fakeSessionCache{
		setPermissionsFunc: func(ctx context.Context, userID string, roles, permissions []string) error {
			cacheCalls++
			return errors.New("redis down")
		},
	})

	resp, err := svc.Login(&LoginRequest{
		Email:    user.Email,
		Password: "CorrectPassword123!",
	})

	if err != nil {
		t.Fatalf("expected successful login despite cache failure, got %v", err)
	}
	if resp == nil || resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatalf("expected valid login response with tokens")
	}
	if cacheCalls != 1 {
		t.Fatalf("expected session cache call once, got %d", cacheCalls)
	}
}
