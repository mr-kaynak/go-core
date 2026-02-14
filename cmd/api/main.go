// @title Go-Core API
// @version 1.0.0
// @description Enterprise-grade Go boilerplate with REST API, gRPC services, and notification system

// @contact.name Go-Core Team
// @contact.url https://github.com/mr-kaynak/go-core

// @license.name MIT

// @host localhost:3000
// @BasePath /api/v1

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description JWT Bearer token. Format: "Bearer {token}"

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/bootstrap"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/listener"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/server"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

const (
	shutdownTimeout            = 30
	identityCleanupInterval    = 6 * time.Hour
	revokedAPIKeyCleanupWindow = 7 * 24 * time.Hour
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		// It's okay if .env doesn't exist, we'll use defaults or env vars
		fmt.Printf("Warning: .env file not found or couldn't be loaded: %v\n", err)
	}

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	if logErr := logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output); logErr != nil {
		fmt.Printf("Failed to initialize logger: %v\n", logErr)
		os.Exit(1)
	}

	// Initialize validation
	validation.Init()

	log := logger.Get()
	log.Info("Starting Go-Core API Server",
		"version", cfg.App.Version,
		"environment", cfg.App.Env,
		"port", cfg.App.Port,
	)

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		log.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}

	// Run database migrations
	if migErr := database.RunMigrations(db, "platform/migrations"); migErr != nil {
		log.Error("Failed to run database migrations", "error", migErr)
		os.Exit(1)
	}

	// Run bootstrap initialization
	if bsErr := runBootstrap(cfg, db, log); bsErr != nil {
		log.Error("Failed to run bootstrap", "error", bsErr)
		os.Exit(1)
	}

	// Initialize OpenTelemetry tracing
	tracingSvc, err := tracing.NewTracingService(cfg)
	if err != nil {
		log.Error("Failed to initialize tracing", "error", err)
	} else {
		log.Info("OpenTelemetry tracing initialized", "endpoint", cfg.OTEL.Endpoint)
	}

	// Initialize Redis (graceful — log error and continue without Redis)
	var redisClient *cache.RedisClient
	rc, redisErr := cache.NewRedisClient(cfg)
	if redisErr != nil {
		log.Warn("Redis not available, running without Redis features", "error", redisErr)
	} else {
		redisClient = rc
	}

	// Initialize outbox listener (LISTEN/NOTIFY)
	outboxListener := listener.NewOutboxListener(cfg.GetDSN())
	outboxListener.Start()

	// Initialize RabbitMQ (graceful — log error and continue without RabbitMQ)
	var rabbitmqService *rabbitmq.RabbitMQService
	outboxRepo := messagingRepo.NewOutboxRepository(db.DB)
	rmqSvc, rmqErr := rabbitmq.NewRabbitMQService(cfg, outboxRepo, outboxListener.SignalCh())
	if rmqErr != nil {
		log.Warn("RabbitMQ not available, running without messaging features", "error", rmqErr)
	} else {
		rabbitmqService = rmqSvc
	}

	// Start identity cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go runIdentityCleanup(cleanupCtx, db, log)

	// Start DB connection pool metrics reporter
	go db.StartConnectionMetrics(cleanupCtx)

	// Create Fiber server
	srv, err := server.New(cfg, db, redisClient, rabbitmqService)
	if err != nil {
		log.Error("Failed to create server", "error", err)
		os.Exit(1)
	}

	// Start server in goroutine
	go func() {
		addr := fmt.Sprintf(":%d", cfg.App.Port)
		log.Info("Server is running", "address", addr)
		if listenErr := srv.Listen(addr); listenErr != nil {
			log.Error("Server failed to start", "error", listenErr)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// Stop identity cleanup goroutine
	cleanupCancel()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout*time.Second)
	defer cancel()

	// Stop SSE service
	srv.StopSSE(ctx)

	// Stop notification workers
	srv.StopNotifications(ctx)

	if shutdownErr := srv.ShutdownWithContext(ctx); shutdownErr != nil {
		log.Error("Server forced to shutdown", "error", shutdownErr)
	}

	// Close RabbitMQ connection
	if rabbitmqService != nil {
		if closeErr := rabbitmqService.Close(); closeErr != nil {
			log.Error("Failed to close RabbitMQ connection", "error", closeErr)
		}
	}

	// Close outbox listener
	if outboxListener != nil {
		outboxListener.Close()
	}

	// Shutdown OpenTelemetry tracing
	if tracingSvc != nil {
		if traceErr := tracingSvc.Shutdown(ctx); traceErr != nil {
			log.Error("Failed to shutdown tracing", "error", traceErr)
		}
	}

	// Close Redis connection
	if redisClient != nil {
		if closeErr := redisClient.Close(); closeErr != nil {
			log.Error("Failed to close Redis connection", "error", closeErr)
		}
	}

	// Close logger file handle
	if closeErr := logger.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}

	log.Info("Server shutdown complete")
}

// runBootstrap initializes the system with default data
func runBootstrap(cfg *config.Config, db *database.DB, log *logger.Logger) error {
	log.Info("Running system bootstrap")

	// Initialize Casbin service
	casbinService, err := authorization.NewCasbinService(cfg, db.DB)
	if err != nil {
		return fmt.Errorf("failed to initialize Casbin service: %w", err)
	}

	// Create repositories
	userRepo := repository.NewUserRepository(db.DB)

	// Create and run bootstrap
	bs := bootstrap.NewBootstrap(db.DB, userRepo, casbinService)
	if err := bs.Run(); err != nil {
		return fmt.Errorf("failed to run bootstrap: %w", err)
	}

	// Initialize notification templates
	log.Info("Initializing system templates")
	templateRepo := notificationRepository.NewTemplateRepository(db.DB)
	templateService := notificationService.NewTemplateService(templateRepo)

	// Create template categories
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
		_, catErr := templateService.CreateCategory(cat.name, cat.description, nil)
		if catErr != nil {
			log.Warn("Failed to create category", "category", cat.name, "error", catErr)
		}
	}

	// Create system templates
	if tplErr := templateService.CreateSystemTemplates(); tplErr != nil {
		log.Error("Failed to create system templates", "error", tplErr)
	}

	log.Info("Bootstrap completed successfully")
	return nil
}

// runIdentityCleanup periodically cleans up expired tokens and revoked API keys
func runIdentityCleanup(ctx context.Context, db *database.DB, log *logger.Logger) {
	ticker := time.NewTicker(identityCleanupInterval)
	defer ticker.Stop()

	userRepo := repository.NewUserRepository(db.DB)
	verificationRepo := repository.NewVerificationTokenRepository(db.DB)
	apiKeyRepo := repository.NewAPIKeyRepository(db.DB)

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
			if err := apiKeyRepo.CleanupRevokedKeys(revokedAPIKeyCleanupWindow); err != nil {
				log.Error("Failed to clean revoked API keys", "error", err)
			}
		}
	}
}
