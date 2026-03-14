package api

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// presignCacher is an optional interface for managing cached presigned URLs.
// Defined here to avoid import cycles with the cache package.
type presignCacher interface {
	GetPresignedURL(ctx context.Context, key string) (string, error)
	SetPresignedURL(ctx context.Context, key, url string) error
	InvalidatePresignedURL(ctx context.Context, key string) error
}

const sniffLen = 512

var (
	allowedFileTypes = map[string]bool{
		"image/jpeg":      true,
		"image/png":       true,
		"image/webp":      true,
		"image/gif":       true,
		"application/pdf": true,
		"text/plain":      true,
	}
	allowedAvatarTypes = map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/webp": true,
	}
)

// UploadHandler handles file upload HTTP requests.
type UploadHandler struct {
	storage      storage.StorageService
	userRepo     repository.UserRepository
	presignCache presignCacher
	maxSize      int64
}

// NewUploadHandler creates a new UploadHandler.
func NewUploadHandler(storageSvc storage.StorageService, userRepo repository.UserRepository, maxSize int64) *UploadHandler {
	return &UploadHandler{
		storage:  storageSvc,
		userRepo: userRepo,
		maxSize:  maxSize,
	}
}

// SetPresignCache sets the optional presigned URL cache (Redis).
func (h *UploadHandler) SetPresignCache(pc presignCacher) {
	h.presignCache = pc
}

// FileURLResponse is the response for file URL retrieval.
type FileURLResponse struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

// AvatarUploadResponse is the response for avatar upload.
type AvatarUploadResponse struct {
	AvatarURL string `json:"avatar_url"`
	AvatarKey string `json:"avatar_key"`
}

// RegisterRoutes registers upload routes.
// RegisterRoutes registers upload routes.
// authzMw is the Casbin authorization middleware; it may be nil when Casbin is not configured.
func (h *UploadHandler) RegisterRoutes(api fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	if authzMw != nil {
		api.Post("/files/upload", authMw, authzMw, h.UploadFile)
		api.Get("/files/url", authMw, authzMw, h.GetFileURL)
		api.Post("/users/avatar", authMw, authzMw, h.UploadAvatar)
		api.Delete("/files/*", authMw, authzMw, h.DeleteFile)
	} else {
		api.Post("/files/upload", authMw, h.UploadFile)
		api.Get("/files/url", authMw, h.GetFileURL)
		api.Post("/users/avatar", authMw, h.UploadAvatar)
		api.Delete("/files/*", authMw, h.DeleteFile)
	}
}

// GetFileURL returns a time-limited presigned URL for a given storage key.
// @Summary      Get file URL
// @Description  Returns a time-limited presigned URL for the given storage key. For S3/MinIO private buckets, URLs are cached in Redis and auto-refreshed before expiry.
// @Tags         Upload
// @Security     Bearer
// @Produce      json
// @Param        key query string true "Storage key (e.g. avatars/uuid/file.jpg)"
// @Success      200 {object} FileURLResponse "url and key"
// @Failure      400 {object} errors.ProblemDetail "Key is required"
// @Failure      401 {object} errors.ProblemDetail "Not authenticated"
// @Failure      403 {object} errors.ProblemDetail "Can only access own files"
// @Router       /files/url [get]
func (h *UploadHandler) GetFileURL(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	key := c.Query("key")
	if key == "" {
		return errors.NewBadRequest("File key is required")
	}

	userPrefix := userID.String() + "/"
	if !strings.HasPrefix(key, "files/"+userPrefix) && !strings.HasPrefix(key, "avatars/"+userPrefix) {
		return errors.NewForbidden("You can only access your own files")
	}

	// Try presign cache first
	if h.presignCache != nil {
		if cached, err := h.presignCache.GetPresignedURL(c, key); err == nil && cached != "" {
			return c.JSON(fiber.Map{"url": cached, "key": key})
		}
	}

	// Cache miss — generate from storage backend
	url, err := h.storage.GetURL(c, key)
	if err != nil {
		return errors.NewInternalError("Failed to generate file URL")
	}

	// Populate cache (best-effort)
	if h.presignCache != nil {
		_ = h.presignCache.SetPresignedURL(c, key, url)
	}

	return c.JSON(fiber.Map{
		"url": url,
		"key": key,
	})
}

// UploadFile handles general file upload.
// @Summary Upload a file
// @Description Upload a general file (images, PDFs, text)
// @Tags Upload
// @Security Bearer
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "File to upload"
// @Success 201 {object} storage.FileInfo "File uploaded successfully"
// @Failure 400 {object} errors.ProblemDetail "Invalid file or type not allowed"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /files/upload [post]
func (h *UploadHandler) UploadFile(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return errors.NewBadRequest("No file provided")
	}

	if err := h.validateFile(file, allowedFileTypes); err != nil {
		return err
	}

	detectedType, _ := detectContentType(file)

	ext := filepath.Ext(file.Filename)
	key := fmt.Sprintf("files/%s/%s%s", userID.String(), uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return errors.NewInternalError("Failed to read uploaded file")
	}
	defer src.Close()

	info, err := h.storage.Upload(c, key, src, file.Size, detectedType)
	if err != nil {
		return errors.NewInternalError("Failed to store file")
	}

	return c.Status(fiber.StatusCreated).JSON(info)
}

