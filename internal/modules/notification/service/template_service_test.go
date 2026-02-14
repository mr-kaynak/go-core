package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
)

type templateRepoStub struct {
	templates map[uuid.UUID]*domain.ExtendedNotificationTemplate
	byName    map[string]*domain.ExtendedNotificationTemplate

	createTemplateFn func(template *domain.ExtendedNotificationTemplate) error
	getByIDFn        func(id uuid.UUID) (*domain.ExtendedNotificationTemplate, error)
	getByNameFn      func(name string) (*domain.ExtendedNotificationTemplate, error)
	updateTemplateFn func(template *domain.ExtendedNotificationTemplate) error
	deleteTemplateFn func(id uuid.UUID) error
	listTemplatesFn  func(filters map[string]interface{}, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error)
	createLangFn     func(variant *domain.TemplateLanguage) error
	getLangFn        func(templateID uuid.UUID, languageCode string) (*domain.TemplateLanguage, error)
	createVariableFn            func(variable *domain.TemplateVariable) error
	getVariablesFn              func(templateID uuid.UUID) ([]*domain.TemplateVariable, error)
	updateVariableFn            func(variable *domain.TemplateVariable) error
	createCategoryFn            func(category *domain.TemplateCategory) error
	getCategoryFn               func(id uuid.UUID) (*domain.TemplateCategory, error)
	updateCategoryFn            func(category *domain.TemplateCategory) error
	deleteCategoryFn            func(id uuid.UUID) error
	countTemplatesByCategoryFn  func(categoryID uuid.UUID) (int64, error)
	listCategoriesFn            func() ([]*domain.TemplateCategory, error)
	incrementUsageFn            func(templateID uuid.UUID) error
	getMostUsedFn               func(limit int) ([]*domain.ExtendedNotificationTemplate, error)
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
func (s *templateRepoStub) ListTemplates(filters map[string]interface{}, offset, limit int) ([]*domain.ExtendedNotificationTemplate, int64, error) {
	if s.listTemplatesFn != nil {
		return s.listTemplatesFn(filters, offset, limit)
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

func TestTemplateServiceCRUDAndRendering(t *testing.T) {
	repo := newTemplateRepoStub()
	svc := NewTemplateService(repo)

	created, err := svc.CreateTemplate(&CreateTemplateRequest{
		Name:    "welcome",
		Type:    domain.NotificationTypeEmail,
		Subject: "Hello {{.Name}}",
		Body:    "Body {{.Name}}",
		Variables: []VariableRequest{
			{Name: "Name", Type: "string", Required: true},
		},
		IsActive: true,
	})
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
	})
	if err != nil {
		t.Fatalf("expected update success, got %v", err)
	}

	if err := svc.DeleteTemplate(created.ID); err != nil {
		t.Fatalf("expected delete success, got %v", err)
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

	_, err := svc.AddVariable(tid, &VariableRequest{Name: "Foo", Type: "string"})
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil || pd.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
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
