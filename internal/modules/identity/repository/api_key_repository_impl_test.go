package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func newTestAPIKeyRepository(t *testing.T) (*gorm.DB, APIKeyRepository) {
	t.Helper()
	db := setupTestDB(t)
	return db, NewAPIKeyRepository(db)
}

func seedAPIKey(t *testing.T, db *gorm.DB, userID uuid.UUID, name, keyHash, keyPrefix string) *domain.APIKey {
	t.Helper()
	key := &domain.APIKey{
		ID:        uuid.New(),
		UserID:    userID,
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		Name:      name,
	}
	if err := db.Create(key).Error; err != nil {
		t.Fatalf("failed to seed API key: %v", err)
	}
	return key
}

func TestAPIKeyRepositoryCreateAndGetByHash(t *testing.T) {
	_, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := &domain.APIKey{
		ID:        uuid.New(),
		UserID:    userID,
		KeyHash:   "hash-abc123",
		KeyPrefix: "gc_",
		Name:      "test-key",
	}

	if err := repo.Create(key); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetched, err := repo.GetByHash("hash-abc123")
	if err != nil {
		t.Fatalf("GetByHash failed: %v", err)
	}
	if fetched.ID != key.ID {
		t.Errorf("GetByHash returned wrong key")
	}
	if fetched.Name != "test-key" {
		t.Errorf("expected name %q, got %q", "test-key", fetched.Name)
	}
}

func TestAPIKeyRepositoryGetByHashNotFound(t *testing.T) {
	_, repo := newTestAPIKeyRepository(t)

	_, err := repo.GetByHash("nonexistent-hash")
	if err == nil {
		t.Errorf("expected error for non-existent hash")
	}
}

func TestAPIKeyRepositoryGetByID(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := seedAPIKey(t, db, userID, "id-key", "hash-id-1", "gc_")

	fetched, err := repo.GetByID(key.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if fetched.Name != "id-key" {
		t.Errorf("expected name id-key, got %q", fetched.Name)
	}
}

func TestAPIKeyRepositoryGetByIDNotFound(t *testing.T) {
	_, repo := newTestAPIKeyRepository(t)

	_, err := repo.GetByID(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent ID")
	}
}

func TestAPIKeyRepositoryGetByHashWithRoles(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := seedAPIKey(t, db, userID, "roles-key", "hash-roles-1", "gc_")
	role := seedRole(t, db, "api-role")
	perm := seedPermission(t, db, "api.read")

	// Assign permission to role
	if err := db.Exec("INSERT INTO role_permissions (role_id, permission_id, created_at) VALUES (?, ?, datetime('now'))",
		role.ID.String(), perm.ID.String()).Error; err != nil {
		t.Fatalf("failed to assign permission to role: %v", err)
	}

	// Assign role to API key
	if err := db.Exec("INSERT INTO api_key_roles (api_key_id, role_id, created_at) VALUES (?, ?, datetime('now'))",
		key.ID.String(), role.ID.String()).Error; err != nil {
		t.Fatalf("failed to assign role to API key: %v", err)
	}

	fetched, err := repo.GetByHashWithRoles("hash-roles-1")
	if err != nil {
		t.Fatalf("GetByHashWithRoles failed: %v", err)
	}
	if len(fetched.Roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(fetched.Roles))
	}
	if fetched.Roles[0].Name != "api-role" {
		t.Errorf("expected role name api-role, got %q", fetched.Roles[0].Name)
	}
	if len(fetched.Roles[0].Permissions) != 1 {
		t.Fatalf("expected 1 permission on role, got %d", len(fetched.Roles[0].Permissions))
	}
}

