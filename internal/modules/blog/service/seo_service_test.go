package service

import (
	"testing"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

func TestSEOService(t *testing.T) {
	cfg := &config.Config{
		Blog: config.BlogConfig{
			SiteURL: "https://example.com/",
		},
	}
	svc := NewSEOService(cfg)

	now := time.Now()
	post := &domain.Post{
		Title:           "SEO Test Post",
		Slug:            "seo-test-post",
		Excerpt:         "Short excerpt.",
		ContentPlain:    "This is a longer plain text content that might be used if excerpt is empty.",
		CoverImageURL:   "https://example.com/img.png",
		MetaTitle:       "Custom Meta Title",
		MetaDescription: "Custom Meta Description",
		CreatedAt:       now,
		UpdatedAt:       now,
		PublishedAt:     &now,
		ReadTime:        5,
		Category: &domain.Category{
			Name: "Tech",
		},
		Tags: []domain.Tag{
			{Name: "Go"},
		},
	}

	t.Run("GenerateMeta Full", func(t *testing.T) {
		meta := svc.GenerateMeta(post, "Author Name")
		if meta.Title != "Custom Meta Title" {
			t.Errorf("expected Title to be Custom Meta Title")
		}
		if meta.Description != "Custom Meta Description" {
			t.Errorf("expected Description to be Custom Meta Description")
		}
		if meta.CanonicalURL != "https://example.com/blog/seo-test-post" {
			t.Errorf("expected CanonicalURL to be correct")
		}
		if meta.OGImage != "https://example.com/img.png" {
			t.Errorf("expected OGImage to be set")
		}
	})

	t.Run("GenerateMeta Fallbacks", func(t *testing.T) {
		postNoMeta := &domain.Post{
			Title:        "Base Title",
			Slug:         "base-title",
			ContentPlain: "Plain text that is very long but acts as a fallback for the description.",
		}
		meta := svc.GenerateMeta(postNoMeta, "Author Name")

		if meta.Title != "Base Title" {
			t.Errorf("expected Base Title fallback")
		}
		if meta.Description != postNoMeta.ContentPlain {
			t.Errorf("expected plain text fallback for description")
		}
	})

	t.Run("Truncation", func(t *testing.T) {
		longText := ""
		for i := 0; i < 200; i++ {
			longText += "a"
		}
		postTrunc := &domain.Post{
			Title:        "Title",
			ContentPlain: longText, // > 160 chars
		}
		meta := svc.GenerateMeta(postTrunc, "A")
		if len(meta.Description) > 163 { // 160 + "..."
			t.Errorf("expected description to be truncated, got %d chars", len(meta.Description))
		}
	})
}
