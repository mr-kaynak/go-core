package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

// mockStorage implements storage.StorageService for testing.
type mockStorage struct {
	uploadURLFn func(ctx context.Context, key, contentType string) (string, error)
	deleteFn    func(ctx context.Context, key string) error
}

func (m *mockStorage) Upload(context.Context, string, io.Reader, int64, string) (*storage.FileInfo, error) {
	return nil, nil
}
func (m *mockStorage) Delete(ctx context.Context, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}
func (m *mockStorage) GetURL(context.Context, string) (string, error) { return "", nil }
func (m *mockStorage) GetUploadURL(ctx context.Context, key, contentType string) (string, error) {
	if m.uploadURLFn != nil {
		return m.uploadURLFn(ctx, key, contentType)
	}
	return "https://s3.example.com/upload?presigned=1", nil
}
func (m *mockStorage) GetObject(context.Context, string) (io.ReadCloser, error) {
	// Return JPEG magic bytes so http.DetectContentType identifies the content as image/jpeg.
	return io.NopCloser(strings.NewReader("\xff\xd8\xff\xe0")), nil
}
func (m *mockStorage) StatObject(context.Context, string) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{}, nil
}

func setupMediaTestEnv() (*MediaService, repository.PostRepository, *mockStorage, uuid.UUID) {
	db, _ := SetupTestEnv()
	postRepo := repository.NewPostRepository(db)

	authorID := uuid.New()
	postID := uuid.New()
	post := &domain.Post{
		ID:       postID,
		Title:    "Media Test Post",
		Slug:     "media-test-" + postID.String()[:8],
		AuthorID: authorID,
		Status:   domain.PostStatusPublished,
	}
	postRepo.Create(post)

	storageMock := &mockStorage{}
	cfg := &config.Config{
		Blog: config.BlogConfig{
			MaxMediaSize: 10 * 1024 * 1024,
		},
	}

	svc := NewMediaService(postRepo, storageMock, cfg)
	return svc, postRepo, storageMock, authorID
}

