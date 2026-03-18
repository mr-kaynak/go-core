package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
)

type templateRepoStub struct {
	templates map[uuid.UUID]*domain.ExtendedNotificationTemplate
	byName    map[string]*domain.ExtendedNotificationTemplate

	createTemplateFn           func(template *domain.ExtendedNotificationTemplate) error
	getByIDFn                  func(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error)
	getByNameFn                func(name string) (*domain.ExtendedNotificationTemplate, error)
	updateTemplateFn           func(template *domain.ExtendedNotificationTemplate) error
	deleteTemplateFn           func(id uuid.UUID) error
	listTemplatesFn            func(filter repository.ListTemplatesFilter, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error)
	createLangFn               func(variant *domain.TemplateLanguage) error
	getLangFn                  func(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error)
	createVariableFn           func(variable *domain.TemplateVariable) error
	getVariablesFn             func(templateID uuid.UUID) ([]*domain.TemplateVariable, error)
	updateVariableFn           func(variable *domain.TemplateVariable) error
	createCategoryFn           func(category *domain.TemplateCategory) error
	getCategoryFn              func(id uuid.UUID) (*domain.TemplateCategory, error)
	updateCategoryFn           func(category *domain.TemplateCategory) error
	deleteCategoryFn           func(id uuid.UUID) error
	countTemplatesByCategoryFn func(categoryID uuid.UUID) (int64, error)
	listCategoriesFn           func() ([]*domain.TemplateCategory, error)
	incrementUsageFn           func(templateID uuid.UUID) error
	getMostUsedFn              func(limit int) ([]*domain.ExtendedNotificationTemplate, error)
}

var _ repository.TemplateRepository = (*templateRepoStub)(nil)

func newTemplateRepoStub() *templateRepoStub {
	return &templateRepoStub{
		templates: make(map[uuid.UUID]*domain.ExtendedNotificationTemplate),
		byName:    make(map[string]*domain.ExtendedNotificationTemplate),
	}
}

