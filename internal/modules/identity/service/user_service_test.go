package service

import (
	"context"
	stderrors "errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/test"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// userRepoStub — full UserRepository stub with configurable function fields.
// Unlike authRepoStub this exposes every method needed by UserService.
// ---------------------------------------------------------------------------

type userRepoStub struct {
	createFn                       func(user *domain.User) error
	updateFn                       func(user *domain.User) error
	deleteFn                       func(id uuid.UUID) error
	getByIDFn                      func(id uuid.UUID) (*domain.User, error)
	getByEmailFn                   func(email string) (*domain.User, error)
	existsByEmailFn                func(email string) (bool, error)
	existsByUsernameFn             func(username string) (bool, error)
	loadRolesFn                    func(user *domain.User) error
	getRoleByNameFn                func(name string) (*domain.Role, error)
	getRoleByIDFn                  func(id uuid.UUID) (*domain.Role, error)
	assignRoleFn                   func(userID, roleID uuid.UUID) error
	removeRoleFn                   func(userID, roleID uuid.UUID) error
	listFilteredFn                 func(filter domain.UserListFilter) ([]*domain.User, int64, error)
	getActiveRefreshTokensByUserFn func(userID uuid.UUID) ([]*domain.RefreshToken, error)
	revokeRefreshTokenByIDFn       func(id uuid.UUID) error
	revokeAllUserRefreshTokensFn   func(userID uuid.UUID) error
}

var _ repository.UserRepository = (*userRepoStub)(nil)

func (s *userRepoStub) WithTx(_ *gorm.DB) repository.UserRepository { return s }

func (s *userRepoStub) Create(user *domain.User) error {
	if s.createFn != nil {
		return s.createFn(user)
	}
	return nil
}

func (s *userRepoStub) Update(user *domain.User) error {
	if s.updateFn != nil {
		return s.updateFn(user)
	}
	return nil
}

func (s *userRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}

func (s *userRepoStub) GetByID(id uuid.UUID) (*domain.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, stderrors.New("not found")
}

func (s *userRepoStub) GetByIDForUpdate(id uuid.UUID) (*domain.User, error) {
	return s.GetByID(id)
}

func (s *userRepoStub) GetByEmail(email string) (*domain.User, error) {
	if s.getByEmailFn != nil {
		return s.getByEmailFn(email)
	}
	return nil, stderrors.New("not found")
}

func (s *userRepoStub) GetByUsername(_ string) (*domain.User, error) {
	return nil, stderrors.New("not found")
}

func (s *userRepoStub) GetAll(_, _ int) ([]*domain.User, error) { return nil, nil }

func (s *userRepoStub) ListFiltered(filter domain.UserListFilter) ([]*domain.User, int64, error) {
	if s.listFilteredFn != nil {
		return s.listFilteredFn(filter)
	}
	return nil, 0, nil
}

func (s *userRepoStub) Count() (int64, error)                      { return 0, nil }
func (s *userRepoStub) CountByStatus(_ string) (int64, error)      { return 0, nil }
func (s *userRepoStub) CountCreatedAfter(_ time.Time) (int64, error) {
	return 0, nil
}

func (s *userRepoStub) ExistsByEmail(email string) (bool, error) {
	if s.existsByEmailFn != nil {
		return s.existsByEmailFn(email)
	}
	return false, nil
}

func (s *userRepoStub) ExistsByUsername(username string) (bool, error) {
	if s.existsByUsernameFn != nil {
		return s.existsByUsernameFn(username)
	}
	return false, nil
}

func (s *userRepoStub) LoadRoles(user *domain.User) error {
	if s.loadRolesFn != nil {
		return s.loadRolesFn(user)
	}
	return nil
}

func (s *userRepoStub) CreateRole(_ *domain.Role) error  { return nil }
func (s *userRepoStub) UpdateRole(_ *domain.Role) error  { return nil }
func (s *userRepoStub) DeleteRole(_ uuid.UUID) error     { return nil }
func (s *userRepoStub) GetAllRoles() ([]*domain.Role, error) { return nil, nil }

func (s *userRepoStub) GetRoleByID(id uuid.UUID) (*domain.Role, error) {
	if s.getRoleByIDFn != nil {
		return s.getRoleByIDFn(id)
	}
	return nil, stderrors.New("not found")
}

func (s *userRepoStub) GetRoleByName(name string) (*domain.Role, error) {
	if s.getRoleByNameFn != nil {
		return s.getRoleByNameFn(name)
	}
	return nil, stderrors.New("not found")
}

func (s *userRepoStub) AssignRole(userID, roleID uuid.UUID) error {
	if s.assignRoleFn != nil {
		return s.assignRoleFn(userID, roleID)
	}
	return nil
}

func (s *userRepoStub) RemoveRole(userID, roleID uuid.UUID) error {
	if s.removeRoleFn != nil {
		return s.removeRoleFn(userID, roleID)
	}
	return nil
}

func (s *userRepoStub) GetUserRoles(_ uuid.UUID) ([]*domain.Role, error) { return nil, nil }

func (s *userRepoStub) CreatePermission(_ *domain.Permission) error { return nil }
func (s *userRepoStub) UpdatePermission(_ *domain.Permission) error { return nil }
func (s *userRepoStub) DeletePermission(_ uuid.UUID) error         { return nil }
func (s *userRepoStub) GetPermissionByID(_ uuid.UUID) (*domain.Permission, error) {
	return nil, nil
}
func (s *userRepoStub) GetAllPermissions() ([]*domain.Permission, error) { return nil, nil }
func (s *userRepoStub) AssignPermissionToRole(_, _ uuid.UUID) error      { return nil }
func (s *userRepoStub) RemovePermissionFromRole(_, _ uuid.UUID) error    { return nil }
func (s *userRepoStub) GetRolePermissions(_ uuid.UUID) ([]*domain.Permission, error) {
	return nil, nil
}

func (s *userRepoStub) CreateRefreshToken(_ *domain.RefreshToken) error { return nil }
func (s *userRepoStub) GetRefreshToken(_ string) (*domain.RefreshToken, error) {
	return nil, stderrors.New("not found")
}
func (s *userRepoStub) RevokeRefreshToken(_ string) error { return nil }

func (s *userRepoStub) RevokeAllUserRefreshTokens(userID uuid.UUID) error {
	if s.revokeAllUserRefreshTokensFn != nil {
		return s.revokeAllUserRefreshTokensFn(userID)
	}
	return nil
}

func (s *userRepoStub) GetActiveRefreshTokensByUser(userID uuid.UUID) ([]*domain.RefreshToken, error) {
	if s.getActiveRefreshTokensByUserFn != nil {
		return s.getActiveRefreshTokensByUserFn(userID)
	}
	return nil, nil
}

func (s *userRepoStub) RevokeRefreshTokenByID(id uuid.UUID) error {
	if s.revokeRefreshTokenByIDFn != nil {
		return s.revokeRefreshTokenByIDFn(id)
	}
	return nil
}

func (s *userRepoStub) CleanExpiredRefreshTokens() error { return nil }
func (s *userRepoStub) GetAllActiveSessions(_, _ int) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (s *userRepoStub) CountActiveSessions() (int64, error) { return 0, nil }

// ---------------------------------------------------------------------------
// storageStub — implements storage.StorageService for avatar URL resolution.
// ---------------------------------------------------------------------------

type storageStub struct {
	getURLFn func(ctx context.Context, key string) (string, error)
}

func (s *storageStub) Upload(_ context.Context, _ string, _ io.Reader, _ int64, _ string) (*storage.FileInfo, error) {
	return nil, nil
}
func (s *storageStub) Delete(_ context.Context, _ string) error { return nil }

func (s *storageStub) GetURL(ctx context.Context, key string) (string, error) {
	if s.getURLFn != nil {
		return s.getURLFn(ctx, key)
	}
	return "https://cdn.example.com/" + key, nil
}

func (s *storageStub) GetUploadURL(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}
func (s *storageStub) GetObject(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, nil
}
func (s *storageStub) StatObject(_ context.Context, _ string) (*storage.ObjectInfo, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// presignCacheStub — implements PresignURLCache.
// ---------------------------------------------------------------------------

type presignCacheStub struct {
	getFn func(ctx context.Context, key string) (string, error)
	setFn func(ctx context.Context, key, url string) error
}

func (p *presignCacheStub) GetPresignedURL(ctx context.Context, key string) (string, error) {
	if p.getFn != nil {
		return p.getFn(ctx, key)
	}
	return "", stderrors.New("miss")
}

func (p *presignCacheStub) SetPresignedURL(ctx context.Context, key, url string) error {
	if p.setFn != nil {
		return p.setFn(ctx, key, url)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newUserService creates a UserService with stubs. db is nil (runInTx fallback).
func newUserService(repo repository.UserRepository) *UserService {
	cfg := test.TestConfig()
	tokenSvc := NewTokenService(cfg, repo)
	return NewUserService(cfg, nil, repo, nil, tokenSvc)
}

// newUserServiceWithAuth creates a UserService backed by both an AuthService and TokenService.
func newUserServiceWithAuth(repo repository.UserRepository) *UserService {
	cfg := test.TestConfig()
	tokenSvc := NewTokenService(cfg, repo)
	authSvc := NewAuthService(cfg, nil, repo, tokenSvc, &verificationRepoStub{}, nil, &enhancedEmailStub{})
	return NewUserService(cfg, nil, repo, authSvc, tokenSvc)
}

func makeUser() *domain.User {
	return &domain.User{
		ID:        uuid.New(),
		Email:     "staff@example.com",
		Username:  "staff",
		FirstName: "First",
		LastName:  "Last",
		Phone:     "+1234567890",
		Status:    domain.UserStatusActive,
		Verified:  true,
	}
}

// ---------------------------------------------------------------------------
// Tests: Constructor and setters
// ---------------------------------------------------------------------------

func TestUserService_NewUserService(t *testing.T) {
	repo := &userRepoStub{}
	svc := newUserService(repo)
	if svc == nil {
		t.Fatal("expected non-nil UserService")
	}
	if svc.storageSvc != nil {
		t.Fatal("expected no storage service by default")
	}
	if svc.presignCache != nil {
		t.Fatal("expected no presign cache by default")
	}
}

func TestUserService_SetStorage(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	st := &storageStub{}
	svc.SetStorage(st)
	if svc.storageSvc == nil {
		t.Fatal("expected storage to be set")
	}
}

func TestUserService_SetPresignCache(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	pc := &presignCacheStub{}
	svc.SetPresignCache(pc)
	if svc.presignCache == nil {
		t.Fatal("expected presign cache to be set")
	}
}

// ---------------------------------------------------------------------------
// Tests: UpdateProfile
// ---------------------------------------------------------------------------

func TestUserService_UpdateProfile_Success(t *testing.T) {
	user := makeUser()
	updated := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			updated = true
			return nil
		},
	}
	svc := newUserService(repo)

	meta := domain.Metadata{"theme": "dark"}
	result, err := svc.UpdateProfile(context.Background(), user.ID, "NewFirst", "NewLast", "+9999", meta)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !updated {
		t.Fatal("expected repo.Update to be called")
	}
	if result.FirstName != "NewFirst" || result.LastName != "NewLast" || result.Phone != "+9999" {
		t.Fatalf("expected fields to be updated, got %+v", result)
	}
	if result.Metadata["theme"] != "dark" {
		t.Fatal("expected metadata to be updated")
	}
	if result.Password != "" {
		t.Fatal("expected password to be cleared in response")
	}
}

func TestUserService_UpdateProfile_NilMetadataPreservesExisting(t *testing.T) {
	user := makeUser()
	user.Metadata = domain.Metadata{"existing": "value"}
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	svc := newUserService(repo)

	result, err := svc.UpdateProfile(context.Background(), user.ID, "F", "L", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Metadata["existing"] != "value" {
		t.Fatal("expected existing metadata to be preserved when nil passed")
	}
}

func TestUserService_UpdateProfile_UserNotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	_, err := svc.UpdateProfile(context.Background(), uuid.New(), "F", "L", "", nil)
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_UpdateProfile_UpdateFails(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.UpdateProfile(context.Background(), user.ID, "F", "L", "", nil)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to update profile")
}

func TestUserService_UpdateProfile_ResolvesAvatarURL(t *testing.T) {
	user := makeUser()
	user.AvatarURL = "avatars/user.jpg"
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	svc := newUserService(repo)
	svc.SetStorage(&storageStub{
		getURLFn: func(_ context.Context, key string) (string, error) {
			return "https://cdn.example.com/" + key, nil
		},
	})

	result, err := svc.UpdateProfile(context.Background(), user.ID, "F", "L", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AvatarURL != "https://cdn.example.com/avatars/user.jpg" {
		t.Fatalf("expected resolved avatar URL, got %q", result.AvatarURL)
	}
}

// ---------------------------------------------------------------------------
// Tests: DeleteAccount
// ---------------------------------------------------------------------------

func TestUserService_DeleteAccount_Success(t *testing.T) {
	userID := uuid.New()
	deleted := false
	repo := &userRepoStub{
		deleteFn: func(id uuid.UUID) error {
			deleted = true
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.DeleteAccount(userID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !deleted {
		t.Fatal("expected repo.Delete to be called")
	}
}

func TestUserService_DeleteAccount_DeleteFails(t *testing.T) {
	repo := &userRepoStub{
		deleteFn: func(id uuid.UUID) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	err := svc.DeleteAccount(uuid.New())
	assertProblem(t, err, http.StatusInternalServerError, "Failed to delete account")
}

func TestUserService_DeleteAccount_TokenRevocationFailureIsNonFatal(t *testing.T) {
	repo := &userRepoStub{
		deleteFn: func(id uuid.UUID) error { return nil },
		revokeAllUserRefreshTokensFn: func(userID uuid.UUID) error {
			return stderrors.New("revoke fail")
		},
	}
	svc := newUserService(repo)

	// Should succeed even when token revocation fails (non-fatal, logged)
	if err := svc.DeleteAccount(uuid.New()); err != nil {
		t.Fatalf("expected success despite token revocation failure, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: GetSessions
// ---------------------------------------------------------------------------

func TestUserService_GetSessions_Success(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	tokens := []*domain.RefreshToken{
		{
			ID:        uuid.New(),
			UserID:    userID,
			IPAddress: "1.2.3.4",
			UserAgent: "Mozilla",
			CreatedAt: now,
			ExpiresAt: now.Add(24 * time.Hour),
		},
		{
			ID:        uuid.New(),
			UserID:    userID,
			IPAddress: "5.6.7.8",
			UserAgent: "Chrome",
			CreatedAt: now,
			ExpiresAt: now.Add(48 * time.Hour),
		},
	}
	repo := &userRepoStub{
		getActiveRefreshTokensByUserFn: func(id uuid.UUID) ([]*domain.RefreshToken, error) {
			return tokens, nil
		},
	}
	svc := newUserService(repo)

	sessions, err := svc.GetSessions(userID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].IPAddress != "1.2.3.4" {
		t.Fatalf("expected IP 1.2.3.4, got %s", sessions[0].IPAddress)
	}
	if sessions[1].UserAgent != "Chrome" {
		t.Fatalf("expected UA Chrome, got %s", sessions[1].UserAgent)
	}
	if sessions[0].ID != tokens[0].ID {
		t.Fatal("expected session ID to match token ID")
	}
}

func TestUserService_GetSessions_Empty(t *testing.T) {
	repo := &userRepoStub{
		getActiveRefreshTokensByUserFn: func(id uuid.UUID) ([]*domain.RefreshToken, error) {
			return nil, nil
		},
	}
	svc := newUserService(repo)

	sessions, err := svc.GetSessions(uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestUserService_GetSessions_RepoError(t *testing.T) {
	repo := &userRepoStub{
		getActiveRefreshTokensByUserFn: func(id uuid.UUID) ([]*domain.RefreshToken, error) {
			return nil, stderrors.New("db error")
		},
	}
	svc := newUserService(repo)

	_, err := svc.GetSessions(uuid.New())
	assertProblem(t, err, http.StatusInternalServerError, "Failed to fetch sessions")
}

// ---------------------------------------------------------------------------
// Tests: RevokeSession
// ---------------------------------------------------------------------------

func TestUserService_RevokeSession_Success(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	revoked := false
	repo := &userRepoStub{
		getActiveRefreshTokensByUserFn: func(id uuid.UUID) ([]*domain.RefreshToken, error) {
			return []*domain.RefreshToken{
				{ID: sessionID, UserID: userID},
				{ID: uuid.New(), UserID: userID},
			}, nil
		},
		revokeRefreshTokenByIDFn: func(id uuid.UUID) error {
			if id != sessionID {
				t.Fatalf("expected revoke for session %s, got %s", sessionID, id)
			}
			revoked = true
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.RevokeSession(userID, sessionID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !revoked {
		t.Fatal("expected session to be revoked")
	}
}

func TestUserService_RevokeSession_NotFound(t *testing.T) {
	userID := uuid.New()
	repo := &userRepoStub{
		getActiveRefreshTokensByUserFn: func(id uuid.UUID) ([]*domain.RefreshToken, error) {
			return []*domain.RefreshToken{
				{ID: uuid.New(), UserID: userID},
			}, nil
		},
	}
	svc := newUserService(repo)

	err := svc.RevokeSession(userID, uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_RevokeSession_FetchError(t *testing.T) {
	repo := &userRepoStub{
		getActiveRefreshTokensByUserFn: func(id uuid.UUID) ([]*domain.RefreshToken, error) {
			return nil, stderrors.New("db error")
		},
	}
	svc := newUserService(repo)

	err := svc.RevokeSession(uuid.New(), uuid.New())
	assertProblem(t, err, http.StatusInternalServerError, "Failed to fetch sessions")
}

// ---------------------------------------------------------------------------
// Tests: RevokeAllSessions
// ---------------------------------------------------------------------------

func TestUserService_RevokeAllSessions_Success(t *testing.T) {
	called := false
	repo := &userRepoStub{
		revokeAllUserRefreshTokensFn: func(userID uuid.UUID) error {
			called = true
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.RevokeAllSessions(uuid.New()); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !called {
		t.Fatal("expected RevokeAllUserRefreshTokens to be called")
	}
}

func TestUserService_RevokeAllSessions_Error(t *testing.T) {
	repo := &userRepoStub{
		revokeAllUserRefreshTokensFn: func(userID uuid.UUID) error {
			return stderrors.New("db error")
		},
	}
	svc := newUserService(repo)

	err := svc.RevokeAllSessions(uuid.New())
	assertProblem(t, err, http.StatusInternalServerError, "Failed to revoke sessions")
}

// ---------------------------------------------------------------------------
// Tests: AdminGetUser
// ---------------------------------------------------------------------------

func TestUserService_AdminGetUser_Success(t *testing.T) {
	user := makeUser()
	user.Password = "should-be-cleared"
	repo := &userRepoStub{
		getByIDFn:   func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error { return nil },
	}
	svc := newUserService(repo)

	result, err := svc.AdminGetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result.Password != "" {
		t.Fatal("expected password to be cleared")
	}
}

func TestUserService_AdminGetUser_NotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminGetUser(context.Background(), uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminGetUser_LoadRolesErrorIsNonFatal(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn:   func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error { return stderrors.New("role load failed") },
	}
	svc := newUserService(repo)

	result, err := svc.AdminGetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("expected success despite role load failure, got %v", err)
	}
	if result == nil {
		t.Fatal("expected user to be returned")
	}
}

func TestUserService_AdminGetUser_ResolvesAvatarURL(t *testing.T) {
	user := makeUser()
	user.AvatarURL = "avatars/admin.png"
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newUserService(repo)
	svc.SetStorage(&storageStub{
		getURLFn: func(_ context.Context, key string) (string, error) {
			return "https://cdn.test.com/" + key, nil
		},
	})

	result, err := svc.AdminGetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AvatarURL != "https://cdn.test.com/avatars/admin.png" {
		t.Fatalf("expected resolved avatar URL, got %q", result.AvatarURL)
	}
}

// ---------------------------------------------------------------------------
// Tests: AdminListUsers
// ---------------------------------------------------------------------------

func TestUserService_AdminListUsers_Success(t *testing.T) {
	users := []*domain.User{
		{ID: uuid.New(), Password: "hash1", Email: "a@b.com"},
		{ID: uuid.New(), Password: "hash2", Email: "c@d.com"},
	}
	repo := &userRepoStub{
		listFilteredFn: func(filter domain.UserListFilter) ([]*domain.User, int64, error) {
			return users, 2, nil
		},
	}
	svc := newUserService(repo)

	result, total, err := svc.AdminListUsers(context.Background(), domain.UserListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	for _, u := range result {
		if u.Password != "" {
			t.Fatalf("expected password to be cleared for user %s", u.ID)
		}
	}
}

func TestUserService_AdminListUsers_RepoError(t *testing.T) {
	repo := &userRepoStub{
		listFilteredFn: func(filter domain.UserListFilter) ([]*domain.User, int64, error) {
			return nil, 0, stderrors.New("db error")
		},
	}
	svc := newUserService(repo)

	_, _, err := svc.AdminListUsers(context.Background(), domain.UserListFilter{})
	assertProblem(t, err, http.StatusInternalServerError, "Failed to fetch users")
}

// ---------------------------------------------------------------------------
// Tests: AdminCreateUser
// ---------------------------------------------------------------------------

func TestUserService_AdminCreateUser_Success(t *testing.T) {
	var created *domain.User
	roleID := uuid.New()
	assigned := false
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(user *domain.User) error {
			created = user
			user.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn: func(userID, rID uuid.UUID) error {
			assigned = true
			return nil
		},
	}
	svc := newUserService(repo)

	result, err := svc.AdminCreateUser(
		context.Background(),
		"ADMIN@Example.COM", "AdminUser", "StrongPass123!", "Admin", "User", "+555", true,
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if created == nil {
		t.Fatal("expected user to be created")
	}
	if created.Email != "admin@example.com" {
		t.Fatalf("expected email normalized, got %q", created.Email)
	}
	if created.Username != "adminuser" {
		t.Fatalf("expected username normalized, got %q", created.Username)
	}
	if created.Status != domain.UserStatusActive {
		t.Fatalf("expected active status for verified user, got %s", created.Status)
	}
	if !created.Verified {
		t.Fatal("expected user to be verified")
	}
	if !assigned {
		t.Fatal("expected default role to be assigned")
	}
	if result.Password != "" {
		t.Fatal("expected password to be cleared in response")
	}
}

func TestUserService_AdminCreateUser_UnverifiedStatusIsPending(t *testing.T) {
	var created *domain.User
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(user *domain.User) error {
			created = user
			user.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return nil, stderrors.New("no role")
		},
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "testuser", "StrongPass123!", "Test", "User", "", false,
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if created.Status != domain.UserStatusPending {
		t.Fatalf("expected pending status for unverified user, got %s", created.Status)
	}
}

func TestUserService_AdminCreateUser_WeakPassword(t *testing.T) {
	svc := newUserService(&userRepoStub{})

	_, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "testuser", "weak", "F", "L", "", true,
	)
	if err == nil {
		t.Fatal("expected validation error for weak password")
	}
}

func TestUserService_AdminCreateUser_EmailConflict(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn: func(email string) (bool, error) { return true, nil },
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"taken@test.com", "testuser", "StrongPass123!", "F", "L", "", true,
	)
	assertProblem(t, err, http.StatusConflict, "Email already registered")
}

func TestUserService_AdminCreateUser_UsernameConflict(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return true, nil },
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"new@test.com", "taken", "StrongPass123!", "F", "L", "", true,
	)
	assertProblem(t, err, http.StatusConflict, "Username already taken")
}

func TestUserService_AdminCreateUser_EmailCheckError(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn: func(email string) (bool, error) { return false, stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "user", "StrongPass123!", "F", "L", "", true,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to check email availability")
}

func TestUserService_AdminCreateUser_UsernameCheckError(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "user", "StrongPass123!", "F", "L", "", true,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to check username availability")
}

func TestUserService_AdminCreateUser_CreateFails(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn:           func(user *domain.User) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "user", "StrongPass123!", "F", "L", "", true,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to create user")
}

func TestUserService_AdminCreateUser_AssignRoleFails(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(user *domain.User) error {
			user.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: uuid.New(), Name: "user"}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error {
			return stderrors.New("assign failed")
		},
	}
	svc := newUserService(repo)

	_, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "user", "StrongPass123!", "F", "L", "", true,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to assign default role")
}

func TestUserService_AdminCreateUser_NoDefaultRoleIsOK(t *testing.T) {
	repo := &userRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn: func(user *domain.User) error {
			user.ID = uuid.New()
			return nil
		},
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return nil, stderrors.New("no default role")
		},
	}
	svc := newUserService(repo)

	result, err := svc.AdminCreateUser(
		context.Background(),
		"test@test.com", "user", "StrongPass123!", "F", "L", "", true,
	)
	if err != nil {
		t.Fatalf("expected success when no default role, got %v", err)
	}
	if result == nil {
		t.Fatal("expected user to be returned")
	}
}

// ---------------------------------------------------------------------------
// Tests: AdminUpdateUser
// ---------------------------------------------------------------------------

func TestUserService_AdminUpdateUser_Success(t *testing.T) {
	user := makeUser()
	updated := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		existsByEmailFn: func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		updateFn: func(u *domain.User) error {
			updated = true
			return nil
		},
	}
	svc := newUserService(repo)

	meta := domain.Metadata{"role": "admin"}
	result, err := svc.AdminUpdateUser(
		context.Background(),
		user.ID, "newemail@test.com", "newuser", "NewFirst", "NewLast", "+111", meta,
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !updated {
		t.Fatal("expected repo.Update to be called")
	}
	if result.Email != "newemail@test.com" {
		t.Fatalf("expected email updated, got %q", result.Email)
	}
	if result.Username != "newuser" {
		t.Fatalf("expected username updated, got %q", result.Username)
	}
	if result.Password != "" {
		t.Fatal("expected password to be cleared in response")
	}
}

func TestUserService_AdminUpdateUser_NotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateUser(context.Background(), uuid.New(), "", "", "", "", "", nil)
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminUpdateUser_EmailConflict(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn:       func(id uuid.UUID) (*domain.User, error) { return user, nil },
		existsByEmailFn: func(email string) (bool, error) { return true, nil },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateUser(
		context.Background(), user.ID, "taken@test.com", "", "", "", "", nil,
	)
	assertProblem(t, err, http.StatusConflict, "Email already registered")
}

func TestUserService_AdminUpdateUser_UsernameConflict(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn:          func(id uuid.UUID) (*domain.User, error) { return user, nil },
		existsByUsernameFn: func(username string) (bool, error) { return true, nil },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateUser(
		context.Background(), user.ID, "", "takenuser", "", "", "", nil,
	)
	assertProblem(t, err, http.StatusConflict, "Username already taken")
}

func TestUserService_AdminUpdateUser_EmailCheckError(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn:       func(id uuid.UUID) (*domain.User, error) { return user, nil },
		existsByEmailFn: func(email string) (bool, error) { return false, stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateUser(
		context.Background(), user.ID, "other@test.com", "", "", "", "", nil,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to check email availability")
}

func TestUserService_AdminUpdateUser_UsernameCheckError(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn:          func(id uuid.UUID) (*domain.User, error) { return user, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateUser(
		context.Background(), user.ID, "", "otheruser", "", "", "", nil,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to check username availability")
}

func TestUserService_AdminUpdateUser_SameEmailNoUniquenessCheck(t *testing.T) {
	user := makeUser()
	emailChecked := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		existsByEmailFn: func(email string) (bool, error) {
			emailChecked = true
			return false, nil
		},
		updateFn: func(u *domain.User) error { return nil },
	}
	svc := newUserService(repo)

	// Passing the same email should not trigger uniqueness check
	_, err := svc.AdminUpdateUser(
		context.Background(), user.ID, user.Email, "", "", "", "", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emailChecked {
		t.Fatal("expected no email uniqueness check when email unchanged")
	}
}

func TestUserService_AdminUpdateUser_UpdateFails(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateUser(
		context.Background(), user.ID, "", "", "F", "", "", nil,
	)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to update user")
}

func TestUserService_AdminUpdateUser_EmptyFieldsNotOverwritten(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	svc := newUserService(repo)

	result, err := svc.AdminUpdateUser(
		context.Background(), user.ID, "", "", "", "", "", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty strings should not overwrite existing values
	if result.FirstName != "First" || result.LastName != "Last" || result.Phone != "+1234567890" {
		t.Fatalf("expected original fields preserved, got first=%q last=%q phone=%q",
			result.FirstName, result.LastName, result.Phone)
	}
}

// ---------------------------------------------------------------------------
// Tests: AdminDeleteUser
// ---------------------------------------------------------------------------

func TestUserService_AdminDeleteUser_Success(t *testing.T) {
	userID := uuid.New()
	adminID := uuid.New()
	deleted := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: userID}, nil
		},
		deleteFn: func(id uuid.UUID) error {
			deleted = true
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminDeleteUser(userID, adminID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !deleted {
		t.Fatal("expected user to be deleted")
	}
}

func TestUserService_AdminDeleteUser_SelfDeleteGuard(t *testing.T) {
	sameID := uuid.New()
	svc := newUserService(&userRepoStub{})

	err := svc.AdminDeleteUser(sameID, sameID)
	assertProblem(t, err, http.StatusBadRequest, "Cannot delete your own account")
}

func TestUserService_AdminDeleteUser_UserNotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	err := svc.AdminDeleteUser(uuid.New(), uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminDeleteUser_DeleteFails(t *testing.T) {
	userID := uuid.New()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: userID}, nil
		},
		deleteFn: func(id uuid.UUID) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	err := svc.AdminDeleteUser(userID, uuid.New())
	assertProblem(t, err, http.StatusInternalServerError, "Failed to delete user")
}

func TestUserService_AdminDeleteUser_TokenRevocationFailureIsNonFatal(t *testing.T) {
	userID := uuid.New()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: userID}, nil
		},
		deleteFn: func(id uuid.UUID) error { return nil },
		revokeAllUserRefreshTokensFn: func(uid uuid.UUID) error {
			return stderrors.New("revoke fail")
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminDeleteUser(userID, uuid.New()); err != nil {
		t.Fatalf("expected success despite token revoke failure, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: AdminUpdateStatus
// ---------------------------------------------------------------------------

func TestUserService_AdminUpdateStatus_Activate(t *testing.T) {
	user := makeUser()
	user.Status = domain.UserStatusLocked
	updated := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			updated = true
			return nil
		},
	}
	svc := newUserService(repo)

	result, err := svc.AdminUpdateStatus(context.Background(), user.ID, "active")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !updated {
		t.Fatal("expected update call")
	}
	if result.Status != domain.UserStatusActive {
		t.Fatalf("expected active status, got %s", result.Status)
	}
	if result.Password != "" {
		t.Fatal("expected password cleared")
	}
}

func TestUserService_AdminUpdateStatus_LockRevokesTokens(t *testing.T) {
	user := makeUser()
	revoked := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
		revokeAllUserRefreshTokensFn: func(userID uuid.UUID) error {
			revoked = true
			return nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateStatus(context.Background(), user.ID, "locked")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !revoked {
		t.Fatal("expected tokens to be revoked on lock")
	}
}

func TestUserService_AdminUpdateStatus_InactiveRevokesTokens(t *testing.T) {
	user := makeUser()
	revoked := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
		revokeAllUserRefreshTokensFn: func(userID uuid.UUID) error {
			revoked = true
			return nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateStatus(context.Background(), user.ID, "inactive")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !revoked {
		t.Fatal("expected tokens to be revoked on inactive")
	}
}

func TestUserService_AdminUpdateStatus_ActiveDoesNotRevokeTokens(t *testing.T) {
	user := makeUser()
	revoked := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
		revokeAllUserRefreshTokensFn: func(userID uuid.UUID) error {
			revoked = true
			return nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateStatus(context.Background(), user.ID, "active")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if revoked {
		t.Fatal("expected tokens NOT to be revoked for active status")
	}
}

func TestUserService_AdminUpdateStatus_InvalidStatus(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateStatus(context.Background(), user.ID, "banned")
	assertProblem(t, err, http.StatusBadRequest, "Invalid status. Allowed: active, inactive, locked")
}

func TestUserService_AdminUpdateStatus_NotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateStatus(context.Background(), uuid.New(), "active")
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminUpdateStatus_UpdateFails(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminUpdateStatus(context.Background(), user.ID, "active")
	assertProblem(t, err, http.StatusInternalServerError, "Failed to update user status")
}

// ---------------------------------------------------------------------------
// Tests: AdminAssignRole
// ---------------------------------------------------------------------------

func TestUserService_AdminAssignRole_Success(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()
	assigned := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: userID}, nil
		},
		getRoleByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "admin"}, nil
		},
		assignRoleFn: func(uid, rid uuid.UUID) error {
			assigned = true
			if uid != userID || rid != roleID {
				t.Fatalf("unexpected IDs: user=%s role=%s", uid, rid)
			}
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminAssignRole(userID, roleID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !assigned {
		t.Fatal("expected role to be assigned")
	}
}

func TestUserService_AdminAssignRole_UserNotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	err := svc.AdminAssignRole(uuid.New(), uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminAssignRole_RoleNotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: id}, nil
		},
		getRoleByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return nil, stderrors.New("not found")
		},
	}
	svc := newUserService(repo)

	err := svc.AdminAssignRole(uuid.New(), uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

// ---------------------------------------------------------------------------
// Tests: AdminRemoveRole
// ---------------------------------------------------------------------------

func TestUserService_AdminRemoveRole_Success(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()
	removed := false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: userID}, nil
		},
		removeRoleFn: func(uid, rid uuid.UUID) error {
			removed = true
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminRemoveRole(userID, roleID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !removed {
		t.Fatal("expected role to be removed")
	}
}

func TestUserService_AdminRemoveRole_UserNotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	err := svc.AdminRemoveRole(uuid.New(), uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

// ---------------------------------------------------------------------------
// Tests: AdminUnlockUser
// ---------------------------------------------------------------------------

func TestUserService_AdminUnlockUser_Success(t *testing.T) {
	user := makeUser()
	user.Status = domain.UserStatusLocked
	user.FailedLoginAttempts = 5
	lockTime := time.Now().Add(time.Hour)
	user.LockedUntil = &lockTime
	updated := false

	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			updated = true
			if u.FailedLoginAttempts != 0 {
				t.Fatalf("expected failed login attempts reset, got %d", u.FailedLoginAttempts)
			}
			if u.LockedUntil != nil {
				t.Fatal("expected LockedUntil to be nil")
			}
			if u.Status != domain.UserStatusActive {
				t.Fatalf("expected active status, got %s", u.Status)
			}
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminUnlockUser(user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !updated {
		t.Fatal("expected update to be called")
	}
}

func TestUserService_AdminUnlockUser_AlreadyActive(t *testing.T) {
	user := makeUser()
	user.Status = domain.UserStatusActive
	user.FailedLoginAttempts = 2

	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			// Should still reset failed login attempts even if not locked
			if u.FailedLoginAttempts != 0 {
				t.Fatalf("expected failed login attempts reset, got %d", u.FailedLoginAttempts)
			}
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminUnlockUser(user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestUserService_AdminUnlockUser_NotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	err := svc.AdminUnlockUser(uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminUnlockUser_UpdateFails(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	err := svc.AdminUnlockUser(user.ID)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to unlock user")
}

// ---------------------------------------------------------------------------
// Tests: AdminResetPassword
// ---------------------------------------------------------------------------

func TestUserService_AdminResetPassword_Success(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByIDFn:    func(id uuid.UUID) (*domain.User, error) { return user, nil },
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	svc := newUserServiceWithAuth(repo)

	if err := svc.AdminResetPassword(user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestUserService_AdminResetPassword_UserNotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserServiceWithAuth(repo)

	err := svc.AdminResetPassword(uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

// ---------------------------------------------------------------------------
// Tests: AdminDisable2FA
// ---------------------------------------------------------------------------

func TestUserService_AdminDisable2FA_Success(t *testing.T) {
	user := makeUser()
	user.TwoFactorEnabled = true
	user.TwoFactorSecret = "encrypted-secret"
	user.TwoFactorBackupCodes = "hash1,hash2"
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			if u.TwoFactorEnabled {
				t.Fatal("expected 2FA to be disabled")
			}
			return nil
		},
	}
	svc := newUserServiceWithAuth(repo)

	if err := svc.AdminDisable2FA(user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestUserService_AdminDisable2FA_NotEnabled(t *testing.T) {
	user := makeUser()
	user.TwoFactorEnabled = false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	svc := newUserServiceWithAuth(repo)

	err := svc.AdminDisable2FA(user.ID)
	assertProblem(t, err, http.StatusBadRequest, "Two-factor authentication is not enabled")
}

// ---------------------------------------------------------------------------
// Tests: AdminGetByEmail
// ---------------------------------------------------------------------------

func TestUserService_AdminGetByEmail_Success(t *testing.T) {
	user := makeUser()
	user.Password = "should-be-cleared"
	repo := &userRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
		loadRolesFn:  func(u *domain.User) error { return nil },
	}
	svc := newUserService(repo)

	result, err := svc.AdminGetByEmail(context.Background(), user.Email)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result.Password != "" {
		t.Fatal("expected password to be cleared")
	}
}

func TestUserService_AdminGetByEmail_NotFound(t *testing.T) {
	repo := &userRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	_, err := svc.AdminGetByEmail(context.Background(), "missing@test.com")
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminGetByEmail_LoadRolesErrorIsNonFatal(t *testing.T) {
	user := makeUser()
	repo := &userRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
		loadRolesFn:  func(u *domain.User) error { return stderrors.New("role load failed") },
	}
	svc := newUserService(repo)

	result, err := svc.AdminGetByEmail(context.Background(), user.Email)
	if err != nil {
		t.Fatalf("expected success despite role load failure, got %v", err)
	}
	if result == nil {
		t.Fatal("expected user to be returned")
	}
}

// ---------------------------------------------------------------------------
// Tests: AdminVerifyUser
// ---------------------------------------------------------------------------

func TestUserService_AdminVerifyUser_Success(t *testing.T) {
	user := makeUser()
	user.Verified = false
	user.IsVerified = false
	user.Status = domain.UserStatusPending
	updated := false

	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			updated = true
			if !u.Verified || !u.IsVerified {
				t.Fatal("expected user to be verified")
			}
			if u.Status != domain.UserStatusActive {
				t.Fatalf("expected active status after verification, got %s", u.Status)
			}
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminVerifyUser(context.Background(), user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !updated {
		t.Fatal("expected update to be called")
	}
}

func TestUserService_AdminVerifyUser_AlreadyActiveStatusNotChanged(t *testing.T) {
	user := makeUser()
	user.Status = domain.UserStatusActive
	user.Verified = false

	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn: func(u *domain.User) error {
			// Status should remain active (not changed because not pending)
			if u.Status != domain.UserStatusActive {
				t.Fatalf("expected status to stay active, got %s", u.Status)
			}
			return nil
		},
	}
	svc := newUserService(repo)

	if err := svc.AdminVerifyUser(context.Background(), user.ID); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestUserService_AdminVerifyUser_NotFound(t *testing.T) {
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return nil, stderrors.New("not found") },
	}
	svc := newUserService(repo)

	err := svc.AdminVerifyUser(context.Background(), uuid.New())
	assertProblem(t, err, http.StatusNotFound, "")
}

func TestUserService_AdminVerifyUser_UpdateFails(t *testing.T) {
	user := makeUser()
	user.Verified = false
	repo := &userRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return stderrors.New("db error") },
	}
	svc := newUserService(repo)

	err := svc.AdminVerifyUser(context.Background(), user.ID)
	assertProblem(t, err, http.StatusInternalServerError, "Failed to verify user")
}

// ---------------------------------------------------------------------------
// Tests: resolveAvatarURL (indirect — via AdminGetUser)
// ---------------------------------------------------------------------------

func TestUserService_ResolveAvatarURL_NilUser(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	svc.SetStorage(&storageStub{})

	// Should not panic on nil user
	svc.resolveAvatarURL(context.Background(), nil)
}

func TestUserService_ResolveAvatarURL_EmptyAvatarURL(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	svc.SetStorage(&storageStub{})

	user := makeUser()
	user.AvatarURL = ""
	svc.resolveAvatarURL(context.Background(), user)
	if user.AvatarURL != "" {
		t.Fatal("expected empty avatar URL to remain empty")
	}
}

func TestUserService_ResolveAvatarURL_NoStorageService(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	// No storage set

	user := makeUser()
	user.AvatarURL = "some/key.jpg"
	svc.resolveAvatarURL(context.Background(), user)
	if user.AvatarURL != "some/key.jpg" {
		t.Fatal("expected avatar URL to remain as object key without storage")
	}
}

func TestUserService_ResolveAvatarURL_CacheHit(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	storageCalled := false
	svc.SetStorage(&storageStub{
		getURLFn: func(_ context.Context, key string) (string, error) {
			storageCalled = true
			return "https://cdn.test.com/" + key, nil
		},
	})
	svc.SetPresignCache(&presignCacheStub{
		getFn: func(_ context.Context, key string) (string, error) {
			return "https://cached.test.com/" + key, nil
		},
	})

	user := makeUser()
	user.AvatarURL = "avatars/cached.jpg"
	svc.resolveAvatarURL(context.Background(), user)

	if user.AvatarURL != "https://cached.test.com/avatars/cached.jpg" {
		t.Fatalf("expected cached URL, got %q", user.AvatarURL)
	}
	if storageCalled {
		t.Fatal("expected storage NOT to be called on cache hit")
	}
}

func TestUserService_ResolveAvatarURL_CacheMissThenPopulate(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	cached := false
	svc.SetStorage(&storageStub{
		getURLFn: func(_ context.Context, key string) (string, error) {
			return "https://storage.test.com/" + key, nil
		},
	})
	svc.SetPresignCache(&presignCacheStub{
		getFn: func(_ context.Context, key string) (string, error) {
			return "", stderrors.New("miss")
		},
		setFn: func(_ context.Context, key, url string) error {
			cached = true
			return nil
		},
	})

	user := makeUser()
	user.AvatarURL = "avatars/miss.jpg"
	svc.resolveAvatarURL(context.Background(), user)

	if user.AvatarURL != "https://storage.test.com/avatars/miss.jpg" {
		t.Fatalf("expected storage URL, got %q", user.AvatarURL)
	}
	if !cached {
		t.Fatal("expected cache to be populated on miss")
	}
}

func TestUserService_ResolveAvatarURL_StorageError(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	svc.SetStorage(&storageStub{
		getURLFn: func(_ context.Context, key string) (string, error) {
			return "", stderrors.New("storage down")
		},
	})

	user := makeUser()
	user.AvatarURL = "avatars/broken.jpg"
	originalKey := user.AvatarURL
	svc.resolveAvatarURL(context.Background(), user)

	// On storage error, the key should remain unchanged
	if user.AvatarURL != originalKey {
		t.Fatalf("expected avatar URL to remain as key on storage error, got %q", user.AvatarURL)
	}
}

func TestUserService_ResolveAvatarURL_CacheSetError(t *testing.T) {
	svc := newUserService(&userRepoStub{})
	svc.SetStorage(&storageStub{
		getURLFn: func(_ context.Context, key string) (string, error) {
			return "https://storage.test.com/" + key, nil
		},
	})
	svc.SetPresignCache(&presignCacheStub{
		getFn: func(_ context.Context, key string) (string, error) {
			return "", stderrors.New("miss")
		},
		setFn: func(_ context.Context, key, url string) error {
			return stderrors.New("cache write failed")
		},
	})

	user := makeUser()
	user.AvatarURL = "avatars/cache-write-fail.jpg"
	svc.resolveAvatarURL(context.Background(), user)

	// URL should still be resolved even if cache write fails
	if user.AvatarURL != "https://storage.test.com/avatars/cache-write-fail.jpg" {
		t.Fatalf("expected storage URL despite cache write failure, got %q", user.AvatarURL)
	}
}
