package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

func TestPostRepository(t *testing.T) {
	ctx := context.Background()

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
		testPostRepositoryCreateAndGetByID(t, ctx, repo, userID, categoryID)
	})

	t.Run("Update", func(t *testing.T) {
		testPostRepositoryUpdate(t, ctx, repo, userID, categoryID)
	})

	t.Run("Delete", func(t *testing.T) {
		testPostRepositoryDelete(t, ctx, repo, userID, categoryID)
	})

	t.Run("GetBySlug", func(t *testing.T) {
		testPostRepositoryGetBySlug(t, ctx, repo, userID, categoryID)
	})

	t.Run("GetBySlugPublished", func(t *testing.T) {
		testPostRepositoryGetBySlugPublished(t, ctx, repo, userID, categoryID)
	})

	t.Run("ListFiltered", func(t *testing.T) {
		testPostRepositoryListFiltered(t, ctx, db, repo, userID)
	})

	t.Run("ExistsBySlug and CountByStatus", func(t *testing.T) {
		testPostRepositoryExistsBySlugAndCountByStatus(t, ctx, repo, userID, categoryID)
	})

	t.Run("Revisions", func(t *testing.T) {
		testPostRepositoryRevisions(t, ctx, repo)
	})

	t.Run("Media", func(t *testing.T) {
		testPostRepositoryMedia(t, ctx, repo, userID)
	})

	t.Run("ReplaceTags", func(t *testing.T) {
		testPostRepositoryReplaceTags(t, ctx, db, repo)
	})

	t.Run("WithTx", func(t *testing.T) {
		testPostRepositoryWithTx(t, ctx, db, repo)
	})
}

func testPostRepositoryCreateAndGetByID(t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID, categoryID uuid.UUID) {
	t.Helper()
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

	err := repo.Create(ctx, post)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	fetched, err := repo.GetByID(ctx, postID)
	if err != nil {
		t.Fatalf("expected nil error on get, got %v", err)
	}
	if fetched.Title != post.Title {
		t.Errorf("expected title %s, got %s", post.Title, fetched.Title)
	}
}

func testPostRepositoryUpdate(t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID, categoryID uuid.UUID) {
	t.Helper()
	postID := uuid.New()
	post := &domain.Post{
		ID:         postID,
		Title:      "Original Title",
		Slug:       "original-title",
		AuthorID:   userID,
		CategoryID: &categoryID,
		Status:     domain.PostStatusDraft,
	}
	if err := repo.Create(ctx, post); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	updatePost, _ := repo.GetByID(ctx, postID)
	updatePost.Title = "Updated Title"
	err := repo.Update(ctx, updatePost)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	fetched, _ := repo.GetByID(ctx, postID)
	if fetched.Title != "Updated Title" {
		t.Errorf("expected updated title %s, got %s", "Updated Title", fetched.Title)
	}
}

