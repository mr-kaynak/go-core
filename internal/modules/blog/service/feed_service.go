package service

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

// FeedService generates RSS, Atom, and Sitemap feeds
type FeedService struct {
	postRepo repository.PostRepository
	siteURL  string
	siteName string
	limit    int
}

// NewFeedService creates a new FeedService
func NewFeedService(postRepo repository.PostRepository, cfg *config.Config) *FeedService {
	return &FeedService{
		postRepo: postRepo,
		siteURL:  strings.TrimRight(cfg.Blog.SiteURL, "/"),
		siteName: cfg.App.Name,
		limit:    cfg.Blog.FeedItemLimit,
	}
}

// RSS 2.0 XML structures
type rssXML struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	PubDate     string    `xml:"pubDate,omitempty"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
}

// GenerateRSS generates an RSS 2.0 feed
func (s *FeedService) GenerateRSS() ([]byte, error) {
	posts, err := s.getPublishedPosts()
	if err != nil {
		return nil, err
	}

	items := make([]rssItem, 0, len(posts))
	for _, post := range posts {
		pubDate := post.CreatedAt
		if post.PublishedAt != nil {
			pubDate = *post.PublishedAt
		}
		items = append(items, rssItem{
			Title:       post.Title,
			Link:        fmt.Sprintf("%s/blog/%s", s.siteURL, post.Slug),
			Description: post.Excerpt,
			PubDate:     pubDate.Format(time.RFC1123Z),
			GUID:        post.ID.String(),
		})
	}

	var pubDate string
	if len(posts) > 0 && posts[0].PublishedAt != nil {
		pubDate = posts[0].PublishedAt.Format(time.RFC1123Z)
	}

	rss := rssXML{
		Version: "2.0",
		Channel: rssChannel{
			Title:       s.siteName + " Blog",
			Link:        s.siteURL + "/blog",
			Description: s.siteName + " Blog Feed",
			Language:    "en",
			PubDate:     pubDate,
			Items:       items,
		},
	}

	output, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), output...), nil
}

// Atom XML structures
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	XMLNS   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	Link    atomLink    `xml:"link"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type atomEntry struct {
	Title   string   `xml:"title"`
	Link    atomLink `xml:"link"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Summary string   `xml:"summary,omitempty"`
}

// GenerateAtom generates an Atom feed
func (s *FeedService) GenerateAtom() ([]byte, error) {
	posts, err := s.getPublishedPosts()
	if err != nil {
		return nil, err
	}

	entries := make([]atomEntry, 0, len(posts))
	for _, post := range posts {
		updated := post.UpdatedAt
		if post.PublishedAt != nil {
			updated = *post.PublishedAt
		}
		entries = append(entries, atomEntry{
			Title:   post.Title,
			Link:    atomLink{Href: fmt.Sprintf("%s/blog/%s", s.siteURL, post.Slug)},
			ID:      fmt.Sprintf("urn:uuid:%s", post.ID.String()),
			Updated: updated.Format(time.RFC3339),
			Summary: post.Excerpt,
		})
	}

	feed := atomFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   s.siteName + " Blog",
		Link:    atomLink{Href: s.siteURL + "/blog/feed/atom", Rel: "self"},
		ID:      s.siteURL + "/blog",
		Updated: time.Now().Format(time.RFC3339),
		Entries: entries,
	}

	output, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), output...), nil
}

// Sitemap XML structures
type sitemapXML struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

// GenerateSitemap generates an XML sitemap for blog posts
func (s *FeedService) GenerateSitemap() ([]byte, error) {
	posts, err := s.getPublishedPosts()
	if err != nil {
		return nil, err
	}

	urls := make([]sitemapURL, 0, len(posts)+1)
	// Blog index
	urls = append(urls, sitemapURL{
		Loc:        s.siteURL + "/blog",
		ChangeFreq: "daily",
		Priority:   "0.8",
	})

	for _, post := range posts {
		urls = append(urls, sitemapURL{
			Loc:        fmt.Sprintf("%s/blog/%s", s.siteURL, post.Slug),
			LastMod:    post.UpdatedAt.Format("2006-01-02"),
			ChangeFreq: "weekly",
			Priority:   "0.7",
		})
	}

	sitemap := sitemapXML{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	output, err := xml.MarshalIndent(sitemap, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), output...), nil
}

func (s *FeedService) getPublishedPosts() ([]*domain.Post, error) {
	posts, _, err := s.postRepo.ListFiltered(repository.PostListFilter{
		Status: string(domain.PostStatusPublished),
		SortBy: "published_at",
		Order:  "desc",
		Limit:  s.limit,
		Offset: 0,
	})
	return posts, err
}
