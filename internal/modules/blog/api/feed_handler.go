package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

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

func (h *FeedHandler) RSS(c *fiber.Ctx) error {
	data, err := h.feedSvc.GenerateRSS()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/rss+xml; charset=utf-8")
	return c.Send(data)
}

func (h *FeedHandler) Atom(c *fiber.Ctx) error {
	data, err := h.feedSvc.GenerateAtom()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/atom+xml; charset=utf-8")
	return c.Send(data)
}

func (h *FeedHandler) Sitemap(c *fiber.Ctx) error {
	data, err := h.feedSvc.GenerateSitemap()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/xml; charset=utf-8")
	return c.Send(data)
}
