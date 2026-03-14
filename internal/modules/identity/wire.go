package identity

import (
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"gorm.io/gorm"
)

// Services holds all identity module service references.
type Services struct {
	TokenService *service.TokenService
	AuthService  *service.AuthService
	UserService  *service.UserService
	UserRepo     repository.UserRepository
}

// WireServices constructs the core identity service graph.
// This is the single source of truth for identity service wiring,
// shared between HTTP (server.go) and gRPC (cmd/grpc/main.go) entry points.
func WireServices(
	cfg *config.Config,
	db *gorm.DB,
	emailSvc *email.EmailService,
	enhancedEmailSvc *notificationService.EnhancedEmailService,
) *Services {
	// Repositories
	userRepo := repository.NewUserRepository(db)
	verificationRepo := repository.NewVerificationTokenRepository(db)

	// Token service
	tokenService := service.NewTokenService(cfg, userRepo)

	// Auth service
	authService := service.NewAuthService(cfg, db, userRepo, tokenService, verificationRepo, emailSvc, enhancedEmailSvc)

	// User service
	userService := service.NewUserService(cfg, db, userRepo, authService, tokenService)

	return &Services{
		TokenService: tokenService,
		AuthService:  authService,
		UserService:  userService,
		UserRepo:     userRepo,
	}
}

// SetBlacklist wires the token blacklist (Redis) into the token service.
func (s *Services) SetBlacklist(rc *cache.RedisClient) {
	if rc == nil {
		return
	}
	blacklist := cache.NewTokenBlacklist(rc)
	s.TokenService.SetBlacklist(blacklist)
	logger.Get().Info("Token blacklist enabled (Redis)")
}

// SetSessionCache wires the session cache (Redis) into the auth service.
func (s *Services) SetSessionCache(rc *cache.RedisClient, expiry interface{ GetDuration() interface{} }) {
	// This is intentionally a no-op stub. The caller wires session cache
	// directly since the TTL type varies. See SetSessionCacheFromConfig.
}

// SetSessionCacheWithTTL wires the session cache with explicit TTL.
func (s *Services) SetSessionCacheWithTTL(rc *cache.RedisClient, cfg *config.Config) {
	if rc == nil {
		return
	}
	sessionCache := cache.NewSessionCache(rc, cfg.JWT.Expiry)
	s.AuthService.SetSessionCache(sessionCache)
	logger.Get().Info("Session cache enabled (Redis)")
}

// SetEventPublisher wires the event publisher (RabbitMQ) into the auth service
// for dispatching email events asynchronously.
func (s *Services) SetEventPublisher(ep service.EventPublisher) {
	if ep == nil {
		return
	}
	s.AuthService.SetEventPublisher(ep)
	logger.Get().Info("Event publisher enabled for auth service")
}

// WireEnhancedEmail creates the template service and enhanced email service
// from shared infrastructure. Returns nil enhanced email service on error.
func WireEnhancedEmail(cfg *config.Config, db *gorm.DB) (*notificationService.TemplateService, *notificationService.EnhancedEmailService) {
	templateRepo := notificationRepository.NewTemplateRepository(db)
	templateSvc := notificationService.NewTemplateService(templateRepo)
	enhancedEmailSvc, err := notificationService.NewEnhancedEmailService(cfg, templateSvc)
	if err != nil {
		logger.Get().Error("Failed to initialize enhanced email service", "error", err)
		return templateSvc, nil
	}
	return templateSvc, enhancedEmailSvc
}
