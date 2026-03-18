package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"regexp"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// dangerousDirectiveRe matches Go template directives that allow context
// enumeration, arbitrary function calls, or data exfiltration.
// Allowed: {{.Field}}, {{.Field | func}}, {{if .Field}}...{{end}}, {{else}}
// Blocked: {{range}}, {{len}}, {{printf}}, {{call}}, {{template}}, {{define}}, {{block}}, {{with}}
var dangerousDirectiveRe = regexp.MustCompile(`\{\{\s*-?\s*(range|len|printf|call|template|define|block|with)\b`)

// validateTemplateBody checks a template body for dangerous directives that
// could be used for SSTI (Server-Side Template Injection) attacks such as
// context enumeration via {{range}}, data exfiltration via {{printf}}, etc.
func validateTemplateBody(body string) error {
	if match := dangerousDirectiveRe.FindString(body); match != "" {
		return errors.NewBadRequest(
			fmt.Sprintf("template body contains disallowed directive: %s", strings.TrimSpace(match)),
		)
	}
	return nil
}

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
	HTMLContent string                   `json:"html_content,omitempty"` // Full HTML document for email rendering
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
	HTMLContent  string `json:"html_content,omitempty"` // Full HTML override for this language
	IsDefault    bool   `json:"is_default"`
}

