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
	createFn               func(apiKey *domain.APIKey) error
	getByHashFn            func(keyHash string) (*domain.APIKey, error)
	getByHashWithRolesFn   func(keyHash string) (*domain.APIKey, error)
	getByIDFn              func(id uuid.UUID) (*domain.APIKey, error)
	getByIDWithRolesFn     func(id uuid.UUID) (*domain.APIKey, error)
	getUserKeysFn          func(userID uuid.UUID) ([]*domain.APIKey, error)
	getUserKeysPaginatedFn func(userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error)
	getAllFn               func(offset, limit int) ([]*domain.APIKey, int64, error)
	revokeFn               func(id uuid.UUID) error
	updateLastUsedFn       func(id uuid.UUID) error
	assignRoleFn           func(apiKeyID, roleID uuid.UUID) error
	removeRoleFn           func(apiKeyID, roleID uuid.UUID) error
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

func (s *apiKeyRepoStub) GetUserKeysPaginated(userID uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error) {
	if s.getUserKeysPaginatedFn != nil {
		return s.getUserKeysPaginatedFn(userID, offset, limit)
	}
	if s.getUserKeysFn != nil {
		keys, err := s.getUserKeysFn(userID)
		return keys, int64(len(keys)), err
	}
	return nil, 0, nil
}

func (s *apiKeyRepoStub) Revoke(id uuid.UUID) error {
	if s.revokeFn != nil {
		return s.revokeFn(id)
	}
	return nil
}

func (s *apiKeyRepoStub) GetAll(offset, limit int) ([]*domain.APIKey, int64, error) {
	if s.getAllFn != nil {
		return s.getAllFn(offset, limit)
	}
	return nil, 0, nil
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

func (s *apiKeyRoleRepoStub) Create(_ *domain.Role) error              { return nil }
func (s *apiKeyRoleRepoStub) GetByName(_ string) (*domain.Role, error) { return nil, nil }
func (s *apiKeyRoleRepoStub) GetAll(_, _ int) ([]domain.Role, error)   { return nil, nil }
func (s *apiKeyRoleRepoStub) Count() (int64, error)                    { return 0, nil }
func (s *apiKeyRoleRepoStub) Update(_ *domain.Role) error              { return nil }
func (s *apiKeyRoleRepoStub) Delete(_ uuid.UUID) error                 { return nil }
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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

	_, err := svc.Create(uuid.New(), &CreateAPIKeyRequest{Name: "ci-bot"})
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to create API key")
}

