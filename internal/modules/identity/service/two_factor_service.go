package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/crypto"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// TwoFactorService owns the TOTP-based two-factor authentication flows. It embeds
// *authCore for shared dependencies and holds the repositories/services needed to
// validate a 2FA login and issue full tokens.
type TwoFactorService struct {
	*authCore
	userRepo     repository.UserReadWriterTx
	tokenService *TokenService
	sessionCache SessionCacheWriter
}

// Enable2FAResult holds the data returned when initiating 2FA setup.
type Enable2FAResult struct {
	OTPAuthURL  string   `json:"otp_url"`
	BackupCodes []string `json:"backup_codes"`
}

// encryptionKey returns the AES-256 key derived from the configured encryption passphrase.
func (s *TwoFactorService) encryptionKey() []byte {
	return crypto.NormalizeKey(s.cfg.Security.EncryptionKey)
}

// Enable2FA generates a TOTP secret for the user and returns the otpauth URL and backup codes.
// The 2FA is not yet active until the user verifies with a valid code via Verify2FA.
func (s *TwoFactorService) Enable2FA(ctx context.Context, userID uuid.UUID) (*Enable2FAResult, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
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

	if err := s.userRepo.Update(ctx, user); err != nil {
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
func (s *TwoFactorService) decryptTOTPSecret(encrypted string) (string, error) {
	return crypto.Decrypt(encrypted, s.encryptionKey())
}

// Verify2FA verifies a TOTP code and enables 2FA for the user.
// This should be called after Enable2FA to confirm the user has set up their authenticator app correctly.
func (s *TwoFactorService) Verify2FA(ctx context.Context, userID uuid.UUID, code string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
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
	if err := s.userRepo.Update(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to enable two-factor authentication")
		return errors.NewInternalError("Failed to enable two-factor authentication")
	}

	s.logger.Info("2FA enabled", "user_id", userID)
	return nil
}

// Disable2FA disables 2FA for the user after verifying a valid TOTP code.
func (s *TwoFactorService) Disable2FA(ctx context.Context, userID uuid.UUID, code string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
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

	if err := s.userRepo.Update(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to disable two-factor authentication")
		return errors.NewInternalError("Failed to disable two-factor authentication")
	}

	s.logger.Info("2FA disabled", "user_id", userID)
	return nil
}

// ForceDisable2FA disables 2FA for a user without requiring a TOTP code (admin operation).
func (s *TwoFactorService) ForceDisable2FA(ctx context.Context, userID uuid.UUID) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	if !user.TwoFactorEnabled {
		return errors.NewBadRequest("Two-factor authentication is not enabled")
	}

	user.TwoFactorEnabled = false
	user.TwoFactorSecret = ""
	user.TwoFactorBackupCodes = ""

	if err := s.userRepo.Update(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to force disable two-factor authentication")
		return errors.NewInternalError("Failed to disable two-factor authentication")
	}

	s.logger.Info("2FA force-disabled by admin", "user_id", userID)
	return nil
}

// Validate2FACode validates a TOTP code during login.
// It checks both the TOTP code and backup codes.
func (s *TwoFactorService) Validate2FACode(ctx context.Context, userID uuid.UUID, code string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
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

	// Try backup codes inside a transaction with row lock to prevent
	// concurrent reuse of the same single-use code.
	if user.TwoFactorBackupCodes != "" {
		codeHash := crypto.HashSHA256Hex(code)
		var matched bool
		txErr := s.runInTx(ctx, func(tx *gorm.DB) error {
			locked, err := s.userRepo.WithTx(tx).GetByIDForUpdate(ctx, userID)
			if err != nil {
				return err
			}
			backupCodes := strings.Split(locked.TwoFactorBackupCodes, ",")
			for i, bc := range backupCodes {
				if secureHashEqual(bc, codeHash) {
					backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
					locked.TwoFactorBackupCodes = strings.Join(backupCodes, ",")
					if err := s.userRepo.WithTx(tx).Update(ctx, locked); err != nil {
						return err
					}
					matched = true
					return nil
				}
			}
			return nil
		})
		if txErr != nil {
			s.logger.WithError(txErr).Error("Failed to validate backup code in transaction")
			return errors.NewInternalError("Failed to verify two-factor code")
		}
		if matched {
			s.logger.Info("2FA validated with backup code", "user_id", userID)
			return nil
		}
	}

	return errors.NewUnauthorized("Invalid two-factor code")
}

// Validate2FALogin validates a 2FA token and TOTP code, then issues full tokens.
func (s *TwoFactorService) Validate2FALogin(
	ctx context.Context, twoFactorToken, code, ipAddress, userAgent string,
) (*LoginResponse, error) {
	if twoFactorToken == "" || code == "" {
		return nil, errors.NewBadRequest("Two-factor token and code are required")
	}

	// Validate the 2FA token to get user ID
	userID, err := s.tokenService.ValidateTwoFactorToken(ctx, twoFactorToken)
	if err != nil {
		return nil, err
	}

	// Validate the TOTP code
	if err := s.Validate2FACode(ctx, userID, code); err != nil {
		return nil, err
	}

	// Load user with roles for token generation
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.NewNotFound("User", userID.String())
	}

	if err := s.userRepo.LoadRoles(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to load user roles")
		return nil, errors.NewInternalError("Failed to load user roles")
	}

	// Generate full token pair
	tokenPair, err := s.tokenService.GenerateTokenPair(ctx, user, SessionMeta{
		IPAddress: ipAddress,
		UserAgent: userAgent,
	})
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate tokens after 2FA")
		return nil, errors.NewInternalError("Failed to generate authentication tokens")
	}

	// Blacklist the consumed 2FA token to prevent replay attacks (best-effort)
	{
		bctx, cancel := context.WithTimeout(ctx, logoutBlacklistTimeout)
		defer cancel()
		if err := s.tokenService.BlacklistAccessToken(bctx, twoFactorToken, twoFactorTokenExpiry); err != nil {
			s.logger.WithError(err).Warn("Failed to blacklist consumed 2FA token")
		}
	}

	// Clear user-level blacklist (2FA login proves password + TOTP)
	{
		bctx, cancel := context.WithTimeout(ctx, sessionCacheTimeout)
		defer cancel()
		if err := s.tokenService.ClearUserBlacklist(bctx, user.ID.String()); err != nil {
			s.logger.WithError(err).Warn("Failed to clear user blacklist after 2FA login")
		}
	}

	// Cache permissions
	if s.sessionCache != nil {
		bctx, cancel := context.WithTimeout(ctx, sessionCacheTimeout)
		defer cancel()
		if err := s.sessionCache.SetPermissions(bctx, user.ID.String(), user.GetRoleNames(), user.GetPermissionNames()); err != nil {
			s.logger.WithError(err).Warn("Failed to cache session permissions")
		}
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := s.userRepo.Update(ctx, user); err != nil {
		s.logger.WithError(err).Error("Failed to update last login")
	}

	s.getMetrics().RecordLoginAttempt(true, "credentials_2fa")
	s.logger.Info("User logged in with 2FA", "user_id", user.ID, "email", user.Email)

	user.Password = ""

	return &LoginResponse{
		User:         user,
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
	}, nil
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
