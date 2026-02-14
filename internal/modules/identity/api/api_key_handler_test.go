package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

type apiKeyHandlerRepoStub struct {
	createFn               func(apiKey *domain.APIKey) error
	getByHashFn            func(keyHash string) (*domain.APIKey, error)
	getByHashWithRolesFn   func(keyHash string) (*domain.APIKey, error)
	getByIDFn              func(id uuid.UUID) (*domain.APIKey, error)
	getByIDWithRolesFn     func(id uuid.UUID) (*domain.APIKey, error)
	getUserKeysFn          func(userID uuid.UUID) ([]*domain.APIKey, error)
	getUserKeysPaginatedFn func(userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error)
	revokeFn               func(id uuid.UUID) error
	updateLastUsedFn       func(id uuid.UUID) error
	assignRoleFn           func(apiKeyID, roleID uuid.UUID) error
	removeRoleFn           func(apiKeyID, roleID uuid.UUID) error
}

var _ repository.APIKeyRepository = (*apiKeyHandlerRepoStub)(nil)

func (s *apiKeyHandlerRepoStub) Create(apiKey *domain.APIKey) error {
	if s.createFn != nil {
		return s.createFn(apiKey)
	}
	return nil
}
func (s *apiKeyHandlerRepoStub) GetByHash(keyHash string) (*domain.APIKey, error) {
	if s.getByHashFn != nil {
		return s.getByHashFn(keyHash)
	}
	return nil, nil
}
func (s *apiKeyHandlerRepoStub) GetByHashWithRoles(keyHash string) (*domain.APIKey, error) {
	if s.getByHashWithRolesFn != nil {
		return s.getByHashWithRolesFn(keyHash)
	}
	if s.getByHashFn != nil {
		return s.getByHashFn(keyHash)
	}
	return nil, nil
}
func (s *apiKeyHandlerRepoStub) GetByID(id uuid.UUID) (*domain.APIKey, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *apiKeyHandlerRepoStub) GetByIDWithRoles(id uuid.UUID) (*domain.APIKey, error) {
	if s.getByIDWithRolesFn != nil {
		return s.getByIDWithRolesFn(id)
	}
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *apiKeyHandlerRepoStub) GetUserKeys(userID uuid.UUID) ([]*domain.APIKey, error) {
	if s.getUserKeysFn != nil {
		return s.getUserKeysFn(userID)
	}
	return nil, nil
}
func (s *apiKeyHandlerRepoStub) GetUserKeysPaginated(userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error) {
	if s.getUserKeysPaginatedFn != nil {
		return s.getUserKeysPaginatedFn(userID, offset, limit)
	}
	if s.getUserKeysFn != nil {
		keys, err := s.getUserKeysFn(userID)
		return keys, int64(len(keys)), err
	}
	return nil, 0, nil
}
func (s *apiKeyHandlerRepoStub) Revoke(id uuid.UUID) error {
	if s.revokeFn != nil {
		return s.revokeFn(id)
	}
	return nil
}
func (s *apiKeyHandlerRepoStub) UpdateLastUsed(id uuid.UUID) error {
	if s.updateLastUsedFn != nil {
		return s.updateLastUsedFn(id)
	}
	return nil
}

func (s *apiKeyHandlerRepoStub) CleanupRevokedKeys(_ time.Duration) error {
	return nil
}

func (s *apiKeyHandlerRepoStub) AssignRole(apiKeyID, roleID uuid.UUID) error {
	if s.assignRoleFn != nil {
		return s.assignRoleFn(apiKeyID, roleID)
	}
	return nil
}

func (s *apiKeyHandlerRepoStub) RemoveRole(apiKeyID, roleID uuid.UUID) error {
	if s.removeRoleFn != nil {
		return s.removeRoleFn(apiKeyID, roleID)
	}
	return nil
}

type handlerRoleRepoStub struct{}

var _ repository.RoleRepository = (*handlerRoleRepoStub)(nil)

