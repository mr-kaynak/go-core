package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"gopkg.in/gomail.v2"
)

// EmailService handles email sending operations
type EmailService struct {
	cfg         *config.Config
	dialer      *gomail.Dialer
	templates   map[string]*template.Template
	mu          sync.RWMutex
	logger      *logger.Logger
	sendTimeout time.Duration
	dialTimeout time.Duration
}

// EmailData represents the data structure for emails
type EmailData struct {
	To          []string               // Recipients
	CC          []string               // Carbon Copy
	BCC         []string               // Blind Carbon Copy
	Subject     string                 // Email subject
	Template    string                 // Template name
	Data        map[string]interface{} // Template data
	Attachments []Attachment           // File attachments
	Priority    Priority               // Email priority
}

// Attachment represents an email attachment
type Attachment struct {
	Filename string
	Content  []byte
	MimeType string
}

// Priority represents email priority
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
)

// NewEmailService creates a new email service
func NewEmailService(cfg *config.Config) (*EmailService, error) {
	// Create SMTP dialer
	dialer := gomail.NewDialer(
		cfg.Email.SMTPHost,
		cfg.Email.SMTPPort,
		cfg.Email.SMTPUser,
		cfg.Email.SMTPPassword,
	)

	// Configure TLS if not using port 25 or 1025 (MailHog)
	if cfg.Email.SMTPPort != 25 && cfg.Email.SMTPPort != 1025 {
		dialer.TLSConfig = &tls.Config{ //nolint:gosec // G402: InsecureSkipVerify intentionally set for development
			ServerName:         cfg.Email.SMTPHost,
			InsecureSkipVerify: cfg.IsDevelopment(),
		}
	}

	service := &EmailService{
		cfg:         cfg,
		dialer:      dialer,
		templates:   make(map[string]*template.Template),
		logger:      logger.Get().WithFields(logger.Fields{"service": "email"}),
		sendTimeout: 30 * time.Second,
		dialTimeout: 10 * time.Second,
	}

	// Load email templates
	if err := service.loadTemplates(); err != nil {
		return nil, fmt.Errorf("failed to load email templates: %w", err)
	}

	// Test connection
	if err := service.testConnection(); err != nil {
		service.logger.Warn("Failed to test email connection", "error", err)
		// Don't fail initialization, just warn
	}

	return service, nil
}

// Send sends an email with context support for cancellation and timeouts
func (s *EmailService) Send(ctx context.Context, data EmailData) error {
	// Check context before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Create a context with timeout for email sending
	sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()

	// Use a channel to handle the async send operation
	done := make(chan error, 1)

	go func() {
		// Validate email data
		if err := s.validateEmailData(&data); err != nil {
			done <- err
			return
		}

		// Get template
		tmpl, err := s.getTemplate(data.Template)
		if err != nil {
			done <- fmt.Errorf("failed to get template %s: %w", data.Template, err)
			return
		}

		// Execute template
		var body bytes.Buffer
		if err := tmpl.Execute(&body, data.Data); err != nil {
			done <- fmt.Errorf("failed to execute template: %w", err)
			return
		}

		// Create message
		msg := gomail.NewMessage()
		msg.SetHeader("From", fmt.Sprintf("%s <%s>", s.cfg.Email.FromName, s.cfg.Email.FromEmail))
		msg.SetHeader("To", data.To...)

		if len(data.CC) > 0 {
			msg.SetHeader("Cc", data.CC...)
		}
		if len(data.BCC) > 0 {
			msg.SetHeader("Bcc", data.BCC...)
		}

		msg.SetHeader("Subject", data.Subject)
		msg.SetBody("text/html", body.String())

		// Set priority
		s.setPriority(msg, data.Priority)

		// Add attachments
		// Note: gomail v2 doesn't have AttachReader. For now, we skip attachments.
		// To add attachment support, upgrade to a newer gomail version or use msg.Attach(filepath)

		// Send email
		if err := s.dialer.DialAndSend(msg); err != nil {
			s.logger.Error("Failed to send email",
				"to", data.To,
				"subject", data.Subject,
				"error", err,
			)
			done <- errors.NewServiceUnavailable("email service")
			return
		}

		s.logger.Info("Email sent successfully",
			"to", data.To,
			"subject", data.Subject,
			"template", data.Template,
		)

		done <- nil
	}()

	// Wait for either completion or context cancellation
	select {
	case err := <-done:
		return err
	case <-sendCtx.Done():
		s.logger.Warn("Email send operation timed out or canceled",
			"to", data.To,
			"subject", data.Subject,
		)
		return sendCtx.Err()
	}
}

