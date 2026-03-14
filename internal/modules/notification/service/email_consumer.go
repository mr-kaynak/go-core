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
	SendPasswordChangedEmail(to, fullName string, languageCode string) error
	SendWelcomeEmail(to, username string, languageCode string) error
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

// sendWithFallback tries the enhanced email service first, then falls back to basic.
func (s *EmailConsumerService) sendWithFallback(name string, enhanced, basic func() error) error {
	if s.enhancedEmailSvc != nil {
		if err := enhanced(); err != nil {
			s.logger.WithError(err).Warn("Enhanced " + name + " failed, trying basic service")
		} else {
			s.logger.Info(name+" sent via consumer", "type", name)
			return nil
		}
	}

	if s.emailSvc != nil {
		if err := basic(); err != nil {
			return fmt.Errorf("failed to send %s: %w", name, err)
		}
		s.logger.Info(name+" sent via consumer (basic)", "type", name)
	}

	return nil
}

func msgStr(data map[string]interface{}, key string) string {
	v, _ := data[key].(string)
	return v
}

func langOrDefault(code string) string {
	if code == "" {
		return "en"
	}
	return code
}

// handleEmailMessage routes an incoming RabbitMQ message to the appropriate handler.
func (s *EmailConsumerService) handleEmailMessage(msg *rabbitmq.Message) error {
	emailAddr := msgStr(msg.Data, "email")
	lang := langOrDefault(msgStr(msg.Data, "language_code"))

	switch msg.Type {
	case "email.verification":
		username := msgStr(msg.Data, "username")
		token := msgStr(msg.Data, "token")
		if emailAddr == "" || token == "" {
			return fmt.Errorf("missing required fields in verification email event")
		}
		return s.sendWithFallback("verification email",
			func() error {
				return s.enhancedEmailSvc.SendVerificationEmail(emailAddr, username, token, lang)
			},
			func() error {
				return s.emailSvc.SendVerificationEmail(context.Background(), emailAddr, username, token)
			},
		)

	case "email.password_reset":
		username := msgStr(msg.Data, "username")
		token := msgStr(msg.Data, "token")
		if emailAddr == "" || token == "" {
			return fmt.Errorf("missing required fields in password reset email event")
		}
		return s.sendWithFallback("password reset email",
			func() error {
				return s.enhancedEmailSvc.SendPasswordResetEmail(emailAddr, username, token, lang)
			},
			func() error {
				return s.emailSvc.SendPasswordResetEmail(context.Background(), emailAddr, username, token)
			},
		)

	case "email.password_changed":
		fullName := msgStr(msg.Data, "full_name")
		if emailAddr == "" {
			return fmt.Errorf("missing email in password changed event")
		}
		return s.sendWithFallback("password changed email",
			func() error {
				return s.enhancedEmailSvc.SendPasswordChangedEmail(emailAddr, fullName, lang)
			},
			func() error {
				return s.emailSvc.SendPasswordChangedEmail(context.Background(), emailAddr, fullName)
			},
		)

	case "user.registered":
		username := msgStr(msg.Data, "username")
		if emailAddr == "" {
			return fmt.Errorf("missing email in user registered event")
		}
		return s.sendWithFallback("welcome email",
			func() error {
				return s.enhancedEmailSvc.SendWelcomeEmail(emailAddr, username, lang)
			},
			func() error {
				return s.emailSvc.SendWelcomeEmail(context.Background(), emailAddr, username)
			},
		)

	default:
		s.logger.Warn("Unknown email event type", "type", msg.Type)
		return nil
	}
}
