package service

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"gopkg.in/gomail.v2"
)

// EnhancedEmailService handles email sending with database template support
type EnhancedEmailService struct {
	cfg             *config.Config
	dialer          *gomail.Dialer
	templateService *TemplateService
	logger          *logger.Logger
}

// EmailRequest represents a request to send an email using a database template
type EmailRequest struct {
	To           []string               // Recipients
	CC           []string               // Carbon Copy
	BCC          []string               // Blind Carbon Copy
	TemplateName string                 // Database template name
	LanguageCode string                 // Language code (e.g., "en", "tr")
	Data         map[string]interface{} // Template variables
	Attachments  []email.Attachment     // File attachments
	Priority     email.Priority         // Email priority
	ScheduledAt  *time.Time             // Optional scheduled send time
}

// NewEnhancedEmailService creates a new enhanced email service
func NewEnhancedEmailService(cfg *config.Config, templateService *TemplateService) (*EnhancedEmailService, error) {
	// Create SMTP dialer
	dialer := gomail.NewDialer(
		cfg.Email.SMTPHost,
		cfg.Email.SMTPPort,
		cfg.Email.SMTPUser,
		cfg.Email.SMTPPassword,
	)

	// Configure TLS if not using port 25 or 1025 (MailHog)
	if cfg.Email.SMTPPort != 25 && cfg.Email.SMTPPort != 1025 {
		dialer.TLSConfig = &tls.Config{
			ServerName:         cfg.Email.SMTPHost,
			InsecureSkipVerify: cfg.IsDevelopment(),
		}
	}

	service := &EnhancedEmailService{
		cfg:             cfg,
		dialer:          dialer,
		templateService: templateService,
		logger:          logger.Get().WithFields(logger.Fields{"service": "enhanced_email"}),
	}

	// Test connection
	if err := service.testConnection(); err != nil {
		service.logger.Warn("Failed to test email connection", "error", err)
	}

	return service, nil
}

// SendWithTemplate sends an email using a database template
func (s *EnhancedEmailService) SendWithTemplate(req *EmailRequest) error {
	// Validate request
	if err := s.validateRequest(req); err != nil {
		return err
	}

	// Reject scheduled emails — scheduling must go through NotificationService
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		return errors.NewBadRequest("scheduled emails are not supported by EnhancedEmailService; use NotificationService for scheduling")
	}

	// Render the template
	rendered, err := s.templateService.RenderTemplate(&RenderTemplateRequest{
		TemplateName: req.TemplateName,
		LanguageCode: req.LanguageCode,
		Data:         req.Data,
	})
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Create message
	msg := gomail.NewMessage()
	msg.SetHeader("From", fmt.Sprintf("%s <%s>", s.cfg.Email.FromName, s.cfg.Email.FromEmail))
	msg.SetHeader("To", req.To...)

	if len(req.CC) > 0 {
		msg.SetHeader("Cc", req.CC...)
	}
	if len(req.BCC) > 0 {
		msg.SetHeader("Bcc", req.BCC...)
	}

	msg.SetHeader("Subject", rendered.Subject)

	// Send as multipart/alternative: plain text + HTML
	// This improves deliverability (lower spam score) and supports plain text email clients
	if rendered.HTMLContent != "" {
		msg.SetBody("text/plain", rendered.Body)
		msg.AddAlternative("text/html", rendered.HTMLContent)
	} else {
		msg.SetBody("text/html", rendered.Body)
	}

	// Set priority headers
	s.setPriority(msg, req.Priority)

	// Add custom headers for tracking
	msg.SetHeader("X-Template", req.TemplateName)
	msg.SetHeader("X-Language", req.LanguageCode)

	// Send email
	if err := s.dialer.DialAndSend(msg); err != nil {
		s.logger.Error("Failed to send email",
			"to", req.To,
			"template", req.TemplateName,
			"error", err,
		)
		return errors.NewServiceUnavailable("email service")
	}

	s.logger.Info("Email sent successfully",
		"to", req.To,
		"template", req.TemplateName,
		"language", req.LanguageCode,
	)

	return nil
}

// SendVerificationEmail sends an email verification using a database template
func (s *EnhancedEmailService) SendVerificationEmail(to, username, token string, languageCode string) error {
	verificationURL := fmt.Sprintf("%s/verify-email?token=%s", s.cfg.App.FrontendURL, token)

	return s.SendWithTemplate(&EmailRequest{
		To:           []string{to},
		TemplateName: "user_verification",
		LanguageCode: languageCode,
		Data: map[string]interface{}{
			"Username":        username,
			"VerificationURL": verificationURL,
			"AppName":         s.cfg.App.Name,
			"ExpiresIn":       "24 hours",
			"Year":            time.Now().Year(),
		},
		Priority: email.PriorityHigh,
	})
}

