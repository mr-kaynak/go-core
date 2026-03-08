package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	grpcpkg "github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UserServiceServer implements the gRPC UserService
type UserServiceServer struct {
	pb.UnimplementedUserServiceServer
	userService     *service.UserService
	eventDispatcher *events.EventDispatcher
	logger          *logger.Logger
}

// NewUserServiceServer creates a new UserServiceServer
func NewUserServiceServer(
	userSvc *service.UserService,
	dispatcher ...*events.EventDispatcher,
) *UserServiceServer {
	s := &UserServiceServer{
		userService: userSvc,
		logger:      logger.Get().WithFields(logger.Fields{"service": "grpc.user"}),
	}
	if len(dispatcher) > 0 && dispatcher[0] != nil {
		s.eventDispatcher = dispatcher[0]
	}
	return s
}

// RegisterGRPC implements ServiceRegistrar to register this service with a gRPC server.
func (s *UserServiceServer) RegisterGRPC(server *grpc.Server) {
	pb.RegisterUserServiceServer(server, s)
}

// GetUser retrieves a user by ID
func (s *UserServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "User ID is required")
	}

	if err := s.requireSelfOrAdmin(ctx, req.Id); err != nil {
		return nil, err
	}

	userID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	user, err := s.userService.AdminGetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &pb.GetUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// ListUsers lists all users with pagination and filtering
func (s *UserServiceServer) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	// Authorization: admin only
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}

	s.logger.Debug("gRPC ListUsers request", "page", req.Page, "page_size", req.PageSize)

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

	offset := (page - 1) * pageSize

	filter := domain.UserListFilter{
		Offset:     offset,
		Limit:      pageSize,
		SortBy:     req.SortBy,
		Order:      req.Order,
		Search:     req.Search,
		Roles:      req.Roles,
		OnlyActive: req.OnlyActive,
	}

	users, total, err := s.userService.AdminListUsers(ctx, filter)
	if err != nil {
		return nil, err
	}

	protoUsers := make([]*pb.User, len(users))
	for i, user := range users {
		protoUsers[i] = domainUserToProto(user)
	}

	return &pb.ListUsersResponse{
		Users:    protoUsers,
		Total:    int32(total),    //nolint:gosec // safe range for pagination
		Page:     int32(page),     //nolint:gosec // safe range for pagination
		PageSize: int32(pageSize), //nolint:gosec // safe range for pagination
	}, nil
}

// CreateUser creates a new user
func (s *UserServiceServer) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	// Authorization: admin only
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}

	if req.Email == "" || req.Username == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "Email, username, and password are required")
	}

	user, err := s.userService.AdminCreateUser(
		ctx, req.Email, req.Username, req.Password,
		req.FirstName, req.LastName, req.Phone, false,
	)
	if err != nil {
		return nil, err
	}

	return &pb.CreateUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// UpdateUser updates an existing user
func (s *UserServiceServer) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.UpdateUserResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "User ID is required")
	}

	userID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	// Authorization: only self-update or admin
	if err := s.requireSelfOrAdmin(ctx, req.Id); err != nil {
		return nil, err
	}

	user, err := s.userService.AdminUpdateUser(
		ctx, userID,
		req.Email, req.Username, req.FirstName, req.LastName, req.Phone,
		domain.Metadata(convertStringMapToInterface(req.Metadata)),
	)
	if err != nil {
		return nil, err
	}

	return &pb.UpdateUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// DeleteUser deletes a user
func (s *UserServiceServer) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*emptypb.Empty, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "User ID is required")
	}

	// Authorization: admin only
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}

	userID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	adminID, _ := grpcpkg.UserIDFromContext(ctx)
	adminUUID, _ := uuid.Parse(adminID)

	err = s.userService.AdminDeleteUser(userID, adminUUID)
	if err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// GetUserByEmail retrieves a user by email
func (s *UserServiceServer) GetUserByEmail(ctx context.Context, req *pb.GetUserByEmailRequest) (*pb.GetUserResponse, error) {
	if req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "Email is required")
	}

	// Authorization: admin only
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}

	user, err := s.userService.AdminGetByEmail(ctx, req.Email)
	if err != nil {
		return nil, err
	}

	return &pb.GetUserResponse{
		User: domainUserToProto(user),
	}, nil
}

// VerifyUser verifies a user's email
func (s *UserServiceServer) VerifyUser(ctx context.Context, req *pb.VerifyUserRequest) (*emptypb.Empty, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "User ID is required")
	}

	// Authorization: admin only
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}

	userID, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid user ID")
	}

	if err := s.userService.AdminVerifyUser(ctx, userID); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// StreamUserEvents streams user events to the gRPC client via the event dispatcher
func (s *UserServiceServer) StreamUserEvents(req *pb.StreamUserEventsRequest, stream pb.UserService_StreamUserEventsServer) error {
	s.logger.Debug("gRPC StreamUserEvents request", "event_types", req.EventTypes)

	if s.eventDispatcher == nil {
		return status.Error(codes.Unavailable, "Event dispatcher not configured")
	}

	filterTypes := make([]events.EventType, 0, len(req.EventTypes))
	for _, et := range req.EventTypes {
		filterTypes = append(filterTypes, events.EventType(et))
	}

	sub := s.eventDispatcher.Subscribe(filterTypes)
	defer s.eventDispatcher.Unsubscribe(sub.ID)

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-sub.Ch:
			if !ok {
				return nil
			}

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

	roles := make([]string, len(user.Roles))
	for i := range user.Roles {
		roles[i] = user.Roles[i].Name
	}

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
		IsActive:    user.IsActive(),
		IsVerified:  user.IsVerified,
		Roles:       roles,
		CreatedAt:   timestamppb.New(user.CreatedAt),
		UpdatedAt:   timestamppb.New(user.UpdatedAt),
		LastLoginAt: lastLoginAt,
		Metadata:    convertMetadataToStringMap(user.Metadata),
	}
}

// requireAdmin checks that the caller has admin or system_admin role.
func (s *UserServiceServer) requireAdmin(ctx context.Context) error {
	roles, ok := grpcpkg.RolesFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "User not authenticated")
	}
	for _, role := range roles {
		if role == "admin" || role == "system_admin" {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "Admin access required")
}

// requireSelfOrAdmin checks that the caller is either the target user or an admin.
func (s *UserServiceServer) requireSelfOrAdmin(ctx context.Context, targetUserID string) error {
	authenticatedID, ok := grpcpkg.UserIDFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "User not authenticated")
	}
	if authenticatedID == targetUserID {
		return nil
	}
	return s.requireAdmin(ctx)
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
