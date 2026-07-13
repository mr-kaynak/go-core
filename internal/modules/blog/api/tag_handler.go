package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/api/helpers"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/service"
)

// swag annotation type references
var (
	_ *errors.ProblemDetail
	_ *domain.Tag
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

// List returns a paginated list of tags.
// @Summary      List tags
// @Description  Returns a paginated list of blog tags
// @Tags         Blog Tags
// @Produce      json
// @Param        page   query  int  false  "Page number"     default(1)
// @Param        limit  query  int  false  "Items per page"  default(50)
// @Success      200  {object}  apiresponse.PaginatedResponse[domain.Tag]
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/tags [get]
func (h *TagHandler) List(c fiber.Ctx) error {
	page, limit, offset := helpers.ParsePagination(c, 50)

	tags, total, err := h.tagSvc.List(c.Context(), offset, limit)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(tags, page, limit, total))
}

// GetPopular returns the most popular tags.
// @Summary      Get popular tags
// @Description  Returns the most popular blog tags sorted by post count
// @Tags         Blog Tags
// @Produce      json
// @Param        limit  query  int  false  "Number of tags"  default(20)
// @Success      200  {object}  map[string][]domain.Tag
// @Failure      500  {object}  errors.ProblemDetail
// @Router       /blog/tags/popular [get]
func (h *TagHandler) GetPopular(c fiber.Ctx) error {
	limit := apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", 20), 20)

	tags, err := h.tagSvc.GetPopular(c.Context(), limit)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"tags": tags})
}
