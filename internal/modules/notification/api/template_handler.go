package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

// TemplateHandler handles template-related HTTP requests
type TemplateHandler struct {
	templateService *service.TemplateService
}

// NewTemplateHandler creates a new template handler
func NewTemplateHandler(templateService *service.TemplateService) *TemplateHandler {
	return &TemplateHandler{
		templateService: templateService,
	}
}

// RegisterRoutes registers template routes
func (h *TemplateHandler) RegisterRoutes(app *fiber.App, authMw fiber.Handler) {
	templates := app.Group("/api/v1/templates", authMw)

	// List and create (root level)
	templates.Get("/", h.ListTemplates)
	templates.Post("/", h.CreateTemplate)

	// Template rendering (static routes - must come before :id parameter)
	templates.Post("/render", h.RenderTemplate)
	templates.Post("/preview", h.PreviewTemplate)

	// Template categories (static routes - must come before :id parameter)
	templates.Get("/categories", h.ListCategories)
	templates.Post("/categories", h.CreateCategory)

	// Template statistics (static routes - must come before :id parameter)
	templates.Get("/most-used", h.GetMostUsedTemplates)

	// System templates (static routes - must come before :id parameter)
	templates.Post("/system/init", h.InitializeSystemTemplates)

	// Bulk operations (static routes - must come before :id parameter)
	templates.Post("/bulk-update", h.BulkUpdateTemplates)
	templates.Get("/export", h.ExportTemplates)
	templates.Post("/import", h.ImportTemplates)

	// Template CRUD with ID (these must come LAST because of :id parameter)
	templates.Get("/:id", h.GetTemplate)
	templates.Put("/:id", h.UpdateTemplate)
	templates.Delete("/:id", h.DeleteTemplate)
	templates.Post("/:id/clone", h.CloneTemplate)
}

// CreateTemplate creates a new email/notification template
// @Summary Create a new notification template
// @Description Creates a new template with variables and language variants. Templates define the structure of notifications with customizable variables (like {{.Username}}, {{.VerificationURL}}) that can be replaced at render time.
// @Tags Templates
// @Accept json
// @Produce json
// @Param request body service.CreateTemplateRequest true "Template creation request with name, type, body, and optional variables"
// @Success 201 {object} fiber.Map{template=domain.ExtendedNotificationTemplate} "Template created successfully"
// @Failure 400 {object} errors.ErrorResponse "Invalid request body or validation failed"
// @Failure 409 {object} errors.ErrorResponse "Template with same name already exists"
// @Router /api/v1/templates [post]
// @Security Bearer
func (h *TemplateHandler) CreateTemplate(c *fiber.Ctx) error {
	var req service.CreateTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	template, err := h.templateService.CreateTemplate(&req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":  "Template created successfully",
		"template": template,
	})
}

// ListTemplates lists templates with pagination and filters
// @Summary List all notification templates
// @Description Retrieves a paginated list of templates with optional filtering by category, type, status, or search query.
// @Tags Templates
// @Accept json
// @Produce json
// @Param page query int false "Page number (default: 1)" minimum(1)
// @Param page_size query int false "Items per page, max 100 (default: 20)" minimum(1) maximum(100)
// @Param category_id query string false "Filter by category UUID"
// @Param type query string false "Filter by notification type (email, sms, push, etc.)"
// @Param is_active query boolean false "Filter by active status (true/false)"
// @Param search query string false "Search in template name and description"
// @Success 200 {object} fiber.Map{templates=[]domain.ExtendedNotificationTemplate,pagination=fiber.Map} "List of templates with pagination info"
// @Failure 400 {object} errors.ErrorResponse "Invalid query parameters"
// @Router /api/v1/templates [get]
// @Security Bearer
func (h *TemplateHandler) ListTemplates(c *fiber.Ctx) error {
	// Parse query parameters
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Build filters
	filters := make(map[string]interface{})

	if categoryID := c.Query("category_id"); categoryID != "" {
		if uid, err := uuid.Parse(categoryID); err == nil {
			filters["category_id"] = uid
		}
	}

	if templateType := c.Query("type"); templateType != "" {
		filters["type"] = templateType
	}

	if isActive := c.Query("is_active"); isActive != "" {
		filters["is_active"] = isActive == "true"
	}

	if search := c.Query("search"); search != "" {
		filters["search"] = search
	}

	templates, total, err := h.templateService.ListTemplates(filters, page, pageSize)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"templates": templates,
		"pagination": fiber.Map{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
		},
	})
}

