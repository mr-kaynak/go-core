package service

import (
	"context"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// emailVerificationService owns the email verification flows (verify, resend).
// It embeds *authCore for shared dependencies and *emailDispatcher for the
// outbound email fan-out.
type emailVerificationService struct {
	*authCore
	*emailDispatcher
	userRepo         repository.UserReadWriterTx
	verificationRepo repository.VerificationTokenRepository
}

// VerifyEmail verifies a user's email address using a verification token
func (s *emailVerificationService) VerifyEmail(ctx context.Context, token string) error {
	// Find token
	verificationToken, err := s.verificationRepo.FindByToken(ctx, token)
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
	user, err := s.userRepo.GetByID(ctx, verificationToken.UserID)
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

	if err := s.runInTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.WithTx(tx).Update(ctx, user); err != nil {
			return errors.NewInternalError("Failed to verify email")
		}
		if err := s.verificationRepo.WithTx(tx).Update(ctx, verificationToken); err != nil {
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
func (s *emailVerificationService) ResendVerificationEmail(ctx context.Context, emailAddr string) error {
	// Get user by email
	user, err := s.userRepo.GetByEmail(ctx, emailAddr)
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
	count, err := s.verificationRepo.CountByUserAndType(ctx, user.ID, domain.TokenTypeEmailVerification, oneHourAgo)
	if err != nil {
		s.logger.WithError(err).Error("Failed to check verification token rate limit")
		return errors.NewInternalError("Failed to resend verification email")
	}

	if count >= maxVerificationPerHour {
		s.logger.Warn("Verification email rate limit exceeded", "user_id", user.ID)
		// Return nil rather than a rate-limit error: revealing that the limit was hit
		// would confirm that the address belongs to an existing account, enabling
		// account enumeration. Callers receive the same success response regardless.
		return nil
	}

	// Delete old tokens
	if err := s.verificationRepo.DeleteByUserAndType(ctx, user.ID, domain.TokenTypeEmailVerification); err != nil {
		s.logger.WithError(err).Warn("Failed to delete old verification tokens")
	}

	// Create new verification token
	verificationToken := &domain.VerificationToken{
		UserID: user.ID,
		Type:   domain.TokenTypeEmailVerification,
	}

	if err := s.verificationRepo.Create(ctx, verificationToken); err != nil {
		s.logger.WithError(err).Error("Failed to create verification token")
		return errors.NewInternalError("Failed to resend verification email")
	}

	// Send verification email
	lang := s.resolveUserLanguage(ctx, user.ID)
	if sendErr := s.sendResendVerificationEmail(ctx, user, verificationToken, lang); sendErr != nil {
		return sendErr
	}

	s.logger.Info("Verification email resent", "user_id", user.ID, "email", user.Email)
	return nil
}
