package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	grpcpkg "github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/test"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newUserGRPCServer(t *testing.T, repo *grpcAuthUserRepoStub) *UserServiceServer {
	t.Helper()
	cfg := test.TestConfig()
	tokenSvc := identityService.NewTokenService(cfg, repo)
	userSvc := identityService.NewUserService(cfg, nil, repo, nil, tokenSvc)
	return NewUserServiceServer(userSvc)
}

// grpcCode converts a raw handler error to its gRPC status code, mirroring
// what ErrorInterceptor does in production.
func grpcCode(err error) codes.Code {
	return status.Code(grpcpkg.ToGRPCError(err))
}

func TestGRPCUserServiceGetUpdateListDelete(t *testing.T) {
	user := &domain.User{
		ID:       uuid.New(),
		Email:    "user@example.com",
		Username: "user",
	}
	repo := &grpcAuthUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			if id != user.ID {
				return nil, coreerrors.NewNotFound("user", id.String())
			}
			return user, nil
		},
		updateFn: func(u *domain.User) error { return nil },
		deleteFn: func(id uuid.UUID) error { return nil },
		getByEmailFn: func(email string) (*domain.User, error) {
			return user, nil
		},
		listFilteredFn: func(filter domain.UserListFilter) ([]*domain.User, int64, error) {
			return []*domain.User{user}, 1, nil
		},
	}

	srv := newUserGRPCServer(t, repo)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	// GetUser requires self-or-admin context
	selfCtx := grpcpkg.ContextWithAuth(context.Background(), user.ID.String(), []string{"user"})
	getResp, err := srv.GetUser(selfCtx, &pb.GetUserRequest{Id: user.ID.String()})
	if err != nil || getResp.User.Id == "" {
		t.Fatalf("expected GetUser success, err=%v", err)
	}

	// Bad UUID should return InvalidArgument (admin context to pass auth check first)
	_, err = srv.GetUser(adminCtx, &pb.GetUserRequest{Id: "bad-id"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for bad id, got %v", status.Code(err))
	}

	// UpdateUser requires self or admin context
	updateResp, err := srv.UpdateUser(selfCtx, &pb.UpdateUserRequest{
		Id:       user.ID.String(),
		Username: "updated",
	})
	if err != nil || updateResp.User.Username != "updated" {
		t.Fatalf("expected UpdateUser success, err=%v", err)
	}

	// ListUsers requires admin context
	listResp, err := srv.ListUsers(adminCtx, &pb.ListUsersRequest{Page: 1, PageSize: 10})
	if err != nil || len(listResp.Users) != 1 {
		t.Fatalf("expected ListUsers success, err=%v", err)
	}

	// DeleteUser requires admin context
	if _, err := srv.DeleteUser(adminCtx, &pb.DeleteUserRequest{Id: user.ID.String()}); err != nil {
		t.Fatalf("expected DeleteUser success, got %v", err)
	}
}

func TestGRPCUserServiceNotFoundAndValidation(t *testing.T) {
	repo := &grpcAuthUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, coreerrors.NewNotFound("user", id.String())
		},
	}
	srv := newUserGRPCServer(t, repo)

	// GetUser not found (admin context, raw error goes through ErrorInterceptor in prod)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})
	_, err := srv.GetUser(adminCtx, &pb.GetUserRequest{Id: uuid.NewString()})
	if grpcCode(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", grpcCode(err))
	}

	// UpdateUser with invalid ID should fail
	_, err = srv.UpdateUser(adminCtx, &pb.UpdateUserRequest{Id: "invalid-id"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", status.Code(err))
	}

	// UpdateUser without auth should be Unauthenticated
	_, err = srv.UpdateUser(context.Background(), &pb.UpdateUserRequest{Id: uuid.NewString()})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for no auth context, got %v", status.Code(err))
	}

	// DeleteUser without admin should be PermissionDenied
	userCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"user"})
	_, err = srv.DeleteUser(userCtx, &pb.DeleteUserRequest{Id: uuid.NewString()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin, got %v", status.Code(err))
	}
}

