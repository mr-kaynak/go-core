package service

import (
	"bytes"
	"context"
	"fmt"
	"time"

	mail "github.com/wneessen/go-mail"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
)

// EnhancedEmailService handles email sending with database template support
type EnhancedEmailService struct {
	cfg    *config.Config
	client *mail.Client
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
	// Determine TLS policy based on port and environment
	var tlsPolicy mail.TLSPolicy
	switch {
	case cfg.Email.SMTPPort == 25 || cfg.Email.SMTPPort == 1025:
		tlsPolicy = mail.NoTLS
	case cfg.IsDevelopment():
		tlsPolicy = mail.TLSOpportunistic
	default:
		tlsPolicy = mail.TLSMandatory
	}

	// Create SMTP client
	opts := []mail.Option{
		mail.WithPort(cfg.Email.SMTPPort),
		mail.WithTLSPolicy(tlsPolicy),
	}
	if cfg.Email.SMTPUser != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.Email.SMTPUser),
			mail.WithPassword(cfg.Email.SMTPPassword))
	}

	client, err := mail.NewClient(cfg.Email.SMTPHost, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create mail client: %w", err)
	}

	service := &EnhancedEmailService{
		cfg:    cfg,
		client: client,
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
	msg := mail.NewMsg()
	if err := msg.FromFormat(s.cfg.Email.FromName, s.cfg.Email.FromEmail); err != nil {
		return errors.NewBadRequest("invalid from address")
	}
	if err := msg.To(req.To...); err != nil {
		return errors.NewBadRequest("invalid recipient address")
	}

	if len(req.CC) > 0 {
		if err := msg.Cc(req.CC...); err != nil {
			return errors.NewBadRequest("invalid cc address")
		}
	}
	if len(req.BCC) > 0 {
		if err := msg.Bcc(req.BCC...); err != nil {
			return errors.NewBadRequest("invalid bcc address")
		}
	}

	msg.Subject(rendered.Subject)

	// Send as multipart/alternative: plain text + HTML
	// This improves deliverability (lower spam score) and supports plain text email clients
	if rendered.HTMLContent != "" {
		msg.SetBodyString(mail.TypeTextPlain, rendered.Body)
		msg.AddAlternativeString(mail.TypeTextHTML, rendered.HTMLContent)
	} else {
		msg.SetBodyString(mail.TypeTextHTML, rendered.Body)
	}

	// Set priority headers
	s.setPriority(msg, req.Priority)

	// Add custom headers for tracking
	msg.SetGenHeader(mail.Header("X-Template"), req.TemplateName)
	msg.SetGenHeader(mail.Header("X-Language"), req.LanguageCode)

	// Add attachments
	for _, att := range req.Attachments {
		msg.AttachReader(att.Filename, bytes.NewReader(att.Content))
	}

	// Send email
	if err := s.client.DialAndSend(msg); err != nil {
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
func (s *EnhancedEmailService) setPriority(msg *mail.Msg, priority email.Priority) {
	switch priority {
	case email.PriorityHigh:
		msg.SetGenHeader(mail.Header("X-Priority"), "1")
		msg.SetGenHeader(mail.Header("Importance"), "high")
	case email.PriorityLow:
		msg.SetGenHeader(mail.Header("X-Priority"), "5")
		msg.SetGenHeader(mail.Header("Importance"), "low")
	default:
		msg.SetGenHeader(mail.Header("X-Priority"), "3")
		msg.SetGenHeader(mail.Header("Importance"), "normal")
	}
}

// PriorityUrgent represents urgent priority
const PriorityUrgent = email.PriorityHigh

// testConnection tests the SMTP connection
func (s *EnhancedEmailService) testConnection() error {
	if err := s.client.DialWithContext(context.Background()); err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer s.client.Close()

	s.logger.Info("SMTP connection test successful",
		"host", s.cfg.Email.SMTPHost,
		"port", s.cfg.Email.SMTPPort,
	)

	return nil
}
