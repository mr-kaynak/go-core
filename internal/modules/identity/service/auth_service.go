package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SessionCacheWriter is an optional interface for caching user session data.
type SessionCacheWriter interface {
	SetPermissions(ctx context.Context, userID string, roles, permissions []string) error
}

// EnhancedEmailSender defines the contract for template-based email delivery
// (verification and password reset emails with i18n support).
type EnhancedEmailSender interface {
	SendVerificationEmail(ctx context.Context, to, username, token string, languageCode string) error
	SendPasswordResetEmail(ctx context.Context, to, username, token string, languageCode string) error
	SendPasswordChangedEmail(ctx context.Context, to, fullName string, languageCode string) error
}

// EventPublisher defines the contract for dispatching domain events via RabbitMQ.
type EventPublisher interface {
	DispatchUserRegistered(ctx context.Context, userID uuid.UUID, email, username, languageCode string) error
	DispatchEmailVerification(ctx context.Context, userID uuid.UUID, email, username, token, languageCode string) error
	DispatchEmailPasswordReset(ctx context.Context, userID uuid.UUID, email, username, token, languageCode string) error
	DispatchEmailPasswordChanged(ctx context.Context, userID uuid.UUID, email, fullName, languageCode string) error
}

// UserLanguageResolver resolves a user's preferred language from their notification preferences.
type UserLanguageResolver interface {
	GetLanguageByUserID(ctx context.Context, userID uuid.UUID) (string, error)
}

// NotificationPreferenceCreator creates initial notification preferences for new users.
type NotificationPreferenceCreator interface {
	CreateInitialPreferences(ctx context.Context, userID uuid.UUID, language string) error
}

// AuthService handles authentication operations.
//
// It is an embedding facade: the shared dependencies live on *authCore and the
// specialised flows live on sub-services (*emailDispatcher, *TwoFactorService,
// *passwordResetService, *emailVerificationService) which AuthService embeds.
// Go method promotion keeps the public surface (Enable2FA, RequestPasswordReset,
// VerifyEmail, …) resolving through AuthService unchanged. All sub-services share
// the single *authCore instance created in NewAuthService.
type AuthService struct {
	*authCore
	*emailDispatcher
	*TwoFactorService
	*passwordResetService
	*emailVerificationService

	userRepo         repository.UserRepository
	tokenService     *TokenService
	verificationRepo repository.VerificationTokenRepository
	sessionCache     SessionCacheWriter
	prefCreator      NotificationPreferenceCreator
	dummyHash        []byte // pre-computed bcrypt hash at configured cost for timing-safe login
}

// NewAuthService creates a new auth service
func NewAuthService(
	cfg *config.Config,
	db *gorm.DB,
	userRepo repository.UserRepository,
	tokenService *TokenService,
	verificationRepo repository.VerificationTokenRepository,
	emailSvc *email.EmailService,
	enhancedEmailSvc EnhancedEmailSender,
) *AuthService {
	// Pre-compute a dummy bcrypt hash at the same cost as real passwords so
	// that login attempts for non-existent users take identical time.
	dh, err := bcrypt.GenerateFromPassword([]byte("timing-safe-dummy"), cfg.Security.BCryptCost)
	if err != nil {
		dh = dummyHashDefault
	}

	// Single shared core so setters (SetMetrics, SetLanguageResolver) propagate
	// to every sub-service, and runInTx uses the same db handle everywhere.
	core := &authCore{
		cfg:    cfg,
		db:     db,
		logger: logger.Get().WithFields(logger.Fields{"service": "auth"}),
	}
	dispatcher := &emailDispatcher{
		authCore:             core,
		enhancedEmailService: enhancedEmailSvc,
		emailSvc:             emailSvc,
	}
	twoFactor := &TwoFactorService{
		authCore:     core,
		userRepo:     userRepo,
		tokenService: tokenService,
	}
	passwordReset := &passwordResetService{
		authCore:         core,
		emailDispatcher:  dispatcher,
		userRepo:         userRepo,
		verificationRepo: verificationRepo,
		tokenService:     tokenService,
	}
	emailVerification := &emailVerificationService{
		authCore:         core,
		emailDispatcher:  dispatcher,
		userRepo:         userRepo,
		verificationRepo: verificationRepo,
	}

	return &AuthService{
		authCore:                 core,
		emailDispatcher:          dispatcher,
		TwoFactorService:         twoFactor,
		passwordResetService:     passwordReset,
		emailVerificationService: emailVerification,
		userRepo:                 userRepo,
		tokenService:             tokenService,
		verificationRepo:         verificationRepo,
		dummyHash:                dh,
	}
}