func TestAPIKeyRepositoryGetByIDWithRoles(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := seedAPIKey(t, db, userID, "id-roles-key", "hash-id-roles-1", "gc_")
	role := seedRole(t, db, "viewer")

	if err := db.Exec("INSERT INTO api_key_roles (api_key_id, role_id, created_at) VALUES (?, ?, datetime('now'))",
		key.ID.String(), role.ID.String()).Error; err != nil {
		t.Fatalf("failed to assign role: %v", err)
	}

	fetched, err := repo.GetByIDWithRoles(key.ID)
	if err != nil {
		t.Fatalf("GetByIDWithRoles failed: %v", err)
	}
	if len(fetched.Roles) != 1 || fetched.Roles[0].Name != "viewer" {
		t.Errorf("expected role viewer to be preloaded")
	}
}

func TestAPIKeyRepositoryGetUserKeys(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	otherUserID := uuid.New()

	seedAPIKey(t, db, userID, "key-1", "hash-u1", "gc_")
	seedAPIKey(t, db, userID, "key-2", "hash-u2", "gc_")
	seedAPIKey(t, db, otherUserID, "other-key", "hash-o1", "gc_")

	keys, err := repo.GetUserKeys(userID)
	if err != nil {
		t.Fatalf("GetUserKeys failed: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys for user, got %d", len(keys))
	}
}

func TestAPIKeyRepositoryGetUserKeysExcludesRevoked(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	activeKey := seedAPIKey(t, db, userID, "active-key", "hash-active", "gc_")
	revokedKey := seedAPIKey(t, db, userID, "revoked-key", "hash-revoked", "gc_")

	if err := repo.Revoke(revokedKey.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	keys, err := repo.GetUserKeys(userID)
	if err != nil {
		t.Fatalf("GetUserKeys failed: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 active key, got %d", len(keys))
	}
	if keys[0].ID != activeKey.ID {
		t.Errorf("expected active key to be returned")
	}
}

func TestAPIKeyRepositoryGetUserKeysPaginated(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	for i := 0; i < 5; i++ {
		seedAPIKey(t, db, userID, "key-"+uuid.New().String(), "hash-"+uuid.New().String(), "gc_")
	}

	keys, total, err := repo.GetUserKeysPaginated(userID, 0, 3)
	if err != nil {
		t.Fatalf("GetUserKeysPaginated failed: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys in page, got %d", len(keys))
	}

	// Second page
	keys2, total2, err := repo.GetUserKeysPaginated(userID, 3, 3)
	if err != nil {
		t.Fatalf("GetUserKeysPaginated page 2 failed: %v", err)
	}
	if total2 != 5 {
		t.Errorf("expected total 5 on page 2, got %d", total2)
	}
	if len(keys2) != 2 {
		t.Errorf("expected 2 keys on page 2, got %d", len(keys2))
	}
}

func TestAPIKeyRepositoryGetAll(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	user1 := uuid.New()
	user2 := uuid.New()
	seedAPIKey(t, db, user1, "key-a", "hash-a", "gc_")
	seedAPIKey(t, db, user2, "key-b", "hash-b", "gc_")

	keys, total, err := repo.GetAll(0, 10)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2, got %d", total)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestAPIKeyRepositoryGetAllPagination(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	for i := 0; i < 5; i++ {
		seedAPIKey(t, db, userID, "key-"+uuid.New().String(), "hash-"+uuid.New().String(), "gc_")
	}

	keys, total, err := repo.GetAll(2, 2)
	if err != nil {
		t.Fatalf("GetAll with offset failed: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys with offset, got %d", len(keys))
	}
}

