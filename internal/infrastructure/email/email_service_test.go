package email

import (
	"context"
	"html/template"
	"testing"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/test"
	"gopkg.in/gomail.v2"
)

func newEmailServiceForTest() *EmailService {
	cfg := test.TestConfig()
	s := &EmailService{
		cfg:         cfg,
		dialer:      gomail.NewDialer("127.0.0.1", 1, "", ""),
		templates:   map[string]*template.Template{},
		logger:      logger.Get().WithField("service", "email-test"),
		sendTimeout: 200 * time.Millisecond,
		dialTimeout: 100 * time.Millisecond,
	}
	s.createDefaultTemplates()
	return s
}

func TestNewEmailServiceAndValidation(t *testing.T) {
	cfg := test.TestConfig()
	svc, err := NewEmailService(cfg)
	if err != nil {
		t.Fatalf("expected NewEmailService success, got %v", err)
	}
	if svc == nil {
		t.Fatalf("expected non-nil service")
	}

	if err := svc.validateEmailData(&EmailData{}); err == nil {
		t.Fatalf("expected validation error for empty email data")
	}
}

func TestEmailServiceSendAndTemplateMethods(t *testing.T) {
	svc := newEmailServiceForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := svc.Send(ctx, EmailData{
		To:       []string{"user@example.com"},
		Subject:  "subject",
		Template: "notification",
		Data: map[string]interface{}{
			"Subject": "s",
			"Message": "m",
			"Year":    2026,
			"AppName": "app",
		},
	})
	if err == nil {
		t.Fatalf("expected canceled context error")
	}

	// SMTP is intentionally unreachable; these should fail gracefully.
	if err := svc.SendVerificationEmail(context.Background(), "user@example.com", "user", "token"); err == nil {
		t.Fatalf("expected SMTP send failure")
	}
	if err := svc.SendPasswordResetEmail(context.Background(), "user@example.com", "user", "token"); err == nil {
		t.Fatalf("expected SMTP send failure")
	}
	if err := svc.SendWelcomeEmail(context.Background(), "user@example.com", "user"); err == nil {
		t.Fatalf("expected SMTP send failure")
	}
}

func TestEmailServiceClose(t *testing.T) {
	svc := newEmailServiceForTest()
	if err := svc.Close(); err != nil {
		t.Fatalf("expected close success, got %v", err)
	}
}