func TestAPIKeyServiceValidate_InvalidKey(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByHashWithRolesFn: func(keyHash string) (*domain.APIKey, error) {
			return nil, stderrors.New("not found")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	_, err := svc.Validate("gck_invalid")
	assertProblemDetail(t, err, http.StatusUnauthorized, "Invalid API key")
}

func TestAPIKeyServiceValidate_RevokedKey(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByHashWithRolesFn: func(keyHash string) (*domain.APIKey, error) {
			return &domain.APIKey{ID: uuid.New(), Revoked: true}, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
	}, &apiKeyRoleRepoStub{}, nil)

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
		getUserKeysPaginatedFn: func(id uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error) {
			if offset != 10 || limit != 5 {
				t.Fatalf("expected offset=10 limit=5, got offset=%d limit=%d", offset, limit)
			}
			return keys, 12, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	got, total, err := svc.List(userID, 10, 5)
	if err != nil {
		t.Fatalf("expected list success, got %v", err)
	}
	if len(got) != len(keys) {
		t.Fatalf("expected %d keys, got %d", len(keys), len(got))
	}
	if total != 12 {
		t.Fatalf("expected total=12, got %d", total)
	}
}

func TestAPIKeyServiceList_RepositoryFailure(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getUserKeysPaginatedFn: func(id uuid.UUID, offset, limit int) ([]*domain.APIKey, int64, error) {
			return nil, 0, stderrors.New("db error")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	_, _, err := svc.List(uuid.New(), 0, 10)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to list API keys")
}

// ---------------------------------------------------------------------------
// roleManagerStub implements repository.RoleManager for API key tests
// ---------------------------------------------------------------------------

type roleManagerStub struct {
	getUserRolesFn func(userID uuid.UUID) ([]*domain.Role, error)
}

var _ repository.RoleManager = (*roleManagerStub)(nil)

func (s *roleManagerStub) CreateRole(_ *domain.Role) error                     { return nil }
func (s *roleManagerStub) UpdateRole(_ *domain.Role) error                     { return nil }
func (s *roleManagerStub) DeleteRole(_ uuid.UUID) error                        { return nil }
func (s *roleManagerStub) GetRoleByID(_ uuid.UUID) (*domain.Role, error)       { return nil, nil }
func (s *roleManagerStub) GetRoleByName(_ string) (*domain.Role, error)        { return nil, nil }
func (s *roleManagerStub) GetAllRoles() ([]*domain.Role, error)                { return nil, nil }
func (s *roleManagerStub) AssignRole(_, _ uuid.UUID) error                     { return nil }
func (s *roleManagerStub) RemoveRole(_, _ uuid.UUID) error                     { return nil }
func (s *roleManagerStub) GetUserRoles(userID uuid.UUID) ([]*domain.Role, error) {
	if s.getUserRolesFn != nil {
		return s.getUserRolesFn(userID)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// ListAll tests
// ---------------------------------------------------------------------------

func TestAPIKeyServiceListAll_Success(t *testing.T) {
	keys := []*domain.APIKey{
		{ID: uuid.New(), Name: "admin-key-1"},
		{ID: uuid.New(), Name: "admin-key-2"},
	}
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getAllFn: func(offset, limit int) ([]*domain.APIKey, int64, error) {
			if offset != 0 || limit != 20 {
				t.Fatalf("expected offset=0 limit=20, got offset=%d limit=%d", offset, limit)
			}
			return keys, 42, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	got, total, err := svc.ListAll(0, 20)
	if err != nil {
		t.Fatalf("expected list all success, got %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	if total != 42 {
		t.Fatalf("expected total=42, got %d", total)
	}
}

func TestAPIKeyServiceListAll_RepositoryFailure(t *testing.T) {
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getAllFn: func(offset, limit int) ([]*domain.APIKey, int64, error) {
			return nil, 0, stderrors.New("db error")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	_, _, err := svc.ListAll(0, 10)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to list API keys")
}

// ---------------------------------------------------------------------------
// AdminRevoke tests
// ---------------------------------------------------------------------------

func TestAPIKeyServiceAdminRevoke_Success(t *testing.T) {
	keyID := uuid.New()
	revoked := false
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, Revoked: false}, nil
		},
		revokeFn: func(id uuid.UUID) error {
			revoked = true
			if id != keyID {
				t.Fatalf("expected revoke id %s, got %s", keyID, id)
			}
			return nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	if err := svc.AdminRevoke(keyID); err != nil {
		t.Fatalf("expected admin revoke success, got %v", err)
	}
	if !revoked {
		t.Fatalf("expected repository revoke call")
	}
}

func TestAPIKeyServiceAdminRevoke_NotFound(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return nil, stderrors.New("not found")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.AdminRevoke(keyID)
	assertProblemDetail(t, err, http.StatusNotFound, "API Key with identifier '"+keyID.String()+"' not found")
}

func TestAPIKeyServiceAdminRevoke_AlreadyRevoked(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, Revoked: true}, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.AdminRevoke(keyID)
	assertProblemDetail(t, err, http.StatusBadRequest, "API key is already revoked")
}

func TestAPIKeyServiceAdminRevoke_RepositoryFailure(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, Revoked: false}, nil
		},
		revokeFn: func(id uuid.UUID) error {
			return stderrors.New("db failure")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.AdminRevoke(keyID)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to revoke API key")
}

// ---------------------------------------------------------------------------
// AssignRole tests
// ---------------------------------------------------------------------------

func TestAPIKeyServiceAssignRole_Success(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()
	assigned := false

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
		assignRoleFn: func(akID, rID uuid.UUID) error {
			assigned = true
			if akID != keyID || rID != roleID {
				t.Fatalf("unexpected assign args: key=%s role=%s", akID, rID)
			}
			return nil
		},
	}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "editor"}, nil
		},
	}, &roleManagerStub{
		getUserRolesFn: func(userID uuid.UUID) ([]*domain.Role, error) {
			return []*domain.Role{{ID: roleID, Name: "editor"}}, nil
		},
	})

	if err := svc.AssignRole(keyID, roleID, ownerID); err != nil {
		t.Fatalf("expected assign role success, got %v", err)
	}
	if !assigned {
		t.Fatalf("expected repository assign call")
	}
}

func TestAPIKeyServiceAssignRole_KeyNotFound(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return nil, stderrors.New("not found")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.AssignRole(keyID, uuid.New(), uuid.New())
	assertProblemDetail(t, err, http.StatusNotFound, "API Key with identifier '"+keyID.String()+"' not found")
}

