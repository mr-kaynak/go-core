package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

func newTemplateHandlerTestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

func doTemplateReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func newTemplateHandlerForTest() *TemplateHandler {
	repo := newTemplateRepoStubForAPI()
	svc := service.NewTemplateService(repo)
	return NewTemplateHandler(svc)
}

type templateRepoStubForAPI struct {
	*templateRepoStub
}

func newTemplateRepoStubForAPI() *templateRepoStubForAPI {
	base := &templateRepoStub{
		templates: map[uuid.UUID]*domain.ExtendedNotificationTemplate{},
		byName:    map[string]*domain.ExtendedNotificationTemplate{},
	}
	return &templateRepoStubForAPI{templateRepoStub: base}
}

// local minimal copy for api package
type templateRepoStub struct {
	templates map[uuid.UUID]*domain.ExtendedNotificationTemplate
	byName    map[string]*domain.ExtendedNotificationTemplate
}

func (s *templateRepoStub) CreateTemplate(template *domain.ExtendedNotificationTemplate) error {
	if template.ID == uuid.Nil {
		template.ID = uuid.New()
	}
	s.templates[template.ID] = template
	s.byName[template.Name] = template
	return nil
}
func (s *templateRepoStub) GetTemplateByID(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
	v, ok := s.templates[id]
	if !ok {
		return nil, coreerrors.NewNotFound("template", id.String())
	}
	return v, nil
}
func (s *templateRepoStub) GetTemplateByName(name string) (*domain.ExtendedNotificationTemplate, error) {
	v, ok := s.byName[name]
	if !ok {
		return nil, coreerrors.NewNotFound("template", name)
	}
	return v, nil
}
func (s *templateRepoStub) UpdateTemplate(template *domain.ExtendedNotificationTemplate) error {
	s.templates[template.ID] = template
	s.byName[template.Name] = template
	return nil
}
func (s *templateRepoStub) DeleteTemplate(id uuid.UUID) error { delete(s.templates, id); return nil }
func (s *templateRepoStub) ListTemplates(filters map[string]interface{}, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	_ = filters
	_ = offset
	_ = limit
	arr := make([]*domain.ExtendedNotificationTemplate, 0, len(s.templates))
	for _, v := range s.templates {
		arr = append(arr, v)
	}
	return arr, int64(len(arr)), nil
}
func (s *templateRepoStub) CreateLanguageVariant(variant *domain.TemplateLanguage) error {
	_ = variant
	return nil
}
func (s *templateRepoStub) GetLanguageVariant(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error) {
	return nil, coreerrors.NewNotFound("lang", languageCode)
}
func (s *templateRepoStub) UpdateLanguageVariant(variant *domain.TemplateLanguage) error {
	_ = variant
	return nil
}
func (s *templateRepoStub) DeleteLanguageVariant(id uuid.UUID) error { _ = id; return nil }
func (s *templateRepoStub) CreateVariable(variable *domain.TemplateVariable) error {
	_ = variable
	return nil
}
func (s *templateRepoStub) GetVariables(templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
	_ = templateID
	return nil, nil
}
func (s *templateRepoStub) UpdateVariable(variable *domain.TemplateVariable) error {
	_ = variable
	return nil
}
func (s *templateRepoStub) DeleteVariable(id uuid.UUID) error { _ = id; return nil }
func (s *templateRepoStub) CreateCategory(category *domain.TemplateCategory) error {
	if strings.EqualFold(category.Name, "existing") {
		return coreerrors.NewConflict("category exists")
	}
	return nil
}
func (s *templateRepoStub) GetCategory(id uuid.UUID) (*domain.TemplateCategory, error) {
	_ = id
	return nil, nil
}
func (s *templateRepoStub) ListCategories() ([]*domain.TemplateCategory, error) {
	return []*domain.TemplateCategory{{ID: uuid.New(), Name: "auth"}}, nil
}
func (s *templateRepoStub) UpdateCategory(category *domain.TemplateCategory) error {
	_ = category
	return nil
}
func (s *templateRepoStub) DeleteCategory(id uuid.UUID) error         { _ = id; return nil }
func (s *templateRepoStub) CountTemplatesByCategory(categoryID uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *templateRepoStub) IncrementUsage(templateID uuid.UUID) error { _ = templateID; return nil }
func (s *templateRepoStub) GetMostUsedTemplates(limit int) ([]*domain.ExtendedNotificationTemplate, error) {
	_ = limit
	return []*domain.ExtendedNotificationTemplate{}, nil
}