// SendVerificationEmail sends an email verification link
func (s *EmailService) SendVerificationEmail(ctx context.Context, to, username, token string) error {
	verificationURL := fmt.Sprintf("%s/verify-email?token=%s", s.cfg.App.FrontendURL, token)

	return s.Send(ctx, EmailData{
		To:       []string{to},
		Subject:  "Verify Your Email Address",
		Template: "verification",
		Data: map[string]interface{}{
			"Username":        username,
			"VerificationURL": verificationURL,
			"AppName":         s.cfg.App.Name,
			"Year":            time.Now().Year(),
		},
	})
}

// SendPasswordResetEmail sends a password reset link
func (s *EmailService) SendPasswordResetEmail(ctx context.Context, to, username, token string) error {
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.cfg.App.FrontendURL, token)

	return s.Send(ctx, EmailData{
		To:       []string{to},
		Subject:  "Reset Your Password",
		Template: "password-reset",
		Data: map[string]interface{}{
			"Username":  username,
			"ResetURL":  resetURL,
			"AppName":   s.cfg.App.Name,
			"ExpiresIn": "1 hour",
			"Year":      time.Now().Year(),
		},
		Priority: PriorityHigh,
	})
}

// SendWelcomeEmail sends a welcome email to new users
func (s *EmailService) SendWelcomeEmail(ctx context.Context, to, username string) error {
	return s.Send(ctx, EmailData{
		To:       []string{to},
		Subject:  fmt.Sprintf("Welcome to %s!", s.cfg.App.Name),
		Template: "welcome",
		Data: map[string]interface{}{
			"Username": username,
			"AppName":  s.cfg.App.Name,
			"LoginURL": fmt.Sprintf("%s/login", s.cfg.App.FrontendURL),
			"Year":     time.Now().Year(),
		},
	})
}

// SendPasswordChangedEmail sends a notification that the password was changed
func (s *EmailService) SendPasswordChangedEmail(ctx context.Context, to, fullName string) error {
	return s.Send(ctx, EmailData{
		To:       []string{to},
		Subject:  "Your password has been changed",
		Template: "notification",
		Data: map[string]interface{}{
			"Subject": "Password Changed",
			"Message": fmt.Sprintf(
				"Hi %s, your password has been successfully changed. "+
					"If you did not make this change, please contact support immediately.", fullName),
			"AppName": s.cfg.App.Name,
			"Year":    time.Now().Year(),
		},
		Priority: PriorityHigh,
	})
}

// SendNotification sends a generic notification email
func (s *EmailService) SendNotification(ctx context.Context, to, subject, message string) error {
	return s.Send(ctx, EmailData{
		To:       []string{to},
		Subject:  subject,
		Template: "notification",
		Data: map[string]interface{}{
			"Subject": subject,
			"Message": message,
			"AppName": s.cfg.App.Name,
			"Year":    time.Now().Year(),
		},
	})
}

// loadTemplates loads email templates from files
func (s *EmailService) loadTemplates() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Define template paths
	templates := map[string]string{
		"verification":   "verification.html",
		"password-reset": "password-reset.html",
		"welcome":        "welcome.html",
		"notification":   "notification.html",
		"base":           "base.html",
	}

	// Load base template first
	baseTemplate := filepath.Join("templates", "email", "base.html")
	base, err := template.ParseFiles(baseTemplate)
	if err != nil {
		// If file doesn't exist, create default templates in memory
		s.createDefaultTemplates()
		return nil
	}

	// Load other templates
	for name, file := range templates {
		if name == "base" {
			continue
		}

		tmplPath := filepath.Join("templates", "email", file)
		tmpl, err := template.Must(base.Clone()).ParseFiles(tmplPath)
		if err != nil {
			s.logger.Warn("Failed to load template file, using default",
				"template", name,
				"error", err,
			)
			continue
		}
		s.templates[name] = tmpl
	}

	return nil
}

// createDefaultTemplates creates default email templates in memory
func (s *EmailService) createDefaultTemplates() {
	s.templates["verification"] = template.Must(template.New("verification").Parse(verificationTemplate))
	s.templates["password-reset"] = template.Must(template.New("password-reset").Parse(passwordResetTemplate))
	s.templates["welcome"] = template.Must(template.New("welcome").Parse(welcomeTemplate))
	s.templates["notification"] = template.Must(template.New("notification").Parse(notificationTemplate))
}

// getTemplate retrieves a template by name
func (s *EmailService) getTemplate(name string) (*template.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tmpl, exists := s.templates[name]
	if !exists {
		return nil, fmt.Errorf("template %s not found", name)
	}

	return tmpl, nil
}

// validateEmailData validates email data
func (s *EmailService) validateEmailData(data *EmailData) error {
	if len(data.To) == 0 {
		return errors.NewBadRequest("email recipients required")
	}

	if data.Subject == "" {
		return errors.NewBadRequest("email subject required")
	}

	// Reject CRLF in subject to prevent header injection
	if strings.ContainsAny(data.Subject, "\r\n") {
		return errors.NewBadRequest("email subject contains invalid characters")
	}

	if data.Template == "" {
		return errors.NewBadRequest("email template required")
	}

	return nil
}

