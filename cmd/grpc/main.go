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
	"github.com/mr-kaynak/go-core/internal/infrastructure/cleanup"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	emailInfra "github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/listener"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"github.com/mr-kaynak/go-core/internal/modules/identity"
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

	// Initialize logger with config values
	if logErr := logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output); logErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", logErr)
	}
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := tracingService.Shutdown(ctx); shutdownErr != nil {
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

	// Initialize email service
	emailSvc, err := emailInfra.NewEmailService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize email service: %w", err)
	}

	// Initialize identity services using shared factory
	_, enhancedEmailSvc := identity.WireEnhancedEmail(cfg, db.DB)
	identitySvcs := identity.WireServices(cfg, db.DB, emailSvc, enhancedEmailSvc)

	// Create gRPC server
	grpcServer, err := grpc.NewServer(cfg, tracingService)
	if err != nil {
		return fmt.Errorf("failed to create gRPC server: %w", err)
	}

	// Set token validator for gRPC auth interceptors
	grpcServer.SetTokenValidator(identitySvcs.TokenService)

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
		identitySvcs.AuthService, identitySvcs.UserRepo, identitySvcs.TokenService, cfg,
	)
	userServiceServer := services.NewUserServiceServer(
		identitySvcs.UserService, eventDispatcher,
	)

	pb.RegisterAuthServiceServer(grpcServer.GetServer(), authServiceServer)
	pb.RegisterUserServiceServer(grpcServer.GetServer(), userServiceServer)

	// Start identity cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go cleanup.RunIdentityCleanup(cleanupCtx, db.DB, log)

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
	gracefulShutdown(log, grpcServer, rabbitmqService, outboxListener, db)

	return nil
}

func gracefulShutdown(
	log *logger.Logger,
	grpcServer *grpc.Server,
	rabbitmqService *rabbitmq.RabbitMQService,
	outboxListener *listener.OutboxListener,
	db *database.DB,
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

	if db != nil {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Failed to close database connection", "error", closeErr)
		}
	}

	if closeErr := logger.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}
}
