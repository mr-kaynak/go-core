package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	mail "github.com/wneessen/go-mail"

	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/test"
)

func newEnhancedEmailServiceForTest(t *testing.T, repo *templateRepoStub) *EnhancedEmailService {
	t.Helper()
	cfg := test.TestConfig()
	tplSvc := NewTemplateService(repo)
	client, err := mail.NewClient("localhost", mail.WithPort(1), mail.WithTLSPolicy(mail.NoTLS))
	if err != nil {
		t.Fatalf("failed to create test mail client: %v", err)
	}
	return &EnhancedEmailService{
		cfg:             cfg,
		client:          client,
		templateService: tplSvc,
		logger:          logger.Get().WithField("service", "enhanced-email-test"),
	}
}

func TestEnhancedEmailServiceValidateRequestAndLanguageFallback(t *testing.T) {
	svc := newEnhancedEmailServiceForTest(t, newTemplateRepoStub())

	req := &EmailRequest{
		To:           []string{"user@example.com"},
		TemplateName: "welcome",
	}
	if err := svc.validateRequest(req); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
	if req.LanguageCode != "en" {
		t.Fatalf("expected default language en, got %q", req.LanguageCode)
	}
}

func TestEnhancedEmailServiceSendWithTemplate_ScheduledReturnsError(t *testing.T) {
	svc := newEnhancedEmailServiceForTest(t, newTemplateRepoStub())
	when := time.Now().Add(10 * time.Minute)

	err := svc.SendWithTemplate(&EmailRequest{
		To:           []string{"user@example.com"},
		TemplateName: "welcome",
		LanguageCode: "en",
		Data:         map[string]interface{}{"Name": "Ada"},
		ScheduledAt:  &when,
	})
	if err == nil {
		t.Fatal("expected scheduled send to return error, got nil")
	}
}

func TestEnhancedEmailServiceTemplateRenderAndVariableInjection(t *testing.T) {
	repo := newTemplateRepoStub()
	tmplID := uuid.New()
	repo.getByNameFn = func(name string) (*domain.ExtendedNotificationTemplate, error) {
		return &domain.ExtendedNotificationTemplate{
			NotificationTemplate: domain.NotificationTemplate{
				ID:       tmplID,
				Name:     name,
				Subject:  "Hello {{.Name}}",
				Body:     "Body {{.Name}}",
				IsActive: true,
			},
			TemplateVariables: []domain.TemplateVariable{
				{Name: "Name", Required: true},
			},
		}, nil
	}
	repo.getLangFn = func(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error) {
		return &domain.TemplateLanguage{
			TemplateID:   templateID,
			LanguageCode: languageCode,
			Subject:      "Hello {{.Name}}",
			Body:         "Body {{.Name}}",
		}, nil
	}

	svc := newEnhancedEmailServiceForTest(t, repo)

	// Missing variable should fail at render stage (before SMTP dial).
	err := svc.SendWithTemplate(&EmailRequest{
		To:           []string{"user@example.com"},
		TemplateName: "welcome",
		LanguageCode: "en",
		Data:         map[string]interface{}{},
	})
	if err == nil {
		t.Fatalf("expected render failure for missing variables")
	}
	if !strings.Contains(err.Error(), "failed to render template") {
		t.Fatalf("expected render error wrapping, got %v", err)
	}

	// Injected variables should render successfully; SMTP is expected to fail in test env.
	err = svc.SendWithTemplate(&EmailRequest{
		To:           []string{"user@example.com"},
		TemplateName: "welcome",
		LanguageCode: "en",
		Data:         map[string]interface{}{"Name": "Ada"},
	})
	if err == nil {
		t.Fatalf("expected SMTP delivery failure in unit test environment")
	}
}
