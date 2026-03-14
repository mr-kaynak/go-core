package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

func TestCommentService(t *testing.T) {
	db, _ := SetupTestEnv()
	catRepo := repository.NewCategoryRepository(db)
	postRepo := repository.NewPostRepository(db)
	commentRepo := repository.NewCommentRepository(db)

	cfg := &config.Config{Blog: config.BlogConfig{AutoApproveComments: false}}
	svc := NewCommentService(cfg, commentRepo, postRepo)

	ctx := context.Background()
	authorID := uuid.New()
	catID := uuid.New()
	catRepo.Create(&domain.Category{ID: catID, Name: "Cat", Slug: "cat"})

	postID := uuid.New()
	publishedPost := &domain.Post{
		ID:         postID,
		Title:      "Published Post",
		Slug:       "pub-post",
		AuthorID:   authorID,
		CategoryID: &catID,
		Status:     domain.PostStatusPublished,
	}
	postRepo.Create(publishedPost)

	draftID := uuid.New()
	draftPost := &domain.Post{
		ID:         draftID,
		Title:      "Draft Post",
		Slug:       "draft-post",
		AuthorID:   authorID,
		CategoryID: &catID,
		Status:     domain.PostStatusDraft,
	}
	postRepo.Create(draftPost)

	t.Run("Create Comment on Published Post", func(t *testing.T) {
		req := &CreateCommentRequest{
			Content:   "Nice article!",
			GuestName: "John Doe",
		}
		comment, err := svc.Create(ctx, postID, req, nil)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if comment.Status != domain.CommentStatusPending {
			t.Errorf("expected pending status, got %s", comment.Status)
		}
	})

	t.Run("Create Comment on Draft Post", func(t *testing.T) {
		req := &CreateCommentRequest{Content: "Should fail"}
		_, err := svc.Create(ctx, draftID, req, &authorID)
		if err == nil {
			t.Errorf("expected error when commenting on draft")
		}
	})

	t.Run("Approve and Reject", func(t *testing.T) {
		req := &CreateCommentRequest{Content: "To Moderate"}
		comment, _ := svc.Create(ctx, postID, req, &authorID)

		approved, err := svc.Approve(ctx, comment.ID)
		if err != nil || approved.Status != domain.CommentStatusApproved {
			t.Errorf("Approve failed")
		}

		// Rejecting an approved comment should fail
		_, err = svc.Reject(ctx, comment.ID)
		if err == nil {
			t.Errorf("expected error rejecting approved comment")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		req := &CreateCommentRequest{Content: "To Delete"}
		comment, _ := svc.Create(ctx, postID, req, &authorID)

		// Non-author tries to delete (not admin)
		err := svc.Delete(ctx, comment.ID, uuid.New(), false)
		if err == nil {
			t.Errorf("expected forbidden error")
		}

		// Author deletes
		err = svc.Delete(ctx, comment.ID, authorID, false)
		if err != nil {
			t.Errorf("Delete failed: %v", err)
		}

		// Admin deletes
		req2 := &CreateCommentRequest{Content: "Admin Delete"}
		comment2, _ := svc.Create(ctx, postID, req2, &authorID)
		err = svc.Delete(ctx, comment2.ID, uuid.New(), true)
		if err != nil {
			t.Errorf("Admin delete failed")
		}
	})

	t.Run("GetThreaded and Pending", func(t *testing.T) {
		reqRoot := &CreateCommentRequest{Content: "Root"}
		root, _ := svc.Create(ctx, postID, reqRoot, &authorID)

		reqChild := &CreateCommentRequest{Content: "Child", ParentID: ptrString(root.ID.String())}
		svc.Create(ctx, postID, reqChild, &authorID)

		threaded, err := svc.GetThreaded(postID)
		if err != nil {
			t.Fatalf("GetThreaded failed: %v", err)
		}
		// Notice threading query handles pending exclusion/inclusion usually at the handler layer
		// or DB queries only specific statuses.
		_ = threaded

		pending, _, err := svc.ListPending(0, 10)
		if err != nil {
			t.Fatalf("ListPending failed: %v", err)
		}
		if len(pending) == 0 {
			t.Errorf("expected pending comments")
		}
	})
}
