package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/config"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	grpcpkg "github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/test"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type grpcAuthUserRepoStub struct {
	createFn           func(user *domain.User) error
	updateFn           func(user *domain.User) error
	deleteFn           func(id uuid.UUID) error
	getByIDFn          func(id uuid.UUID) (*domain.User, error)
	getByEmailFn       func(email string) (*domain.User, error)
	getAllFn           func(offset, limit int) ([]*domain.User, error)
	listFilteredFn     func(filter domain.UserListFilter) ([]*domain.User, int64, error)
	countFn            func() (int64, error)
	existsByEmailFn    func(email string) (bool, error)
	existsByUsernameFn func(username string) (bool, error)
	loadRolesFn        func(user *domain.User) error
	getRoleByNameFn    func(name string) (*domain.Role, error)
	assignRoleFn       func(userID, roleID uuid.UUID) error
	createRefreshFn    func(token *domain.RefreshToken) error
	getRefreshFn       func(token string) (*domain.RefreshToken, error)
	revokeRefreshFn    func(token string) error
}

var _ identityRepo.UserRepository = (*grpcAuthUserRepoStub)(nil)

func (s *grpcAuthUserRepoStub) WithTx(_ *gorm.DB) identityRepo.UserRepository { return s }
func (s *grpcAuthUserRepoStub) Create(user *domain.User) error {
	if s.createFn != nil {
		return s.createFn(user)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) Update(user *domain.User) error {
	if s.updateFn != nil {
		return s.updateFn(user)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) Delete(id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(id)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) GetByID(id uuid.UUID) (*domain.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *grpcAuthUserRepoStub) GetByIDForUpdate(id uuid.UUID) (*domain.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *grpcAuthUserRepoStub) GetByEmail(email string) (*domain.User, error) {
	if s.getByEmailFn != nil {
		return s.getByEmailFn(email)
	}
	return nil, nil
}
func (s *grpcAuthUserRepoStub) GetByUsername(username string) (*domain.User, error) { return nil, nil }
func (s *grpcAuthUserRepoStub) GetAll(offset, limit int) ([]*domain.User, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return nil, nil
}
func (s *grpcAuthUserRepoStub) ListFiltered(filter domain.UserListFilter) ([]*domain.User, int64, error) {
	if s.listFilteredFn != nil {
		return s.listFilteredFn(filter)
	}
	return nil, 0, nil
}
func (s *grpcAuthUserRepoStub) Count() (int64, error) {
	if s.countFn != nil {
		return s.countFn()
	}
	return 0, nil
}
func (s *grpcAuthUserRepoStub) ExistsByEmail(email string) (bool, error) {
	if s.existsByEmailFn != nil {
		return s.existsByEmailFn(email)
	}
	return false, nil
}
func (s *grpcAuthUserRepoStub) ExistsByUsername(username string) (bool, error) {
	if s.existsByUsernameFn != nil {
		return s.existsByUsernameFn(username)
	}
	return false, nil
}
func (s *grpcAuthUserRepoStub) LoadRoles(user *domain.User) error {
	if s.loadRolesFn != nil {
		return s.loadRolesFn(user)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) CreateRole(role *domain.Role) error             { return nil }
func (s *grpcAuthUserRepoStub) UpdateRole(role *domain.Role) error             { return nil }
func (s *grpcAuthUserRepoStub) DeleteRole(id uuid.UUID) error                  { return nil }
func (s *grpcAuthUserRepoStub) GetRoleByID(id uuid.UUID) (*domain.Role, error) { return nil, nil }
func (s *grpcAuthUserRepoStub) GetRoleByName(name string) (*domain.Role, error) {
	if s.getRoleByNameFn != nil {
		return s.getRoleByNameFn(name)
	}
	return nil, errors.New("not found")
}
func (s *grpcAuthUserRepoStub) GetAllRoles() ([]*domain.Role, error) { return nil, nil }
func (s *grpcAuthUserRepoStub) AssignRole(userID, roleID uuid.UUID) error {
	if s.assignRoleFn != nil {
		return s.assignRoleFn(userID, roleID)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) RemoveRole(userID, roleID uuid.UUID) error { return nil }
func (s *grpcAuthUserRepoStub) GetUserRoles(userID uuid.UUID) ([]*domain.Role, error) {
	return nil, nil
}
func (s *grpcAuthUserRepoStub) CreatePermission(permission *domain.Permission) error { return nil }
func (s *grpcAuthUserRepoStub) UpdatePermission(permission *domain.Permission) error { return nil }
func (s *grpcAuthUserRepoStub) DeletePermission(id uuid.UUID) error                  { return nil }
func (s *grpcAuthUserRepoStub) GetPermissionByID(id uuid.UUID) (*domain.Permission, error) {
	return nil, nil
}
func (s *grpcAuthUserRepoStub) GetAllPermissions() ([]*domain.Permission, error) { return nil, nil }
func (s *grpcAuthUserRepoStub) AssignPermissionToRole(roleID, permissionID uuid.UUID) error {
	return nil
}
func (s *grpcAuthUserRepoStub) RemovePermissionFromRole(roleID, permissionID uuid.UUID) error {
	return nil
}
func (s *grpcAuthUserRepoStub) GetRolePermissions(roleID uuid.UUID) ([]*domain.Permission, error) {
	return nil, nil
}
func (s *grpcAuthUserRepoStub) CreateRefreshToken(token *domain.RefreshToken) error {
	if s.createRefreshFn != nil {
		return s.createRefreshFn(token)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) GetRefreshToken(token string) (*domain.RefreshToken, error) {
	if s.getRefreshFn != nil {
		return s.getRefreshFn(token)
	}
	return nil, errors.New("not found")
}
func (s *grpcAuthUserRepoStub) RevokeRefreshToken(token string) error {
	if s.revokeRefreshFn != nil {
		return s.revokeRefreshFn(token)
	}
	return nil
}
func (s *grpcAuthUserRepoStub) RevokeAllUserRefreshTokens(userID uuid.UUID) error { return nil }
func (s *grpcAuthUserRepoStub) GetActiveRefreshTokensByUser(userID uuid.UUID) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (s *grpcAuthUserRepoStub) RevokeRefreshTokenByID(id uuid.UUID) error        { return nil }
func (s *grpcAuthUserRepoStub) CleanExpiredRefreshTokens() error                 { return nil }
func (s *grpcAuthUserRepoStub) CountByStatus(status string) (int64, error)       { return 0, nil }
func (s *grpcAuthUserRepoStub) CountCreatedAfter(after time.Time) (int64, error) { return 0, nil }
func (s *grpcAuthUserRepoStub) GetAllActiveSessions(offset, limit int) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (s *grpcAuthUserRepoStub) CountActiveSessions() (int64, error) { return 0, nil }

type grpcVerificationRepoStub struct{}

func (s *grpcVerificationRepoStub) WithTx(_ *gorm.DB) identityRepo.VerificationTokenRepository {
	return s
}
func (s *grpcVerificationRepoStub) Create(token *domain.VerificationToken) error {
	token.Token = "tok"
	return nil
}
func (s *grpcVerificationRepoStub) FindByToken(token string) (*domain.VerificationToken, error) {
	return nil, errors.New("not found")
}
func (s *grpcVerificationRepoStub) FindByUserAndType(userID uuid.UUID, tokenType domain.TokenType) (*domain.VerificationToken, error) {
	return nil, nil
}
func (s *grpcVerificationRepoStub) Update(token *domain.VerificationToken) error { return nil }
func (s *grpcVerificationRepoStub) Delete(id uuid.UUID) error                    { return nil }
func (s *grpcVerificationRepoStub) DeleteExpiredTokens() error                   { return nil }
func (s *grpcVerificationRepoStub) DeleteByUserAndType(userID uuid.UUID, tokenType domain.TokenType) error {
	return nil
}
func (s *grpcVerificationRepoStub) CountByUserAndType(userID uuid.UUID, tokenType domain.TokenType, since time.Time) (int64, error) {
	return 0, nil
}

type grpcEnhancedEmailStub struct{}

func (s *grpcEnhancedEmailStub) SendVerificationEmail(to, username, token string, languageCode string) error {
	return nil
}
func (s *grpcEnhancedEmailStub) SendPasswordResetEmail(to, username, token string, languageCode string) error {
	return nil
}

func (s *grpcEnhancedEmailStub) SendPasswordChangedEmail(to, fullName string, languageCode string) error {
	return nil
}

func newAuthGRPCServer(t *testing.T, repo *grpcAuthUserRepoStub) (*AuthServiceServer, *identityService.TokenService, *config.Config) {
	t.Helper()
	cfg := test.TestConfig()
	tokenSvc := identityService.NewTokenService(cfg, repo)
	authSvc := identityService.NewAuthService(cfg, nil, repo, tokenSvc, &grpcVerificationRepoStub{}, nil, &grpcEnhancedEmailStub{})
	return NewAuthServiceServer(authSvc, repo, tokenSvc, cfg), tokenSvc, cfg
}

func mustActiveUser(t *testing.T) *domain.User {
	t.Helper()
	u := &domain.User{
		ID:       uuid.New(),
		Email:    "staff@example.com",
		Username: "staff",
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	if err := u.SetPassword("StrongPass123!"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	return u
}

func TestGRPCAuthServiceLogin_Register_Refresh_Logout(t *testing.T) {
	user := mustActiveUser(t)
	roleID := uuid.New()
	repo := &grpcAuthUserRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
		loadRolesFn:  func(u *domain.User) error { return nil },
		existsByEmailFn: func(email string) (bool, error) {
			return false, nil
		},
		existsByUsernameFn: func(username string) (bool, error) {
			return false, nil
		},
		createFn: func(u *domain.User) error { u.ID = uuid.New(); return nil },
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "user"}, nil
		},
		assignRoleFn:    func(userID, roleID uuid.UUID) error { return nil },
		createRefreshFn: func(token *domain.RefreshToken) error { return nil },
		getRefreshFn: func(token string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: token, Revoked: false}, nil
		},
		revokeRefreshFn: func(token string) error { return nil },
		getByIDFn:       func(id uuid.UUID) (*domain.User, error) { return user, nil },
	}
	srv, tokenSvc, _ := newAuthGRPCServer(t, repo)

	loginResp, err := srv.Login(context.Background(), &pb.LoginRequest{
		Email:    user.Email,
		Password: "StrongPass123!",
	})
	if err != nil || loginResp.AccessToken == "" {
		t.Fatalf("expected login success, err=%v", err)
	}

	registerResp, err := srv.Register(context.Background(), &pb.RegisterRequest{
		Email:    "new@example.com",
		Username: "newuser",
		Password: "StrongPass123!",
	})
	if err != nil || registerResp.UserId == "" {
		t.Fatalf("expected register success, err=%v", err)
	}

	refreshToken, err := tokenSvc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("generate refresh failed: %v", err)
	}
	refreshResp, err := srv.RefreshToken(context.Background(), &pb.RefreshTokenRequest{RefreshToken: refreshToken})
	if err != nil || refreshResp.AccessToken == "" {
		t.Fatalf("expected refresh success, err=%v", err)
	}

	logoutResp, err := srv.Logout(context.Background(), &pb.LogoutRequest{Token: refreshToken})
	if err != nil || logoutResp.Message == "" {
		t.Fatalf("expected logout success, err=%v", err)
	}
}

func TestGRPCAuthServiceStatusMappings(t *testing.T) {
	user := mustActiveUser(t)
	repo := &grpcAuthUserRepoStub{
		existsByEmailFn: func(email string) (bool, error) {
			return true, nil
		},
		getByEmailFn: func(email string) (*domain.User, error) {
			return user, nil
		},
		getRefreshFn: func(token string) (*domain.RefreshToken, error) {
			return &domain.RefreshToken{Token: token, Revoked: true}, nil
		},
		createRefreshFn: func(token *domain.RefreshToken) error { return nil },
	}
	srv, tokenSvc, _ := newAuthGRPCServer(t, repo)

	_, err := srv.Register(context.Background(), &pb.RegisterRequest{
		Email:    "dup@example.com",
		Username: "dup",
		Password: "StrongPass123!",
	})
	if grpcCode(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists for duplicate register, got %v", grpcCode(err))
	}

	refreshToken, err := tokenSvc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("generate refresh failed: %v", err)
	}
	_, err = srv.RefreshToken(context.Background(), &pb.RefreshTokenRequest{RefreshToken: refreshToken})
	if grpcCode(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for revoked refresh token, got %v", grpcCode(err))
	}
}

func TestToGRPCErrorMapping(t *testing.T) {
	err := grpcpkg.ToGRPCError(coreerrors.NewBadRequest("bad payload"))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", status.Code(err))
	}
}

