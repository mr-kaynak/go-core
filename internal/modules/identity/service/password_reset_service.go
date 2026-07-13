package service

import (
	"context"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// passwordResetService owns the password reset flows (request, reset, validate).
// It embeds *authCore for shared dependencies and *emailDispatcher for the
// outbound email fan-out.
type passwordResetService struct {
	*authCore
	*emailDispatcher
	userRepo         repository.UserReadWriterTx
	verificationRepo repository.VerificationTokenRepository
	tokenService     *TokenService
}

// RequestPasswordReset initiates the password reset flow
func (s *passwordResetService) RequestPasswordReset(ctx context.Context, email string) error {
	// Get user by email
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		// Don't reveal if email exists or not
		s.logger.Debug("Password reset requested for non-existent email", "email", email)
		return nil
	}

	// Check rate limiting - max 3 requests per hour
	oneHourAgo := time.Now().Add(-time.Hour)
	count, err := s.verificationRepo.CountByUserAndType(ctx, user.ID, domain.TokenTypePasswordReset, oneHourAgo)
	if err != nil {
		s.logger.WithError(err).Error("Failed to check password reset rate limit")
		return errors.NewInternalError("Failed to process password reset request")
	}

	if count >= maxVerificationPerHour {
		return errors.NewTooManyRequests("Too many password reset requests. Please try again later.")
	}

	// Delete old tokens
	if err := s.verificationRepo.DeleteByUserAndType(ctx, user.ID, domain.TokenTypePasswordReset); err != nil {
		s.logger.WithError(err).Warn("Failed to delete old password reset tokens")
	}

	// Create new password reset token
	resetToken := &domain.VerificationToken{
		UserID: user.ID,
		Type:   domain.TokenTypePasswordReset,
	}

	if err := s.verificationRepo.Create(ctx, resetToken); err != nil {
		s.logger.WithError(err).Error("Failed to create password reset token")
		return errors.NewInternalError("Failed to process password reset request")
	}

	// Send password reset email
	lang := s.resolveUserLanguage(ctx, user.ID)
	s.sendPasswordResetEmailNotification(ctx, user, resetToken, lang)

	s.logger.Info("Password reset requested", "user_id", user.ID, "email", user.Email)
	return nil
}

// ResetPassword completes the password reset flow with a token
func (s *passwordResetService) ResetPassword(ctx context.Context, token, newPassword string) error {
	// Validate new password
	if err := validation.Var(newPassword, "required,password"); err != nil {
		return err
	}

	// Find token
	resetToken, err := s.verificationRepo.FindByToken(ctx, token)
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
	user, err := s.userRepo.GetByID(ctx, resetToken.UserID)
	if err != nil {
		return errors.NewNotFound("User", resetToken.UserID.String())
	}

	// Update password
	user.Password = newPassword
	user.BCryptCost = s.cfg.Security.BCryptCost
	if err := user.HashPassword(); err != nil {
		return errors.NewInternalError("Failed to hash password")
	}

	// Mark token as used before persisting
	resetToken.MarkAsUsed()

	// Wrap password update, token invalidation, and cleanup in a single
	// transaction to prevent reset-token reuse on partial failure.
	if err := s.runInTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.WithTx(tx).Update(ctx, user); err != nil {
			return errors.NewInternalError("Failed to update password")
		}
		if err := s.verificationRepo.WithTx(tx).Update(ctx, resetToken); err != nil {
			return errors.NewInternalError("Failed to mark password reset token as used")
		}
		if err := s.verificationRepo.WithTx(tx).DeleteByUserAndType(ctx, user.ID, domain.TokenTypePasswordReset); err != nil {
			s.logger.WithError(err).Warn("Failed to delete old password reset tokens")
		}
		return nil
	}); err != nil {
		return err
	}

	// Invalidate all existing sessions (Redis, best-effort)
	if err := s.tokenService.RevokeAllUserTokens(ctx, user.ID); err != nil {
		s.logger.WithError(err).Warn("Failed to revoke refresh tokens after password reset")
	}
	blacklistCtx, cancel := context.WithTimeout(ctx, logoutBlacklistTimeout)
	defer cancel()
	if err := s.tokenService.BlacklistAllUserTokens(blacklistCtx, user.ID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist access tokens after password reset")
	}

	// Send password changed notification email
	lang := s.resolveUserLanguage(ctx, user.ID)
	s.sendPasswordChangedEmail(ctx, user, lang)

	s.logger.Info("Password reset successfully", "user_id", user.ID)
	return nil
}

// ValidatePasswordResetToken validates if a password reset token is valid
func (s *passwordResetService) ValidatePasswordResetToken(ctx context.Context, token string) error {
	// Find token
	resetToken, err := s.verificationRepo.FindByToken(ctx, token)
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
