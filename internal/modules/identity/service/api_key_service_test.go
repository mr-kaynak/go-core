package service

import (
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

type apiKeyRepoStub struct {
	createFn            func(apiKey *domain.APIKey) error
	getByHashFn         func(keyHash string) (*domain.APIKey, error)
	getByHashWithRolesFn func(keyHash string) (*domain.APIKey, error)
	getByIDFn           func(id uuid.UUID) (*domain.APIKey, error)
	getByIDWithRolesFn  func(id uuid.UUID) (*domain.APIKey, error)
	getUserKeysFn       func(userID uuid.UUID) ([]*domain.APIKey, error)
	revokeFn            func(id uuid.UUID) error
	updateLastUsedFn    func(id uuid.UUID) error
	assignRoleFn        func(apiKeyID, roleID uuid.UUID) error
	removeRoleFn        func(apiKeyID, roleID uuid.UUID) error
}

var _ repository.APIKeyRepository = (*apiKeyRepoStub)(nil)

func (s *apiKeyRepoStub) Create(apiKey *domain.APIKey) error {
	if s.createFn != nil {
		return s.createFn(apiKey)
	}
	return nil
}

func (s *apiKeyRepoStub) GetByHash(keyHash string) (*domain.APIKey, error) {
	if s.getByHashFn != nil {
		return s.getByHashFn(keyHash)
	}
	return nil, nil
}

func (s *apiKeyRepoStub) GetByHashWithRoles(keyHash string) (*domain.APIKey, error) {
	if s.getByHashWithRolesFn != nil {
		return s.getByHashWithRolesFn(keyHash)
	}
	if s.getByHashFn != nil {
		return s.getByHashFn(keyHash)
	}
	return nil, nil
}

func (s *apiKeyRepoStub) GetByID(id uuid.UUID) (*domain.APIKey, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

func (s *apiKeyRepoStub) GetByIDWithRoles(id uuid.UUID) (*domain.APIKey, error) {
	if s.getByIDWithRolesFn != nil {
		return s.getByIDWithRolesFn(id)
	}
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}

func (s *apiKeyRepoStub) GetUserKeys(userID uuid.UUID) ([]*domain.APIKey, error) {
	if s.getUserKeysFn != nil {
		return s.getUserKeysFn(userID)
	}
	return nil, nil
}

func (s *apiKeyRepoStub) Revoke(id uuid.UUID) error {
	if s.revokeFn != nil {
		return s.revokeFn(id)
	}
	return nil
}

func (s *apiKeyRepoStub) UpdateLastUsed(id uuid.UUID) error {
	if s.updateLastUsedFn != nil {
		return s.updateLastUsedFn(id)
	}
	return nil
}

func (s *apiKeyRepoStub) CleanupRevokedKeys(_ time.Duration) error {
	return nil
}

func (s *apiKeyRepoStub) AssignRole(apiKeyID, roleID uuid.UUID) error {
	if s.assignRoleFn != nil {
		return s.assignRoleFn(apiKeyID, roleID)
	}
	return nil
}

func (s *apiKeyRepoStub) RemoveRole(apiKeyID, roleID uuid.UUID) error {
	if s.removeRoleFn != nil {
		return s.removeRoleFn(apiKeyID, roleID)
	}
	return nil
}

type apiKeyRoleRepoStub struct {
	getByIDFn func(id uuid.UUID) (*domain.Role, error)
}

var _ repository.RoleRepository = (*apiKeyRoleRepoStub)(nil)

func (s *apiKeyRoleRepoStub) Create(_ *domain.Role) error                        { return nil }
func (s *apiKeyRoleRepoStub) GetByName(_ string) (*domain.Role, error)           { return nil, nil }
func (s *apiKeyRoleRepoStub) GetAll(_, _ int) ([]domain.Role, error)             { return nil, nil }
func (s *apiKeyRoleRepoStub) Count() (int64, error)                              { return 0, nil }
func (s *apiKeyRoleRepoStub) Update(_ *domain.Role) error                        { return nil }
func (s *apiKeyRoleRepoStub) Delete(_ uuid.UUID) error                           { return nil }
func (s *apiKeyRoleRepoStub) GetByID(id uuid.UUID) (*domain.Role, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return &domain.Role{ID: id, Name: "test-role"}, nil
}

func assertProblemDetail(t *testing.T, err error, status int, detail string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	pd := coreerrors.GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected problem detail, got %T", err)
	}
	if pd.Status != status {
		t.Fatalf("expected status %d, got %d", status, pd.Status)
	}
	if pd.Detail != detail {
		t.Fatalf("expected detail %q, got %q", detail, pd.Detail)
	}
}

func TestAPIKeyServiceCreate_Success(t *testing.T) {
	userID := uuid.New()
	var stored *domain.APIKey

	svc := NewAPIKeyService(&apiKeyRepoStub{
		createFn: func(apiKey *domain.APIKey) error {
			stored = apiKey
			return nil
		},
	}, &apiKeyRoleRepoStub{})

	resp, err := svc.Create(userID, &CreateAPIKeyRequest{
		Name:   "ci-bot",
		Scopes: "read:users,write:users",
	})
	if err != nil {
		t.Fatalf("expected create success, got %v", err)
	}
	if resp == nil || resp.APIKey == nil || resp.RawKey == "" {
		t.Fatalf("expected populated create response")
	}
	if stored == nil {
		t.Fatalf("expected key to be persisted")
	}
	if stored.UserID != userID {
		t.Fatalf("expected persisted user id %s, got %s", userID, stored.UserID)
	}
	if stored.KeyPrefix != resp.RawKey[:8] {
		t.Fatalf("expected key prefix to match raw key")
	}
	if stored.KeyHash != domain.HashAPIKey(resp.RawKey) {
		t.Fatalf("expected stored hash to match raw key hash")
	}
}

func TestAPIKeyServiceCreate_RepositoryFailure(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		createFn: func(apiKey *domain.APIKey) error {
			return stderrors.New("db error")
		},
	}, &apiKeyRoleRepoStub{})

	_, err := svc.Create(uuid.New(), &CreateAPIKeyRequest{Name: "ci-bot"})
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to create API key")
}

