package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

func TestPostRepository(t *testing.T) {
	db := SetupTestDB()
	repo := NewPostRepository(db)

	userID := uuid.New()
	categoryID := uuid.New()

	// Initial dependent setup
	cat := domain.Category{
		ID:          categoryID,
		Name:        "Test Category",
		Slug:        "test-category",
		Description: "A category for testing",
	}
	db.Create(&cat)

	t.Run("Create and GetByID", func(t *testing.T) {
		postID := uuid.New()
		post := &domain.Post{
			ID:          postID,
			Title:       "Test Post",
			Slug:        "test-post",
			ContentHTML: "<p>Test</p>",
			AuthorID:    userID,
			CategoryID:  &categoryID,
			Status:      domain.PostStatusDraft,
		}

		err := repo.Create(post)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}

		fetched, err := repo.GetByID(postID)
		if err != nil {
			t.Fatalf("expected nil error on get, got %v", err)
		}
		if fetched.Title != post.Title {
			t.Errorf("expected title %s, got %s", post.Title, fetched.Title)
		}
	})

	t.Run("Update", func(t *testing.T) {
		postID := uuid.New()
		post := &domain.Post{
			ID:         postID,
			Title:      "Original Title",
			Slug:       "original-title",
			AuthorID:   userID,
			CategoryID: &categoryID,
			Status:     domain.PostStatusDraft,
		}
		repo.Create(post)

		updatePost, _ := repo.GetByID(postID)
		updatePost.Title = "Updated Title"
		err := repo.Update(updatePost)
		if err != nil {
			t.Fatalf("update failed: %v", err)
		}

		fetched, _ := repo.GetByID(postID)
		if fetched.Title != "Updated Title" {
			t.Errorf("expected updated title %s, got %s", "Updated Title", fetched.Title)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		postID := uuid.New()
		post := &domain.Post{
			ID:         postID,
			Title:      "Delete Me",
			Slug:       "delete-me",
			AuthorID:   userID,
			CategoryID: &categoryID,
		}
		repo.Create(post)

		err := repo.Delete(postID)
		if err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		_, err = repo.GetByID(postID)
		if err == nil {
			t.Error("expected error getting deleted post, got nil")
		}
	})

	t.Run("GetBySlug", func(t *testing.T) {
		postID := uuid.New()
		post := &domain.Post{
			ID:         postID,
			Title:      "Get By Slug",
			Slug:       "get-by-slug",
			AuthorID:   userID,
			CategoryID: &categoryID,
			Status:     domain.PostStatusDraft,
		}
		repo.Create(post)

		fetched, err := repo.GetBySlug("get-by-slug")
		if err != nil {
			t.Fatalf("get by slug failed: %v", err)
		}
		if fetched.ID != postID {
			t.Errorf("expected id %v, got %v", postID, fetched.ID)
		}
	})

	t.Run("GetBySlugPublished", func(t *testing.T) {
		publishedID := uuid.New()
		repo.Create(&domain.Post{
			ID:         publishedID,
			Title:      "Published Post",
			Slug:       "published-post",
			AuthorID:   userID,
			CategoryID: &categoryID,
			Status:     domain.PostStatusPublished,
		})

		draftID := uuid.New()
		repo.Create(&domain.Post{
			ID:         draftID,
			Title:      "Draft Post",
			Slug:       "draft-post",
			AuthorID:   userID,
			CategoryID: &categoryID,
			Status:     domain.PostStatusDraft,
		})

		fetched, err := repo.GetBySlugPublished("published-post")
		if err != nil || fetched.ID != publishedID {
			t.Errorf("expected published post, got err %v", err)
		}

		_, err = repo.GetBySlugPublished("draft-post")
		if err == nil {
			t.Error("expected error getting draft post via published query, got nil")
		}
	})

	t.Run("ListFiltered", func(t *testing.T) {
		// Create various posts to filter
		catID2 := uuid.New()
		db.Create(&domain.Category{ID: catID2, Name: "Cat2"})

		feat := true
		for i := 0; i < 5; i++ {
			repo.Create(&domain.Post{
				ID:         uuid.New(),
				Title:      "Filter Post " + string(rune(i)),
				Slug:       "filter-post-" + string(rune(i)),
				AuthorID:   userID,
				CategoryID: &catID2,
				Status:     domain.PostStatusPublished,
				IsFeatured: feat,
			})
			feat = false // only 1 featured
		}

		posts, total, err := repo.ListFiltered(PostListFilter{
			CategoryID: &catID2,
			Status:     string(domain.PostStatusPublished),
			Limit:      10,
		})
		if err != nil {
			t.Fatalf("ListFiltered failed: %v", err)
		}
		if total != 5 {
			t.Errorf("expected 5 posts, got %d", total)
		}
		if len(posts) != 5 {
			t.Errorf("expected 5 posts, got %d", len(posts))
		}

		isFeat := true
		posts, total, err = repo.ListFiltered(PostListFilter{
			CategoryID: &catID2,
			IsFeatured: &isFeat,
		})
		if total != 1 {
			t.Errorf("expected 1 featured post, got %d", total)
		}
	})

	t.Run("ExistsBySlug and CountByStatus", func(t *testing.T) {
		slug := "exist-test-slug"
		postID := uuid.New()
		repo.Create(&domain.Post{
			ID:         postID,
			Title:      "Exist Test",
			Slug:       slug,
			AuthorID:   userID,
			CategoryID: &categoryID,
			Status:     domain.PostStatusArchived,
		})

		exists, err := repo.ExistsBySlug(slug)
		if !exists || err != nil {
			t.Errorf("expected true, got %v, %v", exists, err)
		}

		exists, err = repo.ExistsBySlugExcluding(slug, postID)
		if exists || err != nil {
			t.Errorf("expected false, got %v, %v", exists, err)
		}

		count, err := repo.CountByStatus(string(domain.PostStatusArchived))
		if err != nil || count < 1 {
			t.Error("expected at least 1 archived post")
		}
	})

	t.Run("Revisions", func(t *testing.T) {
		postID := uuid.New()
		repo.Create(&domain.Post{ID: postID, Title: "Rev Post", Slug: "rev-post"})

		rev1 := domain.PostRevision{
			ID:          uuid.New(),
			PostID:      postID,
			Version:     1,
			ContentHTML: "Version 1",
		}
		err := repo.CreateRevision(&rev1)
		if err != nil {
			t.Fatalf("CreateRevision failed: %v", err)
		}

		revs, total, err := repo.ListRevisions(postID, 0, 20)
		if err != nil || len(revs) != 1 || total != 1 {
			t.Errorf("ListRevisions expected 1, got %v (total=%d)", len(revs), total)
		}

		rev, err := repo.GetRevision(rev1.ID)
		if err != nil || rev.ContentHTML != "Version 1" {
			t.Errorf("GetRevision failed")
		}

		v, err := repo.GetLatestRevisionVersion(postID)
		if err != nil || v != 1 {
			t.Errorf("expected version 1, got %v", v)
		}
	})

	t.Run("Media", func(t *testing.T) {
		postID := uuid.New()
		repo.Create(&domain.Post{ID: postID, Title: "Media Post", Slug: "med-post"})

		mediaID := uuid.New()
		media := domain.PostMedia{
			ID:         mediaID,
			PostID:     postID,
			S3Key:      "path/to/image.png",
			Filename:   "image.png",
			MediaType:  domain.MediaTypeImage,
			UploaderID: userID,
			FileSize:   1024,
		}
		err := repo.CreateMedia(&media)
		if err != nil {
			t.Fatalf("CreateMedia failed: %v", err)
		}

		list, err := repo.ListMediaByPost(postID)
		if err != nil || len(list) != 1 {
			t.Errorf("ListMediaByPost expected 1")
		}

		m, err := repo.GetMediaByID(mediaID)
		if err != nil || m.S3Key != media.S3Key {
			t.Errorf("GetMediaByID failed")
		}

		err = repo.DeleteMedia(mediaID)
		if err != nil {
			t.Fatalf("DeleteMedia failed: %v", err)
		}
	})

	t.Run("ReplaceTags", func(t *testing.T) {
		postID := uuid.New()
		repo.Create(&domain.Post{ID: postID, Title: "Tag Post", Slug: "tag-post"})

		tag1ID, tag2ID := uuid.New(), uuid.New()
		db.Create(&domain.Tag{ID: tag1ID, Name: "Tag1", Slug: "tag1"})
		db.Create(&domain.Tag{ID: tag2ID, Name: "Tag2", Slug: "tag2"})

		err := repo.ReplaceTags(postID, []uuid.UUID{tag1ID, tag2ID})
		if err != nil {
			t.Fatalf("ReplaceTags failed: %v", err)
		}

		// Verify tags are associated
		var count int64
		db.Model(&domain.PostTag{}).Where("post_id = ?", postID).Count(&count)
		if count != 2 {
			t.Errorf("expected 2 tags, got %v", count)
		}
	})

	t.Run("WithTx", func(t *testing.T) {
		tx := db.Begin()
		txRepo := repo.WithTx(tx)

		postID := uuid.New()
		txRepo.Create(&domain.Post{ID: postID, Title: "Tx Post", Slug: "tx-post"})
		tx.Commit()

		_, err := repo.GetByID(postID)
		if err != nil {
			t.Errorf("expected to find post committed in tx, got err: %v", err)
		}
	})
}
