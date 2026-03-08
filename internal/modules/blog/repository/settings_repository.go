package repository

import (
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

// SettingsRepository defines the interface for blog settings data operations
type SettingsRepository interface {
	WithTx(tx *gorm.DB) SettingsRepository
	Get() (*domain.BlogSettings, error)
	Upsert(settings *domain.BlogSettings) error
}
