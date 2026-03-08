package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// swag annotation type references
var _ *errors.ProblemDetail

// SEOHandler handles SEO metadata HTTP requests
type SEOHandler struct {
	seoSvc  *service.SEOService
	postSvc *service.PostService
}

// NewSEOHandler creates a new SEOHandler
func NewSEOHandler(seoSvc *service.SEOService, postSvc *service.PostService) *SEOHandler {
	return &SEOHandler{seoSvc: seoSvc, postSvc: postSvc}
}

// RegisterRoutes registers SEO routes
func (h *SEOHandler) RegisterRoutes(blog fiber.Router) {
	blog.Get("/posts/:slug/meta", h.GetMeta)
}

// GetMeta returns SEO metadata for a blog post.
// @Summary      Get SEO metadata
// @Description  Returns SEO metadata including Open Graph and JSON-LD for a blog post
// @Tags         Blog SEO
// @Produce      json
// @Param        slug  path  string  true  "Post slug"
// @Success      200  {object}  service.SEOMeta
// @Failure      404  {object}  errors.ProblemDetail
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/posts/{slug}/meta [get]
func (h *SEOHandler) GetMeta(c *fiber.Ctx) error {
	slug := c.Params("slug")

	post, err := h.postSvc.GetBySlug(slug)
	if err != nil {
		return err
	}

	// TODO: resolve author name from user service when available
	authorName := "Unknown Author"

	meta := h.seoSvc.GenerateMeta(post, authorName)
	return c.JSON(meta)
}
