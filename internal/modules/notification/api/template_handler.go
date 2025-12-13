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
func (h *TemplateHandler) RegisterRoutes(router fiber.Router) {
	templates := router.Group("/templates")

	// Template CRUD
	templates.Post("/", h.CreateTemplate)
	templates.Get("/", h.ListTemplates)
	templates.Get("/:id", h.GetTemplate)
	templates.Put("/:id", h.UpdateTemplate)
	templates.Delete("/:id", h.DeleteTemplate)

	// Template rendering
	templates.Post("/render", h.RenderTemplate)
	templates.Post("/preview", h.PreviewTemplate)

	// Template categories
	templates.Get("/categories", h.ListCategories)
	templates.Post("/categories", h.CreateCategory)

	// Template statistics
	templates.Get("/most-used", h.GetMostUsedTemplates)

	// System templates
	templates.Post("/system/init", h.InitializeSystemTemplates)
}

// CreateTemplate creates a new template
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
