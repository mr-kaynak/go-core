package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/crypto"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SessionCacheWriter is an optional interface for caching user session data.
type SessionCacheWriter interface {
	SetPermissions(ctx context.Context, userID string, roles, permissions []string) error
}

// AuthService handles authentication operations
type AuthService struct {
	cfg                  *config.Config
	db                   *gorm.DB
	userRepo             repository.UserRepository
	tokenService         *TokenService
	verificationRepo     repository.VerificationTokenRepository
	emailSvc             *email.EmailService
	enhancedEmailService interface {
		SendVerificationEmail(to, username, token string, languageCode string) error
		SendPasswordResetEmail(to, username, token string, languageCode string) error
	}
	sessionCache SessionCacheWriter
	logger       *logger.Logger
}

// NewAuthService creates a new auth service
func NewAuthService(
	cfg *config.Config,
	db *gorm.DB,
	userRepo repository.UserRepository,
	tokenService *TokenService,
	verificationRepo repository.VerificationTokenRepository,
	emailSvc *email.EmailService,
	enhancedEmailSvc interface {
		SendVerificationEmail(to, username, token string, languageCode string) error
		SendPasswordResetEmail(to, username, token string, languageCode string) error
	},
) *AuthService {
	return &AuthService{
		cfg:                  cfg,
		db:                   db,
		userRepo:             userRepo,
		tokenService:         tokenService,
		verificationRepo:     verificationRepo,
		emailSvc:             emailSvc,
		enhancedEmailService: enhancedEmailSvc,
		logger:               logger.Get().WithFields(logger.Fields{"service": "auth"}),
	}
}

// runInTx executes fn inside a database transaction. If db is nil (e.g. in
// tests) it calls fn with nil so that repo.WithTx(nil) returns the original
// repository instance.
func (s *AuthService) runInTx(fn func(tx *gorm.DB) error) error {
	if s.db == nil {
		return fn(nil)
	}
	return s.db.Transaction(fn)
}