func (s *templateRepoStub) CreateTemplate(template *domain.ExtendedNotificationTemplate) error {
	if s.createTemplateFn != nil {
		return s.createTemplateFn(template)
	}
	if template.ID == uuid.Nil {
		template.ID = uuid.New()
	}
	s.templates[template.ID] = template
	s.byName[template.Name] = template
	return nil
}
func (s *templateRepoStub) GetTemplateByID(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	v, ok := s.templates[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}
func (s *templateRepoStub) GetTemplateByName(name string) (*domain.ExtendedNotificationTemplate, error) {
	if s.getByNameFn != nil {
		return s.getByNameFn(name)
	}
	v, ok := s.byName[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}
func (s *templateRepoStub) UpdateTemplate(template *domain.ExtendedNotificationTemplate) error {
	if s.updateTemplateFn != nil {
		return s.updateTemplateFn(template)
	}
	s.templates[template.ID] = template
	s.byName[template.Name] = template
	return nil
}
func (s *templateRepoStub) DeleteTemplate(id uuid.UUID) error {
	if s.deleteTemplateFn != nil {
		return s.deleteTemplateFn(id)
	}
	delete(s.templates, id)
	return nil
}
func (s *templateRepoStub) ListTemplates(filter repository.ListTemplatesFilter, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	if s.listTemplatesFn != nil {
		return s.listTemplatesFn(filter, offset, limit)
	}
	list := make([]*domain.ExtendedNotificationTemplate, 0, len(s.templates))
	for _, t := range s.templates {
		list = append(list, t)
	}
	return list, int64(len(list)), nil
}
func (s *templateRepoStub) CreateLanguageVariant(variant *domain.TemplateLanguage) error {
	if s.createLangFn != nil {
		return s.createLangFn(variant)
	}
	return nil
}
func (s *templateRepoStub) GetLanguageVariant(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error) {
	if s.getLangFn != nil {
		return s.getLangFn(templateID, languageCode)
	}
	return nil, errors.New("not found")
}
func (s *templateRepoStub) UpdateLanguageVariant(variant *domain.TemplateLanguage) error {
	_ = variant
	return nil
}
func (s *templateRepoStub) DeleteLanguageVariant(id uuid.UUID) error {
	_ = id
	return nil
}
func (s *templateRepoStub) CreateVariable(variable *domain.TemplateVariable) error {
	if s.createVariableFn != nil {
		return s.createVariableFn(variable)
	}
	return nil
}
func (s *templateRepoStub) GetVariables(templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
	if s.getVariablesFn != nil {
		return s.getVariablesFn(templateID)
	}
	return nil, nil
}
func (s *templateRepoStub) UpdateVariable(variable *domain.TemplateVariable) error {
	if s.updateVariableFn != nil {
		return s.updateVariableFn(variable)
	}
	return nil
}
func (s *templateRepoStub) DeleteVariable(id uuid.UUID) error {
	_ = id
	return nil
}
func (s *templateRepoStub) CreateCategory(category *domain.TemplateCategory) error {
	if s.createCategoryFn != nil {
		return s.createCategoryFn(category)
	}
	return nil
}
func (s *templateRepoStub) GetCategory(id uuid.UUID) (*domain.TemplateCategory, error) {
	if s.getCategoryFn != nil {
		return s.getCategoryFn(id)
	}
	return nil, nil
}
func (s *templateRepoStub) ListCategories() ([]*domain.TemplateCategory, error) {
	if s.listCategoriesFn != nil {
		return s.listCategoriesFn()
	}
	return []*domain.TemplateCategory{}, nil
}
func (s *templateRepoStub) UpdateCategory(category *domain.TemplateCategory) error {
	if s.updateCategoryFn != nil {
		return s.updateCategoryFn(category)
	}
	return nil
}
func (s *templateRepoStub) DeleteCategory(id uuid.UUID) error {
	if s.deleteCategoryFn != nil {
		return s.deleteCategoryFn(id)
	}
	return nil
}
func (s *templateRepoStub) CountTemplatesByCategory(categoryID uuid.UUID) (int64, error) {
	if s.countTemplatesByCategoryFn != nil {
		return s.countTemplatesByCategoryFn(categoryID)
	}
	return 0, nil
}
func (s *templateRepoStub) IncrementUsage(templateID uuid.UUID) error {
	if s.incrementUsageFn != nil {
		return s.incrementUsageFn(templateID)
	}
	return nil
}
func (s *templateRepoStub) GetMostUsedTemplates(limit int) ([]*domain.ExtendedNotificationTemplate, error) {
	if s.getMostUsedFn != nil {
		return s.getMostUsedFn(limit)
	}
	return nil, nil
}
func (s *templateRepoStub) BulkUpdate(templateIDs []uuid.UUID, isActive *bool, categoryID *uuid.UUID) (int, []uuid.UUID, error) {
	var updated int
	var skipped []uuid.UUID
	for _, id := range templateIDs {
		if _, ok := s.templates[id]; !ok {
			skipped = append(skipped, id)
			continue
		}
		t := s.templates[id]
		if isActive != nil {
			t.IsActive = *isActive
		}
		if categoryID != nil {
			t.CategoryID = categoryID
		}
		s.templates[id] = t
		updated++
	}
	return updated, skipped, nil
}

func TestTemplateServiceCRUDAndRendering(t *testing.T) {
	repo := newTemplateRepoStub()
	svc := NewTemplateService(repo)

	callerID := uuid.New()
	created, err := svc.CreateTemplate(&CreateTemplateRequest{
		Name:    "welcome",
		Type:    domain.NotificationTypeEmail,
		Subject: "Hello {{.Name}}",
		Body:    "Body {{.Name}}",
		Variables: []VariableRequest{
			{Name: "Name", Type: "string", Required: true},
		},
		IsActive: true,
	}, callerID)
	if err != nil || created == nil {
		t.Fatalf("expected create template success, got err=%v", err)
	}

	rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
		TemplateName: "welcome",
		Data:         map[string]interface{}{"Name": "Ada"},
	})
	if err != nil {
		t.Fatalf("expected render success, got %v", err)
	}
	if rendered.Subject != "Hello Ada" {
		t.Fatalf("expected rendered subject, got %q", rendered.Subject)
	}

	_, err = svc.UpdateTemplate(created.ID, &CreateTemplateRequest{
		Name:     "welcome_v2",
		Type:     domain.NotificationTypeEmail,
		Subject:  "Hi {{.Name}}",
		Body:     "Body2",
		IsActive: true,
	}, callerID, []string{"admin"})
	if err != nil {
		t.Fatalf("expected update success, got %v", err)
	}

	if err := svc.DeleteTemplate(created.ID, callerID, []string{"admin"}); err != nil {
		t.Fatalf("expected delete success for custom template, got %v", err)
	}
}