func TestAPIKeyRepositoryRevoke(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := seedAPIKey(t, db, userID, "to-revoke", "hash-rev", "gc_")

	if err := repo.Revoke(key.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	fetched, err := repo.GetByID(key.ID)
	if err != nil {
		t.Fatalf("GetByID after revoke failed: %v", err)
	}
	if !fetched.Revoked {
		t.Errorf("expected key to be revoked")
	}
}

func TestAPIKeyRepositoryRevokeNotFound(t *testing.T) {
	_, repo := newTestAPIKeyRepository(t)

	err := repo.Revoke(uuid.New())
	if err == nil {
		t.Errorf("expected error when revoking non-existent key")
	}
}

func TestAPIKeyRepositoryUpdateLastUsed(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := seedAPIKey(t, db, userID, "last-used", "hash-lu", "gc_")

	if err := repo.UpdateLastUsed(key.ID); err != nil {
		t.Fatalf("UpdateLastUsed failed: %v", err)
	}

	fetched, err := repo.GetByID(key.ID)
	if err != nil {
		t.Fatalf("GetByID after UpdateLastUsed failed: %v", err)
	}
	if fetched.LastUsedAt == nil {
		t.Errorf("expected LastUsedAt to be set")
	}
}

func TestAPIKeyRepositoryCleanupRevokedKeys(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	activeKey := seedAPIKey(t, db, userID, "active", "hash-active-c", "gc_")
	revokedKey := seedAPIKey(t, db, userID, "revoked-old", "hash-revoked-c", "gc_")

	// Revoke and backdate the updated_at
	if err := repo.Revoke(revokedKey.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if err := db.Exec("UPDATE api_keys SET updated_at = ? WHERE id = ?", oldTime, revokedKey.ID.String()).Error; err != nil {
		t.Fatalf("failed to backdate updated_at: %v", err)
	}

	// Create an expired key
	expiredKey := &domain.APIKey{
		ID:        uuid.New(),
		UserID:    userID,
		KeyHash:   "hash-expired-c",
		KeyPrefix: "gc_",
		Name:      "expired",
	}
	if err := db.Create(expiredKey).Error; err != nil {
		t.Fatalf("Create expired key failed: %v", err)
	}
	expTime := time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if err := db.Exec("UPDATE api_keys SET expires_at = ? WHERE id = ?", expTime, expiredKey.ID.String()).Error; err != nil {
		t.Fatalf("failed to set expires_at: %v", err)
	}

	// Cleanup revoked keys older than 24 hours
	if err := repo.CleanupRevokedKeys(24 * time.Hour); err != nil {
		t.Fatalf("CleanupRevokedKeys failed: %v", err)
	}

	// Active key should still exist
	_, err := repo.GetByID(activeKey.ID)
	if err != nil {
		t.Errorf("active key should still exist: %v", err)
	}

	// Revoked key should be soft-deleted
	var count int64
	if err := db.Model(&domain.APIKey{}).Where("id = ?", revokedKey.ID).Count(&count).Error; err != nil {
		t.Fatalf("count revoked key failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected revoked key to be cleaned up, found %d", count)
	}

	// Expired key should be soft-deleted
	if err := db.Model(&domain.APIKey{}).Where("id = ?", expiredKey.ID).Count(&count).Error; err != nil {
		t.Fatalf("count expired key failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected expired key to be cleaned up, found %d", count)
	}
}

func TestAPIKeyRepositoryAssignAndRemoveRole(t *testing.T) {
	db, repo := newTestAPIKeyRepository(t)

	userID := uuid.New()
	key := seedAPIKey(t, db, userID, "role-key", "hash-role-k", "gc_")
	role := seedRole(t, db, "editor")

	if err := repo.AssignRole(key.ID, role.ID); err != nil {
		t.Fatalf("AssignRole failed: %v", err)
	}

	// Verify role was assigned via GetByIDWithRoles
	fetched, err := repo.GetByIDWithRoles(key.ID)
	if err != nil {
		t.Fatalf("GetByIDWithRoles failed: %v", err)
	}
	if len(fetched.Roles) != 1 || fetched.Roles[0].ID != role.ID {
		t.Errorf("expected role to be assigned")
	}

	if err := repo.RemoveRole(key.ID, role.ID); err != nil {
		t.Fatalf("RemoveRole failed: %v", err)
	}

	fetched, err = repo.GetByIDWithRoles(key.ID)
	if err != nil {
		t.Fatalf("GetByIDWithRoles after removal failed: %v", err)
	}
	if len(fetched.Roles) != 0 {
		t.Errorf("expected no roles after removal, got %d", len(fetched.Roles))
	}
}
