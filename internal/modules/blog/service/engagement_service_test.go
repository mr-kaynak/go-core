package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

func TestEngagementService(t *testing.T) {
	db, _ := SetupTestEnv()
	engRepo := repository.NewEngagementRepository(db)
	postRepo := repository.NewPostRepository(db)

	cfg := &config.Config{
		Blog: config.BlogConfig{
			ViewCooldown: 1 * time.Second,
			TrendingWeights: config.TrendingWeights{
				View:    1,
				Like:    2,
				Comment: 3,
				Share:   4,
			},
		},
	}

	svc := NewEngagementService(db, cfg, engRepo, postRepo)
	ctx := context.Background()

	// Setup a post for engagement
	postID := uuid.New()
	userID := uuid.New()

	post := &domain.Post{
		ID:       postID,
		Title:    "Engage Me",
		Slug:     "engage-me",
		AuthorID: userID,
		Status:   domain.PostStatusPublished,
	}
	postRepo.Create(post)

	// Create initial stats for the post so IncrementStat does not fail
	engRepo.UpsertStats(&domain.PostStats{PostID: postID, UpdatedAt: time.Now()})

	t.Run("ToggleLike", func(t *testing.T) {
		res, err := svc.ToggleLike(ctx, postID, userID)
		if err != nil {
			t.Fatalf("ToggleLike failed: %v", err)
		}
		if !res.Liked {
			t.Errorf("expected liked to be true")
		}

		isLiked, _ := svc.IsLiked(postID, userID)
		if !isLiked {
			t.Errorf("IsLiked should be true")
		}

		// Toggle off
		res, err = svc.ToggleLike(ctx, postID, userID)
		if err != nil || res.Liked {
			t.Errorf("expected unliked")
		}
	})

	t.Run("RecordView", func(t *testing.T) {
		err := svc.RecordView(ctx, postID, &userID, "127.0.0.1", "Agent", "Ref")
		if err != nil {
			t.Fatalf("RecordView failed: %v", err)
		}

		// Second view inside cooldown should return nil but not increment stats
		err = svc.RecordView(ctx, postID, &userID, "127.0.0.1", "Agent", "Ref")
		if err != nil {
			t.Errorf("RecordView during cooldown shouldn't error")
		}
	})

	t.Run("RecordShare", func(t *testing.T) {
		err := svc.RecordShare(ctx, postID, &userID, "twitter", "127.0.0.1")
		if err != nil {
			t.Fatalf("RecordShare failed: %v", err)
		}
	})

	t.Run("GetStats and Trending", func(t *testing.T) {
		stats, err := svc.GetStats(postID)
		if err != nil {
			t.Fatalf("GetStats failed: %v", err)
		}
		_ = stats

		batch, err := svc.GetBatchStats([]uuid.UUID{postID})
		if err != nil || len(batch) == 0 {
			t.Errorf("GetBatchStats failed")
		}

		popular, err := svc.GetPopular(10)
		if err != nil {
			t.Errorf("GetPopular failed")
		}
		_ = popular
	})
}