func TestTemplateServiceCreateSystemTemplatesIsIdempotent(t *testing.T) {
	repo := newTemplateRepoStub()
	svc := NewTemplateService(repo)

	if err := svc.CreateSystemTemplates(); err != nil {
		t.Fatalf("expected first init success, got %v", err)
	}
	firstCount := len(repo.byName)

	if err := svc.CreateSystemTemplates(); err != nil {
		t.Fatalf("expected second init success, got %v", err)
	}
	if len(repo.byName) != firstCount {
		t.Fatalf("expected idempotent system templates, got first=%d second=%d", firstCount, len(repo.byName))
	}
}

func TestTemplateServiceCreateCategoryUniqueConstraint(t *testing.T) {
	repo := newTemplateRepoStub()
	repo.createCategoryFn = func(category *domain.TemplateCategory) error {
		return coreerrors.NewConflict("category already exists")
	}
	svc := NewTemplateService(repo)

	_, err := svc.CreateCategory("auth", "desc", nil)
	if err == nil {
		t.Fatalf("expected conflict error")
	}
}

func TestTemplateServiceRenderTemplateVariableValidationAndFallback(t *testing.T) {
	t.Run("missing_required_variable", func(t *testing.T) {
		repo := newTemplateRepoStub()
		tid := uuid.New()
		repo.getByNameFn = func(name string) (*domain.ExtendedNotificationTemplate, error) {
			return &domain.ExtendedNotificationTemplate{
				NotificationTemplate: domain.NotificationTemplate{
					ID:       tid,
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
		svc := NewTemplateService(repo)

		_, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "x",
			Data:         map[string]interface{}{},
		})
		if err == nil {
			t.Fatalf("expected missing variable error")
		}
	})

	t.Run("fallback_language_when_variant_missing", func(t *testing.T) {
		repo := newTemplateRepoStub()
		repo.getByNameFn = func(name string) (*domain.ExtendedNotificationTemplate, error) {
			return &domain.ExtendedNotificationTemplate{
				NotificationTemplate: domain.NotificationTemplate{
					ID:       uuid.New(),
					Name:     name,
					Subject:  "Hello {{.Name}}",
					Body:     "Body {{.Name}}",
					IsActive: true,
				},
			}, nil
		}
		repo.getLangFn = func(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error) {
			return nil, errors.New("not found")
		}
		svc := NewTemplateService(repo)

		rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "welcome",
			LanguageCode: "tr",
			Data:         map[string]interface{}{"Name": "Ada"},
		})
		if err != nil {
			t.Fatalf("expected render success, got %v", err)
		}
		if rendered.Body == "" || rendered.Subject == "" {
			t.Fatalf("expected non-empty fallback rendered content")
		}
	})
}

func TestTemplateServiceGetVariables(t *testing.T) {
	repo := newTemplateRepoStub()
	tid := uuid.New()
	repo.templates[tid] = &domain.ExtendedNotificationTemplate{
		NotificationTemplate: domain.NotificationTemplate{ID: tid, Name: "test"},
	}
	repo.byName["test"] = repo.templates[tid]
	expected := []*domain.TemplateVariable{{Name: "Foo"}}
	repo.getVariablesFn = func(templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
		return expected, nil
	}
	svc := NewTemplateService(repo)

	vars, err := svc.GetVariables(tid)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(vars) != 1 || vars[0].Name != "Foo" {
		t.Fatalf("unexpected variables: %v", vars)
	}
}

