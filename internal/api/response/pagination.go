package response

// MaxPaginationLimit is the maximum number of items per page for paginated endpoints.
const MaxPaginationLimit = 100

// --- Offset-based pagination ---

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
	if limit > MaxPaginationLimit {
		limit = MaxPaginationLimit
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

// --- Cursor-based (keyset) pagination ---

// CursorPagination holds cursor-based pagination metadata.
type CursorPagination struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// CursorPaginatedResponse is the response shape for cursor-based paginated endpoints.
type CursorPaginatedResponse[T any] struct {
	Items      []T              `json:"items"`
	Pagination CursorPagination `json:"pagination"`
}

// NewCursorPaginatedResponse constructs a cursor-based paginated response.
// Pass limit+1 items from the query; if len(items) > limit, there are more pages.
// cursorFn extracts the cursor value from the last visible item.
func NewCursorPaginatedResponse[T any](items []T, limit int, cursorFn func(T) string) CursorPaginatedResponse[T] {
	if limit < 1 {
		limit = 1
	}
	if limit > MaxPaginationLimit {
		limit = MaxPaginationLimit
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		nextCursor = cursorFn(items[len(items)-1])
	}

	return CursorPaginatedResponse[T]{
		Items: items,
		Pagination: CursorPagination{
			Limit:      limit,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}
}

// SanitizeLimit clamps a user-provided limit to [1, MaxPaginationLimit].
func SanitizeLimit(limit, defaultLimit int) int {
	if limit < 1 {
		return defaultLimit
	}
	if limit > MaxPaginationLimit {
		return MaxPaginationLimit
	}
	return limit
}
