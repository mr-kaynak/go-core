package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

func TestTagRepository(t *testing.T) {
	db := SetupTestDB()
	repo := NewTagRepository(db)

	t.Run("Create and Get", func(t *testing.T) {
		tagID := uuid.New()
		tag := &domain.Tag{
			ID:   tagID,
			Name: "GoLang",
			Slug: "golang",
		}

		err := repo.Create(tag)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		fetched, err := repo.GetByID(tagID)
		if err != nil || fetched.Name != "GoLang" {
			t.Errorf("GetByID failed")
		}

		fetchedBySlug, err := repo.GetBySlug("golang")
		if err != nil || fetchedBySlug.ID != tagID {
			t.Errorf("GetBySlug failed")
		}
	})

	t.Run("Update and Delete", func(t *testing.T) {
		tagID := uuid.New()
		tag := &domain.Tag{ID: tagID, Name: "React", Slug: "react"}
		repo.Create(tag)

		tag.Name = "ReactJS"
		err := repo.Update(tag)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		fetched, _ := repo.GetByID(tagID)
		if fetched.Name != "ReactJS" {
			t.Errorf("expected ReactJS, got %s", fetched.Name)
		}

		err = repo.Delete(tagID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("GetOrCreateByNames", func(t *testing.T) {
		id1 := uuid.New()
		repo.Create(&domain.Tag{ID: id1, Name: "A", Slug: "a"})

		tags, err := repo.GetOrCreateByNames([]string{"A", "B", "C"}, func(s string) string { return s })
		if err != nil || len(tags) != 3 {
			t.Errorf("GetOrCreateByNames failed: expected 3 tags, got %d", len(tags))
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		idSearch := uuid.New()
		repo.Create(&domain.Tag{ID: idSearch, Name: "Searchable", Slug: "searchable"})

		tags, total, err := repo.GetAll(0, 10)
		if err != nil || total < 1 || len(tags) < 1 {
			t.Errorf("GetAll failed")
		}
	})

	t.Run("GetPopular", func(t *testing.T) {
		tags, err := repo.GetPopular(5)
		if err != nil {
			t.Errorf("GetPopular failed: %v", err)
		}
		_ = tags // We just test it doesn't crash as the query is complex with JOINs
	})

	t.Run("ExistsBySlug", func(t *testing.T) {
		tagID := uuid.New()
		repo.Create(&domain.Tag{ID: tagID, Name: "Ex", Slug: "ex"})

		exists, err := repo.ExistsBySlug("ex")
		if !exists || err != nil {
			t.Errorf("ExistsBySlug failed")
		}


	})
}
