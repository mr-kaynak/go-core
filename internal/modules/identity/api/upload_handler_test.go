package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// --- Stubs ---

// stubStorage is a minimal StorageService stub for testing.
type stubStorage struct {
	uploadFn  func(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*storage.FileInfo, error)
	deleteFn  func(ctx context.Context, key string) error
	getURLFn  func(ctx context.Context, key string) (string, error)
	failOnGet bool
}

func (s *stubStorage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*storage.FileInfo, error) {
	if s.uploadFn != nil {
		return s.uploadFn(ctx, key, reader, size, contentType)
	}
	return &storage.FileInfo{Key: key, Size: size, ContentType: contentType, URL: "http://example.com/" + key}, nil
}

func (s *stubStorage) Delete(ctx context.Context, key string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, key)
	}
	return nil
}

func (s *stubStorage) GetURL(ctx context.Context, key string) (string, error) {
	if s.getURLFn != nil {
		return s.getURLFn(ctx, key)
	}
	if s.failOnGet {
		return "", coreerrors.NewInternalError("storage unavailable")
	}
	return "http://example.com/" + key, nil
}

func (s *stubStorage) GetUploadURL(ctx context.Context, key string, contentType string) (string, error) {
	return "http://example.com/upload/" + key, nil
}

func (s *stubStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("data")), nil
}

func (s *stubStorage) StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{Size: 100}, nil
}

// stubUserRepo is a minimal UserRepository stub for upload handler tests.
type stubUserRepo struct {
	user    *domain.User
	findErr error
	updErr  error
}

func (r *stubUserRepo) WithTx(_ *gorm.DB) repository.UserRepository        { return r }
func (r *stubUserRepo) GetByID(_ uuid.UUID) (*domain.User, error)          { return r.user, r.findErr }
func (r *stubUserRepo) GetByIDForUpdate(_ uuid.UUID) (*domain.User, error) { return r.user, r.findErr }
func (r *stubUserRepo) GetByEmail(_ string) (*domain.User, error)          { return r.user, r.findErr }
func (r *stubUserRepo) GetByUsername(_ string) (*domain.User, error)       { return r.user, r.findErr }
func (r *stubUserRepo) GetAll(_, _ int) ([]*domain.User, error)            { return nil, nil }
func (r *stubUserRepo) ListFiltered(_ domain.UserListFilter) ([]*domain.User, int64, error) {
	return nil, 0, nil
}
func (r *stubUserRepo) Count() (int64, error)                                        { return 0, nil }
func (r *stubUserRepo) ExistsByEmail(_ string) (bool, error)                         { return false, nil }
func (r *stubUserRepo) ExistsByUsername(_ string) (bool, error)                      { return false, nil }
func (r *stubUserRepo) LoadRoles(_ *domain.User) error                               { return nil }
func (r *stubUserRepo) Create(_ *domain.User) error                                  { return nil }
func (r *stubUserRepo) Update(_ *domain.User) error                                  { return r.updErr }
func (r *stubUserRepo) Delete(_ uuid.UUID) error                                     { return nil }
func (r *stubUserRepo) CreateRole(_ *domain.Role) error                              { return nil }
func (r *stubUserRepo) UpdateRole(_ *domain.Role) error                              { return nil }
func (r *stubUserRepo) DeleteRole(_ uuid.UUID) error                                 { return nil }
func (r *stubUserRepo) GetRoleByID(_ uuid.UUID) (*domain.Role, error)                { return nil, nil }
func (r *stubUserRepo) GetRoleByName(_ string) (*domain.Role, error)                 { return nil, nil }
func (r *stubUserRepo) GetAllRoles() ([]*domain.Role, error)                         { return nil, nil }
func (r *stubUserRepo) AssignRole(_, _ uuid.UUID) error                              { return nil }
func (r *stubUserRepo) RemoveRole(_, _ uuid.UUID) error                              { return nil }
func (r *stubUserRepo) GetUserRoles(_ uuid.UUID) ([]*domain.Role, error)             { return nil, nil }
func (r *stubUserRepo) CreatePermission(_ *domain.Permission) error                  { return nil }
func (r *stubUserRepo) UpdatePermission(_ *domain.Permission) error                  { return nil }
func (r *stubUserRepo) DeletePermission(_ uuid.UUID) error                           { return nil }
func (r *stubUserRepo) GetPermissionByID(_ uuid.UUID) (*domain.Permission, error)    { return nil, nil }
func (r *stubUserRepo) GetAllPermissions() ([]*domain.Permission, error)             { return nil, nil }
func (r *stubUserRepo) AssignPermissionToRole(_, _ uuid.UUID) error                  { return nil }
func (r *stubUserRepo) RemovePermissionFromRole(_, _ uuid.UUID) error                { return nil }
func (r *stubUserRepo) GetRolePermissions(_ uuid.UUID) ([]*domain.Permission, error) { return nil, nil }
func (r *stubUserRepo) CreateRefreshToken(_ *domain.RefreshToken) error              { return nil }
func (r *stubUserRepo) GetRefreshToken(_ string) (*domain.RefreshToken, error)       { return nil, nil }
func (r *stubUserRepo) RevokeRefreshToken(_ string) error                            { return nil }
func (r *stubUserRepo) RevokeAllUserRefreshTokens(_ uuid.UUID) error                 { return nil }
func (r *stubUserRepo) GetActiveRefreshTokensByUser(_ uuid.UUID) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (r *stubUserRepo) RevokeRefreshTokenByID(_ uuid.UUID) error { return nil }
func (r *stubUserRepo) CleanExpiredRefreshTokens() error         { return nil }
func (r *stubUserRepo) CountByStatus(_ string) (int64, error)    { return 0, nil }
func (r *stubUserRepo) CountCreatedAfter(_ time.Time) (int64, error) {
	return 0, nil
}
func (r *stubUserRepo) GetAllActiveSessions(_, _ int) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (r *stubUserRepo) CountActiveSessions() (int64, error) { return 0, nil }