// UploadAvatar handles user avatar upload.
// @Summary Upload user avatar
// @Description Upload a new avatar image for the authenticated user
// @Tags Upload
// @Security Bearer
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Avatar image (JPEG, PNG, WebP)"
// @Success 200 {object} AvatarUploadResponse "Avatar uploaded with URL and key"
// @Failure 400 {object} errors.ProblemDetail "Invalid file or type not allowed"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /users/avatar [post]
func (h *UploadHandler) UploadAvatar(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return errors.NewBadRequest("No file provided")
	}

	if err := h.validateFile(file, allowedAvatarTypes); err != nil {
		return err
	}

	detectedType, _ := detectContentType(file)

	// Get current user to check for existing avatar
	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	// Delete old avatar if exists
	if user.AvatarURL != "" {
		oldKey := extractKeyFromURL(user.AvatarURL)
		if oldKey != "" {
			_ = h.storage.Delete(c, oldKey)
			if h.presignCache != nil {
				_ = h.presignCache.InvalidatePresignedURL(c, oldKey)
			}
		}
	}

	ext := filepath.Ext(file.Filename)
	key := fmt.Sprintf("avatars/%s/%s%s", userID.String(), uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return errors.NewInternalError("Failed to read uploaded file")
	}
	defer src.Close()

	info, err := h.storage.Upload(c, key, src, file.Size, detectedType)
	if err != nil {
		return errors.NewInternalError("Failed to store avatar")
	}

	// Store the key in DB (not the presigned URL which expires for S3)
	user.AvatarURL = info.Key
	if err := h.userRepo.Update(user); err != nil {
		return errors.NewInternalError("Failed to update user avatar")
	}

	return c.JSON(fiber.Map{
		"avatar_url": info.URL,
		"avatar_key": info.Key,
	})
}

// DeleteFile handles file deletion.
// @Summary Delete a file
// @Description Delete a file by its storage key (only own files)
// @Tags Upload
// @Security Bearer
// @Produce json
// @Param key path string true "File storage key"
// @Success 200 {object} MessageResponse "File deleted"
// @Failure 400 {object} errors.ProblemDetail "File key required"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 403 {object} errors.ProblemDetail "Can only delete own files"
// @Router /files/{key} [delete]
func (h *UploadHandler) DeleteFile(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	key := c.Params("*")
	if key == "" {
		return errors.NewBadRequest("File key is required")
	}
	if strings.Contains(key, "..") {
		return errors.NewBadRequest("Invalid file key")
	}

	// Verify the file belongs to the authenticated user
	userPrefix := userID.String() + "/"
	if !strings.HasPrefix(key, "files/"+userPrefix) && !strings.HasPrefix(key, "avatars/"+userPrefix) {
		return errors.NewForbidden("You can only delete your own files")
	}

	if err := h.storage.Delete(c, key); err != nil {
		return errors.NewInternalError("Failed to delete file")
	}

	if h.presignCache != nil {
		_ = h.presignCache.InvalidatePresignedURL(c, key)
	}

	return c.JSON(fiber.Map{
		"message": "File deleted successfully",
	})
}

// validateFile checks file size and content type via magic bytes detection.
func (h *UploadHandler) validateFile(file *multipart.FileHeader, allowedTypes map[string]bool) error {
	if file.Size > h.maxSize {
		return errors.NewBadRequest(fmt.Sprintf("File size exceeds maximum allowed size of %d bytes", h.maxSize))
	}

	detected, err := detectContentType(file)
	if err != nil {
		return errors.NewInternalError("Failed to detect file type")
	}
	if !allowedTypes[detected] {
		return errors.NewBadRequest(fmt.Sprintf("File type %s is not allowed", detected))
	}

	return nil
}

// detectContentType reads the first 512 bytes of the file to determine its actual content type.
func detectContentType(fh *multipart.FileHeader) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, sniffLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

// extractKeyFromURL returns the storage key from the stored avatar value.
// The DB stores the key (e.g. "avatars/uuid/file.jpg"); for backwards
// compatibility the legacy "/uploads/" prefix is also stripped.
func extractKeyFromURL(val string) string {
	if val == "" {
		return ""
	}
	// Legacy format: "/uploads/avatars/..." → convert to key
	if strings.HasPrefix(val, "/uploads/") {
		return strings.TrimPrefix(val, "/uploads/")
	}
	// Current format: already a key ("avatars/..." or "files/...")
	return val
}

// fiber:context-methods migrated
