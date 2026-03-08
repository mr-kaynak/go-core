package service

import (
	"context"
	"fmt"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
)

// EmailConsumerService consumes email-related events from RabbitMQ and sends
// emails via the configured SMTP services. This offloads synchronous email
// sending from the HTTP request path.
type EmailConsumerService struct {
	cfg              *config.Config
	emailSvc         *email.EmailService
	enhancedEmailSvc EnhancedEmailSender
	rabbitmq         *rabbitmq.RabbitMQService
	logger           *logger.Logger
}

// EnhancedEmailSender is re-declared locally to avoid import cycles.
// It matches the interface defined in the identity service package.
type EnhancedEmailSender interface {
	SendVerificationEmail(to, username, token string, languageCode string) error
	SendPasswordResetEmail(to, username, token string, languageCode string) error
}

// NewEmailConsumerService creates a new email consumer service.
func NewEmailConsumerService(
	cfg *config.Config,
	emailSvc *email.EmailService,
	enhancedEmailSvc EnhancedEmailSender,
) *EmailConsumerService {
	return &EmailConsumerService{
		cfg:              cfg,
		emailSvc:         emailSvc,
		enhancedEmailSvc: enhancedEmailSvc,
		logger:           logger.Get().WithFields(logger.Fields{"service": "email_consumer"}),
	}
}

// SetRabbitMQ sets the RabbitMQ service for consuming messages.
func (s *EmailConsumerService) SetRabbitMQ(rmq *rabbitmq.RabbitMQService) {
	s.rabbitmq = rmq
}

// StartConsumer declares the email queue with routing keys and starts consuming.
func (s *EmailConsumerService) StartConsumer() error {
	if s.rabbitmq == nil {
		return fmt.Errorf("RabbitMQ service not available")
	}

	queueName := "email.process"
	routingKeys := []string{
		"email.verification",
		"email.password_reset",
		"email.password_changed",
		"user.registered",
	}

	if err := s.rabbitmq.DeclareQueue(queueName, routingKeys); err != nil {
		return fmt.Errorf("failed to declare email queue: %w", err)
	}

	if err := s.rabbitmq.Subscribe(queueName, s.handleEmailMessage); err != nil {
		return fmt.Errorf("failed to subscribe to email queue: %w", err)
	}

	s.logger.Info("Email RabbitMQ consumer started", "queue", queueName, "routing_keys", routingKeys)
	return nil
}

// handleEmailMessage routes an incoming RabbitMQ message to the appropriate handler.
func (s *EmailConsumerService) handleEmailMessage(msg *rabbitmq.Message) error {
	switch msg.Type {
	case "email.verification":
		return s.handleVerificationEmail(msg)
	case "email.password_reset":
		return s.handlePasswordResetEmail(msg)
	case "email.password_changed":
		return s.handlePasswordChangedEmail(msg)
	case "user.registered":
		return s.handleWelcomeEmail(msg)
	default:
		s.logger.Warn("Unknown email event type", "type", msg.Type)
		return nil
	}
}

func (s *EmailConsumerService) handleVerificationEmail(msg *rabbitmq.Message) error {
	emailAddr, _ := msg.Data["email"].(string)
	username, _ := msg.Data["username"].(string)
	token, _ := msg.Data["token"].(string)
	languageCode, _ := msg.Data["language_code"].(string)

	if emailAddr == "" || token == "" {
		return fmt.Errorf("missing required fields in verification email event")
	}
	if languageCode == "" {
		languageCode = "en"
	}

	// Try enhanced email service first, fall back to basic
	if s.enhancedEmailSvc != nil {
		if err := s.enhancedEmailSvc.SendVerificationEmail(emailAddr, username, token, languageCode); err != nil {
			s.logger.WithError(err).Warn("Enhanced verification email failed, trying basic service")
		} else {
			s.logger.Info("Verification email sent via consumer", "email", emailAddr)
			return nil
		}
	}

	if s.emailSvc != nil {
		if err := s.emailSvc.SendVerificationEmail(context.Background(), emailAddr, username, token); err != nil {
			return fmt.Errorf("failed to send verification email: %w", err)
		}
		s.logger.Info("Verification email sent via consumer (basic)", "email", emailAddr)
	}

	return nil
}

func (s *EmailConsumerService) handlePasswordResetEmail(msg *rabbitmq.Message) error {
	emailAddr, _ := msg.Data["email"].(string)
	username, _ := msg.Data["username"].(string)
	token, _ := msg.Data["token"].(string)
	languageCode, _ := msg.Data["language_code"].(string)

	if emailAddr == "" || token == "" {
		return fmt.Errorf("missing required fields in password reset email event")
	}
	if languageCode == "" {
		languageCode = "en"
	}

	// Try enhanced email service first, fall back to basic
	if s.enhancedEmailSvc != nil {
		if err := s.enhancedEmailSvc.SendPasswordResetEmail(emailAddr, username, token, languageCode); err != nil {
			s.logger.WithError(err).Warn("Enhanced password reset email failed, trying basic service")
		} else {
			s.logger.Info("Password reset email sent via consumer", "email", emailAddr)
			return nil
		}
	}

	if s.emailSvc != nil {
		if err := s.emailSvc.SendPasswordResetEmail(context.Background(), emailAddr, username, token); err != nil {
			return fmt.Errorf("failed to send password reset email: %w", err)
		}
		s.logger.Info("Password reset email sent via consumer (basic)", "email", emailAddr)
	}

	return nil
}

func (s *EmailConsumerService) handlePasswordChangedEmail(msg *rabbitmq.Message) error {
	emailAddr, _ := msg.Data["email"].(string)
	fullName, _ := msg.Data["full_name"].(string)

	if emailAddr == "" {
		return fmt.Errorf("missing email in password changed event")
	}

	if s.emailSvc != nil {
		if err := s.emailSvc.SendPasswordChangedEmail(context.Background(), emailAddr, fullName); err != nil {
			return fmt.Errorf("failed to send password changed email: %w", err)
		}
		s.logger.Info("Password changed email sent via consumer", "email", emailAddr)
	}

	return nil
}

func (s *EmailConsumerService) handleWelcomeEmail(msg *rabbitmq.Message) error {
	emailAddr, _ := msg.Data["email"].(string)
	username, _ := msg.Data["username"].(string)

	if emailAddr == "" {
		return fmt.Errorf("missing email in user registered event")
	}

	if s.emailSvc != nil {
		if err := s.emailSvc.SendWelcomeEmail(context.Background(), emailAddr, username); err != nil {
			return fmt.Errorf("failed to send welcome email: %w", err)
		}
		s.logger.Info("Welcome email sent via consumer", "email", emailAddr)
	}

	return nil
}