func TestTemplateServiceAddVariable(t *testing.T) {
	repo := newTemplateRepoStub()
	tid := uuid.New()
	repo.templates[tid] = &domain.ExtendedNotificationTemplate{
		NotificationTemplate: domain.NotificationTemplate{ID: tid, Name: "test"},
	}
	repo.byName["test"] = repo.templates[tid]
	svc := NewTemplateService(repo)

	v, err := svc.AddVariable(tid, &VariableRequest{Name: "Foo", Type: "string", Required: true})
	if err != nil || v == nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestTemplateServiceAddVariableSystemTemplate(t *testing.T) {
	repo := newTemplateRepoStub()
	tid := uuid.New()
	repo.templates[tid] = &domain.ExtendedNotificationTemplate{
		NotificationTemplate: domain.NotificationTemplate{ID: tid, Name: "sys"},
		IsSystem:             true,
	}
	repo.byName["sys"] = repo.templates[tid]
	svc := NewTemplateService(repo)

	// System templates should allow adding variables (they are editable, just not deletable)
	v, err := svc.AddVariable(tid, &VariableRequest{Name: "Foo", Type: "string"})
	if err != nil || v == nil {
		t.Fatalf("expected success adding variable to system template, got %v", err)
	}
}

func TestTemplateServiceUpdateVariable(t *testing.T) {
	repo := newTemplateRepoStub()
	tid := uuid.New()
	vid := uuid.New()
	repo.templates[tid] = &domain.ExtendedNotificationTemplate{
		NotificationTemplate: domain.NotificationTemplate{ID: tid, Name: "test"},
	}
	repo.byName["test"] = repo.templates[tid]
	repo.getVariablesFn = func(templateID uuid.UUID) ([]*domain.TemplateVariable, error) {
		return []*domain.TemplateVariable{{ID: vid, TemplateID: tid, Name: "Foo", Type: "string"}}, nil
	}
	svc := NewTemplateService(repo)

	v, err := svc.UpdateVariable(tid, vid, &UpdateVariableRequest{Name: "Bar", Type: "string"})
	if err != nil || v == nil {
		t.Fatalf("expected success, got %v", err)
	}
	if v.Name != "Bar" {
		t.Fatalf("expected updated name, got %q", v.Name)
	}
}

func TestTemplateServiceUpdateCategory(t *testing.T) {
	repo := newTemplateRepoStub()
	cid := uuid.New()
	repo.getCategoryFn = func(id uuid.UUID) (*domain.TemplateCategory, error) {
		return &domain.TemplateCategory{ID: cid, Name: "old"}, nil
	}
	svc := NewTemplateService(repo)

	cat, err := svc.UpdateCategory(cid, "new", "new desc")
	if err != nil || cat == nil {
		t.Fatalf("expected success, got %v", err)
	}
	if cat.Name != "new" {
		t.Fatalf("expected updated name, got %q", cat.Name)
	}
}

func TestTemplateServiceDeleteCategoryInUse(t *testing.T) {
	repo := newTemplateRepoStub()
	cid := uuid.New()
	repo.getCategoryFn = func(id uuid.UUID) (*domain.TemplateCategory, error) {
		return &domain.TemplateCategory{ID: cid, Name: "auth"}, nil
	}
	repo.countTemplatesByCategoryFn = func(categoryID uuid.UUID) (int64, error) {
		return 3, nil
	}
	svc := NewTemplateService(repo)

	err := svc.DeleteCategory(cid)
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil || pd.Status != http.StatusConflict {
		t.Fatalf("expected 409, got %v", err)
	}
}

func TestTemplateServiceDeleteCategorySuccess(t *testing.T) {
	repo := newTemplateRepoStub()
	cid := uuid.New()
	repo.getCategoryFn = func(id uuid.UUID) (*domain.TemplateCategory, error) {
		return &domain.TemplateCategory{ID: cid, Name: "unused"}, nil
	}
	repo.countTemplatesByCategoryFn = func(categoryID uuid.UUID) (int64, error) {
		return 0, nil
	}
	deleted := false
	repo.deleteCategoryFn = func(id uuid.UUID) error {
		deleted = true
		return nil
	}
	svc := NewTemplateService(repo)

	if err := svc.DeleteCategory(cid); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !deleted {
		t.Fatal("expected delete to be called")
	}
}

func TestTemplateServiceGetTemplateNotFound(t *testing.T) {
	repo := newTemplateRepoStub()
	repo.getByIDFn = func(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
		return nil, errors.New("not found")
	}
	svc := NewTemplateService(repo)

	_, err := svc.GetTemplate(uuid.New())
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil || pd.Status != http.StatusNotFound {
		t.Fatalf("expected 404 problem detail, got %v", err)
	}
}

func TestTemplateServiceRenderHTMLContent(t *testing.T) {
	t.Run("html_content_rendered_when_present", func(t *testing.T) {
		repo := newTemplateRepoStub()
		svc := NewTemplateService(repo)

		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:        "html_test",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Hello {{.Name}}",
			Body:        "Plain text body for {{.Name}}",
			HTMLContent: `<html><body><h1>Hello {{.Name}}</h1></body></html>`,
			IsActive:    true,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}

		rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "html_test",
			Data:         map[string]interface{}{"Name": "Ada"},
		})
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		if rendered.HTMLContent == "" {
			t.Fatal("expected non-empty HTMLContent")
		}
		if rendered.Body == "" {
			t.Fatal("expected non-empty Body (plain text)")
		}
		if rendered.HTMLContent == rendered.Body {
			t.Fatal("HTMLContent and Body should differ")
		}
	})

	t.Run("html_content_empty_when_not_set", func(t *testing.T) {
		repo := newTemplateRepoStub()
		svc := NewTemplateService(repo)

		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "plain_test",
			Type:     domain.NotificationTypeEmail,
			Subject:  "Hi {{.Name}}",
			Body:     "Just text {{.Name}}",
			IsActive: true,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}

		rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "plain_test",
			Data:         map[string]interface{}{"Name": "Bob"},
		})
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		if rendered.HTMLContent != "" {
			t.Fatalf("expected empty HTMLContent, got %q", rendered.HTMLContent)
		}
	})

	t.Run("language_variant_html_overrides_template_html", func(t *testing.T) {
		repo := newTemplateRepoStub()
		tid := uuid.New()
		repo.getByNameFn = func(name string) (*domain.ExtendedNotificationTemplate, error) {
			return &domain.ExtendedNotificationTemplate{
				NotificationTemplate: domain.NotificationTemplate{
					ID:       tid,
					Name:     name,
					Subject:  "Subject",
					Body:     "Body",
					IsActive: true,
				},
				HTMLContent: `<html><body>Template Level</body></html>`,
			}, nil
		}
		repo.getLangFn = func(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error) {
			return &domain.TemplateLanguage{
				Subject:     "Subject TR",
				Body:        "Body TR",
				HTMLContent: `<html><body>Language Override</body></html>`,
			}, nil
		}
		svc := NewTemplateService(repo)

		rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "test",
			LanguageCode: "tr",
			Data:         map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		if !strings.Contains(rendered.HTMLContent, "Language Override") {
			t.Fatalf("expected language variant HTML to override, got %q", rendered.HTMLContent)
		}
	})

	t.Run("year_auto_injected", func(t *testing.T) {
		repo := newTemplateRepoStub()
		svc := NewTemplateService(repo)

		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:        "year_test",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Test",
			Body:        "Year is {{.Year}}",
			HTMLContent: `<html><body>Year is {{.Year}}</body></html>`,
			IsActive:    true,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}

		rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "year_test",
			Data:         map[string]interface{}{}, // Year NOT provided
		})
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		currentYear := fmt.Sprintf("%d", time.Now().Year())
		if !strings.Contains(rendered.Body, currentYear) {
			t.Fatalf("expected Year auto-injected in body, got %q", rendered.Body)
		}
		if !strings.Contains(rendered.HTMLContent, currentYear) {
			t.Fatalf("expected Year auto-injected in HTML, got %q", rendered.HTMLContent)
		}
	})

	t.Run("html_template_escapes_xss", func(t *testing.T) {
		repo := newTemplateRepoStub()
		svc := NewTemplateService(repo)

		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:        "xss_test",
			Type:        domain.NotificationTypeEmail,
			Subject:     "Test",
			Body:        "Hello {{.Name}}",
			HTMLContent: `<html><body><p>Hello {{.Name}}</p></body></html>`,
			IsActive:    true,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}

		rendered, err := svc.RenderTemplate(&RenderTemplateRequest{
			TemplateName: "xss_test",
			Data:         map[string]interface{}{"Name": `<script>alert("xss")</script>`},
		})
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		// HTML content should escape the script tag
		if strings.Contains(rendered.HTMLContent, "<script>") {
			t.Fatalf("XSS not escaped in HTMLContent: %q", rendered.HTMLContent)
		}

		// Plain text body should NOT escape (text/template)
		if !strings.Contains(rendered.Body, "<script>") {
			t.Fatalf("plain text body should not escape HTML: %q", rendered.Body)
		}
	})
}

