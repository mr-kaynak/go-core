package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// htmlTagRe strips HTML/XML tags to prevent stored XSS in user-supplied text fields.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// stripHTMLTags removes all HTML tags from a string.
func stripHTMLTags(s string) string {
	return strings.TrimSpace(htmlTagRe.ReplaceAllString(s, ""))
}

// sanitizeMetadata strips HTML tags from all string values in a metadata map.
func sanitizeMetadata(m domain.Metadata) domain.Metadata {
	if m == nil {
		return nil
	}
	sanitized := make(domain.Metadata, len(m))
	for k, v := range m {
		if str, ok := v.(string); ok {
			sanitized[k] = stripHTMLTags(str)
		} else {
			sanitized[k] = v
		}
	}
	return sanitized
}

// PresignURLCache is an optional interface for caching presigned URLs.
// Defined here to avoid import cycles with the cache package.
type PresignURLCache interface {
	GetPresignedURL(ctx context.Context, key string) (string, error)
	SetPresignedURL(ctx context.Context, key, url string) error
}

// UserService handles user management operations
type UserService struct {
	cfg          *config.Config
	db           *gorm.DB
	userRepo     repository.UserRepository
	authService  *AuthService
	tokenService *TokenService
	storageSvc   storage.StorageService
	presignCache PresignURLCache
	logger       *logger.Logger
}

// NewUserService creates a new user service
func NewUserService(
	cfg *config.Config,
	db *gorm.DB,
	userRepo repository.UserRepository,
	authService *AuthService,
	tokenService *TokenService,
) *UserService {
	return &UserService{
		cfg:          cfg,
		db:           db,
		userRepo:     userRepo,
		authService:  authService,
		tokenService: tokenService,
		logger:       logger.Get().WithFields(logger.Fields{"service": "user"}),
	}
}

// runInTx executes fn inside a database transaction.
func (s *UserService) runInTx(fn func(tx *gorm.DB) error) error {
	if s.db == nil {
		return fn(nil)
	}
	return s.db.Transaction(fn)
}

// SetStorage sets the optional storage service for avatar URL resolution.
func (s *UserService) SetStorage(st storage.StorageService) {
	s.storageSvc = st
}

// SetPresignCache sets the optional presigned URL cache (Redis).
func (s *UserService) SetPresignCache(pc PresignURLCache) {
	s.presignCache = pc
}

// resolveAvatarURL replaces the stored object key with a resolvable URL.
// Cache-then-source pattern: try Redis first, fall back to storage, then populate cache.
func (s *UserService) resolveAvatarURL(ctx context.Context, user *domain.User) {
	if user == nil || user.AvatarURL == "" || s.storageSvc == nil {
		return
	}

	key := user.AvatarURL

	// Try presign cache first
	if s.presignCache != nil {
		if cached, err := s.presignCache.GetPresignedURL(ctx, key); err == nil && cached != "" {
			user.AvatarURL = cached
			return
		}
	}

	// Cache miss — generate from storage backend
	url, err := s.storageSvc.GetURL(ctx, key)
	if err != nil {
		s.logger.Warn("Failed to resolve avatar URL", "key", key, "error", err)
		return
	}

	// Populate cache (best-effort)
	if s.presignCache != nil {
		if cacheErr := s.presignCache.SetPresignedURL(ctx, key, url); cacheErr != nil {
			s.logger.Warn("Failed to cache presigned URL", "key", key, "error", cacheErr)
		}
	}

	user.AvatarURL = url
}

// --- Self-service methods ---

