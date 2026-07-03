package notification

import (
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"gorm.io/gorm"
)

// WireEnhancedEmail creates the template service and enhanced email service
// from shared infrastructure. Returns nil enhanced email service on error.
func WireEnhancedEmail(cfg *config.Config, db *gorm.DB) (*service.TemplateService, *service.EnhancedEmailService) {
	templateRepo := repository.NewTemplateRepository(db)
	templateSvc := service.NewTemplateService(templateRepo)
	enhancedEmailSvc, err := service.NewEnhancedEmailService(cfg, templateSvc)
	if err != nil {
		logger.Get().Error("Failed to initialize enhanced email service", "error", err)
		return templateSvc, nil
	}
	return templateSvc, enhancedEmailSvc
}
