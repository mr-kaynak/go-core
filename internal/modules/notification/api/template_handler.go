package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

// swag annotation type references
var _ *domain.ExtendedNotificationTemplate

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

// CreateCategoryRequest represents a category creation request
type CreateCategoryRequest struct {
	Name        string     `json:"name" validate:"required,min=3,max=50"`
	Description string     `json:"description"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
}

// BulkUpdateTemplatesRequest represents a bulk template update request
type BulkUpdateTemplatesRequest struct {
	TemplateIDs []uuid.UUID `json:"template_ids" validate:"required,min=1"`
	IsActive    *bool       `json:"is_active,omitempty"`
	CategoryID  *uuid.UUID  `json:"category_id,omitempty"`
}

// CloneTemplateRequest represents a template clone request
type CloneTemplateRequest struct {
	NewName string `json:"new_name" validate:"required"`
}

// ImportTemplatesRequest represents a template import request
type ImportTemplatesRequest struct {
	Templates []service.CreateTemplateRequest `json:"templates" validate:"required,min=1"`
	Overwrite bool                            `json:"overwrite"`
}

// ListTemplatesResponse is the standardized paginated response for templates.
type ListTemplatesResponse struct {
	Items      []*domain.ExtendedNotificationTemplate `json:"items"`
	Pagination apiresponse.Pagination                 `json:"pagination"`
}

// TemplateResponse is the response for template operations.
type TemplateResponse struct {
	Message  string                               `json:"message"`
	Template *domain.ExtendedNotificationTemplate `json:"template"`
}

// CategoryResponse is the response for category operations.
type CategoryResponse struct {
	Message  string                   `json:"message"`
	Category *domain.TemplateCategory `json:"category"`
}

// CategoriesResponse is the response for listing categories.
type CategoriesResponse struct {
	Categories []*domain.TemplateCategory `json:"categories"`
}

// VariableResponse is the response for variable operations.
type VariableResponse struct {
	Message  string                   `json:"message"`
	Variable *domain.TemplateVariable `json:"variable"`
}

// VariablesResponse is the response for listing variables.
type VariablesResponse struct {
	Variables []*domain.TemplateVariable `json:"variables"`
}

// MostUsedTemplatesResponse is the response for most used templates.
type MostUsedTemplatesResponse struct {
	Templates []*domain.ExtendedNotificationTemplate `json:"templates"`
	Limit     int                                    `json:"limit"`
}

// ImportTemplatesResponse is the response for template import.
type ImportTemplatesResponse struct {
	Message  string `json:"message"`
	Imported int    `json:"imported"`
	Failed   int    `json:"failed"`
}

// RegisterRoutes registers template routes
func (h *TemplateHandler) RegisterRoutes(app *fiber.App, authMw fiber.Handler) {
	templates := app.Group("/api/v1/templates", authMw)

	// List and create (root level)
	templates.Get("/", h.ListTemplates)
	templates.Post("/", h.CreateTemplate)

	// Template rendering (static routes - must come before :id parameter)
	templates.Post("/render", h.RenderTemplate)

	// Template categories (static routes - must come before :id parameter)
	templates.Get("/categories", h.ListCategories)
	templates.Post("/categories", h.CreateCategory)
	templates.Put("/categories/:id", h.UpdateCategory)
	templates.Delete("/categories/:id", h.DeleteCategory)

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
	templates.Get("/:id/variables", h.GetVariables)
	templates.Post("/:id/variables", h.AddVariable)
	templates.Put("/:id/variables/:varId", h.UpdateVariable)
	templates.Post("/:id/clone", h.CloneTemplate)
}

// CreateTemplate creates a new email/notification template
// @Summary Create a new notification template
// @Description Creates a new template with variables and language variants. Templates define the structure of notifications with customizable variables (like {{.Username}}, {{.VerificationURL}}) that can be replaced at render time.
// @Tags Templates
// @Accept json
// @Produce json
// @Param request body service.CreateTemplateRequest true "Template creation request with name, type, body, and optional variables"
// @Success 201 {object} TemplateResponse "Template created successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid request body or validation failed"
// @Failure 409 {object} errors.ProblemDetail "Template with same name already exists"
// @Router /templates [post]
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
// @Success 200 {object} ListTemplatesResponse "List of templates with pagination info"
// @Failure 400 {object} errors.ProblemDetail "Invalid query parameters"
// @Router /templates [get]
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

	return c.JSON(apiresponse.NewPaginatedResponse(templates, page, pageSize, total))
}

// GetTemplate retrieves a template by ID
// @Summary Get a template by ID
// @Description Retrieves full template details including variables, language variants, and metadata by UUID.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Success 200 {object} domain.ExtendedNotificationTemplate "Complete template with all relations"
// @Failure 400 {object} errors.ProblemDetail "Invalid template ID format"
// @Failure 404 {object} errors.ProblemDetail "Template not found"
// @Router /templates/{id} [get]
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
// @Success 200 {object} TemplateResponse "Updated template"
// @Failure 400 {object} errors.ProblemDetail "Invalid request or template ID"
// @Failure 403 {object} errors.ProblemDetail "Cannot update system templates"
// @Failure 404 {object} errors.ProblemDetail "Template not found"
// @Router /templates/{id} [put]
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

// DeleteTemplate deletes a custom template (system templates cannot be deleted)
// @Summary Delete a template
// @Description Soft deletes a custom template. System templates (is_system=true) cannot be deleted.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Success 200 {object} MessageResponse "Template deleted successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid template ID"
// @Failure 403 {object} errors.ProblemDetail "Cannot delete system templates"
// @Failure 404 {object} errors.ProblemDetail "Template not found"
// @Router /templates/{id} [delete]
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
// @Description Renders a template by replacing variables with provided values. Variables in templates use {{.VariableName}} syntax (e.g., {{.Username}}, {{.VerificationURL}}). The renderer uses Go template syntax with helper functions like {{.Username | upper}}, {{.Name | capitalize}}, etc. If the template has html_content, the rendered HTML is also returned.
// @Tags Templates
// @Accept json
// @Produce json
// @Param request body service.RenderTemplateRequest true "Template name and data to render. Example: {\"template_name\": \"user_verification\", \"language_code\": \"en\", \"data\": {\"Username\": \"john_doe\", \"VerificationURL\": \"https://app.com/verify?token=xyz\", \"AppName\": \"MyApp\"}}"
// @Success 200 {object} service.RenderedTemplate "Rendered template with subject, body, and optional html_content"
// @Failure 400 {object} errors.ProblemDetail "Template not found or missing required variables"
// @Failure 500 {object} errors.ProblemDetail "Template rendering failed"
// @Router /templates/render [post]
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

// ListCategories lists all template categories
// @Summary List all template categories
// @Description Retrieves a hierarchical list of template categories for organizing templates.
// @Tags Categories
// @Accept json
// @Produce json
// @Success 200 {object} CategoriesResponse "List of categories"
// @Router /templates/categories [get]
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
// @Param request body CreateCategoryRequest true "Category request with name, description, and optional parent_id"
// @Success 201 {object} CategoryResponse "Category created successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid request body or validation failed"
// @Router /templates/categories [post]
// @Security Bearer
func (h *TemplateHandler) CreateCategory(c *fiber.Ctx) error {
	var req CreateCategoryRequest

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

// UpdateCategory updates an existing template category
// @Summary Update a template category
// @Description Updates an existing template category's name and description.
// @Tags Categories
// @Accept json
// @Produce json
// @Param id path string true "Category UUID"
// @Param request body CreateCategoryRequest true "Category update request"
// @Success 200 {object} CategoryResponse "Category updated successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid category ID or request body"
// @Failure 404 {object} errors.ProblemDetail "Category not found"
// @Router /templates/categories/{id} [put]
// @Security Bearer
func (h *TemplateHandler) UpdateCategory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid category ID")
	}

	var req CreateCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	category, err := h.templateService.UpdateCategory(id, req.Name, req.Description)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message":  "Category updated successfully",
		"category": category,
	})
}

// DeleteCategory deletes a template category
// @Summary Delete a template category
// @Description Deletes a template category if no templates are using it.
// @Tags Categories
// @Accept json
// @Produce json
// @Param id path string true "Category UUID"
// @Success 200 {object} MessageResponse "Category deleted successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid category ID"
// @Failure 404 {object} errors.ProblemDetail "Category not found"
// @Failure 409 {object} errors.ProblemDetail "Category is in use"
// @Router /templates/categories/{id} [delete]
// @Security Bearer
func (h *TemplateHandler) DeleteCategory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid category ID")
	}

	if err := h.templateService.DeleteCategory(id); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Category deleted successfully",
	})
}

// GetVariables returns all variables for a template
// @Summary List template variables
// @Description Retrieves all variables defined for a specific template.
// @Tags Variables
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Success 200 {object} VariablesResponse "List of template variables"
// @Failure 400 {object} errors.ProblemDetail "Invalid template ID"
// @Failure 404 {object} errors.ProblemDetail "Template not found"
// @Router /templates/{id}/variables [get]
// @Security Bearer
func (h *TemplateHandler) GetVariables(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	variables, err := h.templateService.GetVariables(id)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"variables": variables,
	})
}

// AddVariable adds a variable to a template
// @Summary Add a variable to a template
// @Description Creates a new variable for a template. Cannot add variables to system templates.
// @Tags Variables
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Param request body service.VariableRequest true "Variable creation request"
// @Success 201 {object} VariableResponse "Variable created successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid template ID or request body"
// @Failure 403 {object} errors.ProblemDetail "Cannot modify system templates"
// @Failure 404 {object} errors.ProblemDetail "Template not found"
// @Failure 409 {object} errors.ProblemDetail "Variable with same name exists"
// @Router /templates/{id}/variables [post]
// @Security Bearer
func (h *TemplateHandler) AddVariable(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	var req service.VariableRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	variable, err := h.templateService.AddVariable(id, &req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":  "Variable added successfully",
		"variable": variable,
	})
}

// UpdateVariable updates a template variable
// @Summary Update a template variable
// @Description Updates an existing variable for a template. Cannot modify system template variables.
// @Tags Variables
// @Accept json
// @Produce json
// @Param id path string true "Template UUID"
// @Param varId path string true "Variable UUID"
// @Param request body service.UpdateVariableRequest true "Variable update request"
// @Success 200 {object} VariableResponse "Variable updated successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid ID or request body"
// @Failure 403 {object} errors.ProblemDetail "Cannot modify system templates"
// @Failure 404 {object} errors.ProblemDetail "Template or variable not found"
// @Router /templates/{id}/variables/{varId} [put]
// @Security Bearer
func (h *TemplateHandler) UpdateVariable(c *fiber.Ctx) error {
	templateID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	varID, err := uuid.Parse(c.Params("varId"))
	if err != nil {
		return errors.NewBadRequest("Invalid variable ID")
	}

	var req service.UpdateVariableRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	variable, err := h.templateService.UpdateVariable(templateID, varID, &req)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message":  "Variable updated successfully",
		"variable": variable,
	})
}

// GetMostUsedTemplates retrieves the most frequently used templates
// @Summary Get most frequently used templates
// @Description Retrieves templates sorted by usage count (how many times they've been rendered). Useful for analytics and identifying popular templates.
// @Tags Statistics
// @Accept json
// @Produce json
// @Param limit query int false "Number of templates to return (default: 10, max: 50)"
// @Success 200 {object} MostUsedTemplatesResponse "List of most used templates"
// @Router /templates/most-used [get]
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
// @Success 200 {object} MessageResponse "System templates initialized successfully"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /templates/system/init [post]
// @Security Bearer
func (h *TemplateHandler) InitializeSystemTemplates(c *fiber.Ctx) error {
	// Enforce admin role check
	roles, _ := c.Locals("roles").([]string)
	isAdmin := false
	for _, role := range roles {
		if role == roleAdmin || role == roleSystemAdmin {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return errors.NewForbidden("Admin access required")
	}

	if err := h.templateService.CreateSystemTemplates(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "System templates initialized successfully",
	})
}

// BulkUpdateTemplates updates multiple templates at once
// @Summary Bulk update templates
// @Description Update multiple templates at once
// @Tags Templates
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body BulkUpdateTemplatesRequest true "Bulk update request"
// @Success 200 {object} MessageResponse "Templates updated"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /templates/bulk-update [post]
func (h *TemplateHandler) BulkUpdateTemplates(c *fiber.Ctx) error {
	var req BulkUpdateTemplatesRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.TemplateIDs) == 0 {
		return errors.NewValidationError("template_ids cannot be empty")
	}

	// Validate all UUIDs
	for _, id := range req.TemplateIDs {
		if id == uuid.Nil {
			return errors.NewValidationError(fmt.Sprintf("Invalid template ID: %s", id.String()))
		}
	}

	updated, skipped, err := h.templateService.BulkUpdate(req.TemplateIDs, req.IsActive, req.CategoryID)
	if err != nil {
		return errors.NewInternalError("Failed to bulk update templates")
	}

	return c.JSON(fiber.Map{
		"message": "Templates updated successfully",
		"updated": updated,
		"skipped": skipped,
	})
}

// CloneTemplate creates a copy of an existing template
// @Summary Clone an existing template
// @Description Creates a copy of an existing template with a new name. The cloned template starts in inactive status. All variables and language variants are also cloned.
// @Tags Templates
// @Accept json
// @Produce json
// @Param id path string true "Original template UUID to clone from"
// @Param request body CloneTemplateRequest true "Clone request with new_name field"
// @Success 201 {object} TemplateResponse "Template cloned successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid template ID or request body"
// @Failure 404 {object} errors.ProblemDetail "Template not found"
// @Router /templates/{id}/clone [post]
// @Security Bearer
func (h *TemplateHandler) CloneTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid template ID")
	}

	var req CloneTemplateRequest

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
		HTMLContent: original.HTMLContent,
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
// @Summary Export templates
// @Description Export templates in JSON format
// @Tags Templates
// @Security Bearer
// @Produce json
// @Param ids query string false "Comma-separated template UUIDs"
// @Success 200 {object} MessageResponse "Exported templates"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /templates/export [get]
func (h *TemplateHandler) ExportTemplates(c *fiber.Ctx) error {
	idsParam := strings.TrimSpace(c.Query("ids"))

	var templates []*domain.ExtendedNotificationTemplate

	if idsParam == "" {
		// No ids specified - export all templates
		const maxTemplateExportLimit = 10000
		allTemplates, _, err := h.templateService.ListTemplates(nil, 1, maxTemplateExportLimit)
		if err != nil {
			return err
		}
		templates = allTemplates
	} else {
		// Parse comma-separated UUIDs
		idParts := strings.Split(idsParam, ",")
		for _, idStr := range idParts {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}
			parsedID, err := uuid.Parse(idStr)
			if err != nil {
				return errors.NewBadRequest(fmt.Sprintf("Invalid UUID format: %s", idStr))
			}
			tmpl, err := h.templateService.GetTemplate(parsedID)
			if err != nil {
				return err
			}
			templates = append(templates, tmpl)
		}
	}

	c.Set("Content-Disposition", "attachment; filename=templates_export.json")

	return c.JSON(fiber.Map{
		"templates": templates,
		"count":     len(templates),
	})
}

// ImportTemplates imports templates from JSON
// @Summary Import templates
// @Description Import templates from JSON
// @Tags Templates
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body ImportTemplatesRequest true "Templates to import"
// @Success 200 {object} ImportTemplatesResponse "Import result"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /templates/import [post]
func (h *TemplateHandler) ImportTemplates(c *fiber.Ctx) error {
	var req ImportTemplatesRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	imported := 0
	failed := 0

	for i := range req.Templates {
		_, err := h.templateService.CreateTemplate(&req.Templates[i])
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
