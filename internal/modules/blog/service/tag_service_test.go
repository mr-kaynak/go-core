package service

import (
	"testing"

	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

func TestTagService(t *testing.T) {
	db, _ := SetupTestEnv()
	tagRepo := repository.NewTagRepository(db)
	slugSvc := NewSlugService()
	svc := NewTagService(tagRepo, slugSvc)

	t.Run("Create Success", func(t *testing.T) {
		req := &CreateTagRequest{Name: "Tag One"}
		tag, err := svc.Create(req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if tag.Slug != "tag-one" {
			t.Errorf("expected slug tag-one, got %s", tag.Slug)
		}
	})

	t.Run("Create Conflict", func(t *testing.T) {
		req := &CreateTagRequest{Name: "Tag One"}
		_, err := svc.Create(req)
		if err == nil {
			t.Errorf("expected conflict error")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		req := &CreateTagRequest{Name: "To Delete"}
		tag, _ := svc.Create(req)

		err := svc.Delete(tag.ID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}


	})

	t.Run("List and Popular", func(t *testing.T) {
		svc.Create(&CreateTagRequest{Name: "Pop Tag"})

		tags, total, err := svc.List(0, 10)
		if err != nil || total < 1 || len(tags) < 1 {
			t.Errorf("List failed")
		}

		pop, err := svc.GetPopular(5)
		if err != nil {
			t.Errorf("GetPopular failed")
		}
		_ = pop
	})

	t.Run("GetOrCreateByNames", func(t *testing.T) {
		tags, err := svc.GetOrCreateByNames([]string{"Pop Tag", "New Tag 1"})
		if err != nil || len(tags) != 2 {
			t.Errorf("GetOrCreateByNames failed")
		}
	})
}
