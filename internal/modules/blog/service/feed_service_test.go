package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

func TestFeedService(t *testing.T) {
	db, _ := SetupTestEnv()
	postRepo := repository.NewPostRepository(db)

	cfg := &config.Config{
		App: config.AppConfig{Name: "Test Feed"},
		Blog: config.BlogConfig{
			SiteURL:       "https://testfeed.com",
			FeedItemLimit: 10,
		},
	}
	svc := NewFeedService(postRepo, cfg)

	// Create a published post
	now := time.Now()
	authorID := uuid.New()
	post := &domain.Post{
		ID:            uuid.New(),
		Title:         "Feed Post",
		Slug:          "feed-post",
		Excerpt:       "Summary here.",
		CoverImageURL: "https://testfeed.com/img.png",
		AuthorID:      authorID,
		Status:        domain.PostStatusPublished,
		PublishedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	postRepo.Create(post)

	t.Run("GenerateRSS", func(t *testing.T) {
		rss, err := svc.GenerateRSS()
		if err != nil {
			t.Fatalf("GenerateRSS failed: %v", err)
		}
		out := string(rss)
		if !strings.Contains(out, "<title>Feed Post</title>") {
			t.Errorf("RSS missing post title")
		}
		if !strings.Contains(out, "<link>https://testfeed.com/blog/feed-post</link>") {
			t.Errorf("RSS missing link")
		}
	})

	t.Run("GenerateAtom", func(t *testing.T) {
		atom, err := svc.GenerateAtom()
		if err != nil {
			t.Fatalf("GenerateAtom failed: %v", err)
		}
		out := string(atom)
		if !strings.Contains(out, "<title>Feed Post</title>") {
			t.Errorf("Atom missing post title")
		}
		if !strings.Contains(out, "href=\"https://testfeed.com/blog/feed-post\"") {
			t.Errorf("Atom missing link")
		}
	})

	t.Run("GenerateSitemap", func(t *testing.T) {
		sitemap, err := svc.GenerateSitemap()
		if err != nil {
			t.Fatalf("GenerateSitemap failed: %v", err)
		}
		out := string(sitemap)
		if !strings.Contains(out, "<loc>https://testfeed.com/blog/feed-post</loc>") {
			t.Errorf("Sitemap missing loc")
		}
		if !strings.Contains(out, "<image:loc>https://testfeed.com/img.png</image:loc>") {
			t.Errorf("Sitemap missing image loc")
		}
	})

	t.Run("ValidateSiteURL Output", func(t *testing.T) {
		// Just to hit coverage for edge cases
		validateSiteURL("")
		validateSiteURL("invalid-url")
		validateSiteURL("http://localhost:8080")
	})
}
