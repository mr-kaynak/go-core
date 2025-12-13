package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	authService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthServiceServer implements the gRPC AuthService
type AuthServiceServer struct {
	pb.UnimplementedAuthServiceServer
	authService *authService.AuthService
	userRepo    repository.UserRepository
	logger      *logger.Logger
}

// NewAuthServiceServer creates a new AuthServiceServer
func NewAuthServiceServer(authSvc *authService.AuthService, userRepo repository.UserRepository) *AuthServiceServer {
	return &AuthServiceServer{
		authService: authSvc,
		userRepo:    userRepo,
		logger:      logger.Get().WithFields(logger.Fields{"service": "grpc.auth"}),
	}
}

// Register creates a new user account
func (s *AuthServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	s.logger.Info("gRPC Register request", "email", req.Email, "username", req.Username)

	// Create register request
	registerReq := &authService.RegisterRequest{
		Email:     req.Email,
		Username:  req.Username,
		Password:  req.Password,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Phone:     req.Phone,
	}

	// Register user
	registeredUser, err := s.authService.Register(registerReq)
	if err != nil {
		s.logger.Error("Failed to register user", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.RegisterResponse{
		UserId:  registeredUser.ID.String(),
		Message: "User registered successfully. Please check your email to verify your account.",
	}, nil
}

// Login authenticates a user
func (s *AuthServiceServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	s.logger.Info("gRPC Login request", "email", req.Email)

	// Create login request
	loginReq := &authService.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	}

	// Login
	loginResponse, err := s.authService.Login(loginReq)
	if err != nil {
		s.logger.Error("Failed to login", "error", err)
		return nil, toGRPCError(err)
	}

	// Get user roles and permissions
	roles := make([]string, len(loginResponse.User.Roles))
	permissions := []string{}
	for i, role := range loginResponse.User.Roles {
		roles[i] = role.Name
		for _, perm := range role.Permissions {
			permissions = append(permissions, fmt.Sprintf("%s:%s", perm.Resource, perm.Action))
		}
	}

	return &pb.LoginResponse{
		UserId:       loginResponse.User.ID.String(),
		AccessToken:  loginResponse.AccessToken,
		RefreshToken: loginResponse.RefreshToken,
		ExpiresIn:    3600, // 1 hour in seconds
		TokenType:    "Bearer",
		User: &pb.User{
			Id:         loginResponse.User.ID.String(),
			Email:      loginResponse.User.Email,
			Username:   loginResponse.User.Username,
			FirstName:  loginResponse.User.FirstName,
			LastName:   loginResponse.User.LastName,
			Phone:      loginResponse.User.Phone,
			IsActive:   true,
			IsVerified: loginResponse.User.IsVerified,
			Roles:      roles,
			CreatedAt:  timestamppb.New(loginResponse.User.CreatedAt),
			UpdatedAt:  timestamppb.New(loginResponse.User.UpdatedAt),
			Metadata:   convertMetadataToStringMap(loginResponse.User.Metadata),
		},
		Roles:       roles,
		Permissions: permissions,
	}, nil
}

// Logout logs out a user
func (s *AuthServiceServer) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	s.logger.Info("gRPC Logout request", "token", req.Token[:10]+"...")

	// Extract user ID from token (simplified - should validate token first)
	userID := uuid.New() // This should be extracted from the token

	// Logout (invalidate token)
	err := s.authService.Logout(userID, req.Token)
	if err != nil {
		s.logger.Error("Failed to logout", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.LogoutResponse{
		Message: "User logged out successfully",
	}, nil
}

// RefreshToken refreshes authentication tokens
func (s *AuthServiceServer) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.RefreshTokenResponse, error) {
	s.logger.Info("gRPC RefreshToken request")

	// Refresh tokens
	tokenPair, err := s.authService.RefreshToken(req.RefreshToken)
	if err != nil {
		s.logger.Error("Failed to refresh token", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.RefreshTokenResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    3600, // 1 hour in seconds
		TokenType:    "Bearer",
	}, nil
}

// RequestPasswordReset requests a password reset
func (s *AuthServiceServer) RequestPasswordReset(ctx context.Context, req *pb.RequestPasswordResetRequest) (*pb.RequestPasswordResetResponse, error) {
	s.logger.Info("gRPC RequestPasswordReset request", "email", req.Email)

	// Request password reset
	err := s.authService.RequestPasswordReset(req.Email)
	if err != nil {
		s.logger.Error("Failed to request password reset", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.RequestPasswordResetResponse{
		Message: "Password reset email sent. Please check your inbox.",
	}, nil
}

// ResetPassword resets a user's password
func (s *AuthServiceServer) ResetPassword(ctx context.Context, req *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error) {
	s.logger.Info("gRPC ResetPassword request", "token", req.Token[:10]+"...")

	// Reset password
	err := s.authService.ResetPassword(req.Token, req.NewPassword)
	if err != nil {
		s.logger.Error("Failed to reset password", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.ResetPasswordResponse{
		Message: "Password reset successfully",
	}, nil
}

// VerifyEmail verifies a user's email
func (s *AuthServiceServer) VerifyEmail(ctx context.Context, req *pb.VerifyEmailRequest) (*pb.VerifyEmailResponse, error) {
	s.logger.Info("gRPC VerifyEmail request", "token", req.Token[:10]+"...")

	// Verify email
	err := s.authService.VerifyEmail(req.Token)
	if err != nil {
		s.logger.Error("Failed to verify email", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.VerifyEmailResponse{
		Message: "Email verified successfully. You can now login.",
	}, nil
}

// ResendVerificationEmail resends the verification email
func (s *AuthServiceServer) ResendVerificationEmail(ctx context.Context, req *pb.ResendVerificationEmailRequest) (*emptypb.Empty, error) {
	s.logger.Info("gRPC ResendVerificationEmail request", "email", req.Email)

	// Resend verification email
	err := s.authService.ResendVerificationEmail(req.Email)
	if err != nil {
		s.logger.Error("Failed to resend verification email", "error", err)
		return nil, toGRPCError(err)
	}

	return &emptypb.Empty{}, nil
}

// ChangePassword changes a user's password
func (s *AuthServiceServer) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error) {
	s.logger.Info("gRPC ChangePassword request", "user_id", req.UserId)

	// Parse user ID
	userID, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	// Change password
	err = s.authService.ChangePassword(userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		s.logger.Error("Failed to change password", "error", err)
		return nil, toGRPCError(err)
	}

	return &pb.ChangePasswordResponse{
		Message: "Password changed successfully",
	}, nil
}

// ValidateToken validates an access token
func (s *AuthServiceServer) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	s.logger.Info("gRPC ValidateToken request")

	// For now, we'll do a simple check since ValidateToken doesn't exist on AuthService
	// This should be implemented properly
	if req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "Token is required")
	}

	// TODO: Implement proper token validation
	// For now, return a mock response
	return &pb.ValidateTokenResponse{
		Valid:     true,
		UserId:    uuid.New().String(),
		Email:     "user@example.com",
		Roles:     []string{"user"},
		ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
		IssuedAt:  timestamppb.New(time.Now()),
	}, nil
}

// toGRPCError converts internal errors to gRPC status errors
func toGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Convert custom errors to gRPC errors
	switch e := err.(type) {
	case *errors.ProblemDetail:
		switch e.Status {
		case 404:
			return status.Error(codes.NotFound, e.Detail)
		case 400:
			return status.Error(codes.InvalidArgument, e.Detail)
		case 401:
			return status.Error(codes.Unauthenticated, e.Detail)
		case 403:
			return status.Error(codes.PermissionDenied, e.Detail)
		case 409:
			return status.Error(codes.AlreadyExists, e.Detail)
		case 500:
			return status.Error(codes.Internal, e.Detail)
		case 503:
			return status.Error(codes.Unavailable, e.Detail)
		default:
			return status.Error(codes.Unknown, e.Detail)
		}
	case *errors.Error:
		switch e.Type {
		case errors.ErrorTypeNotFound:
			return status.Error(codes.NotFound, e.Message)
		case errors.ErrorTypeBadRequest:
			return status.Error(codes.InvalidArgument, e.Message)
		case errors.ErrorTypeUnauthorized:
			return status.Error(codes.Unauthenticated, e.Message)
		case errors.ErrorTypeForbidden:
			return status.Error(codes.PermissionDenied, e.Message)
		case errors.ErrorTypeConflict:
			return status.Error(codes.AlreadyExists, e.Message)
		case errors.ErrorTypeInternal:
			return status.Error(codes.Internal, e.Message)
		case errors.ErrorTypeServiceUnavailable:
			return status.Error(codes.Unavailable, e.Message)
		default:
			return status.Error(codes.Unknown, e.Message)
		}
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// convertMetadataToStringMap converts map[string]interface{} to map[string]string
func convertMetadataToStringMap(metadata map[string]interface{}) map[string]string {
	if metadata == nil {
		return nil
	}
	result := make(map[string]string)
	for k, v := range metadata {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}
