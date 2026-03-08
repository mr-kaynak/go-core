package services

import (
	"context"
	"testing"

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
