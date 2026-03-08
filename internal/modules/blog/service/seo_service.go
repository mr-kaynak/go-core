package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

// SEOMeta holds SEO metadata for a blog post
type SEOMeta struct {
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	CanonicalURL  string          `json:"canonical_url"`
	OGTitle       string          `json:"og_title"`
	OGDescription string          `json:"og_description"`
	OGImage       string          `json:"og_image,omitempty"`
	OGURL         string          `json:"og_url"`
	OGType        string          `json:"og_type"`
	TwitterCard   string          `json:"twitter_card"`
	JSONLD        json.RawMessage `json:"json_ld,omitempty"`
}

// SEOService generates SEO metadata for blog posts
type SEOService struct {
	siteURL string
}

// NewSEOService creates a new SEOService
func NewSEOService(cfg *config.Config) *SEOService {
	siteURL := strings.TrimRight(cfg.Blog.SiteURL, "/")
	validateSiteURL(siteURL)
	return &SEOService{
		siteURL: siteURL,
	}
}

// GenerateMeta generates SEO metadata for a post
func (s *SEOService) GenerateMeta(post *domain.Post, authorName string) *SEOMeta {
	const maxMetaDescriptionLen = 160

	title := post.MetaTitle
	if title == "" {
		title = post.Title
	}

	description := post.MetaDescription
	if description == "" {
		description = post.Excerpt
	}
	if description == "" && post.ContentPlain != "" {
		description = truncate(post.ContentPlain, maxMetaDescriptionLen)
	}

	canonicalURL := fmt.Sprintf("%s/blog/%s", s.siteURL, url.PathEscape(post.Slug))

	meta := &SEOMeta{
		Title:         title,
		Description:   description,
		CanonicalURL:  canonicalURL,
		OGTitle:       title,
		OGDescription: description,
		OGImage:       post.CoverImageURL,
		OGURL:         canonicalURL,
		OGType:        "article",
		TwitterCard:   "summary_large_image",
	}

	// Generate JSON-LD
	jsonLD, err := s.GenerateJSONLD(post, authorName)
	if err == nil {
		meta.JSONLD = jsonLD
	}

	return meta
}

// GenerateJSONLD generates Schema.org Article JSON-LD
func (s *SEOService) GenerateJSONLD(post *domain.Post, authorName string) ([]byte, error) {
	ld := map[string]interface{}{
		"@context":     "https://schema.org",
		"@type":        "Article",
		"headline":     post.Title,
		"url":          fmt.Sprintf("%s/blog/%s", s.siteURL, url.PathEscape(post.Slug)),
		"dateCreated":  post.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"dateModified": post.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"author": map[string]interface{}{
			"@type": "Person",
			"name":  authorName,
		},
	}

	if post.PublishedAt != nil {
		ld["datePublished"] = post.PublishedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if post.MetaDescription != "" {
		ld["description"] = post.MetaDescription
	} else if post.Excerpt != "" {
		ld["description"] = post.Excerpt
	}
	if post.CoverImageURL != "" {
		ld["image"] = post.CoverImageURL
	}
	if post.ReadTime > 0 {
		ld["timeRequired"] = fmt.Sprintf("PT%dM", post.ReadTime)
	}
	if post.Category != nil {
		ld["articleSection"] = post.Category.Name
	}
	if len(post.Tags) > 0 {
		keywords := make([]string, len(post.Tags))
		for i, t := range post.Tags {
			keywords[i] = t.Name
		}
		ld["keywords"] = keywords
	}

	return json.Marshal(ld)
}

// truncate truncates a string to the given max rune length at word boundaries
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	truncated := string(runes[:maxLen])
	idx := strings.LastIndex(truncated, " ")
	if idx < 0 {
		idx = len(truncated)
	}
	return truncated[:idx] + "..."
}
