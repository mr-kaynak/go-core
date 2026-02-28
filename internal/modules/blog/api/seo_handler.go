package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

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

func (h *SEOHandler) GetMeta(c *fiber.Ctx) error {
	slug := c.Params("slug")

	post, err := h.postSvc.GetBySlug(slug)
	if err != nil {
		return err
	}

	// Author name from post.Author field would be populated in a real scenario
	authorName := "Author"

	meta := h.seoSvc.GenerateMeta(post, authorName)
	return c.JSON(meta)
}