func TestTemplateService_SSTI_Prevention(t *testing.T) {
	repo := newTemplateRepoStub()
	repo.createTemplateFn = func(template *domain.ExtendedNotificationTemplate) error {
		template.ID = uuid.New()
		return nil
	}
	repo.getByNameFn = func(name string) (*domain.ExtendedNotificationTemplate, error) {
		return nil, errors.New("not found")
	}
	svc := NewTemplateService(repo)

	t.Run("blocks range directive for context enumeration", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-range",
			Type:     "email",
			Subject:  "Test",
			Body:     `{{range $k, $v := .}}KEY={{$k}}{{end}}`,
			IsActive: true,
		})
		if err == nil {
			t.Fatal("expected error for range directive, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 ProblemDetail, got %v", err)
		}
	})

	t.Run("blocks printf directive for data exfiltration", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-printf",
			Type:     "email",
			Subject:  "Test",
			Body:     `{{printf "%v" .Secret}}`,
			IsActive: true,
		})
		if err == nil {
			t.Fatal("expected error for printf directive, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 ProblemDetail, got %v", err)
		}
	})

	t.Run("blocks len directive", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-len",
			Type:     "email",
			Subject:  "Test",
			Body:     `Count: {{len .}}`,
			IsActive: true,
		})
		if err == nil {
			t.Fatal("expected error for len directive, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 ProblemDetail, got %v", err)
		}
	})

	t.Run("blocks call directive", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-call",
			Type:     "email",
			Subject:  "Test",
			Body:     `{{call .Func}}`,
			IsActive: true,
		})
		if err == nil {
			t.Fatal("expected error for call directive, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 ProblemDetail, got %v", err)
		}
	})

	t.Run("blocks dangerous directives in subject", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-subject",
			Type:     "email",
			Subject:  `{{range .}}x{{end}}`,
			Body:     "safe body {{.Name}}",
			IsActive: true,
		})
		if err == nil {
			t.Fatal("expected error for range in subject, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 ProblemDetail, got %v", err)
		}
	})

	t.Run("blocks dangerous directives in html_content", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:        "ssti-html",
			Type:        "email",
			Subject:     "Test",
			Body:        "safe body",
			HTMLContent: `<div>{{range $k, $v := .}}{{$k}}{{end}}</div>`,
			IsActive:    true,
		})
		if err == nil {
			t.Fatal("expected error for range in html_content, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 ProblemDetail, got %v", err)
		}
	})

	t.Run("allows safe variable substitution", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-safe",
			Type:     "email",
			Subject:  "Hello {{.Name}}",
			Body:     "Welcome {{.Name | upper}}, your code is {{.Code}}",
			IsActive: true,
		})
		if err != nil {
			t.Fatalf("safe template should be allowed, got %v", err)
		}
	})

	t.Run("allows if/else conditionals", func(t *testing.T) {
		_, err := svc.CreateTemplate(&CreateTemplateRequest{
			Name:     "ssti-conditional",
			Type:     "email",
			Subject:  "Test",
			Body:     `{{if .Name}}Hello {{.Name}}{{else}}Hello Guest{{end}}`,
			IsActive: true,
		})
		if err != nil {
			t.Fatalf("if/else should be allowed, got %v", err)
		}
	})
}

