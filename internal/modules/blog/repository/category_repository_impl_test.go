package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

func TestCategoryRepository(t *testing.T) {
	ctx := context.Background()

	db := SetupTestDB()
	repo := NewCategoryRepository(db)

	t.Run("Create and Get", func(t *testing.T) {
		catID := uuid.New()
		cat := &domain.Category{
			ID:          catID,
			Name:        "Test Category",
			Slug:        "test-category",
			Description: "Test Description",
		}

		err := repo.Create(ctx, cat)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		fetched, err := repo.GetByID(ctx, catID)
		if err != nil || fetched.Name != "Test Category" {
			t.Errorf("GetByID failed")
		}

		fetchedBySlug, err := repo.GetBySlug(ctx, "test-category")
		if err != nil || fetchedBySlug.ID != catID {
			t.Errorf("GetBySlug failed")
		}
	})

	t.Run("Update and Delete", func(t *testing.T) {
		catID := uuid.New()
		cat := &domain.Category{
			ID:   catID,
			Name: "To Update",
			Slug: "to-update",
		}
		repo.Create(ctx, cat)

		cat.Name = "Updated"
		err := repo.Update(ctx, cat)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		fetched, _ := repo.GetByID(ctx, catID)
		if fetched.Name != "Updated" {
			t.Errorf("expected Updated, got %s", fetched.Name)
		}

		err = repo.Delete(ctx, catID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		var count int64
		repo.WithTx(db).(*categoryRepositoryImpl).db.Model(&domain.Category{}).Where("id = ?", catID).Count(&count)
		if count != 0 {
			t.Errorf("expected category to be deleted")
		}
	})

	t.Run("GetAll and HasChildren", func(t *testing.T) {
		rootID := uuid.New()
		repo.Create(ctx, &domain.Category{ID: rootID, Name: "Root", Slug: "root"})

		childID := uuid.New()
		repo.Create(ctx, &domain.Category{ID: childID, Name: "Child", Slug: "child", ParentID: &rootID})

		all, err := repo.GetAll(ctx)
		if err != nil || len(all) == 0 {
			t.Errorf("GetAll failed")
		}

		hasChildren, err := repo.HasChildren(ctx, rootID)
		if err != nil || !hasChildren {
			t.Errorf("HasChildren failed")
		}

		hasPosts, err := repo.HasPosts(ctx, rootID)
		if err != nil {
			t.Errorf("HasPosts failed")
		}
		_ = hasPosts // Ensure query runs
	})

	t.Run("ExistsBySlug", func(t *testing.T) {
		catID := uuid.New()
		repo.Create(ctx, &domain.Category{ID: catID, Name: "Exists", Slug: "exists"})

		exists, err := repo.ExistsBySlug(ctx, "exists")
		if !exists || err != nil {
			t.Errorf("ExistsBySlug failed")
		}

		existsExcluding, err := repo.ExistsBySlugExcluding(ctx, "exists", catID)
		if existsExcluding || err != nil {
			t.Errorf("ExistsBySlugExcluding failed")
		}
	})
}
