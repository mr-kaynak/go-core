package service

import (
	"context"
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
)

func TestSettingsService(t *testing.T) {
	db, _ := SetupTestEnv()

	// SettingsRepository requires the db to have settings table migrated.
	// Since SetupTestEnv auto-migrates everything in SetupTestDB, we should be okay.
	// We didn't explicitly add BlogSettings to SetupTestDB, but NewSettingsRepository
	// might work if we just define a stub or if it's already auto-migrated.
	// Let's manually auto-migrate just in case.
	db.AutoMigrate(&domain.BlogSettings{})

	repo := repository.NewSettingsRepository(db)
	
	cfg := &config.Config{
		Blog: config.BlogConfig{
			PostsPerPage:  15,
			FeedItemLimit: 25,
		},
	}

	svc := NewSettingsService(cfg, repo)
	ctx := context.Background()

	t.Run("Get Defaults", func(t *testing.T) {
		settings := svc.Get(ctx)
		if settings == nil {
			t.Fatalf("expected defaults")
		}
		if settings.PostsPerPage != 15 {
			t.Errorf("expected 15, got %d", settings.PostsPerPage)
		}
	})

	t.Run("Update and Get", func(t *testing.T) {
		newLimit := 30
		req := &domain.UpdateBlogSettingsRequest{
			PostsPerPage: &newLimit,
		}

		updated, err := svc.Update(ctx, req)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}
		if updated.PostsPerPage != 30 {
			t.Errorf("expected 30, got %d", updated.PostsPerPage)
		}

		// Subsequent Get should return DB values
		settings := svc.Get(ctx)
		if settings.PostsPerPage != 30 {
			t.Errorf("Get after update expected 30, got %d", settings.PostsPerPage)
		}
	})
}