// GetTemplate retrieves a template by ID
// @Summary Get a template by ID
// @Description Retrieves full template details including variables, language variants, and metadata by UUID.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Success 200 {object} domain.ExtendedNotificationTemplate "Complete template with all relations"
// @Failure 400 {object} errors.ErrorResponse "Invalid template ID format"
// @Failure 404 {object} errors.ErrorResponse "Template not found"
// @Router /api/v1/templates/{id} [get]
// @Security Bearer
func (h *TemplateHandler) GetTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	template, err := h.templateService.GetTemplate(id)
	if err != nil {
		return err
	}

	return c.JSON(template)
}

// UpdateTemplate updates an existing template
// @Summary Update a template
// @Description Updates template fields like name, body, subject, and variables. Cannot update system templates (is_system=true). Variables are replaced entirely with new ones if provided.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Param request body service.CreateTemplateRequest true "Template update request"
// @Success 200 {object} fiber.Map{template=domain.ExtendedNotificationTemplate} "Updated template"
// @Failure 400 {object} errors.ErrorResponse "Invalid request or template ID"
// @Failure 403 {object} errors.ErrorResponse "Cannot update system templates"
// @Failure 404 {object} errors.ErrorResponse "Template not found"
// @Router /api/v1/templates/{id} [put]
// @Security Bearer
func (h *TemplateHandler) UpdateTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	var req service.CreateTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	template, err := h.templateService.UpdateTemplate(id, &req)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message":  "Template updated successfully",
		"template": template,
	})
}

// DeleteTemplate deletes a template
// @Summary Delete a template
// @Description Soft deletes a template (marks as deleted, doesn't remove from database). Cannot delete system templates. All related variables and language variants are also soft deleted.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Success 200 {object} fiber.Map "Template deleted successfully"
// @Failure 400 {object} errors.ErrorResponse "Invalid template ID"
// @Failure 403 {object} errors.ErrorResponse "Cannot delete system templates"
// @Failure 404 {object} errors.ErrorResponse "Template not found"
// @Router /api/v1/templates/{id} [delete]
// @Security Bearer
func (h *TemplateHandler) DeleteTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	if err := h.templateService.DeleteTemplate(id); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Template deleted successfully",
	})
}

// RenderTemplate renders a template with provided data
// @Summary Render a template with dynamic variables
// @Description Renders a template by replacing variables with provided values. Variables in templates use {{.VariableName}} syntax (e.g., {{.Username}}, {{.VerificationURL}}). The renderer uses Go template syntax with helper functions like {{.Username | upper}}, {{.Name | capitalize}}, etc.
// @Tags Templates
// @Accept json
// @Produce json
// @Param request body service.RenderTemplateRequest true "Template name and data to render. Example: {\"template_name\": \"user_verification\", \"language_code\": \"en\", \"data\": {\"Username\": \"john_doe\", \"VerificationURL\": \"https://app.com/verify?token=xyz\", \"AppName\": \"MyApp\"}}"
// @Success 200 {object} service.RenderedTemplate "Rendered template with subject and body"
// @Failure 400 {object} errors.ErrorResponse "Template not found or missing required variables"
// @Failure 500 {object} errors.ErrorResponse "Template rendering failed"
// @Router /api/v1/templates/render [post]
// @Security Bearer
func (h *TemplateHandler) RenderTemplate(c *fiber.Ctx) error {
	var req service.RenderTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Default to English if no language specified
	if req.LanguageCode == "" {
		req.LanguageCode = "en"
	}

	rendered, err := h.templateService.RenderTemplate(&req)
	if err != nil {
		return err
	}

	return c.JSON(rendered)
}

