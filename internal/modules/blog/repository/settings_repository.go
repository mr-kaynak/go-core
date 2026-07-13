package repository

import (
	"context"

	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// SettingsRepository defines the interface for blog settings data operations
type SettingsRepository interface {
	WithTx(tx *gorm.DB) SettingsRepository
	Get(ctx context.Context) (*domain.BlogSettings, error)
	Upsert(ctx context.Context, settings *domain.BlogSettings) error
}
