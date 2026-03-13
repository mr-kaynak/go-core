package repository

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func newTestRoleRepository(t *testing.T) (*gorm.DB, RoleRepository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.AutoMigrate(&domain.Role{}); err != nil {
		t.Fatalf("failed to run automigrate: %v", err)
	}

	return db, NewRoleRepository(db)
}

func TestRoleRepositoryCreateGetUpdateDelete(t *testing.T) {
	db, repo := newTestRoleRepository(t)

	role := &domain.Role{
		ID:   uuid.New(),
		Name: "tester",
	}

	if err := repo.Create(role); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	gotByID, err := repo.GetByID(role.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if gotByID.Name != role.Name {
		t.Errorf("GetByID returned wrong name")
	}

	gotByName, err := repo.GetByName(role.Name)
	if err != nil {
		t.Fatalf("GetByName failed: %v", err)
	}
	if gotByName.ID != role.ID {
		t.Errorf("GetByName returned wrong ID")
	}

	role.Description = "updated description"
	if err := repo.Update(role); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updated, err := repo.GetByID(role.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if updated.Description != "updated description" {
		t.Errorf("expected updated description, got %q", updated.Description)
	}

	if err := repo.Delete(role.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	if err := db.Model(&domain.Role{}).Where("id = ?", role.ID).Count(&count).Error; err != nil {
		t.Fatalf("count after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected role to be soft deleted, found %d records", count)
	}
}

func TestRoleRepositoryGetAllAndCount(t *testing.T) {
	_, repo := newTestRoleRepository(t)

	for i := 0; i < 3; i++ {
		role := &domain.Role{
			ID:   uuid.New(),
			Name: "role-" + uuid.New().String(),
		}
		if err := repo.Create(role); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	roles, err := repo.GetAll(0, 10)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(roles) != 3 {
		t.Errorf("expected 3 roles, got %d", len(roles))
	}

	count, err := repo.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestRoleRepositoryGetAllRespectsClampLimit(t *testing.T) {
	_, repo := newTestRoleRepository(t)

	for i := 0; i < 5; i++ {
		role := &domain.Role{
			ID:   uuid.New(),
			Name: "role-" + uuid.New().String(),
		}
		if err := repo.Create(role); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	roles, err := repo.GetAll(0, -1)
	if err != nil {
		t.Fatalf("GetAll with negative limit failed: %v", err)
	}
	if len(roles) == 0 {
		t.Errorf("expected some roles to be returned even with invalid limit")
	}
}

