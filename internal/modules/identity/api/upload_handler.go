package api

import (
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

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
	storage  storage.StorageService
	userRepo repository.UserRepository
	maxSize  int64
}

// NewUploadHandler creates a new UploadHandler.
func NewUploadHandler(storageSvc storage.StorageService, userRepo repository.UserRepository, maxSize int64) *UploadHandler {
	return &UploadHandler{
		storage:  storageSvc,
		userRepo: userRepo,
		maxSize:  maxSize,
	}
}

// RegisterRoutes registers upload routes.
func (h *UploadHandler) RegisterRoutes(api fiber.Router, authMw fiber.Handler) {
	api.Post("/files/upload", authMw, h.UploadFile)
	api.Post("/users/avatar", authMw, h.UploadAvatar)
	api.Delete("/files/*", authMw, h.DeleteFile)
}

// UploadFile handles general file upload.
func (h *UploadHandler) UploadFile(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return errors.NewBadRequest("No file provided")
	}

	if err := h.validateFile(file, allowedFileTypes); err != nil {
		return err
	}

	ext := filepath.Ext(file.Filename)
	key := fmt.Sprintf("files/%s/%s%s", userID.String(), uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return errors.NewInternalError("Failed to read uploaded file")
	}
	defer src.Close()

	contentType := file.Header.Get("Content-Type")
	info, err := h.storage.Upload(c.Context(), key, src, file.Size, contentType)
	if err != nil {
		return errors.NewInternalError("Failed to store file")
	}

	return c.Status(fiber.StatusCreated).JSON(info)
}

// UploadAvatar handles user avatar upload.
func (h *UploadHandler) UploadAvatar(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return errors.NewBadRequest("No file provided")
	}

	if err := h.validateFile(file, allowedAvatarTypes); err != nil {
		return err
	}

	// Get current user to check for existing avatar
	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		return errors.NewNotFound("User", userID.String())
	}

	// Delete old avatar if exists
	if user.AvatarURL != "" {
		oldKey := extractKeyFromURL(user.AvatarURL)
		if oldKey != "" {
			_ = h.storage.Delete(c.Context(), oldKey)
		}
	}

	ext := filepath.Ext(file.Filename)
	key := fmt.Sprintf("avatars/%s/%s%s", userID.String(), uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return errors.NewInternalError("Failed to read uploaded file")
	}
	defer src.Close()

	contentType := file.Header.Get("Content-Type")
	info, err := h.storage.Upload(c.Context(), key, src, file.Size, contentType)
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
func (h *UploadHandler) DeleteFile(c *fiber.Ctx) error {
	_, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	key := c.Params("*")
	if key == "" {
		return errors.NewBadRequest("File key is required")
	}

	if err := h.storage.Delete(c.Context(), key); err != nil {
		return errors.NewInternalError("Failed to delete file")
	}

	return c.JSON(fiber.Map{
		"message": "File deleted successfully",
	})
}

// validateFile checks file size and content type against the allowed set.
func (h *UploadHandler) validateFile(file *multipart.FileHeader, allowedTypes map[string]bool) error {
	if file.Size > h.maxSize {
		return errors.NewBadRequest(fmt.Sprintf("File size exceeds maximum allowed size of %d bytes", h.maxSize))
	}

	contentType := file.Header.Get("Content-Type")
	if !allowedTypes[contentType] {
		return errors.NewBadRequest(fmt.Sprintf("File type %s is not allowed", contentType))
	}

	return nil
}

// extractStorageKey returns the storage key from the stored avatar value.
// DB'de key saklanır (örn: "avatars/uuid/file.jpg"), eski format uyumluluğu
// için "/uploads/" prefix'i de temizlenir.
func extractKeyFromURL(val string) string {
	if val == "" {
		return ""
	}
	// Eski format: "/uploads/avatars/..." → key'e çevir
	if strings.HasPrefix(val, "/uploads/") {
		return strings.TrimPrefix(val, "/uploads/")
	}
	// Yeni format: zaten key ("avatars/..." veya "files/...")
	return val
}