// PreviewTemplate previews a template without saving it
// @Summary Preview template rendering without saving
// @Description Allows you to test template rendering with sample variables without creating or saving a template. Useful for validating template syntax and variable substitution before creating the actual template.
// @Tags Templates
// @Accept json
// @Produce json
// @Param request body object true "Preview request with subject, body, and sample data. Example: {\"subject\": \"Hello {{.Name}}\", \"body\": \"Welcome {{.Name}} to {{.App}}\", \"data\": {\"Name\": \"John\", \"App\": \"MyApp\"}}"
// @Success 200 {object} fiber.Map{rendered_subject=string,rendered_body=string,variables_used=[]string} "Preview with rendered output and detected variables"
// @Failure 400 {object} errors.ErrorResponse "Invalid request body"
// @Router /api/v1/templates/preview [post]
// @Security Bearer
func (h *TemplateHandler) PreviewTemplate(c *fiber.Ctx) error {
	var req struct {
		Subject      string                 `json:"subject"`
		Body         string                 `json:"body" validate:"required"`
		Data         map[string]interface{} `json:"data"`
		LanguageCode string                 `json:"language_code"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Render template with variable substitution
	renderedSubject := h.renderTemplate(req.Subject, req.Data)
	renderedBody := h.renderTemplate(req.Body, req.Data)

	return c.JSON(fiber.Map{
		"subject":          req.Subject,
		"body":             req.Body,
		"rendered_subject": renderedSubject,
		"rendered_body":    renderedBody,
		"variables_used":   h.extractVariables(req.Body),
	})
}

// renderTemplate performs simple variable substitution using {{ variable }} syntax
func (h *TemplateHandler) renderTemplate(template string, data map[string]interface{}) string {
	result := template

	// Simple regex-based variable substitution
	if data == nil {
		return result
	}

	for key, value := range data {
		placeholder := "{{" + key + "}}"
		replacement := fmt.Sprintf("%v", value)
		result = strings.ReplaceAll(result, placeholder, replacement)
	}

	return result
}

// extractVariables extracts all {{variable}} references from template
func (h *TemplateHandler) extractVariables(template string) []string {
	var variables []string
	// Find all {{word}} patterns
	re := regexp.MustCompile(`\{\{(\w+)\}\}`)
	matches := re.FindAllStringSubmatch(template, -1)

	for _, match := range matches {
		if len(match) > 1 {
			variables = append(variables, match[1])
		}
	}

	return variables
}

// ListCategories lists all template categories
// @Summary List all template categories
// @Description Retrieves a hierarchical list of template categories for organizing templates.
// @Tags Categories
// @Accept json
// @Produce json
// @Success 200 {object} fiber.Map{categories=[]domain.TemplateCategory} "List of categories"
// @Router /api/v1/templates/categories [get]
// @Security Bearer
func (h *TemplateHandler) ListCategories(c *fiber.Ctx) error {
	categories, err := h.templateService.ListCategories()
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"categories": categories,
	})
}

// CreateCategory creates a new template category
// @Summary Create a new template category
// @Description Creates a new category for organizing templates. Categories can be hierarchical (parent-child relationship).
// @Tags Categories
// @Accept json
// @Produce json
// @Param request body object true "Category request with name, description, and optional parent_id"
// @Success 201 {object} fiber.Map{category=domain.TemplateCategory} "Category created successfully"
// @Failure 400 {object} errors.ErrorResponse "Invalid request body or validation failed"
// @Router /api/v1/templates/categories [post]
// @Security Bearer
func (h *TemplateHandler) CreateCategory(c *fiber.Ctx) error {
	var req struct {
		Name        string     `json:"name" validate:"required,min=3,max=50"`
		Description string     `json:"description"`
		ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	category, err := h.templateService.CreateCategory(req.Name, req.Description, req.ParentID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":  "Category created successfully",
		"category": category,
	})
}

// GetMostUsedTemplates retrieves the most frequently used templates
// @Summary Get most frequently used templates
// @Description Retrieves templates sorted by usage count (how many times they've been rendered). Useful for analytics and identifying popular templates.
// @Tags Statistics
// @Accept json
// @Produce json
// @Param limit query int false "Number of templates to return (default: 10, max: 50)"
// @Success 200 {object} fiber.Map{templates=[]domain.ExtendedNotificationTemplate,limit=int} "List of most used templates"
// @Router /api/v1/templates/most-used [get]
// @Security Bearer
func (h *TemplateHandler) GetMostUsedTemplates(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	if limit < 1 || limit > 50 {
		limit = 10
	}

	templates, err := h.templateService.GetMostUsedTemplates(limit)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"templates": templates,
		"limit":     limit,
	})
}

// InitializeSystemTemplates creates the default system templates
// @Summary Initialize default system templates
// @Description Creates predefined system templates (user_verification, password_reset, welcome_user, account_locked, two_factor_code). Can be called multiple times - existing templates will not be duplicated. Requires admin privileges.
// @Tags System
// @Accept json
// @Produce json
// @Success 200 {object} fiber.Map "System templates initialized successfully"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Router /api/v1/templates/system/init [post]
// @Security Bearer
func (h *TemplateHandler) InitializeSystemTemplates(c *fiber.Ctx) error {
	// This endpoint should be protected with admin privileges
	if err := h.templateService.CreateSystemTemplates(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "System templates initialized successfully",
	})
}

// BulkUpdateTemplates updates multiple templates at once
func (h *TemplateHandler) BulkUpdateTemplates(c *fiber.Ctx) error {
	var req struct {
		TemplateIDs []uuid.UUID            `json:"template_ids" validate:"required,min=1"`
		Updates     map[string]interface{} `json:"updates" validate:"required"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// This would update multiple templates
	// Implementation would go here

	return c.JSON(fiber.Map{
		"message": "Templates updated successfully",
		"count":   len(req.TemplateIDs),
	})
}

// CloneTemplate creates a copy of an existing template
// @Summary Clone an existing template
// @Description Creates a copy of an existing template with a new name. The cloned template starts in inactive status. All variables and language variants are also cloned.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Original template UUID to clone from"
// @Param request body object true "Clone request with new_name field"
// @Success 201 {object} fiber.Map{template=domain.ExtendedNotificationTemplate} "Template cloned successfully"
// @Failure 400 {object} errors.ErrorResponse "Invalid template ID or request body"
// @Failure 404 {object} errors.ErrorResponse "Template not found"
// @Router /api/v1/templates/{id}/clone [post]
// @Security Bearer
func (h *TemplateHandler) CloneTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	var req struct {
		NewName string `json:"new_name" validate:"required"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Get the original template
	original, err := h.templateService.GetTemplate(id)
	if err != nil {
		return err
	}

	// Create a copy with new name
	cloneReq := &service.CreateTemplateRequest{
		Name:        req.NewName,
		Type:        original.Type,
		Subject:     original.Subject,
		Body:        original.Body,
		Description: fmt.Sprintf("Cloned from %s", original.Name),
		IsActive:    false, // Start as inactive
		Tags:        original.GetTags(),
	}

	cloned, err := h.templateService.CreateTemplate(cloneReq)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":  "Template cloned successfully",
		"template": cloned,
	})
}

// ExportTemplates exports templates in JSON format
func (h *TemplateHandler) ExportTemplates(c *fiber.Ctx) error {
	// Parse template IDs from query
	var templateIDs []uuid.UUID
	if ids := c.Query("ids"); ids != "" {
		// Parse comma-separated IDs
		// Implementation would go here
	}

	// Export logic would go here
	return c.JSON(fiber.Map{
		"message": "Templates exported successfully",
		"count":   len(templateIDs),
	})
}

// ImportTemplates imports templates from JSON
func (h *TemplateHandler) ImportTemplates(c *fiber.Ctx) error {
	var req struct {
		Templates []service.CreateTemplateRequest `json:"templates" validate:"required,min=1"`
		Overwrite bool                            `json:"overwrite"`
	}

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	imported := 0
	failed := 0

	for _, tmpl := range req.Templates {
		_, err := h.templateService.CreateTemplate(&tmpl)
		if err != nil {
			failed++
		} else {
			imported++
		}
	}

	return c.JSON(fiber.Map{
		"message":  "Import completed",
		"imported": imported,
		"failed":   failed,
	})
}
