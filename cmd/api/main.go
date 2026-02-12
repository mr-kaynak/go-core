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
	"github.com/mr-kaynak/go-core/internal/infrastructure/server"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
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
	if err := logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
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
	if err := database.RunMigrations(db, "platform/migrations"); err != nil {
		log.Error("Failed to run database migrations", "error", err)
		os.Exit(1)
	}

	// Run bootstrap initialization
	if err := runBootstrap(cfg, db, log); err != nil {
		log.Error("Failed to run bootstrap", "error", err)
		os.Exit(1)
	}

	// Initialize Redis (graceful — log error and continue without Redis)
	var redisClient *cache.RedisClient
	rc, redisErr := cache.NewRedisClient(cfg)
	if redisErr != nil {
		log.Warn("Redis not available, running without Redis features", "error", redisErr)
	} else {
		redisClient = rc
	}

	// Create Fiber server
	srv, err := server.New(cfg, db, redisClient)
	if err != nil {
		log.Error("Failed to create server", "error", err)
		os.Exit(1)
	}

	// Start server in goroutine
	go func() {
		addr := fmt.Sprintf(":%d", cfg.App.Port)
		log.Info("Server is running", "address", addr)
		if err := srv.Listen(addr); err != nil {
			log.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.ShutdownWithContext(ctx); err != nil {
		log.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	// Close Redis connection
	if redisClient != nil {
		if err := redisClient.Close(); err != nil {
			log.Error("Failed to close Redis connection", "error", err)
		}
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
		_, err := templateService.CreateCategory(cat.name, cat.description, nil)
		if err != nil {
			log.Warn("Failed to create category", "category", cat.name, "error", err)
			// Don't fail bootstrap if category creation fails
		}
	}

	// Create system templates
	if err := templateService.CreateSystemTemplates(); err != nil {
		log.Error("Failed to create system templates", "error", err)
		// Don't fail bootstrap if template creation fails
	}

	log.Info("Bootstrap completed successfully")
	return nil
}