// SendPasswordResetEmail sends a password reset email using a database template
func (s *EnhancedEmailService) SendPasswordResetEmail(to, username, token string, languageCode string) error {
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.cfg.App.FrontendURL, token)

	return s.SendWithTemplate(&EmailRequest{
		To:           []string{to},
		TemplateName: "password_reset",
		LanguageCode: languageCode,
		Data: map[string]interface{}{
			"Username":  username,
			"ResetURL":  resetURL,
			"AppName":   s.cfg.App.Name,
			"ExpiresIn": "1 hour",
			"Year":      time.Now().Year(),
		},
		Priority: email.PriorityHigh,
	})
}

// SendPasswordChangedEmail sends a password changed notification using DB template
func (s *EnhancedEmailService) SendPasswordChangedEmail(to, fullName string, languageCode string) error {
	return s.SendWithTemplate(&EmailRequest{
		To:           []string{to},
		TemplateName: "password_changed",
		LanguageCode: languageCode,
		Data: map[string]interface{}{
			"FullName": fullName,
			"AppName":  s.cfg.App.Name,
			"Year":     time.Now().Year(),
		},
		Priority: email.PriorityHigh,
	})
}

// SendWelcomeEmail sends a welcome email using a database template
func (s *EnhancedEmailService) SendWelcomeEmail(to, username string, languageCode string) error {
	return s.SendWithTemplate(&EmailRequest{
		To:           []string{to},
		TemplateName: "welcome_user",
		LanguageCode: languageCode,
		Data: map[string]interface{}{
			"Username": username,
			"AppName":  s.cfg.App.Name,
			"LoginURL": fmt.Sprintf("%s/login", s.cfg.App.FrontendURL),
			"Year":     time.Now().Year(),
		},
	})
}

// SendTwoFactorCode sends a two-factor authentication code email
func (s *EnhancedEmailService) SendTwoFactorCode(to, username, code string, languageCode string) error {
	return s.SendWithTemplate(&EmailRequest{
		To:           []string{to},
		TemplateName: "two_factor_code",
		LanguageCode: languageCode,
		Data: map[string]interface{}{
			"Username":  username,
			"Code":      code,
			"AppName":   s.cfg.App.Name,
			"ExpiresIn": "10 minutes",
		},
		Priority: PriorityUrgent,
	})
}

// SendAccountLockedNotification sends an account locked notification
func (s *EnhancedEmailService) SendAccountLockedNotification(to, username, reason, action string, languageCode string) error {
	return s.SendWithTemplate(&EmailRequest{
		To:           []string{to},
		TemplateName: "account_locked",
		LanguageCode: languageCode,
		Data: map[string]interface{}{
			"Username": username,
			"Reason":   reason,
			"Action":   action,
			"AppName":  s.cfg.App.Name,
		},
		Priority: email.PriorityHigh,
	})
}

// SendBulkEmails sends emails to multiple recipients using the same template.
// Returns an error listing all failed recipients if any sends fail.
func (s *EnhancedEmailService) SendBulkEmails(
	recipients []string, templateName string, baseData map[string]interface{}, languageCode string,
) error {
	var failedRecipients []string
	for _, recipient := range recipients {
		data := make(map[string]interface{})
		for k, v := range baseData {
			data[k] = v
		}
		data["RecipientEmail"] = recipient

		if err := s.SendWithTemplate(&EmailRequest{
			To:           []string{recipient},
			TemplateName: templateName,
			LanguageCode: languageCode,
			Data:         data,
		}); err != nil {
			s.logger.Error("Failed to send bulk email", "recipient", recipient, "error", err)
			failedRecipients = append(failedRecipients, recipient)
		}
	}
	if len(failedRecipients) > 0 {
		return fmt.Errorf("failed to send to %d/%d recipients: %v", len(failedRecipients), len(recipients), failedRecipients)
	}
	return nil
}

// validateRequest validates the email request
func (s *EnhancedEmailService) validateRequest(req *EmailRequest) error {
	if len(req.To) == 0 {
		return errors.NewBadRequest("email recipients required")
	}

	if req.TemplateName == "" {
		return errors.NewBadRequest("template name required")
	}

	// Default language to English if not specified
	if req.LanguageCode == "" {
		req.LanguageCode = "en"
	}

	return nil
}

// setPriority sets email priority headers
func (s *EnhancedEmailService) setPriority(msg *gomail.Message, priority email.Priority) {
	switch priority {
	case email.PriorityHigh:
		msg.SetHeader("X-Priority", "1")
		msg.SetHeader("Importance", "high")
	case email.PriorityLow:
		msg.SetHeader("X-Priority", "5")
		msg.SetHeader("Importance", "low")
	default:
		msg.SetHeader("X-Priority", "3")
		msg.SetHeader("Importance", "normal")
	}
}

// PriorityUrgent represents urgent priority
const PriorityUrgent = email.PriorityHigh

// testConnection tests the SMTP connection
func (s *EnhancedEmailService) testConnection() error {
	conn, err := s.dialer.Dial()
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	s.logger.Info("SMTP connection test successful",
		"host", s.cfg.Email.SMTPHost,
		"port", s.cfg.Email.SMTPPort,
	)

	return nil
}