func TestAPIKeyServiceValidate_InvalidKey(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByHashWithRolesFn: func(keyHash string) (*domain.APIKey, error) {
			return nil, stderrors.New("not found")
		},
	}, &apiKeyRoleRepoStub{})

	_, err := svc.Validate("gck_invalid")
	assertProblemDetail(t, err, http.StatusUnauthorized, "Invalid API key")
}

func TestAPIKeyServiceValidate_RevokedKey(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByHashWithRolesFn: func(keyHash string) (*domain.APIKey, error) {
			return &domain.APIKey{ID: uuid.New(), Revoked: true}, nil
		},
	}, &apiKeyRoleRepoStub{})

	_, err := svc.Validate("gck_revoked")
	assertProblemDetail(t, err, http.StatusUnauthorized, "API key has been revoked")
}

func TestAPIKeyServiceValidate_ExpiredKey(t *testing.T) {
	exp := time.Now().Add(-time.Minute)
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByHashWithRolesFn: func(keyHash string) (*domain.APIKey, error) {
			return &domain.APIKey{
				ID:        uuid.New(),
				Revoked:   false,
				ExpiresAt: &exp,
			}, nil
		},
	}, &apiKeyRoleRepoStub{})

	_, err := svc.Validate("gck_expired")
	assertProblemDetail(t, err, http.StatusUnauthorized, "API key has expired")
}