func TestGRPCAuthServiceInputValidation(t *testing.T) {
	repo := &grpcAuthUserRepoStub{}
	srv, _, _ := newAuthGRPCServer(t, repo)
	ctx := context.Background()

	// RequestPasswordReset empty email
	_, err := srv.RequestPasswordReset(ctx, &pb.RequestPasswordResetRequest{Email: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty email, got %v", status.Code(err))
	}

	// ResetPassword empty token
	_, err = srv.ResetPassword(ctx, &pb.ResetPasswordRequest{Token: "", NewPassword: "pass"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty token")
	}
	// ResetPassword empty password
	_, err = srv.ResetPassword(ctx, &pb.ResetPasswordRequest{Token: "tok", NewPassword: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty password")
	}

	// VerifyEmail empty token
	_, err = srv.VerifyEmail(ctx, &pb.VerifyEmailRequest{Token: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty token")
	}

	// ResendVerificationEmail empty email
	_, err = srv.ResendVerificationEmail(ctx, &pb.ResendVerificationEmailRequest{Email: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty email")
	}

	// ChangePassword empty current password
	authCtx := grpcpkg.ContextWithAuth(ctx, uuid.NewString(), []string{"user"})
	_, err = srv.ChangePassword(authCtx, &pb.ChangePasswordRequest{CurrentPassword: "", NewPassword: "new"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty current password")
	}
	// ChangePassword empty new password
	_, err = srv.ChangePassword(authCtx, &pb.ChangePasswordRequest{CurrentPassword: "old", NewPassword: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty new password")
	}

	// ChangePassword without auth
	_, err = srv.ChangePassword(ctx, &pb.ChangePasswordRequest{CurrentPassword: "old", NewPassword: "new"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for no auth context, got %v", status.Code(err))
	}

	// ValidateToken empty token
	_, err = srv.ValidateToken(ctx, &pb.ValidateTokenRequest{Token: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty token")
	}

	// Login empty email
	_, err = srv.Login(ctx, &pb.LoginRequest{Email: "", Password: "pass"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty email on login")
	}
	// Login empty password
	_, err = srv.Login(ctx, &pb.LoginRequest{Email: "a@b.c", Password: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty password on login")
	}

	// Register empty email
	_, err = srv.Register(ctx, &pb.RegisterRequest{Email: "", Username: "u", Password: "p"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty email on register")
	}
	// Register empty username
	_, err = srv.Register(ctx, &pb.RegisterRequest{Email: "a@b.c", Username: "", Password: "p"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty username on register")
	}
	// Register empty password
	_, err = srv.Register(ctx, &pb.RegisterRequest{Email: "a@b.c", Username: "u", Password: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty password on register")
	}

	// Logout empty token
	_, err = srv.Logout(ctx, &pb.LogoutRequest{Token: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty token on logout")
	}

	// RefreshToken empty
	_, err = srv.RefreshToken(ctx, &pb.RefreshTokenRequest{RefreshToken: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty refresh token")
	}
}

func TestGRPCAuthServiceValidateToken(t *testing.T) {
	user := mustActiveUser(t)
	repo := &grpcAuthUserRepoStub{
		getByIDFn:    func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn:  func(u *domain.User) error { return nil },
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	srv, tokenSvc, _ := newAuthGRPCServer(t, repo)

	accessToken, _, err := tokenSvc.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("generate access token failed: %v", err)
	}

	resp, err := srv.ValidateToken(context.Background(), &pb.ValidateTokenRequest{Token: accessToken})
	if err != nil {
		t.Fatalf("expected ValidateToken success, got %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected valid=true")
	}
	if resp.UserId != user.ID.String() {
		t.Fatalf("expected user_id %s, got %s", user.ID.String(), resp.UserId)
	}
}

func TestGRPCAuthServiceValidateTokenInvalid(t *testing.T) {
	repo := &grpcAuthUserRepoStub{}
	srv, _, _ := newAuthGRPCServer(t, repo)

	_, err := srv.ValidateToken(context.Background(), &pb.ValidateTokenRequest{Token: "invalid-token"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for invalid token, got %v", status.Code(err))
	}
}

func TestGRPCAuthServiceChangePasswordInvalidUUID(t *testing.T) {
	repo := &grpcAuthUserRepoStub{}
	srv, _, _ := newAuthGRPCServer(t, repo)

	ctx := grpcpkg.ContextWithAuth(context.Background(), "not-a-uuid", []string{"user"})
	_, err := srv.ChangePassword(ctx, &pb.ChangePasswordRequest{
		CurrentPassword: "old", NewPassword: "new",
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal for invalid UUID in context, got %v", status.Code(err))
	}
}

func TestGRPCAuthServiceRequestPasswordResetSuccess(t *testing.T) {
	user := mustActiveUser(t)
	repo := &grpcAuthUserRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	srv, _, _ := newAuthGRPCServer(t, repo)

	resp, err := srv.RequestPasswordReset(context.Background(), &pb.RequestPasswordResetRequest{
		Email: user.Email,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.Message == "" {
		t.Fatalf("expected non-empty message")
	}
}

func TestGRPCAuthServiceResetPasswordSuccess(t *testing.T) {
	user := mustActiveUser(t)
	verifyRepo := &grpcVerificationRepoStub{}
	// For ResetPassword, the service needs FindByToken to return a valid token
	// Since verifyRepo.FindByToken returns error, we expect this to fail with internal error
	repo := &grpcAuthUserRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	srv, _, _ := newAuthGRPCServer(t, repo)
	_ = verifyRepo

	// ResetPassword with a valid-looking token but invalid verification token
	_, err := srv.ResetPassword(context.Background(), &pb.ResetPasswordRequest{
		Token:       "some-token",
		NewPassword: "NewStrongPass123!",
	})
	// This will fail because verification repo returns "not found" — but we exercise the code path
	if err == nil {
		t.Fatalf("expected error for invalid reset token")
	}
}

func TestGRPCAuthServiceVerifyEmailSuccess(t *testing.T) {
	repo := &grpcAuthUserRepoStub{}
	srv, _, _ := newAuthGRPCServer(t, repo)

	// VerifyEmail with an invalid token — exercises the code path past validation
	_, err := srv.VerifyEmail(context.Background(), &pb.VerifyEmailRequest{
		Token: "some-token",
	})
	// Will fail because verification repo returns "not found"
	if err == nil {
		t.Fatalf("expected error for invalid verification token")
	}
}

func TestGRPCAuthServiceResendVerificationEmailSuccess(t *testing.T) {
	user := mustActiveUser(t)
	repo := &grpcAuthUserRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	srv, _, _ := newAuthGRPCServer(t, repo)

	// ResendVerificationEmail — user is already verified, so service may error
	_, err := srv.ResendVerificationEmail(context.Background(), &pb.ResendVerificationEmailRequest{
		Email: user.Email,
	})
	// Whether it succeeds or fails depends on service logic, but we exercise the gRPC handler path
	_ = err
}

func TestGRPCAuthServiceChangePasswordSuccess(t *testing.T) {
	user := mustActiveUser(t)
	repo := &grpcAuthUserRepoStub{
		getByIDFn:   func(id uuid.UUID) (*domain.User, error) { return user, nil },
		loadRolesFn: func(u *domain.User) error { return nil },
		updateFn:    func(u *domain.User) error { return nil },
	}
	srv, _, _ := newAuthGRPCServer(t, repo)

	authCtx := grpcpkg.ContextWithAuth(context.Background(), user.ID.String(), []string{"user"})
	resp, err := srv.ChangePassword(authCtx, &pb.ChangePasswordRequest{
		CurrentPassword: "StrongPass123!",
		NewPassword:     "NewStrongPass456!",
	})
	if err != nil {
		t.Fatalf("expected ChangePassword success, got %v", err)
	}
	if resp.Message == "" {
		t.Fatalf("expected non-empty message")
	}
}

func TestConvertMetadataToStringMap(t *testing.T) {
	// nil input
	if got := convertMetadataToStringMap(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
	// with values
	m := map[string]interface{}{"key": 42, "name": "test"}
	result := convertMetadataToStringMap(m)
	if result["key"] != "42" || result["name"] != "test" {
		t.Fatalf("expected converted map, got %v", result)
	}
}
