package service

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// turkish character mapping
var turkishReplacer = strings.NewReplacer(
	"ş", "s", "Ş", "s",
	"ç", "c", "Ç", "c",
	"ğ", "g", "Ğ", "g",
	"ı", "i", "İ", "i",
	"ö", "o", "Ö", "o",
	"ü", "u", "Ü", "u",
)

var (
	nonAlphanumRegex = regexp.MustCompile(`[^a-z0-9-]`)
	multiDashRegex   = regexp.MustCompile(`-{2,}`)
)

// SlugService handles slug generation from text
type SlugService struct{}

// NewSlugService creates a new SlugService
func NewSlugService() *SlugService {
	return &SlugService{}
}

// Generate creates a URL-friendly slug from the given text
func (s *SlugService) Generate(text string) string {
	// Normalize unicode
	slug := norm.NFKD.String(text)

	// Replace Turkish characters
	slug = turkishReplacer.Replace(slug)

	// Lowercase
	slug = strings.ToLower(slug)

	// Replace non-ascii unicode letters with closest ASCII
	slug = removeDiacritics(slug)

	// Replace spaces and underscores with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")

	// Remove non-alphanumeric characters (except hyphens)
	slug = nonAlphanumRegex.ReplaceAllString(slug, "")

	// Collapse multiple dashes
	slug = multiDashRegex.ReplaceAllString(slug, "-")

	// Trim leading/trailing dashes
	slug = strings.Trim(slug, "-")

	// Fallback for empty input or input that reduces to empty string
	if slug == "" {
		return uuid.New().String()[:8]
	}

	return slug
}

// removeDiacritics strips diacritical marks from characters
func removeDiacritics(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			// Skip combining marks (diacritics)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