func (s *handlerRoleRepoStub) Create(_ *domain.Role) error { return nil }
func (s *handlerRoleRepoStub) GetByID(id uuid.UUID) (*domain.Role, error) {
	return &domain.Role{ID: id, Name: "test-role"}, nil
}
func (s *handlerRoleRepoStub) GetByName(_ string) (*domain.Role, error) { return nil, nil }
func (s *handlerRoleRepoStub) GetAll(_, _ int) ([]domain.Role, error)   { return nil, nil }
func (s *handlerRoleRepoStub) Count() (int64, error)                    { return 0, nil }
func (s *handlerRoleRepoStub) Update(_ *domain.Role) error              { return nil }
func (s *handlerRoleRepoStub) Delete(_ uuid.UUID) error                 { return nil }

func newAPIKeyHandlerApp(h *APIKeyHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
	return app
}

func apiKeyReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func apiKeyBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	return string(data)
}

func TestAPIKeyHandlerCreate_Success(t *testing.T) {
	userID := uuid.New()
	repo := &apiKeyHandlerRepoStub{
		createFn: func(apiKey *domain.APIKey) error { return nil },
	}
	svc := service.NewAPIKeyService(repo, &handlerRoleRepoStub{})
	h := NewAPIKeyHandler(svc)
	app := newAPIKeyHandlerApp(h)
	app.Post("/api-keys", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.CreateAPIKey(c)
	})

	resp := apiKeyReq(t, app, http.MethodPost, "/api-keys", `{"name":"ci-bot","scopes":"read:all"}`)
	body := apiKeyBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"key":"gck_`) {
		t.Fatalf("expected raw key in response, got %s", body)
	}
}

func TestAPIKeyHandlerList_Success(t *testing.T) {
	userID := uuid.New()
	repo := &apiKeyHandlerRepoStub{
		getUserKeysPaginatedFn: func(id uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error) {
			if offset != 0 || limit != 10 {
				t.Fatalf("expected offset=0 limit=10, got offset=%d limit=%d", offset, limit)
			}
			return []*domain.APIKey{{ID: uuid.New(), UserID: id, Name: "k1"}}, 1, nil
		},
	}
	svc := service.NewAPIKeyService(repo, &handlerRoleRepoStub{})
	h := NewAPIKeyHandler(svc)
	app := newAPIKeyHandlerApp(h)
	app.Get("/api-keys", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.ListAPIKeys(c)
	})

	resp := apiKeyReq(t, app, http.MethodGet, "/api-keys", "")
	body := apiKeyBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"items"`) || !strings.Contains(body, `"pagination"`) {
		t.Fatalf("expected paginated response, got %s", body)
	}
}

func TestAPIKeyHandlerRevoke_InvalidID(t *testing.T) {
	repo := &apiKeyHandlerRepoStub{}
	svc := service.NewAPIKeyService(repo, &handlerRoleRepoStub{})
	h := NewAPIKeyHandler(svc)
	app := newAPIKeyHandlerApp(h)
	app.Delete("/api-keys/:id", func(c *fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return h.RevokeAPIKey(c)
	})

	resp := apiKeyReq(t, app, http.MethodDelete, "/api-keys/not-uuid", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAPIKeyHandlerRevoke_Success(t *testing.T) {
	userID := uuid.New()
	keyID := uuid.New()
	repo := &apiKeyHandlerRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: userID, Revoked: false}, nil
		},
		revokeFn: func(id uuid.UUID) error { return nil },
	}
	svc := service.NewAPIKeyService(repo, &handlerRoleRepoStub{})
	h := NewAPIKeyHandler(svc)
	app := newAPIKeyHandlerApp(h)
	app.Delete("/api-keys/:id", func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return h.RevokeAPIKey(c)
	})

	resp := apiKeyReq(t, app, http.MethodDelete, "/api-keys/"+keyID.String(), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
