package api

import (
	"github.com/gofiber/fiber/v2"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// TagHandler handles blog tag HTTP requests
type TagHandler struct {
	tagSvc *service.TagService
}

// NewTagHandler creates a new TagHandler
func NewTagHandler(tagSvc *service.TagService) *TagHandler {
	return &TagHandler{tagSvc: tagSvc}
}

// RegisterRoutes registers tag routes
func (h *TagHandler) RegisterRoutes(blog fiber.Router) {
	tags := blog.Group("/tags")
	tags.Get("/", h.List)
	tags.Get("/popular", h.GetPopular)
}

func (h *TagHandler) List(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 50)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	tags, total, err := h.tagSvc.List(offset, limit)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(tags, page, limit, total))
}

func (h *TagHandler) GetPopular(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}

	tags, err := h.tagSvc.GetPopular(limit)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": tags})
}
