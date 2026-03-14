package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mail "github.com/wneessen/go-mail"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
)

// EmailService handles email sending operations
type EmailService struct {
	cfg         *config.Config
	client      *mail.Client
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

	service := &EmailService{
		cfg:         cfg,
		client:      client,
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

// Send sends an email with context support for cancellation and timeouts.
// Uses DialAndSendWithContext so the SMTP operation respects context
// cancellation, preventing goroutine and connection leaks.
func (s *EmailService) Send(ctx context.Context, data EmailData) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()

	if err := s.validateEmailData(&data); err != nil {
		return err
	}

	tmpl, err := s.getTemplate(data.Template)
	if err != nil {
		return fmt.Errorf("failed to get template %s: %w", data.Template, err)
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, data.Data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	msg := mail.NewMsg()
	if err := msg.FromFormat(s.cfg.Email.FromName, s.cfg.Email.FromEmail); err != nil {
		return errors.NewBadRequest("invalid from address")
	}
	if err := msg.To(data.To...); err != nil {
		return errors.NewBadRequest("invalid recipient address")
	}

	if len(data.CC) > 0 {
		if err := msg.Cc(data.CC...); err != nil {
			return errors.NewBadRequest("invalid cc address")
		}
	}
	if len(data.BCC) > 0 {
		if err := msg.Bcc(data.BCC...); err != nil {
			return errors.NewBadRequest("invalid bcc address")
		}
	}

	msg.Subject(data.Subject)
	msg.SetBodyString(mail.TypeTextHTML, body.String())
	s.setPriority(msg, data.Priority)

	for _, att := range data.Attachments {
		if err := msg.AttachReader(att.Filename, bytes.NewReader(att.Content)); err != nil {
			s.logger.Error("Failed to attach file", "filename", att.Filename, "error", err)
			return fmt.Errorf("failed to attach file %s: %w", att.Filename, err)
		}
	}

	if err := s.client.DialAndSendWithContext(sendCtx, msg); err != nil {
		s.logger.Error("Failed to send email",
			"to", data.To,
			"subject", data.Subject,
			"error", err,
		)
		return errors.NewServiceUnavailable("email service")
	}

	s.logger.Info("Email sent successfully",
		"to", data.To,
		"subject", data.Subject,
		"template", data.Template,
	)
	return nil
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
func (s *EmailService) setPriority(msg *mail.Msg, priority Priority) {
	switch priority {
	case PriorityHigh:
		msg.SetGenHeader(mail.Header("X-Priority"), "1")
		msg.SetGenHeader(mail.Header("Importance"), "high")
	case PriorityLow:
		msg.SetGenHeader(mail.Header("X-Priority"), "5")
		msg.SetGenHeader(mail.Header("Importance"), "low")
	default:
		msg.SetGenHeader(mail.Header("X-Priority"), "3")
		msg.SetGenHeader(mail.Header("Importance"), "normal")
	}
}

// testConnection tests the SMTP connection
func (s *EmailService) testConnection() error {
	if err := s.client.DialWithContext(context.Background()); err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	if err := s.client.Close(); err != nil {
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

	//nolint:gosec // G101: email template constant, not a credential
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

// SendRaw sends an email with pre-rendered HTML body, bypassing template rendering.
// Use this when the body is already fully rendered (e.g., from the template render endpoint).
func (s *EmailService) SendRaw(ctx context.Context, to []string, subject, htmlBody string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if len(to) == 0 {
		return errors.NewBadRequest("email recipients required")
	}
	if subject == "" {
		return errors.NewBadRequest("email subject required")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.NewBadRequest("email subject contains invalid characters")
	}
	if htmlBody == "" {
		return errors.NewBadRequest("email body required")
	}

	sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()

	msg := mail.NewMsg()
	if err := msg.FromFormat(s.cfg.Email.FromName, s.cfg.Email.FromEmail); err != nil {
		return errors.NewBadRequest("invalid from address")
	}
	if err := msg.To(to...); err != nil {
		return errors.NewBadRequest("invalid recipient address")
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextHTML, htmlBody)

	if err := s.client.DialAndSendWithContext(sendCtx, msg); err != nil {
		s.logger.Error("Failed to send raw email",
			"to", to,
			"subject", subject,
			"error", err,
		)
		return errors.NewServiceUnavailable("email service")
	}

	s.logger.Info("Raw email sent successfully",
		"to", to,
		"subject", subject,
	)
	return nil
}

// Close closes the email service and releases resources
func (s *EmailService) Close() error {
	s.logger.Info("Email service shutdown initiated")
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}
