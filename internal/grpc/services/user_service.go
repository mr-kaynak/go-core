package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UserServiceServer implements the gRPC UserService
type UserServiceServer struct {
	pb.UnimplementedUserServiceServer
	userRepo        repository.UserRepository
	eventDispatcher *events.EventDispatcher
	logger          *logger.Logger
}

// NewUserServiceServer creates a new UserServiceServer
func NewUserServiceServer(userRepo repository.UserRepository, dispatcher ...*events.EventDispatcher) *UserServiceServer {
	s := &UserServiceServer{
		userRepo: userRepo,
		logger:   logger.Get().WithFields(logger.Fields{"service": "grpc.user"}),
	}
	if len(dispatcher) > 0 && dispatcher[0] != nil {
		s.eventDispatcher = dispatcher[0]
	}
	return s
}

// GetUser retrieves a user by ID
func (s *UserServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	s.logger.Info("gRPC GetUser request", "user_id", req.Id)

	// Parse user ID
	userID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	// Get user
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		s.logger.Error("Failed to get user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	return &pb.GetUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// ListUsers lists all users with pagination
func (s *UserServiceServer) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	s.logger.Info("gRPC ListUsers request", "page", req.Page, "page_size", req.PageSize)

	// Set defaults
	page := int(req.Page)
	if page < 1 {
		page = 1
	}
	pageSize := int(req.PageSize)
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// Calculate offset
	offset := (page - 1) * pageSize

	// List users
	users, err := s.userRepo.GetAll(offset, pageSize)
	if err != nil {
		s.logger.Error("Failed to list users", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	// Count total
	total, err := s.userRepo.Count()
	if err != nil {
		s.logger.Error("Failed to count users", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	// Convert to proto
	protoUsers := make([]*pb.User, len(users))
	for i, user := range users {
		protoUsers[i] = domainUserToProto(user)
	}

	return &pb.ListUsersResponse{
		Users:    protoUsers,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}

// CreateUser creates a new user
func (s *UserServiceServer) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	s.logger.Info("gRPC CreateUser request", "email", req.Email, "username", req.Username)

	// Create user domain object
	user := &domain.User{
		Email:     req.Email,
		Username:  req.Username,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Phone:     req.Phone,
		Verified:  false,
		Metadata:  convertStringMapToInterface(req.Metadata),
	}

	// Set password hash
	if req.Password != "" {
		user.SetPassword(req.Password)
	}

	// Create user
	err := s.userRepo.Create(user)
	if err != nil {
		s.logger.Error("Failed to create user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	return &pb.CreateUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// UpdateUser updates an existing user
func (s *UserServiceServer) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.UpdateUserResponse, error) {
	s.logger.Info("gRPC UpdateUser request", "user_id", req.Id)

	// Parse user ID
	userID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	// Get existing user
	existingUser, err := s.userRepo.GetByID(userID)
	if err != nil {
		s.logger.Error("Failed to get user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	// Update fields
	if req.Email != "" {
		existingUser.Email = req.Email
	}
	if req.Username != "" {
		existingUser.Username = req.Username
	}
	if req.FirstName != "" {
		existingUser.FirstName = req.FirstName
	}
	if req.LastName != "" {
		existingUser.LastName = req.LastName
	}
	if req.Phone != "" {
		existingUser.Phone = req.Phone
	}
	if req.Metadata != nil {
		existingUser.Metadata = convertStringMapToInterface(req.Metadata)
	}

	// Update user
	err = s.userRepo.Update(existingUser)
	if err != nil {
		s.logger.Error("Failed to update user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	return &pb.UpdateUserResponse{
		User: domainUserToProto(existingUser),
	}, nil
}

// DeleteUser deletes a user
func (s *UserServiceServer) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*emptypb.Empty, error) {
	s.logger.Info("gRPC DeleteUser request", "user_id", req.Id)

	// Parse user ID
	userID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	// Delete user
	err = s.userRepo.Delete(userID)
	if err != nil {
		s.logger.Error("Failed to delete user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	return &emptypb.Empty{}, nil
}

// GetUserByEmail retrieves a user by email
func (s *UserServiceServer) GetUserByEmail(ctx context.Context, req *pb.GetUserByEmailRequest) (*pb.GetUserResponse, error) {
	s.logger.Info("gRPC GetUserByEmail request", "email", req.Email)

	// Get user by email
	user, err := s.userRepo.GetByEmail(req.Email)
	if err != nil {
		s.logger.Error("Failed to get user by email", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	return &pb.GetUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// VerifyUser verifies a user's email
func (s *UserServiceServer) VerifyUser(ctx context.Context, req *pb.VerifyUserRequest) (*emptypb.Empty, error) {
	s.logger.Info("gRPC VerifyUser request", "user_id", req.UserId)

	// Parse user ID
	userID, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	// Get user
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		s.logger.Error("Failed to get user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	// Verify user
	user.Verified = true
	user.IsVerified = true
	err = s.userRepo.Update(user)
	if err != nil {
		s.logger.Error("Failed to verify user", "error", err)
		return nil, toGRPCErrorUser(err)
	}

	return &emptypb.Empty{}, nil
}

// StreamUserEvents streams user events to the gRPC client via the event dispatcher
func (s *UserServiceServer) StreamUserEvents(req *pb.StreamUserEventsRequest, stream pb.UserService_StreamUserEventsServer) error {
	s.logger.Info("gRPC StreamUserEvents request", "event_types", req.EventTypes)

	if s.eventDispatcher == nil {
		return status.Error(codes.Unavailable, "Event dispatcher not configured")
	}

	// Convert requested event types to EventType slice for filtering
	var filterTypes []events.EventType
	for _, et := range req.EventTypes {
		filterTypes = append(filterTypes, events.EventType(et))
	}

	// Subscribe to events
	sub := s.eventDispatcher.Subscribe(filterTypes)
	defer s.eventDispatcher.Unsubscribe(sub.ID)

	s.logger.Info("StreamUserEvents subscriber created", "subscriber_id", sub.ID)

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("StreamUserEvents client disconnected", "subscriber_id", sub.ID)
			return nil
		case event, ok := <-sub.Ch:
			if !ok {
				// Channel closed, subscription ended
				return nil
			}

			// Convert DomainEvent data to map[string]string
			data := make(map[string]string)
			for k, v := range event.Data {
				data[k] = fmt.Sprintf("%v", v)
			}

			userEvent := &pb.UserEvent{
				Id:        event.ID,
				Type:      string(event.Type),
				UserId:    event.UserID,
				Timestamp: timestamppb.New(event.Timestamp),
				Data:      data,
			}

			if err := stream.Send(userEvent); err != nil {
				s.logger.Error("Failed to send event to stream",
					"subscriber_id", sub.ID,
					"event_id", event.ID,
					"error", err,
				)
				return status.Error(codes.Internal, "Failed to send event")
			}
		}
	}
}

// domainUserToProto converts a domain user to a proto user
func domainUserToProto(user *domain.User) *pb.User {
	if user == nil {
		return nil
	}

	// Extract roles
	roles := make([]string, len(user.Roles))
	for i, role := range user.Roles {
		roles[i] = role.Name
	}

	// Convert timestamps
	var lastLoginAt *timestamppb.Timestamp
	if user.LastLoginAt != nil {
		lastLoginAt = timestamppb.New(*user.LastLoginAt)
	}

	return &pb.User{
		Id:          user.ID.String(),
		Email:       user.Email,
		Username:    user.Username,
		FirstName:   user.FirstName,
		LastName:    user.LastName,
		Phone:       user.Phone,
		IsActive:    true, // User domain doesn't have IsActive field
		IsVerified:  user.IsVerified,
		Roles:       roles,
		CreatedAt:   timestamppb.New(user.CreatedAt),
		UpdatedAt:   timestamppb.New(user.UpdatedAt),
		LastLoginAt: lastLoginAt,
		Metadata:    convertMetadataToStringMap(user.Metadata),
	}
}

// toGRPCErrorUser converts internal errors to gRPC status errors for user service
func toGRPCErrorUser(err error) error {
	return toGRPCError(err)
}

// convertStringMapToInterface converts map[string]string to map[string]interface{}
func convertStringMapToInterface(metadata map[string]string) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	result := make(map[string]interface{})
	for k, v := range metadata {
		result[k] = v
	}
	return result
}
