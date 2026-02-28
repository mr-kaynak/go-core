package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/grpc/services"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	emailInfra "github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/listener"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

const (
	identityCleanupTick = 6 * time.Hour
	revokedKeyRetention = 7 * 24 * time.Hour
)

func main() {
	if err := run(); err != nil {
		logger.Get().Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file for local development
	if err := godotenv.Load(); err != nil {
		logger.Get().Warn("Failed to read .env file, using defaults", "error", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// gRPC-specific defaults
	if cfg.App.Name == "go-core" {
		cfg.App.Name = "Go-Core gRPC"
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = "go-core-grpc"
	}
	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = "go-core-grpc"
	}

	// Initialize logger
	log := logger.Get()
	log.Info("Starting gRPC server",
		"version", cfg.App.Version, "env", cfg.App.Env)

	// Initialize metrics
	metrics.InitMetrics("go_core")
	metricsService := metrics.GetMetrics()
	metricsService.SetAppInfo(cfg.App.Version, cfg.App.Env, "grpc")

	// Initialize tracing
	tracingService, err := tracing.NewTracingService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize tracing: %w", err)
	}
	defer func() {
		if shutdownErr := tracingService.Shutdown(context.Background()); shutdownErr != nil {
			log.Error("Failed to shutdown tracing", "error", shutdownErr)
		}
	}()

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Run database migrations
	if migErr := database.RunMigrations(db, "platform/migrations"); migErr != nil {
		return fmt.Errorf("failed to run database migrations: %w", migErr)
	}

	// Initialize repositories
	userRepository := identityRepo.NewUserRepository(db.DB)
	verificationRepo := identityRepo.NewVerificationTokenRepository(db.DB)

	// Initialize email service
	emailSvc, err := emailInfra.NewEmailService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize email service: %w", err)
	}

	// Initialize template service and enhanced email service
	templateRepo := notificationRepository.NewTemplateRepository(db.DB)
	templateService := notificationService.NewTemplateService(templateRepo)
	enhancedEmailService, err := notificationService.NewEnhancedEmailService(
		cfg, templateService,
	)
	if err != nil {
		log.Error("Failed to initialize enhanced email service", "error", err)
		// Continue with fallback to regular email service
		enhancedEmailService = nil
	}

	// Initialize services
	tokenService := identityService.NewTokenService(cfg, userRepository)
	authSvc := identityService.NewAuthService(
		cfg, db.DB, userRepository, tokenService,
		verificationRepo, emailSvc, enhancedEmailService,
	)

	// Create gRPC server
	grpcServer, err := grpc.NewServer(cfg, tracingService)
	if err != nil {
		return fmt.Errorf("failed to create gRPC server: %w", err)
	}

	// Set token validator for gRPC auth interceptors
	grpcServer.SetTokenValidator(tokenService)

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

	// Initialize event dispatcher for streaming
	eventDispatcher := events.NewEventDispatcher(rabbitmqService)

	// Register gRPC services
	authServiceServer := services.NewAuthServiceServer(
		authSvc, userRepository, tokenService, cfg,
	)
	userServiceServer := services.NewUserServiceServer(
		cfg, userRepository, eventDispatcher,
	)

	pb.RegisterAuthServiceServer(grpcServer.GetServer(), authServiceServer)
	pb.RegisterUserServiceServer(grpcServer.GetServer(), userServiceServer)

	// Start identity cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go runIdentityCleanup(cleanupCtx, db, log)

	// Start gRPC server
	if startErr := grpcServer.Start(); startErr != nil {
		cleanupCancel()
		return fmt.Errorf("failed to start gRPC server: %w", startErr)
	}

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit

	log.Info("Shutting down gRPC server...")
	cleanupCancel()
	gracefulShutdown(log, grpcServer, rabbitmqService, outboxListener)

	return nil
}

func gracefulShutdown(
	log *logger.Logger,
	grpcServer *grpc.Server,
	rabbitmqService *rabbitmq.RabbitMQService,
	outboxListener *listener.OutboxListener,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if rabbitmqService != nil {
		if closeErr := rabbitmqService.Close(); closeErr != nil {
			log.Error("Failed to close RabbitMQ connection", "error", closeErr)
		}
	}

	if outboxListener != nil {
		outboxListener.Close()
	}

	shutdownDone := make(chan struct{})
	go func() {
		grpcServer.Stop()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		log.Info("Server shutdown completed")
	case <-ctx.Done():
		log.Error("Server shutdown timed out; forcing stop")
		grpcServer.GetServer().Stop()
	}

	if closeErr := logger.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}
}

// runIdentityCleanup periodically cleans up expired tokens and revoked API keys
func runIdentityCleanup(ctx context.Context, db *database.DB, log *logger.Logger) {
	ticker := time.NewTicker(identityCleanupTick)
	defer ticker.Stop()

	userRepo := identityRepo.NewUserRepository(db.DB)
	verificationRepo := identityRepo.NewVerificationTokenRepository(db.DB)
	apiKeyRepo := identityRepo.NewAPIKeyRepository(db.DB)

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
			if err := apiKeyRepo.CleanupRevokedKeys(revokedKeyRetention); err != nil {
				log.Error("Failed to clean revoked API keys", "error", err)
			}
		}
	}
}
