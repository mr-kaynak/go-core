package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"

	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type tokenValidatorStub struct {
	validateFn func(token string) (*service.Claims, error)
}

func (s *tokenValidatorStub) ValidateAccessToken(token string) (*service.Claims, error) {
	if s.validateFn != nil {
		return s.validateFn(token)
	}
	return nil, errors.New("not implemented")
}

type testServerStream struct {
	ctx context.Context
}

func (s *testServerStream) SetHeader(md metadata.MD) error {
	_ = md
	return nil
}
func (s *testServerStream) SendHeader(md metadata.MD) error {
	_ = md
	return nil
}
func (s *testServerStream) SetTrailer(md metadata.MD) {
	_ = md
}
func (s *testServerStream) Context() context.Context {
	return s.ctx
}
func (s *testServerStream) SendMsg(m interface{}) error {
	_ = m
	return nil
}
func (s *testServerStream) RecvMsg(m interface{}) error {
	_ = m
	return nil
}

func TestAuthInterceptor_MissingMetadataReturnsUnauthenticated(t *testing.T) {
	SetTokenValidator(&tokenValidatorStub{})
	interceptor := AuthInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.UserService/GetUser"}

	_, err := interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	})
	if status.Code(err) != grpccodes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestAuthInterceptor_InvalidTokenReturnsUnauthenticated(t *testing.T) {
	SetTokenValidator(&tokenValidatorStub{
		validateFn: func(token string) (*service.Claims, error) {
			return nil, errors.New("invalid")
		},
	})
	interceptor := AuthInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.UserService/GetUser"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad-token"))

	_, err := interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	})
	if status.Code(err) != grpccodes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestAuthInterceptor_ValidTokenWritesClaimsToContext(t *testing.T) {
	expectedUserID := uuid.New()
	SetTokenValidator(&tokenValidatorStub{
		validateFn: func(token string) (*service.Claims, error) {
			return &service.Claims{
				UserID: expectedUserID,
				Roles:  []string{"admin"},
			}, nil
		},
	})
	interceptor := AuthInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.UserService/GetUser"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer valid-token"))

	resp, err := interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		userID, ok := ctx.Value(ctxKeyUserID).(string)
		if !ok {
			t.Fatalf("expected user id in context")
		}
		roles, ok := ctx.Value(ctxKeyRoles).([]string)
		if !ok {
			t.Fatalf("expected roles in context")
		}
		if userID != expectedUserID.String() {
			t.Fatalf("expected user id %s, got %s", expectedUserID, userID)
		}
		if len(roles) != 1 || roles[0] != "admin" {
			t.Fatalf("expected admin role in context")
		}
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp != "ok" {
		t.Fatalf("expected handler response to pass through")
	}
}

func TestAuthInterceptor_PublicMethodSkipsAuth(t *testing.T) {
	SetTokenValidator(&tokenValidatorStub{
		validateFn: func(token string) (*service.Claims, error) {
			t.Fatalf("validator should not be called for public methods")
			return nil, nil
		},
	})
	interceptor := AuthInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.AuthService/Login"}

	resp, err := interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return "public-ok", nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp != "public-ok" {
		t.Fatalf("expected handler response to pass through")
	}
}

func TestStreamAuthInterceptor_RejectsMissingAndInvalidToken(t *testing.T) {
	SetTokenValidator(&tokenValidatorStub{
		validateFn: func(token string) (*service.Claims, error) {
			return nil, errors.New("invalid")
		},
	})
	interceptor := StreamAuthInterceptor()
	info := &grpc.StreamServerInfo{FullMethod: "/gocore.v1.UserService/WatchUsers"}

	err := interceptor(nil, &testServerStream{ctx: context.Background()}, info, func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	})
	if status.Code(err) != grpccodes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for missing metadata, got %v", status.Code(err))
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer invalid-token"))
	err = interceptor(nil, &testServerStream{ctx: ctx}, info, func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	})
	if status.Code(err) != grpccodes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for invalid token, got %v", status.Code(err))
	}
}

func TestStreamAuthInterceptor_ValidTokenCallsHandler(t *testing.T) {
	SetTokenValidator(&tokenValidatorStub{
		validateFn: func(token string) (*service.Claims, error) {
			return &service.Claims{UserID: uuid.New(), Roles: []string{"user"}}, nil
		},
	})
	interceptor := StreamAuthInterceptor()
	info := &grpc.StreamServerInfo{FullMethod: "/gocore.v1.UserService/WatchUsers"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))

	called := false
	err := interceptor(nil, &testServerStream{ctx: ctx}, info, func(srv interface{}, stream grpc.ServerStream) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !called {
		t.Fatalf("expected stream handler to be called")
	}
}

func TestRecoveryInterceptor_ConvertsPanicToInternal(t *testing.T) {
	interceptor := RecoveryInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.UserService/GetUser"}

	_, err := interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("boom")
	})
	if status.Code(err) != grpccodes.Internal {
		t.Fatalf("expected Internal, got %v", status.Code(err))
	}
}

func TestRateLimitInterceptor_RejectsWhenLimitExceeded(t *testing.T) {
	interceptor := RateLimitInterceptor(0, 0) // zero rate = always reject
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.UserService/GetUser"}

	_, err := interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	})
	if status.Code(err) != grpccodes.ResourceExhausted {
		t.Fatalf("expected ResourceExhausted, got %v", status.Code(err))
	}
}

func TestErrorInterceptor_MapsApplicationErrors(t *testing.T) {
	interceptor := ErrorInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/gocore.v1.UserService/GetUser"}

	_, err := interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, coreerrors.NewBadRequest("invalid payload")
	})
	if status.Code(err) != grpccodes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", status.Code(err))
	}

	_, err = interceptor(context.Background(), nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, errors.New("plain error")
	})
	if status.Code(err) != grpccodes.Internal {
		t.Fatalf("expected Internal, got %v", status.Code(err))
	}
}
