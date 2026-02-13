package service

import (
	"strings"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// UserService handles user management operations
type UserService struct {
	cfg          *config.Config
	userRepo     repository.UserRepository
	authService  *AuthService
	tokenService *TokenService
	logger       *logger.Logger
}

// NewUserService creates a new user service
func NewUserService(
	cfg *config.Config,
	userRepo repository.UserRepository,
	authService *AuthService,
	tokenService *TokenService,
) *UserService {
	return &UserService{
		cfg:          cfg,
		userRepo:     userRepo,
		authService:  authService,
		tokenService: tokenService,
		logger:       logger.Get().WithFields(logger.Fields{"service": "user"}),
	}
}

// --- Self-service methods ---

// UpdateProfile updates the authenticated user's profile fields.
func (s *UserService) UpdateProfile(userID uuid.UUID, firstName, lastName, phone string, metadata domain.Metadata) (*domain.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.NewNotFound("User", userID.String())
	}

	user.FirstName = firstName
	user.LastName = lastName
	user.Phone = phone
	if metadata != nil {
		user.Metadata = metadata
	}

	if err := s.userRepo.Update(user); err != nil {
		return nil, errors.NewInternalError("Failed to update profile")
	}

	user.Password = ""
	return user, nil
}

// DeleteAccount soft-deletes the authenticated user's account and revokes all sessions.
func (s *UserService) DeleteAccount(userID uuid.UUID) error {
	if err := s.userRepo.Delete(userID); err != nil {
		return errors.NewInternalError("Failed to delete account")
	}

	if err := s.tokenService.RevokeAllUserTokens(userID); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke tokens after account deletion")
	}

	return nil
}

// SessionInfo represents a single active session returned to the client.
type SessionInfo struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt string    `json:"created_at"`
	ExpiresAt string    `json:"expires_at"`
}

// GetSessions returns the user's active refresh-token sessions.
func (s *UserService) GetSessions(userID uuid.UUID) ([]SessionInfo, error) {
	tokens, err := s.userRepo.GetActiveRefreshTokensByUser(userID)
	if err != nil {
		return nil, errors.NewInternalError("Failed to fetch sessions")
	}

	sessions := make([]SessionInfo, len(tokens))
	for i, t := range tokens {
		sessions[i] = SessionInfo{
			ID:        t.ID,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ExpiresAt: t.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	return sessions, nil
}

// RevokeSession revokes a single session belonging to the user.
func (s *UserService) RevokeSession(userID, sessionID uuid.UUID) error {
	tokens, err := s.userRepo.GetActiveRefreshTokensByUser(userID)
	if err != nil {
		return errors.NewInternalError("Failed to fetch sessions")
	}

	for _, t := range tokens {
		if t.ID == sessionID {
			return s.userRepo.RevokeRefreshTokenByID(sessionID)
		}
	}

	return errors.NewNotFound("Session", sessionID.String())
}

// RevokeAllSessions revokes all refresh tokens for the user.
func (s *UserService) RevokeAllSessions(userID uuid.UUID) error {
	if err := s.userRepo.RevokeAllUserRefreshTokens(userID); err != nil {
		return errors.NewInternalError("Failed to revoke sessions")
	}
	return nil
}

// --- Admin methods ---

// AdminGetUser retrieves a single user with roles loaded.
func (s *UserService) AdminGetUser(id uuid.UUID) (*domain.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return nil, errors.NewNotFound("User", id.String())
	}

	if err := s.userRepo.LoadRoles(user); err != nil {
		s.logger.WithError(err).Warn("Failed to load user roles")
	}

	user.Password = ""
	return user, nil
}

// AdminListUsers returns a filtered, paginated list of users.
func (s *UserService) AdminListUsers(filter repository.UserListFilter) ([]*domain.User, int64, error) {
	users, total, err := s.userRepo.ListFiltered(filter)
	if err != nil {
		return nil, 0, errors.NewInternalError("Failed to fetch users")
	}

	for _, u := range users {
		u.Password = ""
	}
	return users, total, nil
}

// AdminCreateUser creates a new user account (admin operation).
func (s *UserService) AdminCreateUser(email, username, password, firstName, lastName, phone string, verified bool) (*domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.ToLower(strings.TrimSpace(username))

	if exists, err := s.userRepo.ExistsByEmail(email); err != nil {
		return nil, errors.NewInternalError("Failed to check email availability")
	} else if exists {
		return nil, errors.NewConflict("Email already registered")
	}

	if exists, err := s.userRepo.ExistsByUsername(username); err != nil {
		return nil, errors.NewInternalError("Failed to check username availability")
	} else if exists {
		return nil, errors.NewConflict("Username already taken")
	}

	status := domain.UserStatusPending
	if verified {
		status = domain.UserStatusActive
	}

	user := &domain.User{
		Email:      email,
		Username:   username,
		Password:   password,
		FirstName:  firstName,
		LastName:   lastName,
		Phone:      phone,
		Status:     status,
		Verified:   verified,
		BCryptCost: s.cfg.Security.BCryptCost,
	}

	if err := s.userRepo.Create(user); err != nil {
		return nil, errors.NewInternalError("Failed to create user")
	}

	// Assign default "user" role
	defaultRole, err := s.userRepo.GetRoleByName("user")
	if err == nil {
		_ = s.userRepo.AssignRole(user.ID, defaultRole.ID)
	}

	user.Password = ""
	return user, nil
}

// AdminUpdateUser updates an existing user's profile fields (admin operation).
func (s *UserService) AdminUpdateUser(id uuid.UUID, email, username, firstName, lastName, phone string, metadata domain.Metadata) (*domain.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return nil, errors.NewNotFound("User", id.String())
	}

	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.ToLower(strings.TrimSpace(username))

	// Check uniqueness if changed
	if email != "" && email != user.Email {
		if exists, err := s.userRepo.ExistsByEmail(email); err != nil {
			return nil, errors.NewInternalError("Failed to check email availability")
		} else if exists {
			return nil, errors.NewConflict("Email already registered")
		}
		user.Email = email
	}

	if username != "" && username != user.Username {
		if exists, err := s.userRepo.ExistsByUsername(username); err != nil {
			return nil, errors.NewInternalError("Failed to check username availability")
		} else if exists {
			return nil, errors.NewConflict("Username already taken")
		}
		user.Username = username
	}

	if firstName != "" {
		user.FirstName = firstName
	}
	if lastName != "" {
		user.LastName = lastName
	}
	if phone != "" {
		user.Phone = phone
	}
	if metadata != nil {
		user.Metadata = metadata
	}

	if err := s.userRepo.Update(user); err != nil {
		return nil, errors.NewInternalError("Failed to update user")
	}

	user.Password = ""
	return user, nil
}