// UpdateProfile updates the authenticated user's profile fields.
func (s *UserService) UpdateProfile(
	ctx context.Context, userID uuid.UUID, firstName, lastName, phone string, metadata domain.Metadata,
) (*domain.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.NewNotFound("User", userID.String())
	}

	user.FirstName = stripHTMLTags(firstName)
	user.LastName = stripHTMLTags(lastName)
	user.Phone = phone
	if metadata != nil {
		user.Metadata = sanitizeMetadata(metadata)
	}

	if err := s.userRepo.Update(user); err != nil {
		return nil, errors.NewInternalError("Failed to update profile")
	}

	user.Password = ""
	s.resolveAvatarURL(ctx, user)
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

	// Blacklist access tokens so they cannot be used until expiry
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(ctx, userID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after account deletion")
	}

	return nil
}

// SessionInfo represents a single active session returned to the client.
type SessionInfo struct {
	ID        uuid.UUID `json:"id"`
	IPAddress string    `json:"ip_address,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
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
			IPAddress: t.IPAddress,
			UserAgent: t.UserAgent,
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
func (s *UserService) AdminGetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return nil, errors.NewNotFound("User", id.String())
	}

	if err := s.userRepo.LoadRoles(user); err != nil {
		s.logger.WithError(err).Warn("Failed to load user roles")
	}

	user.Password = ""
	s.resolveAvatarURL(ctx, user)
	return user, nil
}

// AdminListUsers returns a filtered, paginated list of users.
func (s *UserService) AdminListUsers(ctx context.Context, filter domain.UserListFilter) ([]*domain.User, int64, error) {
	users, total, err := s.userRepo.ListFiltered(filter)
	if err != nil {
		return nil, 0, errors.NewInternalError("Failed to fetch users")
	}

	for _, u := range users {
		u.Password = ""
		s.resolveAvatarURL(ctx, u)
	}
	return users, total, nil
}

// AdminCreateUser creates a new user account (admin operation).
func (s *UserService) AdminCreateUser(
	ctx context.Context, email, username, password, firstName, lastName, phone string, verified bool,
) (*domain.User, error) {
	if err := validation.Var(password, "required,password"); err != nil {
		return nil, err
	}

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

	if err := s.runInTx(func(tx *gorm.DB) error {
		txRepo := s.userRepo.WithTx(tx)

		if err := txRepo.Create(user); err != nil {
			return errors.NewInternalError("Failed to create user")
		}

		// Assign default "user" role
		defaultRole, err := txRepo.GetRoleByName("user")
		if err != nil {
			return nil // no default role configured, not an error
		}
		if err := txRepo.AssignRole(user.ID, defaultRole.ID); err != nil {
			s.logger.WithError(err).Warn("Failed to assign default role")
			return errors.NewInternalError("Failed to assign default role")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	user.Password = ""
	s.resolveAvatarURL(ctx, user)
	return user, nil
}

// AdminUpdateUser updates an existing user's profile fields (admin operation).
func (s *UserService) AdminUpdateUser(
	ctx context.Context,
	id uuid.UUID, email, username, firstName, lastName, phone string,
	metadata domain.Metadata,
) (*domain.User, error) {
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
		user.FirstName = stripHTMLTags(firstName)
	}
	if lastName != "" {
		user.LastName = stripHTMLTags(lastName)
	}
	if phone != "" {
		user.Phone = phone
	}
	if metadata != nil {
		user.Metadata = sanitizeMetadata(metadata)
	}

	if err := s.userRepo.Update(user); err != nil {
		return nil, errors.NewInternalError("Failed to update user")
	}

	user.Password = ""
	s.resolveAvatarURL(ctx, user)
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

	// Blacklist access tokens so they cannot be used until expiry
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(ctx, id.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after admin user deletion")
	}

	return nil
}

// AdminUpdateStatus changes a user's status (active/inactive/locked).
func (s *UserService) AdminUpdateStatus(ctx context.Context, id uuid.UUID, status string) (*domain.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return nil, errors.NewNotFound("User", id.String())
	}

	newStatus := domain.UserStatus(status)
	switch newStatus {
	case domain.UserStatusActive, domain.UserStatusInactive, domain.UserStatusLocked:
		user.Status = newStatus
	default:
		return nil, errors.NewBadRequest("Invalid status. Allowed: active, inactive, locked")
	}

	if err := s.userRepo.Update(user); err != nil {
		return nil, errors.NewInternalError("Failed to update user status")
	}

	// Revoke all sessions when locking or deactivating a user
	if newStatus == domain.UserStatusLocked || newStatus == domain.UserStatusInactive {
		if err := s.tokenService.RevokeAllUserTokens(id); err != nil {
			s.logger.WithError(err).Warn("Failed to revoke refresh tokens after status change")
		}
		rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.tokenService.BlacklistAllUserTokens(rctx, id.String(), s.cfg.JWT.Expiry); err != nil {
			s.logger.WithError(err).Warn("Failed to blacklist access tokens after status change")
		}
	}

	user.Password = ""
	s.resolveAvatarURL(ctx, user)
	return user, nil
}

// AdminAssignRole assigns a role to a user.
// callerRoles contains the role names of the requesting user to enforce privilege checks.
func (s *UserService) AdminAssignRole(userID, roleID uuid.UUID, callerRoles []string) error {
	if _, err := s.userRepo.GetByID(userID); err != nil {
		return errors.NewNotFound("User", userID.String())
	}
	role, err := s.userRepo.GetRoleByID(roleID)
	if err != nil {
		return errors.NewNotFound("Role", roleID.String())
	}
	if role.Name == "system_admin" && !containsRole(callerRoles, "system_admin") {
		return errors.NewForbidden("Only system_admin can assign the system_admin role")
	}
	if err := s.userRepo.AssignRole(userID, roleID); err != nil {
		return err
	}
	s.invalidateUserTokens(userID)
	return nil
}

// AdminRemoveRole removes a role from a user.
func (s *UserService) AdminRemoveRole(userID, roleID uuid.UUID) error {
	if _, err := s.userRepo.GetByID(userID); err != nil {
		return errors.NewNotFound("User", userID.String())
	}
	if err := s.userRepo.RemoveRole(userID, roleID); err != nil {
		return err
	}
	s.invalidateUserTokens(userID)
	return nil
}

// invalidateUserTokens revokes all refresh tokens and blacklists all access
// tokens for the given user, forcing re-authentication with fresh claims.
func (s *UserService) invalidateUserTokens(userID uuid.UUID) {
	if err := s.tokenService.RevokeAllUserTokens(userID); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke tokens after role change")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(ctx, userID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after role change")
	}
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
func (s *UserService) AdminResetPassword(ctx context.Context, id uuid.UUID) error {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return errors.NewNotFound("User", id.String())
	}

	return s.authService.RequestPasswordReset(ctx, user.Email)
}

// AdminDisable2FA force-disables 2FA for a user without requiring a TOTP code.
func (s *UserService) AdminDisable2FA(id uuid.UUID) error {
	return s.authService.ForceDisable2FA(id)
}

// AdminGetByEmail retrieves a user by email with roles loaded.
func (s *UserService) AdminGetByEmail(ctx context.Context, email string) (*domain.User, error) {
	user, err := s.userRepo.GetByEmail(email)
	if err != nil {
		return nil, errors.NewNotFound("User", email)
	}

	if err := s.userRepo.LoadRoles(user); err != nil {
		s.logger.WithError(err).Warn("Failed to load user roles")
	}

	user.Password = ""
	s.resolveAvatarURL(ctx, user)
	return user, nil
}

// AdminVerifyUser marks a user as verified.
func (s *UserService) AdminVerifyUser(ctx context.Context, id uuid.UUID) error {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return errors.NewNotFound("User", id.String())
	}

	user.Verified = true
	if user.Status == domain.UserStatusPending {
		user.Status = domain.UserStatusActive
	}

	if err := s.userRepo.Update(user); err != nil {
		return errors.NewInternalError("Failed to verify user")
	}

	return nil
}

func containsRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}
