package bootstrap

import (
	"github.com/mr-kaynak/go-core/internal/core/logger"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"gorm.io/gorm"
)

// SeedTemplates creates default template categories and system templates.
// It is safe to call multiple times (idempotent).
func SeedTemplates(db *gorm.DB) error {
	log := logger.Get().WithFields(logger.Fields{"service": "bootstrap.templates"})

	templateRepo := notificationRepository.NewTemplateRepository(db)
	templateService := notificationService.NewTemplateService(templateRepo)

	log.Info("Creating template categories")
	categories := []struct {
		name        string
		description string
	}{
		{"Verification", "Email verification and user registration"},
		{"Password Management", "Password reset and recovery"},
		{"User Notifications", "General user notifications"},
		{"Security Alerts", "Security-related notifications"},
		{"System", "System templates and notifications"},
	}

	for _, cat := range categories {
		if _, err := templateService.CreateCategory(cat.name, cat.description, nil); err != nil {
			log.Warn("Failed to create category", "category", cat.name, "error", err)
		}
	}

	if err := templateService.CreateSystemTemplates(); err != nil {
		log.Error("Failed to create system templates", "error", err)
		return err
	}

	log.Info("Template seeding completed")
	return nil
}