// AdminDeleteUser soft-deletes a user account (admin operation).
func (s *UserService) AdminDeleteUser(id, adminID uuid.UUID) error {
	if id == adminID {
		return errors.NewBadRequest("Cannot delete your own account")
	}

	if _, err := s.userRepo.GetByID(id); err != nil {
		return errors.NewNotFound("User", id.String())
	}

	if err := s.userRepo.Delete(id); err != nil {
		return errors.NewInternalError("Failed to delete user")
	}

	if err := s.tokenService.RevokeAllUserTokens(id); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke tokens after admin user deletion")
	}

	return nil
}

// AdminUpdateStatus changes a user's status (active/inactive/locked).
func (s *UserService) AdminUpdateStatus(id uuid.UUID, status string) (*domain.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return nil, errors.NewNotFound("User", id.String())
	}

	switch domain.UserStatus(status) {
	case domain.UserStatusActive, domain.UserStatusInactive, domain.UserStatusLocked:
		user.Status = domain.UserStatus(status)
	default:
		return nil, errors.NewBadRequest("Invalid status. Allowed: active, inactive, locked")
	}

	if err := s.userRepo.Update(user); err != nil {
		return nil, errors.NewInternalError("Failed to update user status")
	}

	user.Password = ""
	return user, nil
}

// AdminAssignRole assigns a role to a user.
func (s *UserService) AdminAssignRole(userID, roleID uuid.UUID) error {
	if _, err := s.userRepo.GetByID(userID); err != nil {
		return errors.NewNotFound("User", userID.String())
	}
	if _, err := s.userRepo.GetRoleByID(roleID); err != nil {
		return errors.NewNotFound("Role", roleID.String())
	}
	return s.userRepo.AssignRole(userID, roleID)
}

// AdminRemoveRole removes a role from a user.
func (s *UserService) AdminRemoveRole(userID, roleID uuid.UUID) error {
	if _, err := s.userRepo.GetByID(userID); err != nil {
		return errors.NewNotFound("User", userID.String())
	}
	return s.userRepo.RemoveRole(userID, roleID)
}

// AdminUnlockUser unlocks a locked user account.
func (s *UserService) AdminUnlockUser(id uuid.UUID) error {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return errors.NewNotFound("User", id.String())
	}

	user.ResetFailedLogin()
	if user.Status == domain.UserStatusLocked {
		user.Status = domain.UserStatusActive
	}

	if err := s.userRepo.Update(user); err != nil {
		return errors.NewInternalError("Failed to unlock user")
	}

	return nil
}

// AdminResetPassword sends a password reset email for the given user.
func (s *UserService) AdminResetPassword(id uuid.UUID) error {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return errors.NewNotFound("User", id.String())
	}

	return s.authService.RequestPasswordReset(user.Email)
}

// AdminDisable2FA force-disables 2FA for a user without requiring a TOTP code.
func (s *UserService) AdminDisable2FA(id uuid.UUID) error {
	return s.authService.ForceDisable2FA(id)
}
