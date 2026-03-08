package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"gorm.io/gorm"
)

const (
	settingsCacheKey = "blog:settings"
	settingsCacheTTL = 5 * time.Minute
)

// SettingsService handles blog settings business logic
type SettingsService struct {
	cfg          *config.Config
	settingsRepo repository.SettingsRepository
	redisClient  *cache.RedisClient
	logger       *logger.Logger
}

// NewSettingsService creates a new SettingsService
func NewSettingsService(cfg *config.Config, settingsRepo repository.SettingsRepository) *SettingsService {
	return &SettingsService{
		cfg:          cfg,
		settingsRepo: settingsRepo,
		logger:       logger.Get().WithFields(logger.Fields{"service": "blog_settings"}),
	}
}

// SetRedisClient sets the optional Redis client for caching
func (s *SettingsService) SetRedisClient(rc *cache.RedisClient) {
	s.redisClient = rc
}

// Get returns the current blog settings (cache → DB → config defaults)
func (s *SettingsService) Get(ctx context.Context) *domain.BlogSettings {
	// Try Redis cache first
	if s.redisClient != nil {
		cached, err := s.redisClient.Get(ctx, settingsCacheKey)
		if err == nil && cached != "" {
			var settings domain.BlogSettings
			if err := json.Unmarshal([]byte(cached), &settings); err == nil {
				return &settings
			}
		}
	}

	// Try DB
	settings, err := s.settingsRepo.Get()
	if err == nil {
		s.cacheSettings(ctx, settings)
		return settings
	}

	if err != gorm.ErrRecordNotFound {
		s.logger.Error("Failed to get blog settings from DB", "error", err)
	}

	// Fallback to config defaults
	defaults := s.configDefaults()
	return defaults
}

// Update applies partial updates to blog settings
func (s *SettingsService) Update(ctx context.Context, req *domain.UpdateBlogSettingsRequest) (*domain.BlogSettings, error) {
	// Get current settings (from DB or defaults)
	current := s.Get(ctx)

	// Apply partial updates
	if req.AutoApproveComments != nil {
		current.AutoApproveComments = *req.AutoApproveComments
	}
	if req.PostsPerPage != nil {
		current.PostsPerPage = *req.PostsPerPage
	}
	if req.ViewCooldownMinutes != nil {
		current.ViewCooldownMinutes = *req.ViewCooldownMinutes
	}
	if req.FeedItemLimit != nil {
		current.FeedItemLimit = *req.FeedItemLimit
	}
	if req.ReadTimeWPM != nil {
		current.ReadTimeWPM = *req.ReadTimeWPM
	}

	if err := s.settingsRepo.Upsert(current); err != nil {
		s.logger.Error("Failed to upsert blog settings", "error", err)
		return nil, err
	}

	// Invalidate cache
	s.invalidateCache(ctx)

	s.logger.Info("Blog settings updated")
	return current, nil
}

func (s *SettingsService) configDefaults() *domain.BlogSettings {
	viewCooldownMinutes := int(s.cfg.Blog.ViewCooldown.Minutes())
	if viewCooldownMinutes == 0 {
		viewCooldownMinutes = 30
	}

	postsPerPage := s.cfg.Blog.PostsPerPage
	if postsPerPage == 0 {
		postsPerPage = 20
	}

	feedItemLimit := s.cfg.Blog.FeedItemLimit
	if feedItemLimit == 0 {
		feedItemLimit = 50
	}

	readTimeWPM := s.cfg.Blog.ReadTimeWPM
	if readTimeWPM == 0 {
		readTimeWPM = 200
	}

	return &domain.BlogSettings{
		AutoApproveComments: s.cfg.Blog.AutoApproveComments,
		PostsPerPage:        postsPerPage,
		ViewCooldownMinutes: viewCooldownMinutes,
		FeedItemLimit:       feedItemLimit,
		ReadTimeWPM:         readTimeWPM,
	}
}

func (s *SettingsService) cacheSettings(ctx context.Context, settings *domain.BlogSettings) {
	if s.redisClient == nil {
		return
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return
	}
	_ = s.redisClient.Set(ctx, settingsCacheKey, string(data), settingsCacheTTL)
}

func (s *SettingsService) invalidateCache(ctx context.Context) {
	if s.redisClient == nil {
		return
	}
	_ = s.redisClient.Del(ctx, settingsCacheKey)
}