// UpdateVariableRequest represents a request to update a template variable
type UpdateVariableRequest struct {
	Name         string `json:"name" validate:"required"`
	Type         string `json:"type" validate:"required,oneof=string number boolean date"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"default_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

// RenderTemplateRequest represents a request to render a template
type RenderTemplateRequest struct {
	TemplateName string                 `json:"template_name" validate:"required"`
	LanguageCode string                 `json:"language_code"`
	Data         map[string]interface{} `json:"data"`
}

// RenderedTemplate represents a rendered template result
type RenderedTemplate struct {
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	HTMLContent string `json:"html_content,omitempty"` // Rendered full HTML if available
}

// CreateTemplate creates a new template with all its components.
// callerID is optional; when non-nil it records the user who created the template.
func (s *TemplateService) CreateTemplate(req *CreateTemplateRequest, callerID ...uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
	// Validate template bodies against SSTI
	for _, body := range []string{req.Body, req.HTMLContent, req.Subject} {
		if err := validateTemplateBody(body); err != nil {
			return nil, err
		}
	}

	// Check if template with same name exists
	existing, _ := s.templateRepo.GetTemplateByName(req.Name)
	if existing != nil {
		return nil, errors.NewConflict("template with this name already exists")
	}

	// Marshal variables to JSON
	variablesJSON := json.RawMessage("[]")
	if len(req.Variables) > 0 {
		if varBytes, err := json.Marshal(req.Variables); err == nil {
			variablesJSON = varBytes
		}
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
			Variables:   variablesJSON,
		},
		CategoryID:  req.CategoryID,
		HTMLContent: req.HTMLContent,
	}

	// Set owner if callerID is provided
	if len(callerID) > 0 && callerID[0] != uuid.Nil {
		id := callerID[0]
		template.CreatedBy = &id
	}

	// Set tags
	if len(req.Tags) > 0 {
		template.SetTags(req.Tags)
	}

	// Create the template
	if err := s.templateRepo.CreateTemplate(template); err != nil {
		s.logger.Error("Failed to create template", "error", err)
		return nil, errors.NewInternalError("failed to create template")
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
			HTMLContent:  req.HTMLContent,
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
				HTMLContent:  langReq.HTMLContent,
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
		s.logger.Warn("Failed to reload template after creation, returning partial", "id", template.ID, "error", err)
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

// UpdateTemplate updates an existing template.
// callerID identifies the user making the request.
// callerRoles is used to enforce that only system_admin can modify system templates
// and that non-system templates can only be modified by the creator or system_admin.
func (s *TemplateService) UpdateTemplate(
	id uuid.UUID, req *CreateTemplateRequest, callerID uuid.UUID, callerRoles []string,
) (*domain.ExtendedNotificationTemplate, error) {
	// Validate template bodies against SSTI
	for _, body := range []string{req.Body, req.HTMLContent, req.Subject} {
		if err := validateTemplateBody(body); err != nil {
			return nil, err
		}
	}

	template, err := s.templateRepo.GetTemplateByID(id)
	if err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}

	// System templates can only be modified by system_admin
	if template.IsSystem && !hasRole(callerRoles, "system_admin") {
		return nil, errors.NewForbidden("Only system_admin can modify system templates")
	}

	// Non-system templates: only creator or system_admin can modify
	if !template.IsSystem && !hasRole(callerRoles, "system_admin") {
		if template.CreatedBy == nil || *template.CreatedBy != callerID {
			return nil, errors.NewForbidden("Only the template creator or system_admin can modify this template")
		}
	}

	// System templates: protect name and system flag
	if template.IsSystem {
		req.Name = template.Name
	}

	// Update basic fields
	template.Name = req.Name
	template.Type = req.Type
	template.Subject = req.Subject
	template.Body = req.Body
	template.HTMLContent = req.HTMLContent
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
		return nil, errors.NewInternalError("failed to update template")
	}

	return template, nil
}

// DeleteTemplate deletes a custom template. System templates cannot be deleted.
// Only the creator or system_admin can delete a non-system template.
func (s *TemplateService) DeleteTemplate(id uuid.UUID, callerID uuid.UUID, callerRoles []string) error {
	template, err := s.templateRepo.GetTemplateByID(id)
	if err != nil {
		return errors.NewNotFound("template", "template not found")
	}

	if template.IsSystem {
		return errors.NewForbidden("system templates cannot be deleted")
	}

	// Only creator or system_admin can delete
	if !hasRole(callerRoles, "system_admin") {
		if template.CreatedBy == nil || *template.CreatedBy != callerID {
			return errors.NewForbidden("Only the template creator or system_admin can delete this template")
		}
	}

	return s.templateRepo.DeleteTemplate(id)
}

// BulkUpdate updates multiple templates, only modifying is_active and category_id fields.
// Returns the count of updated templates, a list of skipped IDs (not found), and any error.
func (s *TemplateService) BulkUpdate(
	templateIDs []uuid.UUID, isActive *bool, categoryID *uuid.UUID,
) (updated int, skipped []uuid.UUID, err error) {
	if len(templateIDs) == 0 {
		return 0, nil, errors.NewBadRequest("template_ids cannot be empty")
	}
	return s.templateRepo.BulkUpdate(templateIDs, isActive, categoryID)
}

// ListTemplates lists templates with filters
func (s *TemplateService) ListTemplates(
	filter repository.ListTemplatesFilter, page, pageSize int,
) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	offset := (page - 1) * pageSize
	return s.templateRepo.ListTemplates(filter, offset, pageSize)
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
			Subject:     tmpl.Subject,
			Body:        tmpl.Body,
			HTMLContent: tmpl.HTMLContent,
		}
	}

	// Guard against nil data map
	if req.Data == nil {
		req.Data = make(map[string]interface{})
	}

	// Add default values for missing variables
	for i := range tmpl.TemplateVariables {
		if _, exists := req.Data[tmpl.TemplateVariables[i].Name]; !exists && tmpl.TemplateVariables[i].DefaultValue != "" {
			req.Data[tmpl.TemplateVariables[i].Name] = tmpl.TemplateVariables[i].DefaultValue
		}
	}

	// Auto-inject Year if not provided
	if _, exists := req.Data["Year"]; !exists {
		req.Data["Year"] = time.Now().Year()
	}

	// Render subject (plain text — no HTML escaping)
	renderedSubject, err := s.renderText(variant.Subject, req.Data)
	if err != nil {
		s.logger.Error("Failed to render template subject", "error", err, "template", req.TemplateName)
		return nil, errors.NewInternalError("failed to render template subject")
	}

	// Render body (plain text — used for SMS, push, plain text email part)
	renderedBody, err := s.renderText(variant.Body, req.Data)
	if err != nil {
		s.logger.Error("Failed to render template body", "error", err, "template", req.TemplateName)
		return nil, errors.NewInternalError("failed to render template body")
	}

	// Render HTML content if available (prefer language variant, fall back to template level)
	// Uses html/template for XSS protection
	htmlContent := variant.HTMLContent
	if htmlContent == "" {
		htmlContent = tmpl.HTMLContent
	}
	var renderedHTML string
	if htmlContent != "" {
		renderedHTML, err = s.renderHTML(htmlContent, req.Data)
		if err != nil {
			s.logger.Error("Failed to render template HTML content", "error", err, "template", req.TemplateName)
			return nil, errors.NewInternalError("failed to render template HTML content")
		}
	}

	// Increment usage count
	go func() {
		if err := s.templateRepo.IncrementUsage(tmpl.ID); err != nil {
			s.logger.Warn("Failed to increment template usage", "error", err, "template", tmpl.ID)
		}
	}()

	return &RenderedTemplate{
		Subject:     renderedSubject,
		Body:        renderedBody,
		HTMLContent: renderedHTML,
	}, nil
}

// templateFuncMap returns the shared template function map used by both text and HTML renderers.
func templateFuncMap() map[string]interface{} {
	return map[string]interface{}{
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"title":      cases.Title(language.English).String,
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
	}
}

// renderText renders a template string using text/template (no HTML escaping).
// Suitable for plain text content: subject lines, SMS, push notifications.
func (s *TemplateService) renderText(templateStr string, data map[string]interface{}) (string, error) {
	tmpl := texttemplate.New("text").Funcs(texttemplate.FuncMap(templateFuncMap()))

	tmpl, err := tmpl.Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Sanitize user-supplied values to prevent template injection
	sanitized := make(map[string]interface{}, len(data))
	for k, v := range data {
		if str, ok := v.(string); ok {
			str = strings.ReplaceAll(str, "{{", "")
			str = strings.ReplaceAll(str, "}}", "")
			sanitized[k] = str
		} else {
			sanitized[k] = v
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, sanitized); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// renderHTML renders a template string using html/template (with XSS protection).
// Suitable for HTML email content where user-supplied variables must be escaped.
func (s *TemplateService) renderHTML(templateStr string, data map[string]interface{}) (string, error) {
	tmpl := htmltemplate.New("html").Funcs(htmltemplate.FuncMap(templateFuncMap()))

	tmpl, err := tmpl.Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

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
		return nil, errors.NewInternalError("failed to create category")
	}

	return category, nil
}

// ListCategories lists all template categories
func (s *TemplateService) ListCategories() ([]*domain.TemplateCategory, error) {
	return s.templateRepo.ListCategories()
}

// GetVariables returns all variables for a template
func (s *TemplateService) GetVariables(templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
	if _, err := s.templateRepo.GetTemplateByID(templateID); err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}
	return s.templateRepo.GetVariables(templateID)
}

// AddVariable adds a variable to a template
func (s *TemplateService) AddVariable(templateID uuid.UUID, req *VariableRequest) (*domain.TemplateVariable, error) {
	if _, err := s.templateRepo.GetTemplateByID(templateID); err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}

	// Check for duplicate variable name
	existing, _ := s.templateRepo.GetVariables(templateID)
	for _, v := range existing {
		if v.Name == req.Name {
			return nil, errors.NewConflict("variable with this name already exists")
		}
	}

	variable := &domain.TemplateVariable{
		TemplateID:   templateID,
		Name:         req.Name,
		Type:         req.Type,
		Required:     req.Required,
		DefaultValue: req.DefaultValue,
		Description:  req.Description,
	}
	if err := s.templateRepo.CreateVariable(variable); err != nil {
		s.logger.Error("Failed to create variable", "error", err)
		return nil, errors.NewInternalError("failed to create variable")
	}
	return variable, nil
}

// UpdateVariable updates an existing template variable
func (s *TemplateService) UpdateVariable(templateID, variableID uuid.UUID, req *UpdateVariableRequest) (*domain.TemplateVariable, error) {
	if _, err := s.templateRepo.GetTemplateByID(templateID); err != nil {
		return nil, errors.NewNotFound("template", "template not found")
	}

	// Verify variable belongs to this template
	vars, _ := s.templateRepo.GetVariables(templateID)
	var target *domain.TemplateVariable
	for _, v := range vars {
		if v.ID == variableID {
			target = v
			break
		}
	}
	if target == nil {
		return nil, errors.NewNotFound("variable", "variable not found")
	}

	target.Name = req.Name
	target.Type = req.Type
	target.Required = req.Required
	target.DefaultValue = req.DefaultValue
	target.Description = req.Description

	if err := s.templateRepo.UpdateVariable(target); err != nil {
		s.logger.Error("Failed to update variable", "error", err)
		return nil, errors.NewInternalError("failed to update variable")
	}
	return target, nil
}

// UpdateCategory updates an existing template category
func (s *TemplateService) UpdateCategory(id uuid.UUID, name, description string) (*domain.TemplateCategory, error) {
	category, err := s.templateRepo.GetCategory(id)
	if err != nil || category == nil {
		return nil, errors.NewNotFound("category", "category not found")
	}

	category.Name = name
	category.Description = description

	if err := s.templateRepo.UpdateCategory(category); err != nil {
		s.logger.Error("Failed to update category", "error", err)
		return nil, errors.NewInternalError("failed to update category")
	}
	return category, nil
}

// DeleteCategory deletes a template category if it's not in use
func (s *TemplateService) DeleteCategory(id uuid.UUID) error {
	category, err := s.templateRepo.GetCategory(id)
	if err != nil || category == nil {
		return errors.NewNotFound("category", "category not found")
	}

	count, err := s.templateRepo.CountTemplatesByCategory(id)
	if err != nil {
		s.logger.Error("Failed to count templates by category", "error", err)
		return errors.NewInternalError("failed to check category usage")
	}
	if count > 0 {
		return errors.NewConflict("category is in use by templates")
	}

	return s.templateRepo.DeleteCategory(id)
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
		HTMLContent string
		Description string
		Variables   []VariableRequest
	}{
		{
			Name:    "user_verification",
			Type:    domain.NotificationTypeEmail,
			Subject: "Verify Your Email Address",
			Body: "Hello {{.Username}},\n\n" +
				"Please verify your email by clicking the link below:\n" +
				"{{.VerificationURL}}\n\n" +
				"This link will expire in {{.ExpiresIn}}.\n\n" +
				"Best regards,\n{{.AppName}} Team",
			HTMLContent: systemHTMLVerification,
			Description: "Email verification template for new users",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "VerificationURL", Type: "string", Required: true},
				{Name: "ExpiresIn", Type: "string", Required: true, DefaultValue: "24 hours"},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "Year", Type: "number", Required: false},
			},
		},
		{
			Name:    "password_reset",
			Type:    domain.NotificationTypeEmail,
			Subject: "Reset Your Password",
			Body: "Hello {{.Username}},\n\n" +
				"You requested to reset your password. Click the link below:\n" +
				"{{.ResetURL}}\n\n" +
				"This link will expire in {{.ExpiresIn}}.\n\n" +
				"If you didn't request this, please ignore this email.\n\n" +
				"Best regards,\n{{.AppName}} Team",
			HTMLContent: systemHTMLPasswordReset,
			Description: "Password reset request template",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "ResetURL", Type: "string", Required: true},
				{Name: "ExpiresIn", Type: "string", Required: true, DefaultValue: "1 hour"},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "Year", Type: "number", Required: false},
			},
		},
		{
			Name:    "welcome_user",
			Type:    domain.NotificationTypeEmail,
			Subject: "Welcome to {{.AppName}}!",
			Body: "Hello {{.Username}},\n\n" +
				"Welcome to {{.AppName}}! We're excited to have you on board.\n\n" +
				"To get started, visit: {{.LoginURL}}\n\n" +
				"If you have any questions, don't hesitate to contact us.\n\n" +
				"Best regards,\n{{.AppName}} Team",
			HTMLContent: systemHTMLWelcome,
			Description: "Welcome email for new users",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "LoginURL", Type: "string", Required: true},
				{Name: "Year", Type: "number", Required: false},
			},
		},
		{
			Name:    "account_locked",
			Type:    domain.NotificationTypeEmail,
			Subject: "Account Security Alert",
			Body: "Hello {{.Username}},\n\n" +
				"Your account has been temporarily locked due to {{.Reason}}.\n\n" +
				"To unlock your account, please {{.Action}}.\n\n" +
				"If you didn't attempt to access your account, " +
				"please contact support immediately.\n\n" +
				"Best regards,\n{{.AppName}} Security Team",
			HTMLContent: systemHTMLAccountLocked,
			Description: "Account locked notification",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "Reason", Type: "string", Required: true},
				{Name: "Action", Type: "string", Required: true, DefaultValue: "reset your password"},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "Year", Type: "number", Required: false},
			},
		},
		{
			Name:    "two_factor_code",
			Type:    domain.NotificationTypeEmail,
			Subject: "Your Security Code: {{.Code}}",
			Body: "Hello {{.Username}},\n\n" +
				"Your security code is: {{.Code}}\n\n" +
				"This code will expire in {{.ExpiresIn}}.\n\n" +
				"If you didn't request this code, " +
				"please secure your account immediately.\n\n" +
				"Best regards,\n{{.AppName}} Security Team",
			HTMLContent: systemHTMLTwoFactor,
			Description: "Two-factor authentication code",
			Variables: []VariableRequest{
				{Name: "Username", Type: "string", Required: true},
				{Name: "Code", Type: "string", Required: true},
				{Name: "ExpiresIn", Type: "string", Required: true, DefaultValue: "10 minutes"},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "Year", Type: "number", Required: false},
			},
		},
		{
			Name:        "notification",
			Type:        domain.NotificationTypeEmail,
			Subject:     "{{.Subject}}",
			Body:        "{{.Message}}",
			HTMLContent: systemHTMLNotification,
			Description: "Generic notification email template",
			Variables: []VariableRequest{
				{Name: "Subject", Type: "string", Required: true},
				{Name: "Message", Type: "string", Required: true},
				{Name: "AppName", Type: "string", Required: true},
				{Name: "Year", Type: "number", Required: false},
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
			HTMLContent: st.HTMLContent,
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
		_ = s.templateRepo.UpdateTemplate(template)
	}

	s.logger.Info("System templates created successfully")
	return nil
}

func hasRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}