func TestTemplateService_SystemTemplateOwnership(t *testing.T) {
	systemTemplateID := uuid.New()
	customTemplateID := uuid.New()
	creatorID := uuid.New()
	otherAdminID := uuid.New()
	systemAdminID := uuid.New()

	repo := newTemplateRepoStub()
	repo.getByIDFn = func(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
		switch id {
		case systemTemplateID:
			tmpl := &domain.ExtendedNotificationTemplate{
				NotificationTemplate: domain.NotificationTemplate{
					ID:   systemTemplateID,
					Name: "welcome_user",
					Body: "Welcome {{.Name}}",
				},
			}
			tmpl.IsSystem = true
			return tmpl, nil
		case customTemplateID:
			return &domain.ExtendedNotificationTemplate{
				NotificationTemplate: domain.NotificationTemplate{
					ID:   customTemplateID,
					Name: "custom_template",
					Body: "Hello {{.Name}}",
				},
				CreatedBy: &creatorID,
			}, nil
		default:
			return nil, errors.New("not found")
		}
	}
	repo.updateTemplateFn = func(template *domain.ExtendedNotificationTemplate) error {
		return nil
	}
	svc := NewTemplateService(repo)

	t.Run("admin cannot modify system template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(systemTemplateID, &CreateTemplateRequest{
			Name:     "welcome_user",
			Type:     "email",
			Subject:  "Test",
			Body:     "Poisoned body",
			IsActive: true,
		}, otherAdminID, []string{"admin"})
		if err == nil {
			t.Fatal("expected error for admin modifying system template, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %v", err)
		}
	})

	t.Run("user cannot modify system template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(systemTemplateID, &CreateTemplateRequest{
			Name:     "welcome_user",
			Type:     "email",
			Subject:  "Test",
			Body:     "Poisoned body",
			IsActive: true,
		}, otherAdminID, []string{"user"})
		if err == nil {
			t.Fatal("expected error for user modifying system template, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %v", err)
		}
	})

	t.Run("nil roles cannot modify system template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(systemTemplateID, &CreateTemplateRequest{
			Name:     "welcome_user",
			Type:     "email",
			Subject:  "Test",
			Body:     "Poisoned body",
			IsActive: true,
		}, otherAdminID, nil)
		if err == nil {
			t.Fatal("expected error for nil roles modifying system template, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %v", err)
		}
	})

	t.Run("system_admin can modify system template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(systemTemplateID, &CreateTemplateRequest{
			Name:     "welcome_user",
			Type:     "email",
			Subject:  "Updated Subject",
			Body:     "Updated body {{.Name}}",
			IsActive: true,
		}, systemAdminID, []string{"system_admin"})
		if err != nil {
			t.Fatalf("system_admin should be able to modify system template, got %v", err)
		}
	})

	t.Run("creator can modify own non-system template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(customTemplateID, &CreateTemplateRequest{
			Name:     "custom_template",
			Type:     "email",
			Subject:  "Updated",
			Body:     "Updated {{.Name}}",
			IsActive: true,
		}, creatorID, []string{"admin"})
		if err != nil {
			t.Fatalf("creator should be able to modify own template, got %v", err)
		}
	})

	t.Run("non-creator admin cannot modify another admins template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(customTemplateID, &CreateTemplateRequest{
			Name:     "custom_template",
			Type:     "email",
			Subject:  "Hijacked",
			Body:     "Hijacked {{.Name}}",
			IsActive: true,
		}, otherAdminID, []string{"admin"})
		if err == nil {
			t.Fatal("expected error for non-creator admin modifying another admin's template, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %v", err)
		}
	})

	t.Run("system_admin can modify any non-system template", func(t *testing.T) {
		_, err := svc.UpdateTemplate(customTemplateID, &CreateTemplateRequest{
			Name:     "custom_template",
			Type:     "email",
			Subject:  "Updated by sysadmin",
			Body:     "Updated {{.Name}}",
			IsActive: true,
		}, systemAdminID, []string{"system_admin"})
		if err != nil {
			t.Fatalf("system_admin should be able to modify any template, got %v", err)
		}
	})
}

