package service

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
)

// TemplateService handles template operations
type TemplateService struct {
	templateRepo repository.TemplateRepository
	logger       *logger.Logger
}

// NewTemplateService creates a new template service
func NewTemplateService(templateRepo repository.TemplateRepository) *TemplateService {
	return &TemplateService{
		templateRepo: templateRepo,
		logger:       logger.Get().WithFields(logger.Fields{"service": "template"}),
	}
}

// CreateTemplateRequest represents a request to create a template
type CreateTemplateRequest struct {
	Name        string                   `json:"name" validate:"required,min=3,max=100"`
	Type        domain.NotificationType  `json:"type" validate:"required"`
	CategoryID  *uuid.UUID               `json:"category_id,omitempty"`
	Subject     string                   `json:"subject,omitempty"`
	Body        string                   `json:"body" validate:"required"`
	Description string                   `json:"description,omitempty"`
	Variables   []VariableRequest        `json:"variables,omitempty"`
	Languages   []LanguageVariantRequest `json:"languages,omitempty"`
	Tags        []string                 `json:"tags,omitempty"`
	IsActive    bool                     `json:"is_active"`
}

// VariableRequest represents a template variable in a request
type VariableRequest struct {
	Name         string `json:"name" validate:"required"`
	Type         string `json:"type" validate:"required,oneof=string number boolean date"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"default_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

// LanguageVariantRequest represents a language variant in a request
type LanguageVariantRequest struct {
	LanguageCode string `json:"language_code" validate:"required,min=2,max=10"`
	Subject      string `json:"subject,omitempty"`
	Body         string `json:"body" validate:"required"`
	IsDefault    bool   `json:"is_default"`
}

// RenderTemplateRequest represents a request to render a template
type RenderTemplateRequest struct {
	TemplateName string                 `json:"template_name" validate:"required"`
	LanguageCode string                 `json:"language_code"`
	Data         map[string]interface{} `json:"data"`
}

// RenderedTemplate represents a rendered template result
type RenderedTemplate struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// CreateTemplate creates a new template with all its components
func (s *TemplateService) CreateTemplate(req *CreateTemplateRequest) (*domain.ExtendedNotificationTemplate, error) {
	// Check if template with same name exists
	existing, _ := s.templateRepo.GetTemplateByName(req.Name)
	if existing != nil {
		return nil, errors.NewConflict("template with this name already exists")
	}

	// Create the base template
	template := &domain.ExtendedNotificationTemplate{
		NotificationTemplate: domain.NotificationTemplate{
			Name:        req.Name,
			Type:        req.Type,
			Subject:     req.Subject,
			Body:        req.Body,
			Description: req.Description,
			IsActive:    req.IsActive,
		},
		CategoryID: req.CategoryID,
	}

	// Set tags
	if len(req.Tags) > 0 {
		template.SetTags(req.Tags)
	}

	// Create the template
	if err := s.templateRepo.CreateTemplate(template); err != nil {
		s.logger.Error("Failed to create template", "error", err)
		return nil, errors.NewInternal("failed to create template")
	}

	// Create variables
	for _, varReq := range req.Variables {
		variable := &domain.TemplateVariable{
			TemplateID:   template.ID,
			Name:         varReq.Name,
			Type:         varReq.Type,
			Required:     varReq.Required,
			DefaultValue: varReq.DefaultValue,
			Description:  varReq.Description,
		}
		if err := s.templateRepo.CreateVariable(variable); err != nil {
			s.logger.Error("Failed to create template variable", "error", err, "variable", varReq.Name)
		}
	}

	// Create language variants
	if len(req.Languages) == 0 {
		// Create default English variant
		variant := &domain.TemplateLanguage{
			TemplateID:   template.ID,
			LanguageCode: "en",
			Subject:      req.Subject,
			Body:         req.Body,
			IsDefault:    true,
		}
		if err := s.templateRepo.CreateLanguageVariant(variant); err != nil {
			s.logger.Error("Failed to create default language variant", "error", err)
		}
	} else {
		for _, langReq := range req.Languages {
			variant := &domain.TemplateLanguage{
				TemplateID:   template.ID,
				LanguageCode: langReq.LanguageCode,
				Subject:      langReq.Subject,
				Body:         langReq.Body,
				IsDefault:    langReq.IsDefault,
			}
			if err := s.templateRepo.CreateLanguageVariant(variant); err != nil {
				s.logger.Error("Failed to create language variant", "error", err, "language", langReq.LanguageCode)
			}
		}
	}

	// Reload the template with all relationships
	fullTemplate, err := s.templateRepo.GetTemplateByID(template.ID)
	if err != nil {
		return template, nil
	}

	return fullTemplate, nil
}

// GetTemplate retrieves a template by ID
func (s *TemplateService) GetTemplate(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
	template, err := s.templateRepo.GetTemplateByID(id)
	if err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}
	return template, nil
}

// GetTemplateByName retrieves a template by name
func (s *TemplateService) GetTemplateByName(name string) (*domain.ExtendedNotificationTemplate, error) {
	template, err := s.templateRepo.GetTemplateByName(name)
	if err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}
	return template, nil
}

// UpdateTemplate updates an existing template
func (s *TemplateService) UpdateTemplate(id uuid.UUID, req *CreateTemplateRequest) (*domain.ExtendedNotificationTemplate, error) {
	template, err := s.templateRepo.GetTemplateByID(id)
	if err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}

	// Don't allow updating system templates
	if template.IsSystem {
		return nil, errors.NewForbidden("cannot update system templates")
	}

	// Update basic fields
	template.Name = req.Name
	template.Type = req.Type
	template.Subject = req.Subject
	template.Body = req.Body
	template.Description = req.Description
	template.CategoryID = req.CategoryID
	template.IsActive = req.IsActive
	template.Version++

	// Update tags
	if len(req.Tags) > 0 {
		template.SetTags(req.Tags)
	}

	// Update the template
	if err := s.templateRepo.UpdateTemplate(template); err != nil {
		s.logger.Error("Failed to update template", "error", err)
		return nil, errors.NewInternal("failed to update template")
	}

	return template, nil
}

// DeleteTemplate deletes a template
func (s *TemplateService) DeleteTemplate(id uuid.UUID) error {
	template, err := s.templateRepo.GetTemplateByID(id)
	if err != nil {
		return errors.NewNotFound("template", "template not found")
	}

	// Don't allow deleting system templates
	if template.IsSystem {
		return errors.NewForbidden("cannot delete system templates")
	}

	return s.templateRepo.DeleteTemplate(id)
}

// ListTemplates lists templates with filters
func (s *TemplateService) ListTemplates(filters map[string]interface{}, page, pageSize int) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateRepo.ListTemplates(filters, offset, pageSize)
}

// RenderTemplate renders a template with the provided data
func (s *TemplateService) RenderTemplate(req *RenderTemplateRequest) (*RenderedTemplate, error) {
	// Get the template
	tmpl, err := s.templateRepo.GetTemplateByName(req.TemplateName)
	if err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}

	if !tmpl.IsActive {
		return nil, errors.NewBadRequest("template is not active")
	}

	// Validate required variables
	if err := tmpl.ValidateVariables(req.Data); err != nil {
		return nil, errors.NewBadRequest("missing required template variables")
	}

	// Get the appropriate language variant
	languageCode := req.LanguageCode
	if languageCode == "" {
		languageCode = "en" // Default to English
	}

	variant, err := s.templateRepo.GetLanguageVariant(tmpl.ID, languageCode)
	if err != nil {
		// Fall back to the template's default body
		variant = &domain.TemplateLanguage{
			Subject: tmpl.Subject,
			Body:    tmpl.Body,
		}
	}

	// Add default values for missing variables
	for _, variable := range tmpl.TemplateVariables {
		if _, exists := req.Data[variable.Name]; !exists && variable.DefaultValue != "" {
			req.Data[variable.Name] = variable.DefaultValue
		}
	}

	// Render subject
	renderedSubject, err := s.renderString(variant.Subject, req.Data)
	if err != nil {
		s.logger.Error("Failed to render template subject", "error", err, "template", req.TemplateName)
		return nil, errors.NewInternal("failed to render template subject")
	}

	// Render body
	renderedBody, err := s.renderString(variant.Body, req.Data)
	if err != nil {
		s.logger.Error("Failed to render template body", "error", err, "template", req.TemplateName)
		return nil, errors.NewInternal("failed to render template body")
	}

	// Increment usage count
	go func() {
		if err := s.templateRepo.IncrementUsage(tmpl.ID); err != nil {
			s.logger.Warn("Failed to increment template usage", "error", err, "template", tmpl.ID)
		}
	}()

	return &RenderedTemplate{
		Subject: renderedSubject,
		Body:    renderedBody,
	}, nil
}

// renderString renders a template string with the provided data
func (s *TemplateService) renderString(templateStr string, data map[string]interface{}) (string, error) {
	// Create template with custom functions
	tmpl := template.New("email").Funcs(template.FuncMap{
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"title":      strings.Title,
		"trim":       strings.TrimSpace,
		"capitalize": capitalize,
		"pluralize":  pluralize,
		"formatDate": formatDate,
		"default": func(defaultVal, val interface{}) interface{} {
			if val == nil || val == "" {
				return defaultVal
			}
			return val
		},
	})

	// Parse the template
	tmpl, err := tmpl.Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute the template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// Helper functions for templates
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func formatDate(format string, t interface{}) string {
	// Simple date formatting - extend as needed
	return fmt.Sprintf("%v", t)
}

// CreateCategory creates a new template category
func (s *TemplateService) CreateCategory(name, description string, parentID *uuid.UUID) (*domain.TemplateCategory, error) {
	category := &domain.TemplateCategory{
		Name:        name,
		Description: description,
		ParentID:    parentID,
	}

	if err := s.templateRepo.CreateCategory(category); err != nil {
		s.logger.Error("Failed to create category", "error", err)
		return nil, errors.NewInternal("failed to create category")
	}

	return category, nil
}

// ListCategories lists all template categories
func (s *TemplateService) ListCategories() ([]*domain.TemplateCategory, error) {
	return s.templateRepo.ListCategories()
}

// GetMostUsedTemplates retrieves the most frequently used templates
func (s *TemplateService) GetMostUsedTemplates(limit int) ([]*domain.ExtendedNotificationTemplate, error) {
	return s.templateRepo.GetMostUsedTemplates(limit)
}

// CreateSystemTemplates creates predefined system templates
func (s *TemplateService) CreateSystemTemplates() error {
	systemTemplates := []struct {
		Name        string
		Type        domain.NotificationType
		Subject     string
		Body        string
		Description string
		Variables   []VariableRequest
	}{
		{
			Name:        "user_verification",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Verify Your Email Address",
			Body:        "Hello {{.Username}},\n\nPlease verify your email by clicking the link below:\n{{.VerificationURL}}\n\nThis link will expire in {{.ExpiresIn}}.\n\nBest regards,\n{{.AppName}} Team",
			Description: "Email verification template for new users",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "VerificationURL", Type: "string", Required: true},
				{Name: "ExpiresIn", Type: "string", Required: true, DefaultValue: "24 hours"},
				{Name: "AppName", Type: "string", Required: true},
			},
		},
		{
			Name:        "password_reset",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Reset Your Password",
			Body:        "Hello {{.Username}},\n\nYou requested to reset your password. Click the link below:\n{{.ResetURL}}\n\nThis link will expire in {{.ExpiresIn}}.\n\nIf you didn't request this, please ignore this email.\n\nBest regards,\n{{.AppName}} Team",
			Description: "Password reset request template",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "ResetURL", Type: "string", Required: true},
				{Name: "ExpiresIn", Type: "string", Required: true, DefaultValue: "1 hour"},
				{Name: "AppName", Type: "string", Required: true},
			},
		},
		{
			Name:        "welcome_user",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Welcome to {{.AppName}}!",
			Body:        "Hello {{.Username}},\n\nWelcome to {{.AppName}}! We're excited to have you on board.\n\nTo get started, visit: {{.LoginURL}}\n\nIf you have any questions, don't hesitate to contact us.\n\nBest regards,\n{{.AppName}} Team",
			Description: "Welcome email for new users",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "LoginURL", Type: "string", Required: true},
			},
		},
		{
			Name:        "account_locked",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Account Security Alert",
			Body:        "Hello {{.Username}},\n\nYour account has been temporarily locked due to {{.Reason}}.\n\nTo unlock your account, please {{.Action}}.\n\nIf you didn't attempt to access your account, please contact support immediately.\n\nBest regards,\n{{.AppName}} Security Team",
			Description: "Account locked notification",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "Reason", Type: "string", Required: true},
				{Name: "Action", Type: "string", Required: true, DefaultValue: "reset your password"},
				{Name: "AppName", Type: "string", Required: true},
			},
		},
		{
			Name:        "two_factor_code",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Your Security Code: {{.Code}}",
			Body:        "Hello {{.Username}},\n\nYour security code is: {{.Code}}\n\nThis code will expire in {{.ExpiresIn}}.\n\nIf you didn't request this code, please secure your account immediately.\n\nBest regards,\n{{.AppName}} Security Team",
			Description: "Two-factor authentication code",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "Code", Type: "string", Required: true},
				{Name: "ExpiresIn", Type: "string", Required: true, DefaultValue: "10 minutes"},
				{Name: "AppName", Type: "string", Required: true},
			},
		},
	}

	for _, st := range systemTemplates {
		// Check if template already exists
		existing, _ := s.templateRepo.GetTemplateByName(st.Name)
		if existing != nil {
			continue
		}

		req := &CreateTemplateRequest{
			Name:        st.Name,
			Type:        st.Type,
			Subject:     st.Subject,
			Body:        st.Body,
			Description: st.Description,
			Variables:   st.Variables,
			IsActive:    true,
			Tags:        []string{"system"},
		}

		template, err := s.CreateTemplate(req)
		if err != nil {
			s.logger.Error("Failed to create system template", "error", err, "template", st.Name)
			continue
		}

		// Mark as system template
		template.IsSystem = true
		s.templateRepo.UpdateTemplate(template)
	}

	s.logger.Info("System templates created successfully")
	return nil
}
