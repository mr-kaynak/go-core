package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// swag annotation type references
var _ *errors.ProblemDetail

// FeedHandler handles RSS, Atom, and Sitemap HTTP requests
type FeedHandler struct {
	feedSvc *service.FeedService
}

// NewFeedHandler creates a new FeedHandler
func NewFeedHandler(feedSvc *service.FeedService) *FeedHandler {
	return &FeedHandler{feedSvc: feedSvc}
}

// RegisterRoutes registers feed routes
func (h *FeedHandler) RegisterRoutes(blog fiber.Router) {
	blog.Get("/feed/rss", h.RSS)
	blog.Get("/feed/atom", h.Atom)
	blog.Get("/sitemap.xml", h.Sitemap)
}

// RSS returns the blog RSS feed.
// @Summary      Get RSS feed
// @Description  Returns the blog RSS 2.0 feed
// @Tags         Blog Feed
// @Produce      xml
// @Success      200  {string}  string  "RSS XML"
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/feed/rss [get]
func (h *FeedHandler) RSS(c fiber.Ctx) error {
	data, err := h.feedSvc.GenerateRSS()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/rss+xml; charset=utf-8")
	return c.Send(data)
}

// Atom returns the blog Atom feed.
// @Summary      Get Atom feed
// @Description  Returns the blog Atom 1.0 feed
// @Tags         Blog Feed
// @Produce      xml
// @Success      200  {string}  string  "Atom XML"
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/feed/atom [get]
func (h *FeedHandler) Atom(c fiber.Ctx) error {
	data, err := h.feedSvc.GenerateAtom()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/atom+xml; charset=utf-8")
	return c.Send(data)
}

// Sitemap returns the blog sitemap.
// @Summary      Get sitemap
// @Description  Returns the blog sitemap XML for search engine indexing
// @Tags         Blog Feed
// @Produce      xml
// @Success      200  {string}  string  "Sitemap XML"
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/sitemap.xml [get]
func (h *FeedHandler) Sitemap(c fiber.Ctx) error {
	data, err := h.feedSvc.GenerateSitemap()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/xml; charset=utf-8")
	return c.Send(data)
}