func TestTemplateService_TemplateDeleteOwnership(t *testing.T) {
	creatorID := uuid.New()
	otherAdminID := uuid.New()
	systemAdminID := uuid.New()
	templateID := uuid.New()

	repo := newTemplateRepoStub()
	repo.getByIDFn = func(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error) {
		if id == templateID {
			return &domain.ExtendedNotificationTemplate{
				NotificationTemplate: domain.NotificationTemplate{
					ID:   templateID,
					Name: "custom_template",
					Body: "Hello {{.Name}}",
				},
				CreatedBy: &creatorID,
			}, nil
		}
		return nil, errors.New("not found")
	}
	repo.deleteTemplateFn = func(id uuid.UUID) error {
		return nil
	}
	svc := NewTemplateService(repo)

	t.Run("creator can delete own template", func(t *testing.T) {
		err := svc.DeleteTemplate(templateID, creatorID, []string{"admin"})
		if err != nil {
			t.Fatalf("creator should be able to delete own template, got %v", err)
		}
	})

	t.Run("non-creator admin cannot delete another admins template", func(t *testing.T) {
		err := svc.DeleteTemplate(templateID, otherAdminID, []string{"admin"})
		if err == nil {
			t.Fatal("expected error for non-creator admin deleting another admin's template, got nil")
		}
		pd := coreerrors.GetProblemDetail(err)
		if pd == nil || pd.Status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %v", err)
		}
	})

	t.Run("system_admin can delete any template", func(t *testing.T) {
		err := svc.DeleteTemplate(templateID, systemAdminID, []string{"system_admin"})
		if err != nil {
			t.Fatalf("system_admin should be able to delete any template, got %v", err)
		}
	})
}