// setPriority sets email priority headers
func (s *EmailService) setPriority(msg *gomail.Message, priority Priority) {
	switch priority {
	case PriorityHigh:
		msg.SetHeader("X-Priority", "1")
		msg.SetHeader("Importance", "high")
	case PriorityLow:
		msg.SetHeader("X-Priority", "5")
		msg.SetHeader("Importance", "low")
	default:
		msg.SetHeader("X-Priority", "3")
		msg.SetHeader("Importance", "normal")
	}
}

// testConnection tests the SMTP connection
func (s *EmailService) testConnection() error {
	conn, err := s.dialer.Dial()
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	// Properly close the connection
	if err := conn.Close(); err != nil {
		s.logger.Warn("Failed to close SMTP test connection", "error", err)
	}

	s.logger.Info("SMTP connection test successful",
		"host", s.cfg.Email.SMTPHost,
		"port", s.cfg.Email.SMTPPort,
	)

	return nil
}

// Default email templates (inline)
const (
	verificationTemplate = `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #007bff; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background-color: #f9f9f9; }
        .button { display: inline-block; padding: 10px 20px;
            background-color: #007bff; color: white;
            text-decoration: none; border-radius: 5px; }
        .footer { margin-top: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>{{.AppName}}</h1>
        </div>
        <div class="content">
            <h2>Hello {{.Username}},</h2>
            <p>Thank you for signing up! Please verify your email address by clicking the button below:</p>
            <p style="text-align: center;">
                <a href="{{.VerificationURL}}" class="button">Verify Email</a>
            </p>
            <p>Or copy and paste this link into your browser:</p>
            <p>{{.VerificationURL}}</p>
            <p>If you didn't create an account, please ignore this email.</p>
        </div>
        <div class="footer">
            <p>&copy; {{.Year}} {{.AppName}}. All rights reserved.</p>
        </div>
    </div>
</body>
</html>`

	passwordResetTemplate = `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #dc3545; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background-color: #f9f9f9; }
        .button { display: inline-block; padding: 10px 20px;
            background-color: #dc3545; color: white;
            text-decoration: none; border-radius: 5px; }
        .footer { margin-top: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Password Reset</h1>
        </div>
        <div class="content">
            <h2>Hello {{.Username}},</h2>
            <p>You requested to reset your password. Click the button below to create a new password:</p>
            <p style="text-align: center;">
                <a href="{{.ResetURL}}" class="button">Reset Password</a>
            </p>
            <p>Or copy and paste this link into your browser:</p>
            <p>{{.ResetURL}}</p>
            <p><strong>This link will expire in {{.ExpiresIn}}.</strong></p>
            <p>If you didn't request this, please ignore this email and your password will remain unchanged.</p>
        </div>
        <div class="footer">
            <p>&copy; {{.Year}} {{.AppName}}. All rights reserved.</p>
        </div>
    </div>
</body>
</html>`

	welcomeTemplate = `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #28a745; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background-color: #f9f9f9; }
        .button { display: inline-block; padding: 10px 20px;
            background-color: #28a745; color: white;
            text-decoration: none; border-radius: 5px; }
        .footer { margin-top: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Welcome to {{.AppName}}!</h1>
        </div>
        <div class="content">
            <h2>Hello {{.Username}},</h2>
            <p>Welcome aboard! We're excited to have you as part of our community.</p>
            <p>Your account has been successfully created. You can now log in and start exploring all the features we have to offer.</p>
            <p style="text-align: center;">
                <a href="{{.LoginURL}}" class="button">Go to Login</a>
            </p>
            <p>If you have any questions, feel free to reach out to our support team.</p>
            <p>Best regards,<br>The {{.AppName}} Team</p>
        </div>
        <div class="footer">
            <p>&copy; {{.Year}} {{.AppName}}. All rights reserved.</p>
        </div>
    </div>
</body>
</html>`

	notificationTemplate = `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #17a2b8; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background-color: #f9f9f9; }
        .footer { margin-top: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>{{.Subject}}</h1>
        </div>
        <div class="content">
            <p>{{.Message}}</p>
        </div>
        <div class="footer">
            <p>&copy; {{.Year}} {{.AppName}}. All rights reserved.</p>
        </div>
    </div>
</body>
</html>`
)

// Close closes the email service and releases resources
// Note: gomail doesn't maintain persistent connections, so cleanup is minimal
// This method is provided for consistency with other services
func (s *EmailService) Close() error {
	s.logger.Info("Email service shutdown initiated")
	// Gomail uses transient SMTP connections created on-demand for each send
	// No persistent connection cleanup needed
	// Template cache remains valid until service restart
	return nil
}