// SetSessionCache sets the optional session cache for caching user permissions on login.
func (s *AuthService) SetSessionCache(sc SessionCacheWriter) {
	s.sessionCache = sc
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Email     string `json:"email" validate:"required,email"`
	Username  string `json:"username" validate:"required,username"`
	Password  string `json:"password" validate:"required,password"`
	FirstName string `json:"first_name" validate:"max=50"`
	LastName  string `json:"last_name" validate:"max=50"`
	Phone     string `json:"phone" validate:"omitempty,phone"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	User         *domain.User `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

const (
	maxFailedLoginAttempts = 5
	accountLockDuration    = 15 * time.Minute
	maxVerificationPerHour = 3
	sessionCacheTimeout    = 2 * time.Second
	logoutBlacklistTimeout = 3 * time.Second
	defaultBackupCodeCount = 8
	backupCodeBytes        = 8 // 64-bit entropy per backup code
)

// dummyHash is a pre-computed bcrypt hash used to burn constant CPU time
// when a login attempt targets a non-existent email, preventing timing-based
// user enumeration.
//
//nolint:gosec // intentionally weak dummy — never used for real authentication
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-safe-dummy"), bcrypt.DefaultCost)

// Login authenticates a user and returns tokens
func (s *AuthService) Login(req *LoginRequest) (*LoginResponse, error) { //nolint:gocyclo // login flow requires many validation steps
	// Validate request
	if err := validation.Struct(req); err != nil {
		return nil, err
	}

	// Find user by email — burn bcrypt time even on miss to prevent
	// timing-based email enumeration.
	user, err := s.userRepo.GetByEmail(req.Email)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		s.logger.WithError(err).Warn("Login failed: user not found", "email", req.Email)
		return nil, errors.NewUnauthorized("Invalid credentials")
	}

	// Check if account is locked
	if user.IsLocked() {
		s.logger.Warn("Login failed: account locked", "email", req.Email)
		return nil, errors.NewUnauthorized(
			"Your account has been temporarily locked due to too many failed login attempts. Please try again later.")
	}

	// Check password
	if err := user.ComparePassword(req.Password); err != nil {
		s.logger.Warn("Login failed: invalid password", "email", req.Email)
		user.IncrementFailedLogin()

		// Lock account after too many failed attempts
		if user.FailedLoginAttempts >= maxFailedLoginAttempts {
			user.Lock(accountLockDuration)
			s.logger.Warn("Account locked due to too many failed attempts", "email", req.Email)
		}

		if updateErr := s.userRepo.Update(user); updateErr != nil {
			s.logger.WithError(updateErr).Error("Failed to update failed login count")
		}

		return nil, errors.NewUnauthorized("Invalid credentials")
	}

	// Check if user is active
	if !user.IsActive() {
		s.logger.Warn("Login failed: user not active", "email", req.Email, "status", user.Status)
		if !user.Verified {
			return nil, errors.NewUnauthorized("Please verify your email before logging in")
		}
		if user.Status == domain.UserStatusLocked {
			return nil, errors.NewUnauthorized("Your account has been locked")
		}
		return nil, errors.NewUnauthorized("Your account is not active")
	}

	// Reset failed login counter on successful login
	if user.FailedLoginAttempts > 0 {
		user.ResetFailedLogin()
	}

	// Load user roles
	if err := s.userRepo.LoadRoles(user); err != nil {
		s.logger.WithError(err).Error("Failed to load user roles")
		return nil, errors.NewInternalError("Failed to load user roles")
	}

	// Generate tokens
	tokenPair, err := s.tokenService.GenerateTokenPair(user)
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate tokens")
		return nil, errors.NewInternalError("Failed to generate authentication tokens")
	}

	// Cache user permissions in Redis (optional)
	if s.sessionCache != nil {
		var roleNames []string
		var permNames []string
		for i := range user.Roles {
			roleNames = append(roleNames, user.Roles[i].Name)
			for j := range user.Roles[i].Permissions {
				permNames = append(permNames, user.Roles[i].Permissions[j].Name)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), sessionCacheTimeout)
		defer cancel()
		if err := s.sessionCache.SetPermissions(ctx, user.ID.String(), roleNames, permNames); err != nil {
			s.logger.WithError(err).Warn("Failed to cache session permissions")
		}
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := s.userRepo.Update(user); err != nil {
		// Log but don't fail the login
		s.logger.WithError(err).Error("Failed to update last login")
	}

	s.logger.Info("User logged in successfully", "user_id", user.ID, "email", user.Email)

	// Clear sensitive data
	user.Password = ""

	return &LoginResponse{
		User:         user,
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
	}, nil
}

// Register creates a new user account
func (s *AuthService) Register(req *RegisterRequest) (*domain.User, error) {
	// Validate request
	if err := validation.Struct(req); err != nil {
		return nil, err
	}

	// Normalize email and username
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))

	// Check if email already exists
	if exists, err := s.userRepo.ExistsByEmail(req.Email); err != nil {
		return nil, errors.NewInternalError("Failed to check email availability")
	} else if exists {
		return nil, errors.NewConflict("Email already registered")
	}

	// Check if username already exists
	if exists, err := s.userRepo.ExistsByUsername(req.Username); err != nil {
		return nil, errors.NewInternalError("Failed to check username availability")
	} else if exists {
		return nil, errors.NewConflict("Username already taken")
	}

	// Create user
	user := &domain.User{
		Email:      req.Email,
		Username:   req.Username,
		Password:   req.Password,
		FirstName:  req.FirstName,
		LastName:   req.LastName,
		Phone:      req.Phone,
		Status:     domain.UserStatusPending,
		Verified:   false,
		BCryptCost: s.cfg.Security.BCryptCost,
	}

	var verificationToken *domain.VerificationToken

	// Wrap all write operations in a transaction so that a failure in any
	// step (user create, role assignment, token create) rolls back everything.
	if err := s.runInTx(func(tx *gorm.DB) error {
		txUserRepo := s.userRepo.WithTx(tx)
		txVerificationRepo := s.verificationRepo.WithTx(tx)

		// Save user (password will be hashed in BeforeCreate hook)
		if err := txUserRepo.Create(user); err != nil {
			s.logger.WithError(err).Error("Failed to create user")
			return errors.NewInternalError("Failed to create user account")
		}

		// Assign default role
		if err := s.assignDefaultRole(txUserRepo, user); err != nil {
			s.logger.WithError(err).Error("Failed to assign default role")
			return errors.NewInternalError("Failed to assign default role")
		}

		// Create verification token
		verificationToken = &domain.VerificationToken{
			UserID: user.ID,
			Type:   domain.TokenTypeEmailVerification,
		}
		if err := txVerificationRepo.Create(verificationToken); err != nil {
			s.logger.WithError(err).Error("Failed to create verification token")
			return errors.NewInternalError("Failed to create verification token")
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// Send verification email outside the transaction
	s.sendVerificationEmail(user, verificationToken)

	s.logger.Info("User registered successfully", "user_id", user.ID, "email", user.Email)

	// Clear sensitive data
	user.Password = ""

	return user, nil
}

// RefreshToken refreshes an access token using a refresh token (with token rotation)
func (s *AuthService) RefreshToken(refreshToken string) (*TokenPair, error) {
	// Validate refresh token
	userID, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// Revoke old refresh token (token rotation)
	if err := s.tokenService.RevokeRefreshToken(refreshToken); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke old refresh token during rotation")
	}

	// Get user
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.NewUnauthorized("Invalid refresh token")
	}

	// Check if user is active
	if !user.IsActive() {
		return nil, errors.NewUnauthorized("User account is not active")
	}

	// Load user roles
	if err := s.userRepo.LoadRoles(user); err != nil {
		s.logger.WithError(err).Error("Failed to load user roles")
		return nil, errors.NewInternalError("Failed to load user roles")
	}

	// Generate new token pair
	tokenPair, err := s.tokenService.GenerateTokenPair(user)
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate tokens")
		return nil, errors.NewInternalError("Failed to generate authentication tokens")
	}

	s.logger.Debug("Access token refreshed", "user_id", user.ID)

	return tokenPair, nil
}

