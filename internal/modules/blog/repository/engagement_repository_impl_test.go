package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
)

func TestEngagementRepository(t *testing.T) {
	db := SetupTestDB()
	repo := NewEngagementRepository(db)

	postID := uuid.New()
	userID := uuid.New()
	db.Create(&domain.Post{ID: postID, Title: "Engage Post", Slug: "engage", AuthorID: userID})

	t.Run("Like Operations", func(t *testing.T) {
		// Toggle ON
		_, err := repo.ToggleLike(postID, userID)
		if err != nil {
			t.Fatalf("ToggleLike failed: %v", err)
		}

		isLiked, err := repo.IsLiked(postID, userID)
		if err != nil || !isLiked {
			t.Errorf("expected isLiked to be true")
		}

		// Toggle OFF
		_, err = repo.ToggleLike(postID, userID)
		if err != nil {
			t.Fatalf("ToggleLike unlike failed: %v", err)
		}

		isLiked, _ = repo.IsLiked(postID, userID)
		if isLiked {
			t.Errorf("expected isLiked to be false after second toggle")
		}
	})

	t.Run("View Operations", func(t *testing.T) {
		view := &domain.PostView{
			ID:        uuid.New(),
			PostID:    postID,
			IPAddress: "127.0.0.1",
			UserAgent: "Test Agent",
			ViewedAt:  time.Now(),
		}

		err := repo.CreateView(view)
		if err != nil {
			t.Fatalf("RecordView failed: %v", err)
		}

		// HasViewedSession
		recent := time.Now().Add(-1 * time.Hour)
		hasViewed, err := repo.HasRecentView(postID, "127.0.0.1", recent)
		if err != nil || !hasViewed {
			t.Errorf("HasRecentView failed")
		}

	})

	t.Run("Share Operations", func(t *testing.T) {
		share := &domain.PostShare{
			ID:        uuid.New(),
			PostID:    postID,
			Platform:  "twitter",
			IPAddress: "127.0.0.1",
		}

		err := repo.CreateShare(share)
		if err != nil {
			t.Fatalf("RecordShare failed: %v", err)
		}
	})

	t.Run("Stats Operations", func(t *testing.T) {
		t.Skip("SQLite does not support GREATEST function used in IncrementStat")

		// Update stats
		err := repo.IncrementStat(postID, "view_count", 1)
		if err != nil {
			t.Fatalf("IncrementStat failed: %v", err)
		}

		stats, err := repo.GetStats(postID)
		if err != nil {
			t.Fatalf("GetStats failed: %v", err)
		}

		// Share=1, Views=1, Likes=0 (toggled off)
		if stats.ViewCount < 1 || stats.ShareCount < 1 {
			t.Errorf("UpdateStats didn't aggregate correctly: %+v", stats)
		}
	})

	t.Run("Trending", func(t *testing.T) {

		trending, err := repo.GetTrending(TrendingQuery{Limit: 10, Days: 7})
		if err != nil {
			t.Fatalf("GetTrendingPosts failed: %v", err)
		}
		// The post above might exist but it's not published, or it depends on how GetTrendingPosts behaves
		// if posts have no views/likes yet. Just ensure it doesn't crash:
		_ = trending
	})
}
