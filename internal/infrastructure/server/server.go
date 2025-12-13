package server

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	authMiddleware "github.com/mr-kaynak/go-core/internal/middleware/auth"
	identityAPI "github.com/mr-kaynak/go-core/internal/modules/identity/api"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// New creates a new Fiber server with all middleware and routes configured
func New(cfg *config.Config, db *database.DB) (*fiber.App, error) {
	// Create Fiber app with configuration
	app := fiber.New(fiber.Config{
		AppName:               cfg.App.Name,
		ServerHeader:          cfg.App.Name,
		DisableStartupMessage: true,
		ErrorHandler:          errorHandler,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Second,
		IdleTimeout:           120 * time.Second,
		BodyLimit:             4 * 1024 * 1024, // 4MB
	})

	// Setup middleware
	setupMiddleware(app, cfg)

	// Setup routes
	setupRoutes(app, cfg, db)

	// Setup health checks
	setupHealthChecks(app, db)

	return app, nil
}

// setupMiddleware configures all middleware for the application
func setupMiddleware(app *fiber.App, cfg *config.Config) {
	// Request ID middleware (should be first)
	app.Use(requestid.New())

	// Logger middleware
	app.Use(fiberlogger.New(fiberlogger.Config{
		Format: "${time} | ${status} | ${latency} | ${ip} | ${method} | ${path} | ${error}\n",
	}))

	// Recovery middleware
	app.Use(recover.New(recover.Config{
		EnableStackTrace: cfg.IsDevelopment(),
	}))

	// Security headers
	app.Use(helmet.New())

	// CORS middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins:     joinStrings(cfg.CORS.AllowedOrigins, ","),
		AllowMethods:     joinStrings(cfg.CORS.AllowedMethods, ","),
		AllowHeaders:     joinStrings(cfg.CORS.AllowedHeaders, ","),
		AllowCredentials: cfg.CORS.AllowCredentials,
		MaxAge:           86400,
	}))

	// Compression middleware
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))

	// Rate limiting middleware
	app.Use(limiter.New(limiter.Config{
		Max:        cfg.RateLimit.PerMinute,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			// Use IP address as key, but could be customized for authenticated users
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return errors.NewRateLimitExceeded(cfg.RateLimit.PerMinute)
		},
	}))
}

// setupRoutes configures all application routes
func setupRoutes(app *fiber.App, cfg *config.Config, db *database.DB) {
	// API v1 routes
	api := app.Group("/api/v1")

	// Public routes
	api.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name":    cfg.App.Name,
			"version": cfg.App.Version,
			"status":  "running",
			"time":    time.Now().UTC(),
		})
	})

	// Initialize repositories
	userRepo := repository.NewUserRepository(db.DB)
	verificationRepo := repository.NewVerificationTokenRepository(db.DB)

	// Initialize infrastructure services
	emailSvc, err := email.NewEmailService(cfg)
	if err != nil {
		// Log error and continue without email service
		logger.Get().Error("Failed to initialize email service", "error", err)
		// In production, you might want to panic here
	}

	// Initialize services
	tokenService := service.NewTokenService(cfg)
	authService := service.NewAuthService(cfg, userRepo, tokenService, verificationRepo, emailSvc)

	// Initialize handlers
	authHandler := identityAPI.NewAuthHandler(authService)

	// Register auth routes (public)
	authHandler.RegisterRoutes(api)

	// Initialize auth middleware
	authMw := authMiddleware.New(tokenService)

	// Protected routes group
	protected := api.Group("/", authMw.Handle)

	// User profile routes (protected)
	protected.Get("/users/profile", func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		user, err := userRepo.GetByID(claims.UserID)
		if err != nil {
			return errors.NewNotFound("User", claims.UserID.String())
		}

		// Clear sensitive data
		user.Password = ""

		return c.JSON(user)
	})

	// Admin routes (protected with role check)
	admin := api.Group("/admin")
	admin.Use(authMw.Handle)
	admin.Use(authMiddleware.RequireRoles("admin"))

	admin.Get("/users", func(c *fiber.Ctx) error {
		// Get pagination parameters
		page := c.QueryInt("page", 1)
		limit := c.QueryInt("limit", 10)
		offset := (page - 1) * limit

		users, err := userRepo.GetAll(offset, limit)
		if err != nil {
			return errors.NewInternalError("Failed to fetch users")
		}

		count, err := userRepo.Count()
		if err != nil {
			return errors.NewInternalError("Failed to count users")
		}

		// Clear passwords
		for _, user := range users {
			user.Password = ""
		}

		return c.JSON(fiber.Map{
			"users": users,
			"total": count,
			"page":  page,
			"limit": limit,
		})
	})

	// TODO: Add notification module routes
}

// setupHealthChecks configures health check endpoints
func setupHealthChecks(app *fiber.App, db *database.DB) {
	// Liveness probe - simple check if service is alive
	app.Get("/livez", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().UTC(),
		})
	})

	// Readiness probe - check if service is ready to accept requests
	app.Get("/readyz", func(c *fiber.Ctx) error {
		// Check database connection
		if err := db.HealthCheck(); err != nil {
			logger.Get().Error("Database health check failed", "error", err)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "not ready",
				"reason": "database connection failed",
				"error":  err.Error(),
				"time":   time.Now().UTC(),
			})
		}

		// Note: Redis and RabbitMQ health checks can be added here
		// when those services are fully integrated into the server setup.
		// For now, we perform a basic database check as a proxy for overall health.

		return c.JSON(fiber.Map{
			"status": "ready",
			"checks": fiber.Map{
				"database": "ok",
			},
			"time": time.Now().UTC(),
		})
	})

	// Metrics endpoint
	app.Get("/metrics", func(c *fiber.Ctx) error {
		// TODO: Implement Prometheus metrics
		return c.SendString("# Metrics endpoint - TODO: Implement Prometheus metrics")
	})
}

// errorHandler is the global error handler for the application
func errorHandler(c *fiber.Ctx, err error) error {
	// Get request ID for tracing (safely handle nil)
	var requestID string
	if rid := c.Locals("requestid"); rid != nil {
		if id, ok := rid.(string); ok {
			requestID = id
		}
	}

	// Log the error
	log := logger.Get().WithFields(logger.Fields{
		"request_id": requestID,
		"method":     c.Method(),
		"path":       c.Path(),
		"ip":         c.IP(),
	})

	// Check if it's a ProblemDetail error
	if problemDetail := errors.GetProblemDetail(err); problemDetail != nil {
		problemDetail.WithTraceID(requestID)
		problemDetail.WithInstance(c.Path())

		log.WithError(err).Error("Request failed with problem detail")

		return c.Status(problemDetail.Status).JSON(problemDetail)
	}

	// Check if it's a Fiber error
	if fiberErr, ok := err.(*fiber.Error); ok {
		log.WithError(err).Error("Request failed with Fiber error")

		problemDetail := errors.New(
			errors.CodeInternal,
			fiberErr.Code,
			"Request Failed",
			fiberErr.Message,
		).WithTraceID(requestID).WithInstance(c.Path())

		return c.Status(fiberErr.Code).JSON(problemDetail)
	}

	// Unknown error - return internal server error
	log.WithError(err).Error("Request failed with unknown error")

	problemDetail := errors.NewInternalError("An unexpected error occurred").
		WithTraceID(requestID).
		WithInstance(c.Path())

	return c.Status(fiber.StatusInternalServerError).JSON(problemDetail)
}

// joinStrings joins a slice of strings with a delimiter
func joinStrings(strs []string, delimiter string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += delimiter + strs[i]
	}
	return result
}