// Logout logs out a user by revoking their refresh token and blacklisting the access token.
func (s *AuthService) Logout(userID uuid.UUID, refreshToken string, accessToken string) error {
	// Revoke refresh token
	if err := s.tokenService.RevokeRefreshToken(refreshToken); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke refresh token during logout")
		// Don't return error — continue with blacklist
	}

	// Blacklist the access token so it can't be reused
	if accessToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), logoutBlacklistTimeout)
		defer cancel()
		if err := s.tokenService.BlacklistAccessToken(ctx, accessToken, s.cfg.JWT.Expiry); err != nil {
			s.logger.WithError(err).Warn("Failed to blacklist access token during logout")
		}
	}

	s.logger.Info("User logged out", "user_id", userID)
	return nil
}

// VerifyEmail verifies a user's email address using a verification token
func (s *AuthService) VerifyEmail(token string) error {
	// Find token
	verificationToken, err := s.verificationRepo.FindByToken(token)
	if err != nil {
		return errors.NewBadRequest("Invalid or expired verification token")
	}

	// Check if token is valid
	if !verificationToken.IsValid() {
		if verificationToken.IsExpired() {
			return errors.NewBadRequest("Verification token has expired")
		}
		return errors.NewBadRequest("Verification token has already been used")
	}

	// Check token type
	if verificationToken.Type != domain.TokenTypeEmailVerification {
		return errors.NewBadRequest("Invalid token type")
	}

	// Get user
	user, err := s.userRepo.GetByID(verificationToken.UserID)
	if err != nil {
		return errors.NewNotFound("User", verificationToken.UserID.String())
	}

	// Check if already verified
	if user.Verified {
		return errors.NewConflict("Email already verified")
	}

	// Update user and mark token as used in a single transaction
	user.Verified = true
	if user.Status == domain.UserStatusPending {
		user.Status = domain.UserStatusActive
	}
	verificationToken.MarkAsUsed()

	if err := s.runInTx(func(tx *gorm.DB) error {
		if err := s.userRepo.WithTx(tx).Update(user); err != nil {
			return errors.NewInternalError("Failed to verify email")
		}
		if err := s.verificationRepo.WithTx(tx).Update(verificationToken); err != nil {
			return errors.NewInternalError("Failed to mark verification token as used")
		}
		return nil
	}); err != nil {
		return err
	}

	s.logger.Info("Email verified successfully", "user_id", user.ID)
	return nil
}

