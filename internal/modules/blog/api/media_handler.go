package api

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// Cache-Control header values
const (
	cacheControlPublic  = "public, max-age=86400"
	cacheControlPrivate = "private, no-store"
)

// swag annotation type references
var _ *domain.PostMedia

// MediaHandler handles blog media HTTP requests
type MediaHandler struct {
	mediaSvc   *service.MediaService
	storageSvc storage.StorageService
}

// NewMediaHandler creates a new MediaHandler
func NewMediaHandler(mediaSvc *service.MediaService, storageSvc storage.StorageService) *MediaHandler {
	return &MediaHandler{mediaSvc: mediaSvc, storageSvc: storageSvc}
}

// RegisterRoutes registers media routes
func (h *MediaHandler) RegisterRoutes(blog fiber.Router, authMw fiber.Handler, optionalAuthMw fiber.Handler) {
	// Public proxy endpoint with optional auth — allows authenticated users
	// to view media on their own draft/archived posts while keeping published
	// media fully public.
	blog.Get("/media/file/*", optionalAuthMw, h.ServeFile)

	media := blog.Group("/media", authMw)
	media.Post("/presign", h.GeneratePresignedUpload)
	media.Post("/", h.Register)
	media.Delete("/:id", h.Delete)

	blog.Get("/posts/:postId/media", authMw, h.ListByPost)
}

// PresignRequest holds the request for generating a presigned upload URL
type PresignRequest struct {
	PostID      string `json:"post_id" validate:"required,uuid"`
	Filename    string `json:"filename" validate:"required,max=255"`
	ContentType string `json:"content_type" validate:"required,max=100"`
}

// GeneratePresignedUpload generates a presigned URL for media upload.
// @Summary      Generate presigned upload URL
// @Description  Generates a presigned S3 URL for uploading media to a blog post
// @Tags         Blog Media
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request  body  PresignRequest  true  "Upload request data"
// @Success      200  {object}  service.PresignedUploadResponse
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/media/presign [post]
func (h *MediaHandler) GeneratePresignedUpload(c fiber.Ctx) error {
	var req PresignRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	postID, err := uuid.Parse(req.PostID)
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}
	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	resp, err := h.mediaSvc.GeneratePresignedUpload(c, postID, req.Filename, req.ContentType, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.JSON(resp)
}

// Register registers an uploaded media file.
// @Summary      Register uploaded media
// @Description  Registers a media file that was uploaded via presigned URL
// @Tags         Blog Media
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request  body  service.RegisterMediaRequest  true  "Media registration data"
// @Success      201  {object}  map[string]interface{}  "{ message: string, media: PostMedia }"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/media [post]
func (h *MediaHandler) Register(c fiber.Ctx) error {
	var req service.RegisterMediaRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	media, err := h.mediaSvc.Register(c, &req, *userID, isAdmin(c))
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "Media registered successfully",
		"media":   media,
	})
}

// Delete deletes a media file.
// @Summary      Delete media
// @Description  Deletes a media file from the blog post (owner or admin only)
// @Tags         Blog Media
// @Security     Bearer
// @Param        id  path  string  true  "Media ID (UUID)"
// @Success      204  "No Content"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/media/{id} [delete]
func (h *MediaHandler) Delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid media ID format")
	}

	userID := requireUserID(c)
	if userID == nil {
		return errors.NewUnauthorized("Authentication required")
	}

	if err := h.mediaSvc.Delete(c, id, *userID, isAdmin(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ListByPost returns media files for a blog post.
// @Summary      List post media
// @Description  Returns all media files associated with a blog post
// @Tags         Blog Media
// @Produce      json
// @Security     Bearer
// @Param        postId  path  string  true  "Post ID (UUID)"
// @Success      200  {object}  map[string][]domain.PostMedia
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      401  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{postId}/media [get]
func (h *MediaHandler) ListByPost(c fiber.Ctx) error {
	postID, err := uuid.Parse(c.Params("postId"))
	if err != nil {
		return errors.NewBadRequest("Invalid post ID format")
	}

	media, err := h.mediaSvc.ListByPost(c, postID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"media": media})
}

// ServeFile serves a media file through the proxy with access control.
// @Summary      Serve media file
// @Description  Proxies a media file from storage with access control. Published post media is public, draft/archived requires authentication.
// @Tags         Blog Media
// @Produce      octet-stream
// @Param        key  path  string  true  "S3 object key"
// @Success      200  "File content"
// @Success      304  "Not Modified"
// @Failure      400  {object}  errors.ProblemDetail
// @Failure      403  {object}  errors.ProblemDetail
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/media/file/{key} [get]
func (h *MediaHandler) ServeFile(c fiber.Ctx) error {
	s3Key, _ := url.PathUnescape(c.Params("*1"))
	if s3Key == "" {
		return errors.NewBadRequest("Missing file key")
	}

	// Extract postID from key format: blog/{postID}/{filename}
	parts := strings.SplitN(s3Key, "/", 3)
	if len(parts) < 3 || parts[0] != "blog" {
		return errors.NewBadRequest("Invalid file key format")
	}

	postID, err := uuid.Parse(parts[1])
	if err != nil {
		return errors.NewBadRequest("Invalid post ID in file key")
	}

	// Access control
	info, err := h.mediaSvc.GetPostAccessInfo(c, postID)
	if err != nil {
		return errors.New(errors.CodeBlogPostNotFound, fiber.StatusNotFound, "Not Found", "File not found")
	}

	if info.Status != domain.PostStatusPublished {
		userID := getUserIDFromCtx(c)
		if userID == nil || (*userID != info.AuthorID && !isAdmin(c)) {
			return errors.NewForbidden("Access denied")
		}
	}

	// Get object metadata for ETag and Content-Length
	stat, err := h.storageSvc.StatObject(c, s3Key)
	if err != nil {
		return errors.New(errors.CodeBlogMediaNotFound, fiber.StatusNotFound, "Not Found", "File not found")
	}

	// ETag conditional request — return 304 if unchanged
	if match := c.Get("If-None-Match"); match == "*" || match == stat.ETag || strings.Contains(match, stat.ETag) {
		c.Set("Cache-Control", cacheControlFor(info.Status))
		c.Set("ETag", stat.ETag)
		return c.SendStatus(fiber.StatusNotModified)
	}

	// Set response headers
	c.Set("ETag", stat.ETag)
	c.Set("Content-Type", stat.ContentType)
	c.Set("Cache-Control", cacheControlFor(info.Status))
	c.Set("Cross-Origin-Resource-Policy", "cross-origin")
	if !strings.HasPrefix(stat.ContentType, "image/") {
		c.Set("Content-Disposition", "attachment")
	}

	// Stream the object — do NOT defer obj.Close() here because SendStream
	// only stores the reader reference; fasthttp reads from it AFTER the handler
	// returns. Closing here would truncate the stream. fasthttp closes it after
	// the response is fully written.
	obj, err := h.storageSvc.GetObject(c, s3Key)
	if err != nil {
		return errors.NewInternalError("Failed to read file")
	}

	return c.SendStream(obj, int(stat.Size))
}

// cacheControlFor returns the appropriate Cache-Control header value.
func cacheControlFor(status domain.PostStatus) string {
	if status == domain.PostStatusPublished {
		return cacheControlPublic
	}
	return cacheControlPrivate
}

// fiber:context-methods migrated
