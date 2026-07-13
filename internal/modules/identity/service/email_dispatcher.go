package service

import (
	"context"

	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// emailDispatcher owns the outbound transactional-email fan-out logic. Each
// helper tries the RabbitMQ event publisher first and falls back to the
// enhanced (template/i18n) email service, then to the plain email service.
// It embeds *authCore for shared access to the logger.
type emailDispatcher struct {
	*authCore
	eventPublisher       EventPublisher
	enhancedEmailService EnhancedEmailSender
	emailSvc             *email.EmailService
}

// SetEventPublisher sets the optional event publisher for dispatching emails via RabbitMQ.
func (s *emailDispatcher) SetEventPublisher(ep EventPublisher) {
	s.eventPublisher = ep
}

// sendPasswordResetEmailNotification sends a password reset email using the appropriate email service.
// Uses RawToken (unhashed) which is only available right after token creation.
func (s *emailDispatcher) sendPasswordResetEmailNotification(
	ctx context.Context, user *domain.User, resetToken *domain.VerificationToken, language string,
) {
	raw := resetToken.RawToken

	// Try dispatching via event publisher (RabbitMQ) first
	if s.eventPublisher != nil {
		err := s.eventPublisher.DispatchEmailPasswordReset(
			ctx, user.ID, user.Email, user.Username, raw, language,
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to dispatch password reset email event, falling back to direct send")
		} else {
			return
		}
	}

	if s.enhancedEmailService != nil {
		if emailErr := s.enhancedEmailService.SendPasswordResetEmail(ctx, user.Email, user.Username, raw, language); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send password reset email")
		}
	} else if s.emailSvc != nil {
		if emailErr := s.emailSvc.SendPasswordResetEmail(ctx, user.Email, user.Username, raw); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send password reset email")
		}
	}
}

// sendResendVerificationEmail sends a verification email (used in resend flow, returns error).
// Uses RawToken (unhashed) which is only available right after token creation.
func (s *emailDispatcher) sendResendVerificationEmail(
	ctx context.Context, user *domain.User, token *domain.VerificationToken, language string,
) error {
	raw := token.RawToken

	// Try dispatching via event publisher (RabbitMQ) first
	if s.eventPublisher != nil {
		err := s.eventPublisher.DispatchEmailVerification(
			ctx, user.ID, user.Email, user.Username, raw, language,
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to dispatch verification email event, falling back to direct send")
		} else {
			return nil
		}
	}

	if s.enhancedEmailService != nil {
		if emailErr := s.enhancedEmailService.SendVerificationEmail(ctx, user.Email, user.Username, raw, language); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
			return errors.NewInternalError("Failed to resend verification email")
		}
	} else if s.emailSvc != nil {
		if emailErr := s.emailSvc.SendVerificationEmail(ctx, user.Email, user.Username, raw); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
			return errors.NewInternalError("Failed to resend verification email")
		}
	}
	return nil
}

// sendVerificationEmail sends a verification email using the appropriate email service.
// Uses RawToken (unhashed) which is only available right after token creation.
func (s *emailDispatcher) sendVerificationEmail(ctx context.Context, user *domain.User, token *domain.VerificationToken, language string) {
	raw := token.RawToken

	// Try dispatching via event publisher (RabbitMQ) first
	if s.eventPublisher != nil {
		err := s.eventPublisher.DispatchEmailVerification(
			ctx, user.ID, user.Email, user.Username, raw, language,
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to dispatch verification email event, falling back to direct send")
		} else {
			s.logger.Info("Verification email dispatched via event publisher", "user_id", user.ID, "email", user.Email)
			return
		}
	}

	if s.enhancedEmailService != nil {
		if emailErr := s.enhancedEmailService.SendVerificationEmail(ctx, user.Email, user.Username, raw, language); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
		} else {
			s.logger.Info("Verification email sent", "user_id", user.ID, "email", user.Email)
		}
	} else if s.emailSvc != nil {
		if emailErr := s.emailSvc.SendVerificationEmail(ctx, user.Email, user.Username, raw); emailErr != nil {
			s.logger.WithError(emailErr).Error("Failed to send verification email")
		} else {
			s.logger.Info("Verification email sent", "user_id", user.ID, "email", user.Email)
		}
	}
}

// sendPasswordChangedEmail sends a notification email when password is changed
func (s *emailDispatcher) sendPasswordChangedEmail(ctx context.Context, user *domain.User, language string) {
	// Try dispatching via event publisher (RabbitMQ) first
	if s.eventPublisher != nil {
		err := s.eventPublisher.DispatchEmailPasswordChanged(
			ctx, user.ID, user.Email, user.GetFullName(), language,
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to dispatch password changed email event, falling back to direct send")
		} else {
			return
		}
	}

	if s.enhancedEmailService != nil {
		if err := s.enhancedEmailService.SendPasswordChangedEmail(ctx, user.Email, user.GetFullName(), language); err != nil {
			s.logger.WithError(err).Warn("Failed to send password changed email via enhanced service", "user_id", user.ID)
		}
		return
	}

	if s.emailSvc == nil {
		return
	}
	if err := s.emailSvc.SendPasswordChangedEmail(ctx, user.Email, user.GetFullName()); err != nil {
		s.logger.WithError(err).Warn("Failed to send password changed notification", "user_id", user.ID)
	}
}