// --- Test app setup ---

func newUploadTestApp(handler *UploadHandler, userID uuid.UUID) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	api := app.Group("/api")
	authMw := func(c fiber.Ctx) error {
		if userID != uuid.Nil {
			c.Locals("userID", userID)
		}
		return c.Next()
	}
	handler.RegisterRoutes(api, authMw)
	return app
}

// --- extractKeyFromURL ---

func TestExtractKeyFromURL(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want string
	}{
		{"empty", "", ""},
		{"legacy format", "/uploads/avatars/uuid/file.jpg", "avatars/uuid/file.jpg"},
		{"current format", "avatars/uuid/file.jpg", "avatars/uuid/file.jpg"},
		{"files prefix", "files/uuid/file.pdf", "files/uuid/file.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeyFromURL(tt.val)
			if got != tt.want {
				t.Errorf("extractKeyFromURL(%q) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

// --- UploadFile ---

func TestUploadHandlerUploadFile_NoAuth(t *testing.T) {
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, uuid.Nil)
	resp := doRequest(t, app, http.MethodPost, "/api/files/upload", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerUploadFile_NoFile(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	// Send a request without multipart form data
	resp := doRequest(t, app, http.MethodPost, "/api/files/upload", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerUploadFile_FileTooLarge(t *testing.T) {
	userID := uuid.New()
	// Set max size to 10 bytes to trigger the size check
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10)
	app := newUploadTestApp(h, userID)

	body, contentType := createMultipartFile(t, "file", "test.txt", "this content is longer than 10 bytes definitely")
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerUploadFile_Success(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)

	// Create a minimal valid JPEG (starts with JPEG magic bytes)
	jpegContent := createMinimalJPEG()
	body, contentType := createMultipartFileBytes(t, "file", "photo.jpg", jpegContent)
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		respBody := readBody(t, resp)
		t.Fatalf("expected 201, got %d; body: %s", resp.StatusCode, respBody)
	}
}

// --- UploadAvatar ---

func TestUploadHandlerUploadAvatar_NoAuth(t *testing.T) {
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, uuid.Nil)
	resp := doRequest(t, app, http.MethodPost, "/api/users/avatar", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerUploadAvatar_NoFile(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{user: &domain.User{ID: userID}}, 10<<20)
	app := newUploadTestApp(h, userID)
	resp := doRequest(t, app, http.MethodPost, "/api/users/avatar", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerUploadAvatar_InvalidFileType(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{user: &domain.User{ID: userID}}, 10<<20)
	app := newUploadTestApp(h, userID)

	// Upload a text file as avatar — should be rejected because avatars only allow image types
	body, contentType := createMultipartFile(t, "file", "test.txt", "this is plain text not an image")
	req := httptest.NewRequest(http.MethodPost, "/api/users/avatar", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- GetFileURL ---

func TestUploadHandlerGetFileURL_NoAuth(t *testing.T) {
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, uuid.Nil)
	resp := doRequest(t, app, http.MethodGet, "/api/files/url?key=files/abc/test.txt", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerGetFileURL_MissingKey(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	resp := doRequest(t, app, http.MethodGet, "/api/files/url", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerGetFileURL_ForbiddenOtherUser(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	// Try to access another user's file
	resp := doRequest(t, app, http.MethodGet, "/api/files/url?key=files/"+otherUserID.String()+"/test.txt", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerGetFileURL_Success(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	key := "files/" + userID.String() + "/test.txt"
	resp := doRequest(t, app, http.MethodGet, "/api/files/url?key="+key, "")
	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "url") {
		t.Fatalf("expected response to contain url field, got: %s", body)
	}
}

func TestUploadHandlerGetFileURL_AvatarKey(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	key := "avatars/" + userID.String() + "/avatar.jpg"
	resp := doRequest(t, app, http.MethodGet, "/api/files/url?key="+key, "")
	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}
}

// --- DeleteFile ---

func TestUploadHandlerDeleteFile_NoAuth(t *testing.T) {
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, uuid.Nil)
	resp := doRequest(t, app, http.MethodDelete, "/api/files/files/abc/test.txt", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerDeleteFile_ForbiddenOtherUser(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	resp := doRequest(t, app, http.MethodDelete, "/api/files/files/"+otherUserID.String()+"/test.txt", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestUploadHandlerDeleteFile_Success(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	key := "files/" + userID.String() + "/test.txt"
	resp := doRequest(t, app, http.MethodDelete, "/api/files/"+key, "")
	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}
}

func TestUploadHandlerDeleteFile_AvatarKey(t *testing.T) {
	userID := uuid.New()
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	app := newUploadTestApp(h, userID)
	key := "avatars/" + userID.String() + "/avatar.jpg"
	resp := doRequest(t, app, http.MethodDelete, "/api/files/"+key, "")
	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}
}

// --- SetPresignCache ---

func TestUploadHandlerSetPresignCache(t *testing.T) {
	h := NewUploadHandler(&stubStorage{}, &stubUserRepo{}, 10<<20)
	if h.presignCache != nil {
		t.Fatal("expected nil presign cache initially")
	}
	h.SetPresignCache(nil)
	if h.presignCache != nil {
		t.Fatal("expected nil presign cache after setting nil")
	}
}

// --- Helpers ---

func createMultipartFile(t *testing.T, fieldName, fileName, content string) (*bytes.Buffer, string) {
	t.Helper()
	return createMultipartFileBytes(t, fieldName, fileName, []byte(content))
}

func createMultipartFileBytes(t *testing.T, fieldName, fileName string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("failed to write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	return &buf, writer.FormDataContentType()
}

// createMinimalJPEG returns bytes that http.DetectContentType identifies as image/jpeg.
func createMinimalJPEG() []byte {
	// JPEG files start with FF D8 FF
	data := make([]byte, 512)
	data[0] = 0xFF
	data[1] = 0xD8
	data[2] = 0xFF
	data[3] = 0xE0 // JFIF marker
	return data
}