func TestGRPCUserServiceCreateUser(t *testing.T) {
	createdID := uuid.New()
	repo := &grpcAuthUserRepoStub{
		existsByEmailFn:    func(email string) (bool, error) { return false, nil },
		existsByUsernameFn: func(username string) (bool, error) { return false, nil },
		createFn:           func(u *domain.User) error { u.ID = createdID; return nil },
		getRoleByNameFn: func(name string) (*domain.Role, error) {
			return &domain.Role{ID: uuid.New(), Name: name}, nil
		},
		assignRoleFn: func(userID, roleID uuid.UUID) error { return nil },
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return &domain.User{ID: id, Email: "new@test.com", Username: "newuser", Status: domain.UserStatusActive, Verified: true}, nil
		},
	}
	srv := newUserGRPCServer(t, repo)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	resp, err := srv.CreateUser(adminCtx, &pb.CreateUserRequest{
		Email: "new@test.com", Username: "newuser", Password: "StrongPass123!",
		FirstName: "New", LastName: "User",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.User == nil {
		t.Fatalf("expected user in response")
	}
}

func TestGRPCUserServiceCreateUserValidation(t *testing.T) {
	srv := newUserGRPCServer(t, &grpcAuthUserRepoStub{})
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	// Empty email
	_, err := srv.CreateUser(adminCtx, &pb.CreateUserRequest{Email: "", Username: "u", Password: "p"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty email, got %v", status.Code(err))
	}

	// Empty username
	_, err = srv.CreateUser(adminCtx, &pb.CreateUserRequest{Email: "a@b.c", Username: "", Password: "p"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty username, got %v", status.Code(err))
	}

	// Empty password
	_, err = srv.CreateUser(adminCtx, &pb.CreateUserRequest{Email: "a@b.c", Username: "u", Password: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty password, got %v", status.Code(err))
	}

	// Non-admin
	userCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"user"})
	_, err = srv.CreateUser(userCtx, &pb.CreateUserRequest{Email: "a@b.c", Username: "u", Password: "p"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin, got %v", status.Code(err))
	}

	// Unauthenticated
	_, err = srv.CreateUser(context.Background(), &pb.CreateUserRequest{Email: "a@b.c", Username: "u", Password: "p"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestGRPCUserServiceGetUserByEmail(t *testing.T) {
	user := &domain.User{ID: uuid.New(), Email: "found@test.com", Username: "found", Status: domain.UserStatusActive, Verified: true}
	repo := &grpcAuthUserRepoStub{
		getByEmailFn: func(email string) (*domain.User, error) { return user, nil },
	}
	srv := newUserGRPCServer(t, repo)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	resp, err := srv.GetUserByEmail(adminCtx, &pb.GetUserByEmailRequest{Email: "found@test.com"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.User.Email != "found@test.com" {
		t.Fatalf("expected email found@test.com, got %s", resp.User.Email)
	}

	// Empty email
	_, err = srv.GetUserByEmail(adminCtx, &pb.GetUserByEmailRequest{Email: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty email, got %v", status.Code(err))
	}

	// Non-admin
	userCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"user"})
	_, err = srv.GetUserByEmail(userCtx, &pb.GetUserByEmailRequest{Email: "a@b.c"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin, got %v", status.Code(err))
	}

	// Unauthenticated
	_, err = srv.GetUserByEmail(context.Background(), &pb.GetUserByEmailRequest{Email: "a@b.c"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestGRPCUserServiceVerifyUser(t *testing.T) {
	user := &domain.User{ID: uuid.New(), Email: "verify@test.com", Username: "verify", Status: domain.UserStatusActive}
	repo := &grpcAuthUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	srv := newUserGRPCServer(t, repo)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	_, err := srv.VerifyUser(adminCtx, &pb.VerifyUserRequest{UserId: user.ID.String()})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// Empty user_id
	_, err = srv.VerifyUser(adminCtx, &pb.VerifyUserRequest{UserId: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty user_id, got %v", status.Code(err))
	}

	// Invalid UUID
	_, err = srv.VerifyUser(adminCtx, &pb.VerifyUserRequest{UserId: "bad-uuid"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for bad UUID, got %v", status.Code(err))
	}

	// Non-admin
	userCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"user"})
	_, err = srv.VerifyUser(userCtx, &pb.VerifyUserRequest{UserId: uuid.NewString()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin, got %v", status.Code(err))
	}

	// Unauthenticated
	_, err = srv.VerifyUser(context.Background(), &pb.VerifyUserRequest{UserId: uuid.NewString()})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestGRPCUserServiceEmptyIDValidation(t *testing.T) {
	srv := newUserGRPCServer(t, &grpcAuthUserRepoStub{})
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	// GetUser empty ID
	_, err := srv.GetUser(adminCtx, &pb.GetUserRequest{Id: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty GetUser ID, got %v", status.Code(err))
	}

	// UpdateUser empty ID
	_, err = srv.UpdateUser(adminCtx, &pb.UpdateUserRequest{Id: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty UpdateUser ID, got %v", status.Code(err))
	}

	// DeleteUser empty ID
	_, err = srv.DeleteUser(adminCtx, &pb.DeleteUserRequest{Id: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty DeleteUser ID, got %v", status.Code(err))
	}

	// DeleteUser invalid UUID
	_, err = srv.DeleteUser(adminCtx, &pb.DeleteUserRequest{Id: "bad-uuid"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for invalid UUID, got %v", status.Code(err))
	}
}

func TestGRPCUserServiceListUsersPagination(t *testing.T) {
	repo := &grpcAuthUserRepoStub{
		listFilteredFn: func(filter domain.UserListFilter) ([]*domain.User, int64, error) {
			return []*domain.User{}, 0, nil
		},
	}
	srv := newUserGRPCServer(t, repo)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	// Zero page and page_size should default
	resp, err := srv.ListUsers(adminCtx, &pb.ListUsersRequest{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.Page != 1 || resp.PageSize != 10 {
		t.Fatalf("expected defaults page=1, pageSize=10, got page=%d, pageSize=%d", resp.Page, resp.PageSize)
	}

	// PageSize > 100 should be capped
	resp, err = srv.ListUsers(adminCtx, &pb.ListUsersRequest{Page: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.PageSize != 100 {
		t.Fatalf("expected pageSize capped to 100, got %d", resp.PageSize)
	}

	// ListUsers without auth
	_, err = srv.ListUsers(context.Background(), &pb.ListUsersRequest{Page: 1, PageSize: 10})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}

	// ListUsers with filter parameters
	resp, err = srv.ListUsers(adminCtx, &pb.ListUsersRequest{
		Page: 2, PageSize: 5, Search: "test", SortBy: "email", Order: "asc",
		Roles: []string{"admin"}, OnlyActive: true,
	})
	if err != nil {
		t.Fatalf("expected success with filters, got %v", err)
	}
	if resp.Page != 2 || resp.PageSize != 5 {
		t.Fatalf("expected page=2, pageSize=5, got page=%d, pageSize=%d", resp.Page, resp.PageSize)
	}
}

func TestConvertStringMapToInterface(t *testing.T) {
	// nil
	if got := convertStringMapToInterface(nil); got != nil {
		t.Fatalf("expected nil for nil input")
	}
	// with values
	m := map[string]string{"a": "1", "b": "2"}
	result := convertStringMapToInterface(m)
	if len(result) != 2 || result["a"] != "1" {
		t.Fatalf("expected converted map, got %v", result)
	}
}

func TestDomainUserToProtoNil(t *testing.T) {
	if got := domainUserToProto(nil); got != nil {
		t.Fatalf("expected nil for nil user")
	}
}

func TestDomainUserToProtoWithLastLogin(t *testing.T) {
	now := time.Now()
	user := &domain.User{
		ID:        uuid.New(),
		Email:     "test@example.com",
		Username:  "test",
		Status:    domain.UserStatusActive,
		Verified:  true,
		LastLogin: &now,
		Roles:     []domain.Role{{Name: "admin"}, {Name: "user"}},
		Metadata:  domain.Metadata{"theme": "dark"},
	}
	proto := domainUserToProto(user)
	if proto.LastLoginAt == nil {
		t.Fatalf("expected LastLoginAt to be set")
	}
	if len(proto.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(proto.Roles))
	}
	if proto.Metadata["theme"] != "dark" {
		t.Fatalf("expected metadata theme=dark, got %s", proto.Metadata["theme"])
	}
	if !proto.IsActive {
		t.Fatalf("expected IsActive=true for active verified user")
	}
}

func TestDomainUserToProtoWithoutLastLogin(t *testing.T) {
	user := &domain.User{
		ID:       uuid.New(),
		Email:    "nologin@example.com",
		Username: "nologin",
		Status:   domain.UserStatusActive,
	}
	proto := domainUserToProto(user)
	if proto.LastLoginAt != nil {
		t.Fatalf("expected LastLoginAt to be nil")
	}
	if proto.Metadata != nil {
		t.Fatalf("expected nil metadata")
	}
}

func TestGRPCUserServiceUpdateUserWithMetadata(t *testing.T) {
	user := &domain.User{
		ID:       uuid.New(),
		Email:    "meta@test.com",
		Username: "meta",
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	repo := &grpcAuthUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		updateFn:  func(u *domain.User) error { return nil },
	}
	srv := newUserGRPCServer(t, repo)
	selfCtx := grpcpkg.ContextWithAuth(context.Background(), user.ID.String(), []string{"user"})

	resp, err := srv.UpdateUser(selfCtx, &pb.UpdateUserRequest{
		Id:       user.ID.String(),
		Username: "updated",
		Metadata: map[string]string{"theme": "dark"},
	})
	if err != nil {
		t.Fatalf("expected UpdateUser success, got %v", err)
	}
	if resp.User == nil {
		t.Fatalf("expected user in response")
	}
}

func TestGRPCUserServiceDeleteUserSuccess(t *testing.T) {
	user := &domain.User{ID: uuid.New(), Email: "del@test.com", Username: "del", Status: domain.UserStatusActive}
	repo := &grpcAuthUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) { return user, nil },
		deleteFn:  func(id uuid.UUID) error { return nil },
	}
	srv := newUserGRPCServer(t, repo)
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})

	_, err := srv.DeleteUser(adminCtx, &pb.DeleteUserRequest{Id: user.ID.String()})
	if err != nil {
		t.Fatalf("expected DeleteUser success, got %v", err)
	}
}

func TestGRPCUserServiceStreamUserEventsNoDispatcher(t *testing.T) {
	srv := newUserGRPCServer(t, &grpcAuthUserRepoStub{})
	// StreamUserEvents without dispatcher should return Unavailable
	err := srv.StreamUserEvents(&pb.StreamUserEventsRequest{}, nil)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("expected Unavailable for no dispatcher, got %v", status.Code(err))
	}
}
