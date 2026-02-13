package response

// Pagination holds pagination metadata returned by list endpoints.
type Pagination struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int64 `json:"total_pages"`
}

// PaginatedResponse is the standardized response shape for paginated endpoints.
type PaginatedResponse[T any] struct {
	Items      []T        `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// NewPaginatedResponse constructs a standard paginated response.
func NewPaginatedResponse[T any](items []T, page, limit int, total int64) PaginatedResponse[T] {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 1
	}

	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}

	return PaginatedResponse[T]{
		Items: items,
		Pagination: Pagination{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}
}