func TestAPIKeyServiceValidate_ValidKeyUpdatesLastUsedAsync(t *testing.T) {
	key := &domain.APIKey{
		ID:      uuid.New(),
		Revoked: false,
	}
	done := make(chan struct{}, 1)

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByHashWithRolesFn: func(keyHash string) (*domain.APIKey, error) {
			return key, nil
		},
		updateLastUsedFn: func(id uuid.UUID) error {
			if id != key.ID {
				t.Fatalf("expected update for key id %s, got %s", key.ID, id)
			}
			done <- struct{}{}
			return nil
		},
	}, &apiKeyRoleRepoStub{})

	got, err := svc.Validate("gck_valid")
	if err != nil {
		t.Fatalf("expected validate success, got %v", err)
	}
	if got == nil || got.ID != key.ID {
		t.Fatalf("expected validated key to be returned")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("expected UpdateLastUsed async callback")
	}
}

func TestAPIKeyServiceRevoke_NotFound(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return nil, stderrors.New("missing")
		},
	}, &apiKeyRoleRepoStub{})

	err := svc.Revoke(keyID, uuid.New())
	assertProblemDetail(t, err, http.StatusNotFound, "API Key with identifier '"+keyID.String()+"' not found")
}

func TestAPIKeyServiceRevoke_ForbiddenOwnerMismatch(t *testing.T) {
	keyID := uuid.New()
	ownerID := uuid.New()
	otherUserID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{
				ID:     keyID,
				UserID: ownerID,
			}, nil
		},
	}, &apiKeyRoleRepoStub{})

	err := svc.Revoke(keyID, otherUserID)
	assertProblemDetail(t, err, http.StatusForbidden, "You do not have permission to revoke this API key")
}

func TestAPIKeyServiceRevoke_AlreadyRevoked(t *testing.T) {
	keyID := uuid.New()
	userID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{
				ID:      keyID,
				UserID:  userID,
				Revoked: true,
			}, nil
		},
	}, &apiKeyRoleRepoStub{})

	err := svc.Revoke(keyID, userID)
	assertProblemDetail(t, err, http.StatusBadRequest, "API key is already revoked")
}

func TestAPIKeyServiceRevoke_RepositoryFailure(t *testing.T) {
	keyID := uuid.New()
	userID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{
				ID:      keyID,
				UserID:  userID,
				Revoked: false,
			}, nil
		},
		revokeFn: func(id uuid.UUID) error {
			return stderrors.New("db failure")
		},
	}, &apiKeyRoleRepoStub{})

	err := svc.Revoke(keyID, userID)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to revoke API key")
}

func TestAPIKeyServiceRevoke_Success(t *testing.T) {
	keyID := uuid.New()
	userID := uuid.New()
	called := false
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{
				ID:      keyID,
				UserID:  userID,
				Revoked: false,
			}, nil
		},
		revokeFn: func(id uuid.UUID) error {
			called = true
			if id != keyID {
				t.Fatalf("expected revoke id %s, got %s", keyID, id)
			}
			return nil
		},
	}, &apiKeyRoleRepoStub{})

	if err := svc.Revoke(keyID, userID); err != nil {
		t.Fatalf("expected revoke success, got %v", err)
	}
	if !called {
		t.Fatalf("expected repository revoke call")
	}
}

func TestAPIKeyServiceList_Success(t *testing.T) {
	userID := uuid.New()
	keys := []*domain.APIKey{
		{ID: uuid.New(), UserID: userID, Name: "k1"},
		{ID: uuid.New(), UserID: userID, Name: "k2"},
	}

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getUserKeysFn: func(id uuid.UUID) ([]*domain.APIKey, error) {
			return keys, nil
		},
	}, &apiKeyRoleRepoStub{})

	got, err := svc.List(userID)
	if err != nil {
		t.Fatalf("expected list success, got %v", err)
	}
	if len(got) != len(keys) {
		t.Fatalf("expected %d keys, got %d", len(keys), len(got))
	}
}

func TestAPIKeyServiceList_RepositoryFailure(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getUserKeysFn: func(id uuid.UUID) ([]*domain.APIKey, error) {
			return nil, stderrors.New("db error")
		},
	}, &apiKeyRoleRepoStub{})

	_, err := svc.List(uuid.New())
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to list API keys")
}