func TestAPIKeyServiceAssignRole_OwnershipMismatch(t *testing.T) {
	keyID := uuid.New()
	ownerID := uuid.New()
	otherUser := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.AssignRole(keyID, uuid.New(), otherUser)
	assertProblemDetail(t, err, http.StatusForbidden, "You do not have permission to manage this API key")
}

func TestAPIKeyServiceAssignRole_RoleNotFound(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
	}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return nil, stderrors.New("not found")
		},
	}, nil)

	err := svc.AssignRole(keyID, roleID, ownerID)
	assertProblemDetail(t, err, http.StatusNotFound, "Role with identifier '"+roleID.String()+"' not found")
}

func TestAPIKeyServiceAssignRole_UserDoesNotPossessRole(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
	}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "admin"}, nil
		},
	}, &roleManagerStub{
		getUserRolesFn: func(userID uuid.UUID) ([]*domain.Role, error) {
			// Return a different role, not the one being assigned
			return []*domain.Role{{ID: uuid.New(), Name: "user"}}, nil
		},
	})

	err := svc.AssignRole(keyID, roleID, ownerID)
	assertProblemDetail(t, err, http.StatusForbidden, "You cannot assign role 'admin' that you do not possess")
}

func TestAPIKeyServiceAssignRole_GetUserRolesFailure(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
	}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "editor"}, nil
		},
	}, &roleManagerStub{
		getUserRolesFn: func(userID uuid.UUID) ([]*domain.Role, error) {
			return nil, stderrors.New("db error")
		},
	})

	err := svc.AssignRole(keyID, roleID, ownerID)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to verify user roles")
}

func TestAPIKeyServiceAssignRole_RepoAssignFailure(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
		assignRoleFn: func(akID, rID uuid.UUID) error {
			return stderrors.New("constraint violation")
		},
	}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "editor"}, nil
		},
	}, &roleManagerStub{
		getUserRolesFn: func(userID uuid.UUID) ([]*domain.Role, error) {
			return []*domain.Role{{ID: roleID, Name: "editor"}}, nil
		},
	})

	err := svc.AssignRole(keyID, roleID, ownerID)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to assign role to API key")
}

// ---------------------------------------------------------------------------
// RemoveRole tests
// ---------------------------------------------------------------------------

func TestAPIKeyServiceRemoveRole_Success(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()
	removed := false

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
		removeRoleFn: func(akID, rID uuid.UUID) error {
			removed = true
			if akID != keyID || rID != roleID {
				t.Fatalf("unexpected remove args: key=%s role=%s", akID, rID)
			}
			return nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	if err := svc.RemoveRole(keyID, roleID, ownerID); err != nil {
		t.Fatalf("expected remove role success, got %v", err)
	}
	if !removed {
		t.Fatalf("expected repository remove call")
	}
}

func TestAPIKeyServiceRemoveRole_KeyNotFound(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return nil, stderrors.New("not found")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.RemoveRole(keyID, uuid.New(), uuid.New())
	assertProblemDetail(t, err, http.StatusNotFound, "API Key with identifier '"+keyID.String()+"' not found")
}

func TestAPIKeyServiceRemoveRole_OwnershipMismatch(t *testing.T) {
	keyID := uuid.New()
	ownerID := uuid.New()
	otherUser := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.RemoveRole(keyID, uuid.New(), otherUser)
	assertProblemDetail(t, err, http.StatusForbidden, "You do not have permission to manage this API key")
}

func TestAPIKeyServiceRemoveRole_RepoFailure(t *testing.T) {
	keyID := uuid.New()
	roleID := uuid.New()
	ownerID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
		removeRoleFn: func(akID, rID uuid.UUID) error {
			return stderrors.New("db error")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	err := svc.RemoveRole(keyID, roleID, ownerID)
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to remove role from API key")
}

// ---------------------------------------------------------------------------
// GetAPIKeyRoles tests
// ---------------------------------------------------------------------------

func TestAPIKeyServiceGetAPIKeyRoles_Success(t *testing.T) {
	keyID := uuid.New()
	ownerID := uuid.New()
	roles := []domain.Role{
		{ID: uuid.New(), Name: "editor"},
		{ID: uuid.New(), Name: "viewer"},
	}

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDWithRolesFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID, Roles: roles}, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	got, err := svc.GetAPIKeyRoles(keyID, ownerID)
	if err != nil {
		t.Fatalf("expected get roles success, got %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(got))
	}
	if got[0].Name != "editor" || got[1].Name != "viewer" {
		t.Fatalf("unexpected role names: %v", got)
	}
}

func TestAPIKeyServiceGetAPIKeyRoles_KeyNotFound(t *testing.T) {
	keyID := uuid.New()
	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDWithRolesFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return nil, stderrors.New("not found")
		},
	}, &apiKeyRoleRepoStub{}, nil)

	_, err := svc.GetAPIKeyRoles(keyID, uuid.New())
	assertProblemDetail(t, err, http.StatusNotFound, "API Key with identifier '"+keyID.String()+"' not found")
}

