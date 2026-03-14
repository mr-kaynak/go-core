package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

func TestPostService(t *testing.T) {
	db, _ := SetupTestEnv()
	postRepo := repository.NewPostRepository(db)
	catRepo := repository.NewCategoryRepository(db)
	tagRepo := repository.NewTagRepository(db)

	contentSvc := NewContentService()
	slugSvc := NewSlugService()
	readTimeSvc := NewReadTimeService(200)

	svc := NewPostService(db, postRepo, catRepo, tagRepo, contentSvc, slugSvc, readTimeSvc)

	ctx := context.Background()
	authorID := uuid.New()
	
	t.Run("CreateDraft", func(t *testing.T) {
		draft, err := svc.CreateDraft(ctx, authorID)
		if err != nil {
			t.Fatalf("CreateDraft failed: %v", err)
		}
		if draft.Status != domain.PostStatusDraft {
			t.Errorf("expected Draft status")
		}
	})

	t.Run("Create and Update Post", func(t *testing.T) {
		validJSON := json.RawMessage(`[{"type":"paragraph","children":[{"text":"Hello World"}]}]`)

		req := &CreatePostRequest{
			Title:       "Test Post",
			ContentJSON: validJSON,
			Excerpt:     "Test Excerpt",
		}
		
		post, err := svc.Create(ctx, req, authorID)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		
		if post.Title != "Test Post" {
			t.Errorf("expected 'Test Post', got %s", post.Title)
		}
		
		// Ensure a revision was created
		revisions, err := svc.ListRevisions(post.ID)
		if err != nil || len(revisions) != 1 {
			t.Errorf("expected 1 revision, got %d", len(revisions))
		}

		// Update Post
		newTitle := "Updated Test Post"
		updateReq := &UpdatePostRequest{
			Title: &newTitle,
		}
		
		updatedPost, err := svc.Update(ctx, post.ID, updateReq, authorID, false)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}
		
		if updatedPost.Title != "Updated Test Post" {
			t.Errorf("expected 'Updated Test Post', got %s", updatedPost.Title)
		}
		
		// Ensure another revision was created
		revisions, _ = svc.ListRevisions(post.ID)
		if len(revisions) != 2 {
			t.Errorf("expected 2 revisions after update, got %d", len(revisions))
		}
	})

	t.Run("Transitions (Publish and Archive)", func(t *testing.T) {
		validJSON := json.RawMessage(`[{"type":"paragraph","children":[{"text":"Transitions"}]}]`)
		req := &CreatePostRequest{
			Title:       "Transitions Post",
			ContentJSON: validJSON,
		}
		post, err := svc.Create(ctx, req, authorID)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		published, err := svc.Publish(ctx, post.ID, authorID, false)
		if err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
		if published.Status != domain.PostStatusPublished {
			t.Errorf("expected published status")
		}

		archived, err := svc.Archive(ctx, post.ID, authorID, false)
		if err != nil {
			t.Fatalf("Archive failed: %v", err)
		}
		if archived.Status != domain.PostStatusArchived {
			t.Errorf("expected archived status")
		}
	})
	
	t.Run("Delete", func(t *testing.T) {
		req := &CreatePostRequest{
			Title:       "To Delete",
			ContentJSON: json.RawMessage(`[{"type":"paragraph","children":[{"text":"Delete Me"}]}]`),
		}
		post, _ := svc.Create(ctx, req, authorID)
		
		err := svc.Delete(ctx, post.ID, authorID, false)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("Read Operations", func(t *testing.T) {
		req := &CreatePostRequest{
			Title:       "Read Operations Post",
			ContentJSON: json.RawMessage(`[{"type":"paragraph","children":[{"text":"Read Me"}]}]`),
		}
		post, _ := svc.Create(ctx, req, authorID)
		svc.Publish(ctx, post.ID, authorID, false)

		fetched, err := svc.GetBySlug(post.Slug)
		if err != nil || fetched.ID != post.ID {
			t.Errorf("GetBySlug failed")
		}

		editable, err := svc.GetForEdit(post.ID, authorID, false)
		if err != nil || editable.ID != post.ID {
			t.Errorf("GetForEdit failed")
		}

		posts, count, err := svc.List(repository.PostListFilter{Status: string(domain.PostStatusPublished)})
		if err != nil || count == 0 || len(posts) == 0 {
			t.Errorf("List failed")
		}

		revisions, _ := svc.ListRevisions(post.ID)
		if len(revisions) > 0 {
			rev, err := svc.GetRevision(post.ID, revisions[0].ID)
			if err != nil || rev.ID != revisions[0].ID {
				t.Errorf("GetRevision failed")
			}
		}
	})
}
