package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

// SEOMeta holds SEO metadata for a blog post
type SEOMeta struct {
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	CanonicalURL  string            `json:"canonical_url"`
	OGTitle       string            `json:"og_title"`
	OGDescription string            `json:"og_description"`
	OGImage       string            `json:"og_image,omitempty"`
	OGURL         string            `json:"og_url"`
	OGType        string            `json:"og_type"`
	TwitterCard   string            `json:"twitter_card"`
	JSONLD        json.RawMessage   `json:"json_ld,omitempty"`
}

// SEOService generates SEO metadata for blog posts
type SEOService struct {
	siteURL string
}

// NewSEOService creates a new SEOService
func NewSEOService(cfg *config.Config) *SEOService {
	return &SEOService{
		siteURL: strings.TrimRight(cfg.Blog.SiteURL, "/"),
	}
}

// GenerateMeta generates SEO metadata for a post
func (s *SEOService) GenerateMeta(post *domain.Post, authorName string) *SEOMeta {
	description := post.Excerpt
	if description == "" && post.ContentPlain != "" {
		description = truncate(post.ContentPlain, 160)
	}

	canonicalURL := fmt.Sprintf("%s/blog/%s", s.siteURL, post.Slug)

	meta := &SEOMeta{
		Title:         post.Title,
		Description:   description,
		CanonicalURL:  canonicalURL,
		OGTitle:       post.Title,
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
		"@context":      "https://schema.org",
		"@type":         "Article",
		"headline":      post.Title,
		"url":           fmt.Sprintf("%s/blog/%s", s.siteURL, post.Slug),
		"dateCreated":   post.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"dateModified":  post.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"author": map[string]interface{}{
			"@type": "Person",
			"name":  authorName,
		},
	}

	if post.PublishedAt != nil {
		ld["datePublished"] = post.PublishedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if post.Excerpt != "" {
		ld["description"] = post.Excerpt
	}
	if post.CoverImageURL != "" {
		ld["image"] = post.CoverImageURL
	}
	if post.ReadTime > 0 {
		ld["timeRequired"] = fmt.Sprintf("PT%dM", post.ReadTime/60)
	}

	return json.Marshal(ld)
}

// truncate truncates a string to the given max length at word boundaries
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Find the last space before maxLen
	idx := strings.LastIndex(s[:maxLen], " ")
	if idx < 0 {
		idx = maxLen
	}
	return s[:idx] + "..."
}
