package repository

import (
	"context"

	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type settingsRepositoryImpl struct {
	db *gorm.DB
}

// NewSettingsRepository creates a new SettingsRepository
func NewSettingsRepository(db *gorm.DB) SettingsRepository {
	return &settingsRepositoryImpl{db: db}
}

func (r *settingsRepositoryImpl) WithTx(tx *gorm.DB) SettingsRepository {
	if tx == nil {
		return r
	}
	return &settingsRepositoryImpl{db: tx}
}

func (r *settingsRepositoryImpl) Get(ctx context.Context) (*domain.BlogSettings, error) {
	db := r.db.WithContext(ctx)
	var settings domain.BlogSettings
	err := db.First(&settings).Error
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *settingsRepositoryImpl) Upsert(ctx context.Context, settings *domain.BlogSettings) error {
	db := r.db.WithContext(ctx)
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"auto_approve_comments", "posts_per_page", "view_cooldown_minutes",
			"feed_item_limit", "read_time_wpm", "updated_at",
		}),
	}).Create(settings).Error
}
