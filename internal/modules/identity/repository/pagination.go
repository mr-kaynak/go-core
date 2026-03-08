package repository

const (
	defaultPageLimit = 20
	maxPageLimit     = 500
)

// clampLimit normalises a pagination limit to a safe range.
// Zero, negative, or excessively large values are replaced with defaults.
func clampLimit(limit int) int {
	if limit <= 0 || limit > maxPageLimit {
		return defaultPageLimit
	}
	return limit
}