// ResendVerificationEmail resends the email verification link.
// Always returns nil to the caller regardless of whether the email exists
// or is already verified, preventing email enumeration.
func (s *AuthService) ResendVerificationEmail(emailAddr string) error {
	// Get user by email
	user, err := s.userRepo.GetByEmail(emailAddr)
	if err != nil {
		s.logger.Debug("Resend verification: email not found", "email", emailAddr)
		return nil
	}

	// Already verified — return success without revealing state
	if user.Verified {
		s.logger.Debug("Resend verification: already verified", "user_id", user.ID)
		return nil
	}

	// Check rate limiting - max 3 requests per hour
	oneHourAgo := time.Now().Add(-time.Hour)
	count, err := s.verificationRepo.CountByUserAndType(user.ID, domain.TokenTypeEmailVerification, oneHourAgo)
	if err != nil {
		s.logger.WithError(err).Error("Failed to check verification token rate limit")
		return errors.NewInternalError("Failed to resend verification email")
	}

	if count >= maxVerificationPerHour {
		return errors.NewTooManyRequests("Too many verification email requests. Please try again later.")
	}

	// Delete old tokens
	if err := s.verificationRepo.DeleteByUserAndType(user.ID, domain.TokenTypeEmailVerification); err != nil {
		s.logger.WithError(err).Warn("Failed to delete old verification tokens")
	}

	// Create new verification token
	verificationToken := &domain.VerificationToken{
		UserID: user.ID,
		Type:   domain.TokenTypeEmailVerification,
	}

	if err := s.verificationRepo.Create(verificationToken); err != nil {
		s.logger.WithError(err).Error("Failed to create verification token")
		return errors.NewInternalError("Failed to resend verification email")
	}

	// Send verification email
	if sendErr := s.sendResendVerificationEmail(user, verificationToken); sendErr != nil {
		return sendErr
	}

	s.logger.Info("Verification email resent", "user_id", user.ID, "email", user.Email)
	return nil
}

// ChangePassword changes a user's password
func (s *AuthService) ChangePassword(userID uuid.UUID, oldPassword, newPassword string) error {
	// Validate new password
	if err := validation.Var(newPassword, "required,password"); err != nil {
		return err
	}

	// Get user
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	// Check old password
	if err := user.ComparePassword(oldPassword); err != nil {
		return errors.NewUnauthorized("Invalid current password")
	}

	// Update password
	user.Password = newPassword
	user.BCryptCost = s.cfg.Security.BCryptCost
	if err := user.HashPassword(); err != nil {
		return errors.NewInternalError("Failed to hash password")
	}

	if err := s.userRepo.Update(user); err != nil {
		return errors.NewInternalError("Failed to update password")
	}

	// Invalidate all existing sessions
	if err := s.tokenService.RevokeAllUserTokens(userID); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke refresh tokens after password change")
	}
	ctx, cancel := context.WithTimeout(context.Background(), logoutBlacklistTimeout)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(ctx, userID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after password change")
	}

	// Send password changed notification email
	s.sendPasswordChangedEmail(user)

	s.logger.Info("Password changed successfully", "user_id", userID)
	return nil
}

// RequestPasswordReset initiates the password reset flow
func (s *AuthService) RequestPasswordReset(email string) error {
	// Get user by email
	user, err := s.userRepo.GetByEmail(email)
	if err != nil {
		// Don't reveal if email exists or not
		s.logger.Debug("Password reset requested for non-existent email", "email", email)
		return nil
	}

	// Check rate limiting - max 3 requests per hour
	oneHourAgo := time.Now().Add(-time.Hour)
	count, err := s.verificationRepo.CountByUserAndType(user.ID, domain.TokenTypePasswordReset, oneHourAgo)
	if err != nil {
		s.logger.WithError(err).Error("Failed to check password reset rate limit")
		return errors.NewInternalError("Failed to process password reset request")
	}

	if count >= maxVerificationPerHour {
		return errors.NewTooManyRequests("Too many password reset requests. Please try again later.")
	}

	// Delete old tokens
	if err := s.verificationRepo.DeleteByUserAndType(user.ID, domain.TokenTypePasswordReset); err != nil {
		s.logger.WithError(err).Warn("Failed to delete old password reset tokens")
	}

	// Create new password reset token
	resetToken := &domain.VerificationToken{
		UserID: user.ID,
		Type:   domain.TokenTypePasswordReset,
	}

	if err := s.verificationRepo.Create(resetToken); err != nil {
		s.logger.WithError(err).Error("Failed to create password reset token")
		return errors.NewInternalError("Failed to process password reset request")
	}

	// Send password reset email
	s.sendPasswordResetEmailNotification(user, resetToken)

	s.logger.Info("Password reset requested", "user_id", user.ID, "email", user.Email)
	return nil
}

