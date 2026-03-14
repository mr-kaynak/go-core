package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func newTestPermissionRepository(t *testing.T) (*gorm.DB, PermissionRepository) {
	t.Helper()
	db := setupTestDB(t)
	return db, NewPermissionRepository(db)
}

func TestPermissionRepositoryCreate(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	perm := &domain.Permission{
		ID:          uuid.New(),
		Name:        "users.create",
		Description: "Create users",
		Category:    "users",
	}

	if err := repo.Create(perm); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
}

func TestPermissionRepositoryGetByID(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	perm := &domain.Permission{
		ID:          uuid.New(),
		Name:        "users.read",
		Description: "Read users",
		Category:    "users",
	}
	if err := repo.Create(perm); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetched, err := repo.GetByID(perm.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if fetched.Name != "users.read" {
		t.Errorf("expected name users.read, got %q", fetched.Name)
	}
	if fetched.Category != "users" {
		t.Errorf("expected category users, got %q", fetched.Category)
	}
}

func TestPermissionRepositoryGetByIDNotFound(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	_, err := repo.GetByID(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent permission ID")
	}
}

func TestPermissionRepositoryGetByName(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	perm := &domain.Permission{
		ID:   uuid.New(),
		Name: "roles.manage",
	}
	if err := repo.Create(perm); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetched, err := repo.GetByName("roles.manage")
	if err != nil {
		t.Fatalf("GetByName failed: %v", err)
	}
	if fetched.ID != perm.ID {
		t.Errorf("GetByName returned wrong permission")
	}
}

func TestPermissionRepositoryGetByNameNotFound(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	_, err := repo.GetByName("nonexistent.permission")
	if err == nil {
		t.Errorf("expected error for non-existent permission name")
	}
}

func TestPermissionRepositoryGetAll(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	for i := 0; i < 3; i++ {
		perm := &domain.Permission{
			ID:   uuid.New(),
			Name: "perm-" + uuid.New().String(),
		}
		if err := repo.Create(perm); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	perms, err := repo.GetAll(0, 10)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(perms) != 3 {
		t.Errorf("expected 3 permissions, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetAllPagination(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	for i := 0; i < 5; i++ {
		perm := &domain.Permission{
			ID:   uuid.New(),
			Name: "pg-perm-" + uuid.New().String(),
		}
		if err := repo.Create(perm); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	perms, err := repo.GetAll(2, 2)
	if err != nil {
		t.Fatalf("GetAll with offset failed: %v", err)
	}
	if len(perms) != 2 {
		t.Errorf("expected 2 permissions with offset, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetAllClampLimit(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	for i := 0; i < 3; i++ {
		perm := &domain.Permission{
			ID:   uuid.New(),
			Name: "clamp-" + uuid.New().String(),
		}
		if err := repo.Create(perm); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	perms, err := repo.GetAll(0, -1)
	if err != nil {
		t.Fatalf("GetAll with negative limit failed: %v", err)
	}
	if len(perms) != 3 {
		t.Errorf("expected 3 permissions with clamped limit, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetByCategory(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	for _, name := range []string{"users.create", "users.read", "users.delete"} {
		perm := &domain.Permission{
			ID:       uuid.New(),
			Name:     name,
			Category: "users",
		}
		if err := repo.Create(perm); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	other := &domain.Permission{
		ID:       uuid.New(),
		Name:     "roles.manage",
		Category: "roles",
	}
	if err := repo.Create(other); err != nil {
		t.Fatalf("Create other failed: %v", err)
	}

	perms, err := repo.GetByCategory("users")
	if err != nil {
		t.Fatalf("GetByCategory failed: %v", err)
	}
	if len(perms) != 3 {
		t.Errorf("expected 3 user permissions, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetByCategoryEmpty(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	perms, err := repo.GetByCategory("nonexistent")
	if err != nil {
		t.Fatalf("GetByCategory for unknown category failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected 0 permissions, got %d", len(perms))
	}
}

func TestPermissionRepositoryCount(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	for i := 0; i < 4; i++ {
		perm := &domain.Permission{
			ID:   uuid.New(),
			Name: "cnt-" + uuid.New().String(),
		}
		if err := repo.Create(perm); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	count, err := repo.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 4 {
		t.Errorf("expected count 4, got %d", count)
	}
}

func TestPermissionRepositoryUpdate(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	perm := &domain.Permission{
		ID:          uuid.New(),
		Name:        "update.perm",
		Description: "original",
	}
	if err := repo.Create(perm); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	perm.Description = "updated"
	perm.Category = "admin"
	if err := repo.Update(perm); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	fetched, err := repo.GetByID(perm.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if fetched.Description != "updated" {
		t.Errorf("expected description 'updated', got %q", fetched.Description)
	}
	if fetched.Category != "admin" {
		t.Errorf("expected category 'admin', got %q", fetched.Category)
	}
}

func TestPermissionRepositoryDelete(t *testing.T) {
	db, repo := newTestPermissionRepository(t)

	perm := &domain.Permission{
		ID:   uuid.New(),
		Name: "delete.perm",
	}
	if err := repo.Create(perm); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.Delete(perm.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	if err := db.Model(&domain.Permission{}).Where("id = ?", perm.ID).Count(&count).Error; err != nil {
		t.Fatalf("count after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected permission to be soft deleted, found %d", count)
	}
}

func TestPermissionRepositoryAddAndRemovePermissionToRole(t *testing.T) {
	db, repo := newTestPermissionRepository(t)

	role := seedRole(t, db, "test-role")
	perm := &domain.Permission{
		ID:   uuid.New(),
		Name: "test.permission",
	}
	if err := repo.Create(perm); err != nil {
		t.Fatalf("Create permission failed: %v", err)
	}

	if err := repo.AddPermissionToRole(role.ID, perm.ID); err != nil {
		t.Fatalf("AddPermissionToRole failed: %v", err)
	}

	rolePerms, err := repo.GetRolePermissions(role.ID)
	if err != nil {
		t.Fatalf("GetRolePermissions failed: %v", err)
	}
	if len(rolePerms) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(rolePerms))
	}
	if rolePerms[0].ID != perm.ID {
		t.Errorf("expected permission ID %v, got %v", perm.ID, rolePerms[0].ID)
	}

	if err := repo.RemovePermissionFromRole(role.ID, perm.ID); err != nil {
		t.Fatalf("RemovePermissionFromRole failed: %v", err)
	}

	rolePerms, err = repo.GetRolePermissions(role.ID)
	if err != nil {
		t.Fatalf("GetRolePermissions after removal failed: %v", err)
	}
	if len(rolePerms) != 0 {
		t.Errorf("expected 0 permissions after removal, got %d", len(rolePerms))
	}
}

func TestPermissionRepositoryGetRolePermissionsEmpty(t *testing.T) {
	db, repo := newTestPermissionRepository(t)

	role := seedRole(t, db, "empty-role")

	perms, err := repo.GetRolePermissions(role.ID)
	if err != nil {
		t.Fatalf("GetRolePermissions for empty role failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected 0 permissions for role with no assignments, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetUserPermissions(t *testing.T) {
	db, repo := newTestPermissionRepository(t)

	user := seedUser(t, db, "perms@example.com", "perm-user")
	role := seedRole(t, db, "perm-role")
	perm1 := &domain.Permission{
		ID:   uuid.New(),
		Name: "perm1",
	}
	perm2 := &domain.Permission{
		ID:   uuid.New(),
		Name: "perm2",
	}
	if err := repo.Create(perm1); err != nil {
		t.Fatalf("Create perm1 failed: %v", err)
	}
	if err := repo.Create(perm2); err != nil {
		t.Fatalf("Create perm2 failed: %v", err)
	}

	// Assign permissions to role
	if err := repo.AddPermissionToRole(role.ID, perm1.ID); err != nil {
		t.Fatalf("AddPermissionToRole perm1 failed: %v", err)
	}
	if err := repo.AddPermissionToRole(role.ID, perm2.ID); err != nil {
		t.Fatalf("AddPermissionToRole perm2 failed: %v", err)
	}

	// Assign role to user
	if err := db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
		user.ID.String(), role.ID.String()).Error; err != nil {
		t.Fatalf("failed to assign role to user: %v", err)
	}

	perms, err := repo.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("GetUserPermissions failed: %v", err)
	}
	if len(perms) != 2 {
		t.Errorf("expected 2 permissions for user, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetUserPermissionsEmpty(t *testing.T) {
	_, repo := newTestPermissionRepository(t)

	perms, err := repo.GetUserPermissions(uuid.New())
	if err != nil {
		t.Fatalf("GetUserPermissions for user with no roles failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected 0 permissions, got %d", len(perms))
	}
}

func TestPermissionRepositoryGetRolePermissionsMultiple(t *testing.T) {
	db, repo := newTestPermissionRepository(t)

	role := seedRole(t, db, "multi-role")
	for i := 0; i < 3; i++ {
		perm := &domain.Permission{
			ID:   uuid.New(),
			Name: "multi-perm-" + uuid.New().String(),
		}
		if err := repo.Create(perm); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if err := repo.AddPermissionToRole(role.ID, perm.ID); err != nil {
			t.Fatalf("AddPermissionToRole failed: %v", err)
		}
	}

	perms, err := repo.GetRolePermissions(role.ID)
	if err != nil {
		t.Fatalf("GetRolePermissions failed: %v", err)
	}
	if len(perms) != 3 {
		t.Errorf("expected 3 permissions, got %d", len(perms))
	}
}