func TestAPIKeyServiceGetAPIKeyRoles_OwnershipMismatch(t *testing.T) {
	keyID := uuid.New()
	ownerID := uuid.New()
	otherUser := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{
		getByIDWithRolesFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{ID: keyID, UserID: ownerID}, nil
		},
	}, &apiKeyRoleRepoStub{}, nil)

	_, err := svc.GetAPIKeyRoles(keyID, otherUser)
	assertProblemDetail(t, err, http.StatusForbidden, "You do not have permission to view this API key")
}

// ---------------------------------------------------------------------------
// Create with roles (additional branch coverage)
// ---------------------------------------------------------------------------

func TestAPIKeyServiceCreate_WithRoles_Success(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()
	assignCalled := false

	svc := NewAPIKeyService(&apiKeyRepoStub{
		createFn: func(apiKey *domain.APIKey) error { return nil },
		assignRoleFn: func(akID, rID uuid.UUID) error {
			assignCalled = true
			if rID != roleID {
				t.Fatalf("expected role id %s, got %s", roleID, rID)
			}
			return nil
		},
		getByIDWithRolesFn: func(id uuid.UUID) (*domain.APIKey, error) {
			return &domain.APIKey{
				ID:     id,
				UserID: userID,
				Roles:  []domain.Role{{ID: roleID, Name: "editor"}},
			}, nil
		},
	}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "editor"}, nil
		},
	}, &roleManagerStub{
		getUserRolesFn: func(uid uuid.UUID) ([]*domain.Role, error) {
			return []*domain.Role{{ID: roleID, Name: "editor"}}, nil
		},
	})

	resp, err := svc.Create(userID, &CreateAPIKeyRequest{
		Name:    "ci-bot",
		RoleIDs: []uuid.UUID{roleID},
	})
	if err != nil {
		t.Fatalf("expected create with roles success, got %v", err)
	}
	if !assignCalled {
		t.Fatalf("expected assign role to be called")
	}
	if resp.APIKey == nil || len(resp.APIKey.Roles) != 1 {
		t.Fatalf("expected reloaded key with 1 role")
	}
}

func TestAPIKeyServiceCreate_WithRoles_UserDoesNotPossessRole(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return &domain.Role{ID: roleID, Name: "admin"}, nil
		},
	}, &roleManagerStub{
		getUserRolesFn: func(uid uuid.UUID) ([]*domain.Role, error) {
			return []*domain.Role{{ID: uuid.New(), Name: "user"}}, nil
		},
	})

	_, err := svc.Create(userID, &CreateAPIKeyRequest{
		Name:    "ci-bot",
		RoleIDs: []uuid.UUID{roleID},
	})
	assertProblemDetail(t, err, http.StatusForbidden, "You cannot assign role 'admin' that you do not possess")
}

func TestAPIKeyServiceCreate_WithRoles_GetUserRolesFailure(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{}, &apiKeyRoleRepoStub{}, &roleManagerStub{
		getUserRolesFn: func(uid uuid.UUID) ([]*domain.Role, error) {
			return nil, stderrors.New("db error")
		},
	})

	_, err := svc.Create(userID, &CreateAPIKeyRequest{
		Name:    "ci-bot",
		RoleIDs: []uuid.UUID{roleID},
	})
	assertProblemDetail(t, err, http.StatusInternalServerError, "Failed to verify user roles")
}

func TestAPIKeyServiceCreate_WithRoles_RoleNotFound(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()

	svc := NewAPIKeyService(&apiKeyRepoStub{}, &apiKeyRoleRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.Role, error) {
			return nil, stderrors.New("not found")
		},
	}, &roleManagerStub{
		getUserRolesFn: func(uid uuid.UUID) ([]*domain.Role, error) {
			return []*domain.Role{{ID: roleID, Name: "editor"}}, nil
		},
	})

	_, err := svc.Create(userID, &CreateAPIKeyRequest{
		Name:    "ci-bot",
		RoleIDs: []uuid.UUID{roleID},
	})
	assertProblemDetail(t, err, http.StatusBadRequest, "Role not found: "+roleID.String())
}
