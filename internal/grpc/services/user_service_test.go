package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
		getAllFn: func(offset, limit int) ([]*domain.User, error) {
			return []*domain.User{user}, nil
		},
		countFn: func() (int64, error) { return 1, nil },
	}

	srv := NewUserServiceServer(repo)

	getResp, err := srv.GetUser(context.Background(), &pb.GetUserRequest{Id: user.ID.String()})
	if err != nil || getResp.User.Id == "" {
		t.Fatalf("expected GetUser success, err=%v", err)
	}

	_, err = srv.GetUser(context.Background(), &pb.GetUserRequest{Id: "bad-id"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for bad id, got %v", status.Code(err))
	}

	updateResp, err := srv.UpdateUser(context.Background(), &pb.UpdateUserRequest{
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

	if _, err := srv.DeleteUser(context.Background(), &pb.DeleteUserRequest{Id: user.ID.String()}); err != nil {
		t.Fatalf("expected DeleteUser success, got %v", err)
	}
}

func TestGRPCUserServiceNotFoundAndValidation(t *testing.T) {
	repo := &grpcAuthUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			return nil, coreerrors.NewNotFound("user", id.String())
		},
	}
	srv := NewUserServiceServer(repo)

	_, err := srv.GetUser(context.Background(), &pb.GetUserRequest{Id: uuid.NewString()})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", status.Code(err))
	}

	_, err = srv.UpdateUser(context.Background(), &pb.UpdateUserRequest{Id: "invalid-id"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", status.Code(err))
	}
}
