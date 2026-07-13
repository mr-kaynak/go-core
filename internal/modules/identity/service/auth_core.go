package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"gorm.io/gorm"
)

// authCore holds the shared dependencies used across the auth sub-services
// (config, database handle, logger, metrics recorder and language resolver).
// It is embedded by AuthService and every sub-service so that method promotion
// keeps runInTx, getMetrics and resolveUserLanguage resolving through a single
// shared instance.
type authCore struct {
	cfg              *config.Config
	db               *gorm.DB
	logger           *logger.Logger
	metrics          metrics.MetricsRecorder
	languageResolver UserLanguageResolver
}

// runInTx executes fn inside a database transaction. If db is nil (e.g. in
// tests) it calls fn with nil so that repo.WithTx(nil) returns the original
// repository instance.
func (s *authCore) runInTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if s.db == nil {
		return fn(nil)
	}
	return s.db.WithContext(ctx).Transaction(fn)
}

// SetMetrics sets the optional metrics recorder. Falls back to global singleton if not set.
func (s *authCore) SetMetrics(m metrics.MetricsRecorder) {
	s.metrics = m
}

// SetLanguageResolver sets the optional language resolver for dynamic user language lookup.
func (s *authCore) SetLanguageResolver(lr UserLanguageResolver) {
	s.languageResolver = lr
}

// resolveUserLanguage returns the user's preferred language, falling back to "en".
func (s *authCore) resolveUserLanguage(ctx context.Context, userID uuid.UUID) string {
	if s.languageResolver == nil {
		return "en"
	}
	lang, err := s.languageResolver.GetLanguageByUserID(ctx, userID)
	if err != nil || lang == "" || !validation.IsValidLanguageCode(lang) {
		return "en"
	}
	return lang
}

func (s *authCore) getMetrics() metrics.MetricsRecorder {
	if s.metrics != nil {
		return s.metrics
	}
	if m := metrics.GetMetrics(); m != nil {
		return m
	}
	return metrics.NoOpMetrics{}
}
