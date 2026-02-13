package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/config"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	grpcpkg "github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testConfig() *config.Config {
	return &config.Config{
		Security: config.SecurityConfig{
			BCryptCost:    4,
			EncryptionKey: "test-encryption-key-minimum-32-characters-long",
		},
	}
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
		listFilteredFn: func(filter identityRepo.UserListFilter) ([]*domain.User, int64, error) {
			return []*domain.User{user}, 1, nil
		},
	}

	srv := NewUserServiceServer(testConfig(), repo)

	getResp, err := srv.GetUser(context.Background(), &pb.GetUserRequest{Id: user.ID.String()})
	if err != nil || getResp.User.Id == "" {
		t.Fatalf("expected GetUser success, err=%v", err)
	}

	_, err = srv.GetUser(context.Background(), &pb.GetUserRequest{Id: "bad-id"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for bad id, got %v", status.Code(err))
	}

	// UpdateUser requires self or admin context
	selfCtx := grpcpkg.ContextWithAuth(context.Background(), user.ID.String(), []string{"user"})
	updateResp, err := srv.UpdateUser(selfCtx, &pb.UpdateUserRequest{
		Id:       user.ID.String(),
		Username: "updated",
	})
	if err != nil || updateResp.User.Username != "updated" {
		t.Fatalf("expected UpdateUser success, err=%v", err)
	}

	listResp, err := srv.ListUsers(context.Background(), &pb.ListUsersRequest{Page: 1, PageSize: 10})
	if err != nil || len(listResp.Users) != 1 {
		t.Fatalf("expected ListUsers success, err=%v", err)
	}

	// DeleteUser requires admin context
	adminCtx := grpcpkg.ContextWithAuth(context.Background(), uuid.NewString(), []string{"admin"})
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
	srv := NewUserServiceServer(testConfig(), repo)

	_, err := srv.GetUser(context.Background(), &pb.GetUserRequest{Id: uuid.NewString()})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", status.Code(err))
	}

	// UpdateUser with invalid ID should fail before auth check
	_, err = srv.UpdateUser(context.Background(), &pb.UpdateUserRequest{Id: "invalid-id"})
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
