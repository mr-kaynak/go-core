package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	grpcpkg "github.com/mr-kaynak/go-core/internal/grpc"
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
	authService  *authService.AuthService
	tokenService *authService.TokenService
	userRepo     repository.UserRepository
	cfg          *config.Config
	logger       *logger.Logger
}

// NewAuthServiceServer creates a new AuthServiceServer
func NewAuthServiceServer(
	authSvc *authService.AuthService, userRepo repository.UserRepository,
	tokenSvc *authService.TokenService, cfg *config.Config,
) *AuthServiceServer {
	return &AuthServiceServer{
		authService:  authSvc,
		tokenService: tokenSvc,
		userRepo:     userRepo,
		cfg:          cfg,
		logger:       logger.Get().WithFields(logger.Fields{"service": "grpc.auth"}),
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
		return nil, grpcpkg.ToGRPCError(err)
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
		return nil, grpcpkg.ToGRPCError(err)
	}

	// Get user roles and permissions
	roles := make([]string, len(loginResponse.User.Roles))
	permissions := []string{}
	for i := range loginResponse.User.Roles {
		roles[i] = loginResponse.User.Roles[i].Name
		for j := range loginResponse.User.Roles[i].Permissions {
			permissions = append(permissions, loginResponse.User.Roles[i].Permissions[j].Name)
		}
	}

	return &pb.LoginResponse{
		UserId:       loginResponse.User.ID.String(),
		AccessToken:  loginResponse.AccessToken,
		RefreshToken: loginResponse.RefreshToken,
		ExpiresIn:    int64(s.cfg.JWT.Expiry.Seconds()),
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
	s.logger.Info("gRPC Logout request", "token", safeTokenPrefix(req.Token, 10))

	// Extract user ID from refresh token
	userID, err := s.tokenService.ValidateRefreshToken(req.Token)
	if err != nil {
		s.logger.Warn("Failed to validate token during logout", "error", err)
		return nil, grpcpkg.ToGRPCError(err)
	}

	// Logout (invalidate token) — gRPC doesn't have access token in this flow
	err = s.authService.Logout(userID, req.Token, "")
	if err != nil {
		s.logger.Error("Failed to logout", "error", err)
		return nil, grpcpkg.ToGRPCError(err)
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
		return nil, grpcpkg.ToGRPCError(err)
	}

	return &pb.RefreshTokenResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    int64(s.cfg.JWT.Expiry.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

// RequestPasswordReset requests a password reset
func (s *AuthServiceServer) RequestPasswordReset(
	ctx context.Context, req *pb.RequestPasswordResetRequest,
) (*pb.RequestPasswordResetResponse, error) {
	s.logger.Info("gRPC RequestPasswordReset request", "email", req.Email)

	// Request password reset
	err := s.authService.RequestPasswordReset(req.Email)
	if err != nil {
		s.logger.Error("Failed to request password reset", "error", err)
		return nil, grpcpkg.ToGRPCError(err)
	}

	return &pb.RequestPasswordResetResponse{
		Message: "Password reset email sent. Please check your inbox.",
	}, nil
}

// ResetPassword resets a user's password
func (s *AuthServiceServer) ResetPassword(ctx context.Context, req *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error) {
	s.logger.Info("gRPC ResetPassword request", "token", safeTokenPrefix(req.Token, 10))

	// Reset password
	err := s.authService.ResetPassword(req.Token, req.NewPassword)
	if err != nil {
		s.logger.Error("Failed to reset password", "error", err)
		return nil, grpcpkg.ToGRPCError(err)
	}

	return &pb.ResetPasswordResponse{
		Message: "Password reset successfully",
	}, nil
}

// VerifyEmail verifies a user's email
func (s *AuthServiceServer) VerifyEmail(ctx context.Context, req *pb.VerifyEmailRequest) (*pb.VerifyEmailResponse, error) {
	s.logger.Info("gRPC VerifyEmail request", "token", safeTokenPrefix(req.Token, 10))

	// Verify email
	err := s.authService.VerifyEmail(req.Token)
	if err != nil {
		s.logger.Error("Failed to verify email", "error", err)
		return nil, grpcpkg.ToGRPCError(err)
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
		return nil, grpcpkg.ToGRPCError(err)
	}

	return &emptypb.Empty{}, nil
}

// ChangePassword changes a user's password
func (s *AuthServiceServer) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error) {
	// Use authenticated user from context, not from request body
	authenticatedID, ok := grpcpkg.UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "User not authenticated")
	}

	s.logger.Info("gRPC ChangePassword request", "user_id", authenticatedID)

	userID, err := uuid.Parse(authenticatedID)
	if err != nil {
		return nil, status.Error(codes.Internal, "Invalid user ID in context")
	}

	// Change password
	err = s.authService.ChangePassword(userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		s.logger.Error("Failed to change password", "error", err)
		return nil, grpcpkg.ToGRPCError(err)
	}

	return &pb.ChangePasswordResponse{
		Message: "Password changed successfully",
	}, nil
}

// ValidateToken validates an access token
func (s *AuthServiceServer) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	s.logger.Info("gRPC ValidateToken request")

	// Validate input
	if req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "Token is required")
	}

	// Use the existing token service to validate the token
	claims, err := s.tokenService.ValidateAccessToken(req.Token)
	if err != nil {
		s.logger.Warn("Token validation failed", "error", err)
		return nil, status.Error(codes.Unauthenticated, "Invalid token")
	}

	// Get user with roles
	user, err := s.userRepo.GetByID(claims.UserID)
	if err != nil {
		s.logger.Error("Failed to fetch user for token validation", "user_id", claims.UserID, "error", err)
		return nil, status.Error(codes.Internal, "Failed to validate token")
	}

	return &pb.ValidateTokenResponse{
		Valid:     true,
		UserId:    user.ID.String(),
		Email:     user.Email,
		Roles:     claims.Roles,
		ExpiresAt: timestamppb.New(claims.ExpiresAt.Time),
		IssuedAt:  timestamppb.New(claims.IssuedAt.Time),
	}, nil
}

// safeTokenPrefix returns a safe prefix of a token string for logging
func safeTokenPrefix(token string, maxLen int) string {
	if len(token) <= maxLen {
		return token + "..."
	}
	return token[:maxLen] + "..."
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