// ResetPassword completes the password reset flow with a token
func (s *AuthService) ResetPassword(token, newPassword string) error {
	// Validate new password
	if err := validation.Var(newPassword, "required,password"); err != nil {
		return err
	}

	// Find token
	resetToken, err := s.verificationRepo.FindByToken(token)
	if err != nil {
		return errors.NewBadRequest("Invalid or expired password reset token")
	}

	// Check if token is valid
	if !resetToken.IsValid() {
		if resetToken.IsExpired() {
			return errors.NewBadRequest("Password reset token has expired")
		}
		return errors.NewBadRequest("Password reset token has already been used")
	}

	// Check token type
	if resetToken.Type != domain.TokenTypePasswordReset {
		return errors.NewBadRequest("Invalid token type")
	}

	// Get user
	user, err := s.userRepo.GetByID(resetToken.UserID)
	if err != nil {
		return errors.NewNotFound("User", resetToken.UserID.String())
	}

	// Update password
	user.Password = newPassword
	user.BCryptCost = s.cfg.Security.BCryptCost
	if err := user.HashPassword(); err != nil {
		return errors.NewInternalError("Failed to hash password")
	}

	if err := s.userRepo.Update(user); err != nil {
		return errors.NewInternalError("Failed to update password")
	}

	// Invalidate all existing sessions
	if err := s.tokenService.RevokeAllUserTokens(user.ID); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke refresh tokens after password reset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), logoutBlacklistTimeout)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(ctx, user.ID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after password reset")
	}

	// Mark token as used
	resetToken.MarkAsUsed()
	if err := s.verificationRepo.Update(resetToken); err != nil {
		s.logger.WithError(err).Warn("Failed to mark password reset token as used")
	}

	// Delete all other password reset tokens for this user
	if err := s.verificationRepo.DeleteByUserAndType(user.ID, domain.TokenTypePasswordReset); err != nil {
		s.logger.WithError(err).Warn("Failed to delete old password reset tokens")
	}

	// Send password changed notification email
	s.sendPasswordChangedEmail(user)

	s.logger.Info("Password reset successfully", "user_id", user.ID)
	return nil
}

// ValidatePasswordResetToken validates if a password reset token is valid
func (s *AuthService) ValidatePasswordResetToken(token string) error {
	// Find token
	resetToken, err := s.verificationRepo.FindByToken(token)
	if err != nil {
		return errors.NewBadRequest("Invalid password reset token")
	}

	// Check if token is valid
	if !resetToken.IsValid() {
		if resetToken.IsExpired() {
			return errors.NewBadRequest("Password reset token has expired")
		}
		return errors.NewBadRequest("Password reset token has already been used")
	}

	// Check token type
	if resetToken.Type != domain.TokenTypePasswordReset {
		return errors.NewBadRequest("Invalid token type")
	}

	return nil
}

// sendPasswordResetEmailNotification sends a password reset email using the appropriate email service
func (s *AuthService) sendPasswordResetEmailNotification(user *domain.User, resetToken *domain.VerificationToken) {
	if s.enhancedEmailService != nil {
		if emailErr := s.enhancedEmailService.SendPasswordResetEmail(user.Email, user.Username, resetToken.Token, "en"); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send password reset email")
		}
	} else if s.emailSvc != nil {
		if emailErr := s.emailSvc.SendPasswordResetEmail(user.Email, user.Username, resetToken.Token); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send password reset email")
		}
	}
}

// sendResendVerificationEmail sends a verification email (used in resend flow, returns error)
func (s *AuthService) sendResendVerificationEmail(user *domain.User, token *domain.VerificationToken) error {
	if s.enhancedEmailService != nil {
		if emailErr := s.enhancedEmailService.SendVerificationEmail(user.Email, user.Username, token.Token, "en"); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
			return errors.NewInternalError("Failed to resend verification email")
		}
	} else if s.emailSvc != nil {
		if emailErr := s.emailSvc.SendVerificationEmail(user.Email, user.Username, token.Token); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
			return errors.NewInternalError("Failed to resend verification email")
		}
	}
	return nil
}

