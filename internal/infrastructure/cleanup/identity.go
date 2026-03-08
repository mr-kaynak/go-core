package cleanup

import (
	"context"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

const (
	IdentityCleanupInterval    = 6 * time.Hour
	RevokedAPIKeyCleanupWindow = 7 * 24 * time.Hour
)

// RunIdentityCleanup periodically cleans up expired tokens and revoked API keys.
// It blocks until ctx is canceled; call it in a goroutine.
func RunIdentityCleanup(ctx context.Context, db *gorm.DB, log *logger.Logger) {
	ticker := time.NewTicker(IdentityCleanupInterval)
	defer ticker.Stop()

	userRepo := repository.NewUserRepository(db)
	verificationRepo := repository.NewVerificationTokenRepository(db)
	apiKeyRepo := repository.NewAPIKeyRepository(db)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := userRepo.CleanExpiredRefreshTokens(); err != nil {
				log.Error("Failed to clean expired refresh tokens", "error", err)
			}
			if err := verificationRepo.DeleteExpiredTokens(); err != nil {
				log.Error("Failed to clean expired verification tokens", "error", err)
			}
			if err := apiKeyRepo.CleanupRevokedKeys(RevokedAPIKeyCleanupWindow); err != nil {
				log.Error("Failed to clean revoked API keys", "error", err)
			}
		}
	}
}