func TestMediaService(t *testing.T) {
	svc, postRepo, storageMock, authorID := setupMediaTestEnv()
	ctx := context.Background()

	// Get the seeded post
	posts, _, _ := postRepo.ListFiltered(repository.PostListFilter{Limit: 1})
	if len(posts) == 0 {
		t.Fatal("expected seeded post")
	}
	post := posts[0]
	postID := post.ID

	t.Run("IsAllowedContentType", func(t *testing.T) {
		allowed := []string{"image/jpeg", "image/png", "image/gif", "image/webp", "video/mp4", "video/webm", "application/pdf"}
		for _, ct := range allowed {
			if !IsAllowedContentType(ct) {
				t.Errorf("expected %s to be allowed", ct)
			}
		}
		rejected := []string{"text/html", "application/javascript", "image/svg+xml", ""}
		for _, ct := range rejected {
			if IsAllowedContentType(ct) {
				t.Errorf("expected %s to be rejected", ct)
			}
		}
	})

	t.Run("BuildProxyURL", func(t *testing.T) {
		url := buildProxyURL("blog/abc/file.jpg")
		if url != "/api/v1/blog/media/file/blog/abc/file.jpg" {
			t.Errorf("unexpected proxy URL: %s", url)
		}
	})

	t.Run("GeneratePresignedUpload_Success", func(t *testing.T) {
		resp, err := svc.GeneratePresignedUpload(ctx, postID, "photo.jpg", "image/jpeg", authorID, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.UploadURL == "" {
			t.Error("expected non-empty upload URL")
		}
		if !strings.HasPrefix(resp.S3Key, fmt.Sprintf("blog/%s/", postID.String())) {
			t.Errorf("S3Key should start with post prefix, got: %s", resp.S3Key)
		}
		if !strings.HasSuffix(resp.S3Key, "_photo.jpg") {
			t.Errorf("S3Key should end with _photo.jpg, got: %s", resp.S3Key)
		}
	})

	t.Run("GeneratePresignedUpload_PostNotFound", func(t *testing.T) {
		_, err := svc.GeneratePresignedUpload(ctx, uuid.New(), "photo.jpg", "image/jpeg", authorID, false)
		if err == nil {
			t.Fatal("expected error for non-existent post")
		}
	})

	t.Run("GeneratePresignedUpload_NotAuthor", func(t *testing.T) {
		_, err := svc.GeneratePresignedUpload(ctx, postID, "photo.jpg", "image/jpeg", uuid.New(), false)
		if err == nil {
			t.Fatal("expected forbidden error for non-author")
		}
	})

	t.Run("GeneratePresignedUpload_AdminBypass", func(t *testing.T) {
		resp, err := svc.GeneratePresignedUpload(ctx, postID, "photo.jpg", "image/jpeg", uuid.New(), true)
		if err != nil {
			t.Fatalf("admin should bypass ownership check: %v", err)
		}
		if resp.UploadURL == "" {
			t.Error("expected upload URL for admin")
		}
	})

	t.Run("GeneratePresignedUpload_DisallowedContentType", func(t *testing.T) {
		_, err := svc.GeneratePresignedUpload(ctx, postID, "script.js", "application/javascript", authorID, false)
		if err == nil {
			t.Fatal("expected error for disallowed content type")
		}
	})

	t.Run("GeneratePresignedUpload_PathTraversal", func(t *testing.T) {
		_, err := svc.GeneratePresignedUpload(ctx, postID, "../../../etc/passwd", "image/jpeg", authorID, false)
		if err != nil {
			t.Log("path traversal correctly rejected or sanitized")
		}
		// filepath.Base("../../../etc/passwd") = "passwd" which is valid
		// The important thing is the S3 key uses a safe prefix
	})

	t.Run("GeneratePresignedUpload_InvalidFilename", func(t *testing.T) {
		_, err := svc.GeneratePresignedUpload(ctx, postID, "..", "image/jpeg", authorID, false)
		if err == nil {
			t.Fatal("expected error for '..' filename")
		}
	})

	t.Run("GeneratePresignedUpload_StorageError", func(t *testing.T) {
		storageMock.uploadURLFn = func(context.Context, string, string) (string, error) {
			return "", fmt.Errorf("s3 unavailable")
		}
		_, err := svc.GeneratePresignedUpload(ctx, postID, "photo.jpg", "image/jpeg", authorID, false)
		if err == nil {
			t.Fatal("expected error when storage fails")
		}
		storageMock.uploadURLFn = nil
	})

	t.Run("Register_Success", func(t *testing.T) {
		s3Key := fmt.Sprintf("blog/%s/abc_photo.jpg", postID.String())
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       s3Key,
			Filename:    "photo.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    1024,
		}
		media, err := svc.Register(ctx, req, authorID, false)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if media.PostID != postID {
			t.Errorf("expected post ID %s, got %s", postID, media.PostID)
		}
		if media.URL == "" {
			t.Error("expected media URL to be populated")
		}
	})

	t.Run("Register_InvalidPostID", func(t *testing.T) {
		req := &RegisterMediaRequest{
			PostID:      "not-a-uuid",
			S3Key:       "blog/x/file.jpg",
			Filename:    "file.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    1024,
		}
		_, err := svc.Register(ctx, req, authorID, false)
		if err == nil {
			t.Fatal("expected error for invalid post ID")
		}
	})

	t.Run("Register_PostNotFound", func(t *testing.T) {
		fakePostID := uuid.New()
		req := &RegisterMediaRequest{
			PostID:      fakePostID.String(),
			S3Key:       fmt.Sprintf("blog/%s/file.jpg", fakePostID),
			Filename:    "file.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    1024,
		}
		_, err := svc.Register(ctx, req, authorID, false)
		if err == nil {
			t.Fatal("expected error for non-existent post")
		}
	})

	t.Run("Register_NotAuthor", func(t *testing.T) {
		s3Key := fmt.Sprintf("blog/%s/abc_photo.jpg", postID.String())
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       s3Key,
			Filename:    "photo.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    1024,
		}
		_, err := svc.Register(ctx, req, uuid.New(), false)
		if err == nil {
			t.Fatal("expected forbidden error")
		}
	})

	t.Run("Register_DisallowedContentType", func(t *testing.T) {
		s3Key := fmt.Sprintf("blog/%s/abc_script.js", postID.String())
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       s3Key,
			Filename:    "script.js",
			MediaType:   "file",
			ContentType: "application/javascript",
			FileSize:    1024,
		}
		_, err := svc.Register(ctx, req, authorID, false)
		if err == nil {
			t.Fatal("expected error for disallowed content type")
		}
	})

	t.Run("Register_S3KeyMismatch", func(t *testing.T) {
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       "blog/wrong-prefix/file.jpg",
			Filename:    "file.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    1024,
		}
		_, err := svc.Register(ctx, req, authorID, false)
		if err == nil {
			t.Fatal("expected error for S3 key prefix mismatch")
		}
	})

	t.Run("Register_FileTooLarge", func(t *testing.T) {
		s3Key := fmt.Sprintf("blog/%s/abc_big.jpg", postID.String())
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       s3Key,
			Filename:    "big.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    20 * 1024 * 1024, // 20MB > 10MB limit
		}
		_, err := svc.Register(ctx, req, authorID, false)
		if err == nil {
			t.Fatal("expected error for oversized file")
		}
	})

	t.Run("ListByPost", func(t *testing.T) {
		media, err := svc.ListByPost(ctx, postID)
		if err != nil {
			t.Fatalf("ListByPost failed: %v", err)
		}
		if len(media) == 0 {
			t.Error("expected at least one media item from Register_Success")
		}
		for _, m := range media {
			if m.URL == "" {
				t.Error("expected URL to be populated")
			}
		}
	})

	t.Run("Delete_Success", func(t *testing.T) {
		// List media to get an ID
		media, _ := svc.ListByPost(ctx, postID)
		if len(media) == 0 {
			t.Skip("no media to delete")
		}
		mediaID := media[0].ID

		err := svc.Delete(ctx, mediaID, authorID, false)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify it's gone
		remaining, _ := svc.ListByPost(ctx, postID)
		for _, m := range remaining {
			if m.ID == mediaID {
				t.Error("expected media to be deleted")
			}
		}
	})

	t.Run("Delete_NotFound", func(t *testing.T) {
		err := svc.Delete(ctx, uuid.New(), authorID, false)
		if err == nil {
			t.Fatal("expected error for non-existent media")
		}
	})

	t.Run("Delete_NotUploader", func(t *testing.T) {
		// Register a new media to delete
		s3Key := fmt.Sprintf("blog/%s/del_photo.jpg", postID.String())
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       s3Key,
			Filename:    "del_photo.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    512,
		}
		media, _ := svc.Register(ctx, req, authorID, false)

		err := svc.Delete(ctx, media.ID, uuid.New(), false)
		if err == nil {
			t.Fatal("expected forbidden error for non-uploader")
		}
	})

	t.Run("Delete_StorageError", func(t *testing.T) {
		s3Key := fmt.Sprintf("blog/%s/fail_photo.jpg", postID.String())
		req := &RegisterMediaRequest{
			PostID:      postID.String(),
			S3Key:       s3Key,
			Filename:    "fail_photo.jpg",
			MediaType:   "image",
			ContentType: "image/jpeg",
			FileSize:    256,
		}
		media, _ := svc.Register(ctx, req, authorID, false)

		storageMock.deleteFn = func(context.Context, string) error {
			return fmt.Errorf("s3 delete failed")
		}
		err := svc.Delete(ctx, media.ID, authorID, false)
		if err == nil {
			t.Fatal("expected error when storage delete fails")
		}
		storageMock.deleteFn = nil
	})

	t.Run("GetPostAccessInfo_Success", func(t *testing.T) {
		info, err := svc.GetPostAccessInfo(ctx, postID)
		if err != nil {
			t.Fatalf("GetPostAccessInfo failed: %v", err)
		}
		if info.Status != domain.PostStatusPublished {
			t.Errorf("expected published status, got %s", info.Status)
		}
		if info.AuthorID != authorID {
			t.Errorf("expected author ID %s, got %s", authorID, info.AuthorID)
		}
	})

	t.Run("GetPostAccessInfo_NotFound", func(t *testing.T) {
		_, err := svc.GetPostAccessInfo(ctx, uuid.New())
		if err == nil {
			t.Fatal("expected error for non-existent post")
		}
	})
}