// sendVerificationEmail sends a verification email using the appropriate email service
func (s *AuthService) sendVerificationEmail(user *domain.User, token *domain.VerificationToken) {
	if s.enhancedEmailService != nil {
		if emailErr := s.enhancedEmailService.SendVerificationEmail(user.Email, user.Username, token.Token, "en"); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
		} else {
			s.logger.Info("Verification email sent", "user_id", user.ID, "email", user.Email)
		}
	} else if s.emailSvc != nil {
		if emailErr := s.emailSvc.SendVerificationEmail(user.Email, user.Username, token.Token); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
		} else {
			s.logger.Info("Verification email sent", "user_id", user.ID, "email", user.Email)
		}
	}
}

// sendPasswordChangedEmail sends a notification email when password is changed
func (s *AuthService) sendPasswordChangedEmail(user *domain.User) {
	if s.emailSvc == nil {
		return
	}
	if err := s.emailSvc.SendPasswordChangedEmail(user.Email, user.GetFullName()); err != nil {
		s.logger.WithError(err).Warn("Failed to send password changed notification", "user_id", user.ID)
	}
}

// assignDefaultRole assigns the default role to a user using the provided
// (possibly transaction-scoped) repository.
func (s *AuthService) assignDefaultRole(userRepo repository.UserRepository, user *domain.User) error {
	// Get or create default role
	defaultRole, err := userRepo.GetRoleByName("user")
	if err != nil {
		// Create default role if it doesn't exist
		defaultRole = &domain.Role{
			Name:        "user",
			Description: "Default user role",
		}
		if err := userRepo.CreateRole(defaultRole); err != nil {
			return fmt.Errorf("failed to create default role: %w", err)
		}
	}

	// Assign role to user
	return userRepo.AssignRole(user.ID, defaultRole.ID)
}

// Enable2FAResult holds the data returned when initiating 2FA setup.
type Enable2FAResult struct {
	OTPAuthURL  string   `json:"otp_url"`
	BackupCodes []string `json:"backup_codes"`
}

// encryptionKey returns the AES-256 key derived from the configured encryption passphrase.
func (s *AuthService) encryptionKey() []byte {
	return crypto.DeriveKey(s.cfg.Security.EncryptionKey)
}

// Enable2FA generates a TOTP secret for the user and returns the otpauth URL and backup codes.
// The 2FA is not yet active until the user verifies with a valid code via Verify2FA.
func (s *AuthService) Enable2FA(userID uuid.UUID) (*Enable2FAResult, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.NewNotFound("User", userID.String())
	}

	if user.TwoFactorEnabled {
		return nil, errors.NewConflict("Two-factor authentication is already enabled")
	}

	// Generate TOTP key
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.cfg.App.Name,
		AccountName: user.Email,
	})
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate TOTP key")
		return nil, errors.NewInternalError("Failed to generate two-factor secret")
	}

	// Generate backup codes (plaintext — will be shown to user once, then hashed for storage)
	backupCodes, err := generateBackupCodes(defaultBackupCodeCount)
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate backup codes")
		return nil, errors.NewInternalError("Failed to generate backup codes")
	}

	// Encrypt TOTP secret with AES-256-GCM before storage
	encryptedSecret, err := crypto.Encrypt(key.Secret(), s.encryptionKey())
	if err != nil {
		s.logger.WithError(err).Error("Failed to encrypt two-factor secret")
		return nil, errors.NewInternalError("Failed to save two-factor secret")
	}

	// Hash backup codes with SHA-256 before storage
	hashedCodes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		hashedCodes[i] = crypto.HashSHA256Hex(code)
	}

	user.TwoFactorSecret = encryptedSecret
	user.TwoFactorBackupCodes = strings.Join(hashedCodes, ",")

	if err := s.userRepo.Update(user); err != nil {
		s.logger.WithError(err).Error("Failed to save two-factor secret")
		return nil, errors.NewInternalError("Failed to save two-factor secret")
	}

	s.logger.Info("2FA setup initiated", "user_id", userID)

	return &Enable2FAResult{
		OTPAuthURL:  key.URL(),
		BackupCodes: backupCodes,
	}, nil
}