// SetSessionCache sets the optional session cache for caching user permissions on login.
func (s *AuthService) SetSessionCache(sc SessionCacheWriter) {
	s.sessionCache = sc
	s.TwoFactorService.sessionCache = sc
}

// SetEventPublisher sets the optional event publisher for dispatching emails via RabbitMQ.
func (s *AuthService) SetEventPublisher(ep EventPublisher) {
	s.emailDispatcher.SetEventPublisher(ep)
}

// SetNotificationPreferenceCreator sets the optional creator for initial notification preferences.
func (s *AuthService) SetNotificationPreferenceCreator(pc NotificationPreferenceCreator) {
	s.prefCreator = pc
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email     string `json:"email" validate:"required,email"`
	Password  string `json:"password" validate:"required,min=8"`
	IPAddress string `json:"-"`
	UserAgent string `json:"-"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Email        string `json:"email" validate:"required,email"`
	Username     string `json:"username" validate:"required,username"`
	Password     string `json:"password" validate:"required,password"`
	FirstName    string `json:"first_name" validate:"max=50"`
	LastName     string `json:"last_name" validate:"max=50"`
	Phone        string `json:"phone" validate:"omitempty,phone"`
	CaptchaToken string `json:"captcha_token" validate:"omitempty,max=2048"`
	Language     string `json:"-"` // Set from Accept-Language header
}

// LoginResponse represents a login response
type LoginResponse struct {
	User              *domain.User `json:"user"`
	AccessToken       string       `json:"access_token,omitempty"`
	RefreshToken      string       `json:"refresh_token,omitempty"`
	ExpiresAt         time.Time    `json:"expires_at,omitempty"`
	RequiresTwoFactor bool         `json:"requires_two_factor,omitempty"`
	TwoFactorToken    string       `json:"two_factor_token,omitempty"`
}

const (
	maxVerificationPerHour = 3
	sessionCacheTimeout    = 2 * time.Second
	logoutBlacklistTimeout = 3 * time.Second
	defaultBackupCodeCount = 8
	backupCodeBytes        = 8 // 64-bit entropy per backup code
)

// dummyHashDefault is a fallback bcrypt hash at DefaultCost, used only if
// the struct-level dummyHash was not initialized (should not happen in practice).
var dummyHashDefault, _ = bcrypt.GenerateFromPassword([]byte("timing-safe-dummy"), bcrypt.DefaultCost)

// Login authenticates a user and returns tokens
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	// Validate request
	if err := validation.Struct(req); err != nil {
		return nil, err
	}

	// Find user by email — burn bcrypt time even on miss to prevent
	// timing-based email enumeration.
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(req.Password))
		s.logger.WithError(err).Warn("Login failed: user not found", "email", req.Email)
		s.getMetrics().RecordLoginAttempt(false, "credentials")
		return nil, errors.NewUnauthorized("Invalid credentials")
	}

	// Check if account is locked
	if user.IsLocked() {
		s.logger.Warn("Login failed: account locked", "email", req.Email)
		s.getMetrics().RecordLoginAttempt(false, "credentials")
		return nil, errors.NewUnauthorized(
			"Your account has been temporarily locked due to too many failed login attempts. Please try again later.")
	}

	// Check password
	if err := user.ComparePassword(req.Password); err != nil {
		s.logger.Warn("Login failed: invalid password", "email", req.Email)
		user.IncrementFailedLogin()

		// Lock account after too many failed attempts
		if user.FailedLoginAttempts >= s.cfg.Security.MaxLoginAttempts {
			user.Lock(s.cfg.Security.AccountLockDuration)
			s.logger.Warn("Account locked due to too many failed attempts", "email", req.Email)
		}

		if updateErr := s.userRepo.Update(ctx, user); updateErr != nil {
			s.logger.WithError(updateErr).Error("Failed to update failed login count")
		}

		s.getMetrics().RecordLoginAttempt(false, "credentials")
		return nil, errors.NewUnauthorized("Invalid credentials")
	}

	// Check if user is active
	if !user.IsActive() {
		s.logger.Warn("Login failed: user not active", "email", req.Email, "status", user.Status)
		s.getMetrics().RecordLoginAttempt(false, "credentials")
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

	// Check if 2FA is enabled — return a challenge token instead of full tokens
	if user.TwoFactorEnabled {
		// Persist the reset failed-login counter before issuing the 2FA challenge.
		// Deliberate tradeoff: a failure here is bookkeeping, not authentication —
		// failing the whole login for a stale attempt counter would be worse UX and
		// would let a transient DB blip lock users out of the 2FA flow. We keep the
		// login proceeding but log at Error with explicit fields so persistent write
		// failures (which would let FailedLoginAttempts drift and eventually cause
		// spurious lockouts) surface loudly in monitoring rather than silently.
		if err := s.userRepo.Update(ctx, user); err != nil {
			s.logger.WithError(err).Error(
				"Failed to persist failed-login reset before 2FA challenge; login proceeds",
				"user_id", user.ID,
			)
		}

		twoFactorToken, err := s.tokenService.GenerateTwoFactorToken(user.ID)
		if err != nil {
			s.logger.WithError(err).Error("Failed to generate 2FA token")
			return nil, errors.NewInternalError("Failed to generate authentication tokens")
		}

		s.getMetrics().RecordLoginAttempt(true, "credentials_2fa_pending")
		s.logger.Info("Login requires 2FA verification", "user_id", user.ID, "email", user.Email)

		user.Password = ""
		return &LoginResponse{
			User:              user,
			RequiresTwoFactor: true,
			TwoFactorToken:    twoFactorToken,
		}, nil
	}

	// Load user roles
	if err := s.userRepo.LoadRoles(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to load user roles")
		return nil, errors.NewInternalError("Failed to load user roles")
	}

	// Generate token pair and update last-login atomically: a crash between
	// the refresh-token insert and the user update would leave the user with
	// a valid token but a stale FailedLoginAttempts / LastLogin state.
	var tokenPair *TokenPair
	now := time.Now()
	user.LastLogin = &now
	if err := s.runInTx(ctx, func(tx *gorm.DB) error {
		var txErr error
		tokenPair, txErr = s.tokenService.GenerateTokenPairWithTx(ctx, tx, user, SessionMeta{
			IPAddress: req.IPAddress,
			UserAgent: req.UserAgent,
		})
		if txErr != nil {
			return txErr
		}
		return s.userRepo.WithTx(tx).Update(ctx, user)
	}); err != nil {
		s.logger.WithError(err).Error("Failed to generate tokens or update last login")
		return nil, errors.NewInternalError("Failed to generate authentication tokens")
	}

	// Clear user-level blacklist so newly issued tokens are accepted.
	// This is safe here because the user proved knowledge of the current
	// password — unlike RefreshToken which only proves possession of a
	// bearer token and must NOT clear the blacklist.
	// Redis operations stay outside the transaction (best-effort).
	{
		bctx, cancel := context.WithTimeout(ctx, sessionCacheTimeout)
		defer cancel()
		if err := s.tokenService.ClearUserBlacklist(bctx, user.ID.String()); err != nil {
			s.logger.WithError(err).Warn("Failed to clear user blacklist after login")
		}
	}

	// Cache user permissions in Redis (optional)
	if s.sessionCache != nil {
		bctx, cancel := context.WithTimeout(ctx, sessionCacheTimeout)
		defer cancel()
		if err := s.sessionCache.SetPermissions(bctx, user.ID.String(), user.GetRoleNames(), user.GetPermissionNames()); err != nil {
			s.logger.WithError(err).Warn("Failed to cache session permissions")
		}
	}

	s.getMetrics().RecordLoginAttempt(true, "credentials")
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
func (s *AuthService) Register(ctx context.Context, req *RegisterRequest) (*domain.User, error) {
	// Validate request
	if err := validation.Struct(req); err != nil {
		return nil, err
	}

	// Normalize email and username
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))

	// Check if email already exists
	if exists, err := s.userRepo.ExistsByEmail(ctx, req.Email); err != nil {
		return nil, errors.NewInternalError("Failed to check email availability")
	} else if exists {
		return nil, errors.NewConflict("Email already registered")
	}

	// Check if username already exists
	if exists, err := s.userRepo.ExistsByUsername(ctx, req.Username); err != nil {
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
	if err := s.runInTx(ctx, func(tx *gorm.DB) error {
		txUserRepo := s.userRepo.WithTx(tx)
		txVerificationRepo := s.verificationRepo.WithTx(tx)

		// Save user (password will be hashed in BeforeCreate hook).
		// Detect unique-constraint violations that can race past the pre-checks
		// (TOCTOU): two concurrent requests with the same email/username both
		// pass ExistsByEmail/ExistsByUsername, but only one INSERT wins.
		if err := txUserRepo.Create(ctx, user); err != nil {
			var pgErr *pgconn.PgError
			if stderrors.As(err, &pgErr) && pgErr.Code == "23505" {
				if strings.Contains(pgErr.ConstraintName, "username") ||
					strings.Contains(pgErr.TableName, "username") ||
					strings.Contains(pgErr.Detail, "username") {
					return errors.NewConflict("Username already taken")
				}
				return errors.NewConflict("Email already registered")
			}
			s.logger.WithError(err).Error("Failed to create user")
			return errors.NewInternalError("Failed to create user account")
		}

		// Assign default role
		if err := s.assignDefaultRole(ctx, txUserRepo, user); err != nil {
			s.logger.WithError(err).Error("Failed to assign default role")
			return errors.NewInternalError("Failed to assign default role")
		}

		// Create verification token
		verificationToken = &domain.VerificationToken{
			UserID: user.ID,
			Type:   domain.TokenTypeEmailVerification,
		}
		if err := txVerificationRepo.Create(ctx, verificationToken); err != nil {
			s.logger.WithError(err).Error("Failed to create verification token")
			return errors.NewInternalError("Failed to create verification token")
		}

		// Dispatch user.registered inside the transaction so the outbox row
		// commits atomically with the user record (at-least-once delivery).
		// A dispatch failure is non-fatal: registration must not depend on
		// messaging availability.
		if s.eventPublisher != nil {
			txCtx := events.ContextWithTx(ctx, tx)
			if err := s.eventPublisher.DispatchUserRegistered(txCtx, user.ID, user.Email, user.Username, req.Language); err != nil {
				s.logger.WithError(err).Warn("Failed to dispatch user registered event")
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// Create initial notification preferences (non-critical)
	if s.prefCreator != nil {
		if err := s.prefCreator.CreateInitialPreferences(ctx, user.ID, req.Language); err != nil {
			s.logger.WithError(err).Warn("Failed to create notification preferences", "user_id", user.ID)
		}
	}

	// Send verification email outside the transaction
	s.sendVerificationEmail(ctx, user, verificationToken, req.Language)

	s.getMetrics().RecordUserRegistration()
	s.logger.Info("User registered successfully", "user_id", user.ID, "email", user.Email)

	// Clear sensitive data
	user.Password = ""

	return user, nil
}

// RefreshToken refreshes an access token using a refresh token (with token rotation)
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string, meta ...SessionMeta) (*TokenPair, error) {
	// Validate refresh token
	userID, err := s.tokenService.ValidateRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	// Revoke old refresh token first (token rotation).
	// If this fails because the token was already revoked by a concurrent
	// request, reject early to prevent duplicate token generation.
	if err := s.tokenService.RevokeRefreshToken(ctx, refreshToken); err != nil {
		return nil, errors.NewUnauthorized("Refresh token not found")
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.NewUnauthorized("Invalid refresh token")
	}

	// Check if user is active
	if !user.IsActive() {
		return nil, errors.NewUnauthorized("User account is not active")
	}

	// Load user roles
	if err := s.userRepo.LoadRoles(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to load user roles")
		return nil, errors.NewInternalError("Failed to load user roles")
	}

	// Generate new token pair
	tokenPair, err := s.tokenService.GenerateTokenPair(ctx, user, meta...)
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate tokens")
		return nil, errors.NewInternalError("Failed to generate authentication tokens")
	}

	s.logger.Debug("Access token refreshed", "user_id", user.ID)

	return tokenPair, nil
}

// Logout logs out a user by revoking their refresh token and blacklisting the access token.
func (s *AuthService) Logout(ctx context.Context, userID uuid.UUID, refreshToken string, accessToken string) error {
	// Revoke refresh token — propagate error so caller knows the session is not fully invalidated
	var revokeErr error
	if refreshToken != "" {
		if err := s.tokenService.RevokeRefreshToken(ctx, refreshToken); err != nil {
			s.logger.WithError(err).Warn("Failed to revoke refresh token during logout")
			revokeErr = err
		}
	}

	// Blacklist the access token so it can't be reused (best-effort)
	if accessToken != "" {
		bctx, cancel := context.WithTimeout(ctx, logoutBlacklistTimeout)
		defer cancel()
		if err := s.tokenService.BlacklistAccessToken(bctx, accessToken, s.cfg.JWT.Expiry); err != nil {
			s.logger.WithError(err).Warn("Failed to blacklist access token during logout")
		}
	}

	s.logger.Info("User logged out", "user_id", userID)
	if revokeErr != nil {
		return errors.NewInternalError("Failed to fully invalidate session")
	}
	return nil
}

// ChangePassword changes a user's password
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	// Validate new password
	if err := validation.Var(newPassword, "required,password"); err != nil {
		return err
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, userID)
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

	if err := s.userRepo.Update(ctx, user); err != nil {
		return errors.NewInternalError("Failed to update password")
	}

	// Invalidate all existing sessions
	if err := s.tokenService.RevokeAllUserTokens(ctx, userID); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke refresh tokens after password change")
	}
	blacklistCtx, cancel := context.WithTimeout(ctx, logoutBlacklistTimeout)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(blacklistCtx, userID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after password change")
	}

	// Send password changed notification email
	lang := s.resolveUserLanguage(ctx, userID)
	s.sendPasswordChangedEmail(ctx, user, lang)

	s.logger.Info("Password changed successfully", "user_id", userID)
	return nil
}

// assignDefaultRole assigns the default role to a user using the provided
// (possibly transaction-scoped) repository.
func (s *AuthService) assignDefaultRole(ctx context.Context, userRepo repository.UserRepository, user *domain.User) error {
	// Get or create default role
	defaultRole, err := userRepo.GetRoleByName(ctx, "user")
	if err != nil {
		// Create default role if it doesn't exist
		defaultRole = &domain.Role{
			Name:        "user",
			Description: "Default user role",
		}
		if err := userRepo.CreateRole(ctx, defaultRole); err != nil {
			return fmt.Errorf("failed to create default role: %w", err)
		}
	}

	// Assign role to user
	return userRepo.AssignRole(ctx, user.ID, defaultRole.ID)
}
