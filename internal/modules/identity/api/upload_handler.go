package api

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

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

	detectedType, _ := detectContentType(file)

	ext := filepath.Ext(file.Filename)
	key := fmt.Sprintf("files/%s/%s%s", userID.String(), uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return errors.NewInternalError("Failed to read uploaded file")
	}
	defer src.Close()

	info, err := h.storage.Upload(c.Context(), key, src, file.Size, detectedType)
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
// @Success 200 {object} fiber.Map "Avatar uploaded with URL and key"
// @Failure 400 {object} errors.ProblemDetail "Invalid file or type not allowed"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /users/avatar [post]
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

	info, err := h.storage.Upload(c.Context(), key, src, file.Size, detectedType)
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
// @Success 200 {object} fiber.Map "File deleted"
// @Failure 400 {object} errors.ProblemDetail "File key required"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 403 {object} errors.ProblemDetail "Can only delete own files"
// @Router /files/{key} [delete]
func (h *UploadHandler) DeleteFile(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	key := c.Params("*")
	if key == "" {
		return errors.NewBadRequest("File key is required")
	}

	// Verify the file belongs to the authenticated user
	userPrefix := userID.String() + "/"
	if !strings.HasPrefix(key, "files/"+userPrefix) && !strings.HasPrefix(key, "avatars/"+userPrefix) {
		return errors.NewForbidden("You can only delete your own files")
	}

	if err := h.storage.Delete(c.Context(), key); err != nil {
		return errors.NewInternalError("Failed to delete file")
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