// decryptTOTPSecret decrypts the stored TOTP secret.
func (s *AuthService) decryptTOTPSecret(encrypted string) (string, error) {
	return crypto.Decrypt(encrypted, s.encryptionKey())
}

// Verify2FA verifies a TOTP code and enables 2FA for the user.
// This should be called after Enable2FA to confirm the user has set up their authenticator app correctly.
func (s *AuthService) Verify2FA(userID uuid.UUID, code string) error {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	if user.TwoFactorEnabled {
		return errors.NewConflict("Two-factor authentication is already enabled")
	}

	if user.TwoFactorSecret == "" {
		return errors.NewBadRequest("Two-factor authentication has not been initiated. Please call enable first.")
	}

	// Decrypt the stored TOTP secret
	secret, err := s.decryptTOTPSecret(user.TwoFactorSecret)
	if err != nil {
		s.logger.WithError(err).Error("Failed to decrypt two-factor secret")
		return errors.NewInternalError("Failed to verify two-factor code")
	}

	// Validate the TOTP code
	if !totp.Validate(code, secret) {
		return errors.NewBadRequest("Invalid two-factor code")
	}

	// Enable 2FA
	user.TwoFactorEnabled = true
	if err := s.userRepo.Update(user); err != nil {
		s.logger.WithError(err).Error("Failed to enable two-factor authentication")
		return errors.NewInternalError("Failed to enable two-factor authentication")
	}

	s.logger.Info("2FA enabled", "user_id", userID)
	return nil
}

// Disable2FA disables 2FA for the user after verifying a valid TOTP code.
func (s *AuthService) Disable2FA(userID uuid.UUID, code string) error {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	if !user.TwoFactorEnabled {
		return errors.NewBadRequest("Two-factor authentication is not enabled")
	}

	// Decrypt the stored TOTP secret
	secret, err := s.decryptTOTPSecret(user.TwoFactorSecret)
	if err != nil {
		s.logger.WithError(err).Error("Failed to decrypt two-factor secret")
		return errors.NewInternalError("Failed to verify two-factor code")
	}

	// Validate the TOTP code
	if !totp.Validate(code, secret) {
		return errors.NewBadRequest("Invalid two-factor code")
	}

	// Disable 2FA and clear secrets
	user.TwoFactorEnabled = false
	user.TwoFactorSecret = ""
	user.TwoFactorBackupCodes = ""

	if err := s.userRepo.Update(user); err != nil {
		s.logger.WithError(err).Error("Failed to disable two-factor authentication")
		return errors.NewInternalError("Failed to disable two-factor authentication")
	}

	s.logger.Info("2FA disabled", "user_id", userID)
	return nil
}

// Validate2FACode validates a TOTP code during login.
// It checks both the TOTP code and backup codes.
func (s *AuthService) Validate2FACode(userID uuid.UUID, code string) error {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	if !user.TwoFactorEnabled {
		return errors.NewBadRequest("Two-factor authentication is not enabled")
	}

	// Decrypt the stored TOTP secret
	secret, err := s.decryptTOTPSecret(user.TwoFactorSecret)
	if err != nil {
		s.logger.WithError(err).Error("Failed to decrypt two-factor secret")
		return errors.NewInternalError("Failed to verify two-factor code")
	}

	// Try TOTP validation first
	if totp.Validate(code, secret) {
		return nil
	}

	// Try backup codes (stored as SHA-256 hashes)
	if user.TwoFactorBackupCodes != "" {
		codeHash := crypto.HashSHA256Hex(code)
		backupCodes := strings.Split(user.TwoFactorBackupCodes, ",")
		for i, bc := range backupCodes {
			if secureHashEqual(bc, codeHash) {
				// Remove used backup code
				backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
				user.TwoFactorBackupCodes = strings.Join(backupCodes, ",")
				if err := s.userRepo.Update(user); err != nil {
					s.logger.WithError(err).Error("Failed to update backup codes after use")
				}
				s.logger.Info("2FA validated with backup code", "user_id", userID)
				return nil
			}
		}
	}

	return errors.NewUnauthorized("Invalid two-factor code")
}

func secureHashEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// generateBackupCodes generates a set of random backup codes.
func generateBackupCodes(count int) ([]string, error) {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, backupCodeBytes)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = hex.EncodeToString(b)
	}
	return codes, nil
}