func TestTemplateHandlerCRUDAndCategoryEndpoints(t *testing.T) {
	h := newTemplateHandlerForTest()
	app := newTemplateHandlerTestApp()
	app.Post("/templates", h.CreateTemplate)
	app.Get("/templates", h.ListTemplates)
	app.Get("/templates/categories", h.ListCategories)
	app.Post("/templates/categories", h.CreateCategory)
	app.Get("/templates/:id", h.GetTemplate)
	app.Put("/templates/:id", h.UpdateTemplate)
	app.Delete("/templates/:id", h.DeleteTemplate)

	invalid := doTemplateReq(t, app, http.MethodPost, "/templates", "{invalid")
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d", invalid.StatusCode)
	}

	create := doTemplateReq(t, app, http.MethodPost, "/templates",
		`{"name":"welcome","type":"email","subject":"Hi {{.Name}}","body":"Body {{.Name}}","is_active":true}`)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for create, got %d", create.StatusCode)
	}

	list := doTemplateReq(t, app, http.MethodGet, "/templates?page=1&page_size=10", "")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for list, got %d", list.StatusCode)
	}

	categories := doTemplateReq(t, app, http.MethodGet, "/templates/categories", "")
	if categories.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for list categories, got %d", categories.StatusCode)
	}
}

func TestTemplateHandlerVariableEndpoints(t *testing.T) {
	h := newTemplateHandlerForTest()
	app := newTemplateHandlerTestApp()
	app.Get("/templates/:id/variables", h.GetVariables)
	app.Post("/templates/:id/variables", h.AddVariable)
	app.Put("/templates/:id/variables/:varId", h.UpdateVariable)

	// Invalid template ID for GET variables
	resp := doTemplateReq(t, app, http.MethodGet, "/templates/not-uuid/variables", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", resp.StatusCode)
	}

	// Invalid body for POST variable
	resp = doTemplateReq(t, app, http.MethodPost, "/templates/"+uuid.New().String()+"/variables", "{bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d", resp.StatusCode)
	}

	// Invalid variable ID for PUT
	resp = doTemplateReq(t, app, http.MethodPut, "/templates/"+uuid.New().String()+"/variables/not-uuid", `{"name":"x","type":"string"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid varId, got %d", resp.StatusCode)
	}
}

func TestTemplateHandlerCategoryUpdateDeleteEndpoints(t *testing.T) {
	h := newTemplateHandlerForTest()
	app := newTemplateHandlerTestApp()
	app.Put("/templates/categories/:id", h.UpdateCategory)
	app.Delete("/templates/categories/:id", h.DeleteCategory)

	// Invalid category ID for PUT
	resp := doTemplateReq(t, app, http.MethodPut, "/templates/categories/not-uuid", `{"name":"test"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", resp.StatusCode)
	}

	// Invalid category ID for DELETE
	resp = doTemplateReq(t, app, http.MethodDelete, "/templates/categories/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", resp.StatusCode)
	}
}

func TestTemplateHandlerValidationAndCategoryConflict(t *testing.T) {
	h := newTemplateHandlerForTest()
	app := newTemplateHandlerTestApp()
	app.Get("/templates/:id", h.GetTemplate)
	app.Post("/templates/categories", h.CreateCategory)

	badID := doTemplateReq(t, app, http.MethodGet, "/templates/not-uuid", "")
	if badID.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", badID.StatusCode)
	}

	conflict := doTemplateReq(t, app, http.MethodPost, "/templates/categories",
		`{"name":"existing","description":"duplicate"}`)
	if conflict.StatusCode != http.StatusInternalServerError {
		// current service maps category create failures to internal errors
		t.Fatalf("expected 500 for category create failure, got %d", conflict.StatusCode)
	}
}
