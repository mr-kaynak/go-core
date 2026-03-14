package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

func TestCommentRepository(t *testing.T) {
	db := SetupTestDB()
	repo := NewCommentRepository(db)

	postID := uuid.New()
	userID := uuid.New()

	db.Create(&domain.Post{ID: postID, Title: "Comment Post", Slug: "comment-post", AuthorID: userID})

	t.Run("Create and Get", func(t *testing.T) {
		commentID := uuid.New()
		comment := &domain.Comment{
			ID:       commentID,
			PostID:   postID,
			AuthorID: &userID,
			Content:  "This is a comment",
			Status:   domain.CommentStatusApproved,
		}

		err := repo.Create(comment)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		fetched, err := repo.GetByID(commentID)
		if err != nil || fetched.Content != "This is a comment" {
			t.Errorf("GetByID failed")
		}
	})

	t.Run("Update and Delete", func(t *testing.T) {
		commentID := uuid.New()
		comment := &domain.Comment{
			ID:       commentID,
			PostID:   postID,
			AuthorID: &userID,
			Content:  "To Update",
			Status:   domain.CommentStatusPending,
		}
		repo.Create(comment)

		comment.Content = "Updated"
		comment.Status = domain.CommentStatusApproved
		err := repo.Update(comment)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		fetched, _ := repo.GetByID(commentID)
		if fetched.Content != "Updated" || fetched.Status != domain.CommentStatusApproved {
			t.Errorf("expected Updated and Approved, got %s and %s", fetched.Content, fetched.Status)
		}

		err = repo.Delete(commentID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("ListPending", func(t *testing.T) {
		postID2 := uuid.New()
		db.Create(&domain.Post{ID: postID2, Title: "Filter Post", Slug: "filter-p", AuthorID: userID})

		repo.Create(&domain.Comment{
			ID:       uuid.New(),
			PostID:   postID2,
			AuthorID: &userID,
			Content:  "A",
			Status:   domain.CommentStatusApproved,
		})
		repo.Create(&domain.Comment{
			ID:       uuid.New(),
			PostID:   postID2,
			AuthorID: &userID,
			Content:  "B",
			Status:   domain.CommentStatusPending,
		})

		comments, total, err := repo.ListPending(0, 10)

		if err != nil || total != 1 || len(comments) != 1 {
			t.Errorf("ListPending failed: expected 1 pending comment, got %d", total)
		}
	})

	t.Run("GetThreaded", func(t *testing.T) {
		postID3 := uuid.New()
		db.Create(&domain.Post{ID: postID3, Title: "Tree Post", Slug: "tree-p", AuthorID: userID})

		rootID := uuid.New()
		repo.Create(&domain.Comment{
			ID:        rootID,
			PostID:    postID3,
			AuthorID:  &userID,
			Content:   "Root",
			Status:    domain.CommentStatusApproved,
			CreatedAt: time.Now().Add(-2 * time.Hour),
		})

		childID := uuid.New()
		repo.Create(&domain.Comment{
			ID:        childID,
			PostID:    postID3,
			AuthorID:  &userID,
			ParentID:  &rootID,
			Content:   "Child",
			Status:    domain.CommentStatusApproved,
			CreatedAt: time.Now(),
		})

		threaded, err := repo.GetThreaded(postID3)
		if err != nil {
			t.Fatalf("GetThreaded failed: %v", err)
		}
		if len(threaded) != 1 {
			t.Errorf("expected 1 root comment, got %d", len(threaded))
		}
		if len(threaded[0].Children) != 1 {
			t.Errorf("expected 1 reply, got %d", len(threaded[0].Children))
		}
	})

	t.Run("CountByPost", func(t *testing.T) {
		count, err := repo.CountByPost(postID)
		if err != nil || count < 0 {
			t.Errorf("CountByPost failed")
		}
	})
}