func testPostRepositoryDelete(t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID, categoryID uuid.UUID) {
	t.Helper()
	postID := uuid.New()
	post := &domain.Post{
		ID:         postID,
		Title:      "Delete Me",
		Slug:       "delete-me",
		AuthorID:   userID,
		CategoryID: &categoryID,
	}
	if err := repo.Create(ctx, post); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	err := repo.Delete(ctx, postID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = repo.GetByID(ctx, postID)
	if err == nil {
		t.Error("expected error getting deleted post, got nil")
	}
}

func testPostRepositoryGetBySlug(t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID, categoryID uuid.UUID) {
	t.Helper()
	postID := uuid.New()
	post := &domain.Post{
		ID:         postID,
		Title:      "Get By Slug",
		Slug:       "get-by-slug",
		AuthorID:   userID,
		CategoryID: &categoryID,
		Status:     domain.PostStatusDraft,
	}
	if err := repo.Create(ctx, post); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	fetched, err := repo.GetBySlug(ctx, "get-by-slug")
	if err != nil {
		t.Fatalf("get by slug failed: %v", err)
	}
	if fetched.ID != postID {
		t.Errorf("expected id %v, got %v", postID, fetched.ID)
	}
}

func testPostRepositoryGetBySlugPublished(t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID, categoryID uuid.UUID) {
	t.Helper()
	publishedID := uuid.New()
	if err := repo.Create(ctx, &domain.Post{
		ID:         publishedID,
		Title:      "Published Post",
		Slug:       "published-post",
		AuthorID:   userID,
		CategoryID: &categoryID,
		Status:     domain.PostStatusPublished,
	}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	draftID := uuid.New()
	if err := repo.Create(ctx, &domain.Post{
		ID:         draftID,
		Title:      "Draft Post",
		Slug:       "draft-post",
		AuthorID:   userID,
		CategoryID: &categoryID,
		Status:     domain.PostStatusDraft,
	}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	fetched, err := repo.GetBySlugPublished(ctx, "published-post")
	if err != nil || fetched.ID != publishedID {
		t.Errorf("expected published post, got err %v", err)
	}

	_, err = repo.GetBySlugPublished(ctx, "draft-post")
	if err == nil {
		t.Error("expected error getting draft post via published query, got nil")
	}
}

func testPostRepositoryListFiltered(t *testing.T, ctx context.Context, db *gorm.DB, repo PostRepository, userID uuid.UUID) {
	t.Helper()
	// Create various posts to filter
	catID2 := uuid.New()
	db.Create(&domain.Category{ID: catID2, Name: "Cat2"})

	feat := true
	for i := 0; i < 5; i++ {
		if err := repo.Create(ctx, &domain.Post{
			ID:         uuid.New(),
			Title:      "Filter Post " + string(rune(i)),
			Slug:       "filter-post-" + string(rune(i)),
			AuthorID:   userID,
			CategoryID: &catID2,
			Status:     domain.PostStatusPublished,
			IsFeatured: feat,
		}); err != nil {
			t.Fatalf("test setup or operation failed: %v", err)
		}
		feat = false // only 1 featured
	}

	posts, total, err := repo.ListFiltered(ctx, PostListFilter{
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
	posts, total, err = repo.ListFiltered(ctx, PostListFilter{
		CategoryID: &catID2,
		IsFeatured: &isFeat,
	})
	if err != nil {
		t.Fatalf("ListFiltered featured posts failed: %v", err)
	}
	if len(posts) != 1 {
		t.Errorf("expected 1 featured result, got %d", len(posts))
	}
	if total != 1 {
		t.Errorf("expected 1 featured post, got %d", total)
	}
}

func testPostRepositoryExistsBySlugAndCountByStatus(
	t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID, categoryID uuid.UUID,
) {
	t.Helper()
	slug := "exist-test-slug"
	postID := uuid.New()
	if err := repo.Create(ctx, &domain.Post{
		ID:         postID,
		Title:      "Exist Test",
		Slug:       slug,
		AuthorID:   userID,
		CategoryID: &categoryID,
		Status:     domain.PostStatusArchived,
	}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	exists, err := repo.ExistsBySlug(ctx, slug)
	if !exists || err != nil {
		t.Errorf("expected true, got %v, %v", exists, err)
	}

	exists, err = repo.ExistsBySlugExcluding(ctx, slug, postID)
	if exists || err != nil {
		t.Errorf("expected false, got %v, %v", exists, err)
	}

	count, err := repo.CountByStatus(ctx, string(domain.PostStatusArchived))
	if err != nil || count < 1 {
		t.Error("expected at least 1 archived post")
	}
}

func testPostRepositoryRevisions(t *testing.T, ctx context.Context, repo PostRepository) {
	t.Helper()
	postID := uuid.New()
	if err := repo.Create(ctx, &domain.Post{ID: postID, Title: "Rev Post", Slug: "rev-post"}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	rev1 := domain.PostRevision{
		ID:          uuid.New(),
		PostID:      postID,
		Version:     1,
		ContentHTML: "Version 1",
	}
	err := repo.CreateRevision(ctx, &rev1)
	if err != nil {
		t.Fatalf("CreateRevision failed: %v", err)
	}

	revs, total, err := repo.ListRevisions(ctx, postID, 0, 20)
	if err != nil || len(revs) != 1 || total != 1 {
		t.Errorf("ListRevisions expected 1, got %v (total=%d)", len(revs), total)
	}

	rev, err := repo.GetRevision(ctx, rev1.ID)
	if err != nil || rev.ContentHTML != "Version 1" {
		t.Errorf("GetRevision failed")
	}

	v, err := repo.GetLatestRevisionVersion(ctx, postID)
	if err != nil || v != 1 {
		t.Errorf("expected version 1, got %v", v)
	}
}

func testPostRepositoryMedia(t *testing.T, ctx context.Context, repo PostRepository, userID uuid.UUID) {
	t.Helper()
	postID := uuid.New()
	if err := repo.Create(ctx, &domain.Post{ID: postID, Title: "Media Post", Slug: "med-post"}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

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
	err := repo.CreateMedia(ctx, &media)
	if err != nil {
		t.Fatalf("CreateMedia failed: %v", err)
	}

	list, err := repo.ListMediaByPost(ctx, postID)
	if err != nil || len(list) != 1 {
		t.Errorf("ListMediaByPost expected 1")
	}

	m, err := repo.GetMediaByID(ctx, mediaID)
	if err != nil || m.S3Key != media.S3Key {
		t.Errorf("GetMediaByID failed")
	}

	err = repo.DeleteMedia(ctx, mediaID)
	if err != nil {
		t.Fatalf("DeleteMedia failed: %v", err)
	}
}

func testPostRepositoryReplaceTags(t *testing.T, ctx context.Context, db *gorm.DB, repo PostRepository) {
	t.Helper()
	postID := uuid.New()
	if err := repo.Create(ctx, &domain.Post{ID: postID, Title: "Tag Post", Slug: "tag-post"}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}

	tag1ID, tag2ID := uuid.New(), uuid.New()
	db.Create(&domain.Tag{ID: tag1ID, Name: "Tag1", Slug: "tag1"})
	db.Create(&domain.Tag{ID: tag2ID, Name: "Tag2", Slug: "tag2"})

	err := repo.ReplaceTags(ctx, postID, []uuid.UUID{tag1ID, tag2ID})
	if err != nil {
		t.Fatalf("ReplaceTags failed: %v", err)
	}

	// Verify tags are associated
	var count int64
	db.Model(&domain.PostTag{}).Where("post_id = ?", postID).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 tags, got %v", count)
	}
}

func testPostRepositoryWithTx(t *testing.T, ctx context.Context, db *gorm.DB, repo PostRepository) {
	t.Helper()
	tx := db.Begin()
	txRepo := repo.WithTx(tx)

	postID := uuid.New()
	if err := txRepo.Create(ctx, &domain.Post{ID: postID, Title: "Tx Post", Slug: "tx-post"}); err != nil {
		t.Fatalf("test setup or operation failed: %v", err)
	}
	tx.Commit()

	_, err := repo.GetByID(ctx, postID)
	if err != nil {
		t.Errorf("expected to find post committed in tx, got err: %v", err)
	}
}
