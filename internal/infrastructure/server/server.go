package server

import (
	"bytes"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	authzMiddleware "github.com/mr-kaynak/go-core/internal/api/middleware"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	"github.com/mr-kaynak/go-core/internal/infrastructure/push"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/infrastructure/webhook"
	authMiddleware "github.com/mr-kaynak/go-core/internal/middleware/auth"
	identityAPI "github.com/mr-kaynak/go-core/internal/modules/identity/api"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationAPI "github.com/mr-kaynak/go-core/internal/modules/notification/api"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// New creates a new Fiber server with all middleware and routes configured.
func New(
	cfg *config.Config,
	db *database.DB,
	redisClient *cache.RedisClient,
	rabbitmqService *rabbitmq.RabbitMQService,
) (*fiber.App, error) {
	// Create Fiber app with configuration
	app := fiber.New(fiber.Config{
		AppName:               cfg.App.Name,
		ServerHeader:          "",
		DisableStartupMessage: true,
		ErrorHandler:          errorHandler,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Second,
		IdleTimeout:           120 * time.Second,
		BodyLimit:             4 * 1024 * 1024, // 4MB
	})

	// Setup middleware
	setupMiddleware(app, cfg, redisClient)

	// Setup routes
	setupRoutes(app, cfg, db, redisClient)

	// Setup health checks
	setupHealthChecks(app, db, redisClient, rabbitmqService)

	return app, nil
}

// setupMiddleware configures all middleware for the application
func setupMiddleware(app *fiber.App, cfg *config.Config, rc *cache.RedisClient) {
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

	// HSTS header for production
	if cfg.IsProduction() {
		app.Use(func(c *fiber.Ctx) error {
			c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			return c.Next()
		})
	}

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

	// CSRF protection (optional, active when cookie-based auth is used)
	if cfg.GetBool("security.csrf_enabled") {
		app.Use(csrf.New(csrf.Config{
			KeyLookup:  "header:X-CSRF-Token",
			Expiration: 1 * time.Hour,
		}))
	}

	// Rate limiting middleware
	limiterCfg := limiter.Config{
		Max:        cfg.RateLimit.PerMinute,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return errors.NewRateLimitExceeded(cfg.RateLimit.PerMinute)
		},
	}

	// Use Redis-backed storage for distributed rate limiting when available
	if rc != nil {
		rateLimiter := cache.NewRateLimiter(rc)
		limiterCfg.Storage = rateLimiter.FiberStorage()
		logger.Get().Info("Rate limiter using Redis storage")
	}

	app.Use(limiter.New(limiterCfg))
}

// setupRoutes configures all application routes
//
//nolint:gocyclo // route setup requires many route definitions
func setupRoutes(app *fiber.App, cfg *config.Config, db *database.DB, rc *cache.RedisClient) {
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
	roleRepo := repository.NewRoleRepository(db.DB)

	// Initialize infrastructure services
	emailSvc, err := email.NewEmailService(cfg)
	if err != nil {
		// Log error and continue without email service
		logger.Get().Error("Failed to initialize email service", "error", err)
		// In production, you might want to panic here
	}

	// Initialize infrastructure services
	casbinService, err := authorization.NewCasbinService(cfg, db.DB)
	if err != nil {
		logger.Get().Error("Failed to initialize Casbin service", "error", err)
		// Continue without Casbin, but features requiring it will fail
	}

	// Initialize notification repositories and services
	templateRepo := notificationRepository.NewTemplateRepository(db.DB)
	templateService := notificationService.NewTemplateService(templateRepo)
	enhancedEmailService, err := notificationService.NewEnhancedEmailService(cfg, templateService)
	if err != nil {
		logger.Get().Error("Failed to initialize enhanced email service", "error", err)
		// Continue without enhanced email service, will fallback to basic email service
	}

	// Initialize services
	tokenService := service.NewTokenService(cfg, userRepo)

	// Wire token blacklist if Redis is available
	if rc != nil {
		blacklist := cache.NewTokenBlacklist(rc)
		tokenService.SetBlacklist(blacklist)
		logger.Get().Info("Token blacklist enabled (Redis)")
	}

	authService := service.NewAuthService(cfg, userRepo, tokenService, verificationRepo, emailSvc, enhancedEmailService)

	// Wire session cache if Redis is available
	if rc != nil {
		sessionCache := cache.NewSessionCache(rc, cfg.JWT.Expiry)
		authService.SetSessionCache(sessionCache)
		logger.Get().Info("Session cache enabled (Redis)")
	}

	roleService := service.NewRoleService(roleRepo, casbinService)

	// Initialize repositories
	permissionRepo := repository.NewPermissionRepository(db.DB)

	// Initialize API key and audit repositories
	apiKeyRepo := repository.NewAPIKeyRepository(db.DB)
	auditLogRepo := repository.NewAuditLogRepository(db.DB)

	// Initialize API key and audit services
	apiKeyService := service.NewAPIKeyService(apiKeyRepo)
	auditService := service.NewAuditService(auditLogRepo)

	// Initialize handlers
	authHandler := identityAPI.NewAuthHandler(authService)
	authHandler.SetAuditService(auditService)

	roleHandler := identityAPI.NewRoleHandler(roleService)
	permissionHandler := identityAPI.NewPermissionHandler(permissionRepo)
	templateHandler := notificationAPI.NewTemplateHandler(templateService)

	twoFactorHandler := identityAPI.NewTwoFactorHandler(authService)
	twoFactorHandler.SetAuditService(auditService)

	apiKeyHandler := identityAPI.NewAPIKeyHandler(apiKeyService)
	apiKeyHandler.SetAuditService(auditService)

	policyHandler := identityAPI.NewPolicyHandler(casbinService)

	// Initialize notification service and SSE handler
	var sseHandler *notificationAPI.SSEHandler
	notificationRepo := notificationRepository.NewNotificationRepository(db.DB)
	notificationSvc := notificationService.NewNotificationService(cfg, notificationRepo, emailSvc)

	// Wire FCM push provider
	if cfg.FCM.Enabled {
		if cfg.FCM.ServerKey == "" || cfg.FCM.ProjectID == "" {
			logger.Get().Error("FCM enabled but server_key or project_id not set")
		} else {
			fcmSvc := push.NewFCMService(push.FCMConfig{
				ServerKey: cfg.FCM.ServerKey,
				ProjectID: cfg.FCM.ProjectID,
			})
			notificationSvc.SetPushProvider(fcmSvc)
			logger.Get().Info("FCM push provider enabled", "project_id", cfg.FCM.ProjectID)
		}
	}

	// Wire webhook provider
	if cfg.Webhook.Enabled {
		webhookSvc := webhook.NewWebhookService(webhook.WebhookConfig{
			Secret:     cfg.Webhook.Secret,
			Timeout:    cfg.Webhook.Timeout,
			MaxRetries: cfg.Webhook.MaxRetries,
		})
		notificationSvc.SetWebhookProvider(webhookSvc)
		logger.Get().Info("Webhook notification provider enabled")
	}

	// SMS provider is pluggable — implement SMSProvider interface and call SetSMSProvider().
	// Example: notificationSvc.SetSMSProvider(twilioSvc)

	if sseService := notificationSvc.GetSSEService(); sseService != nil {
		sseHandler = notificationAPI.NewSSEHandler(sseService, notificationSvc)

		// Wire Redis SSE bridge for cross-instance broadcasting
		if rc != nil && cfg.GetBool("sse.enable_redis") {
			channel := cfg.GetString("sse.redis_channel")
			if channel == "" {
				channel = "notifications:sse"
			}
			bridge := cache.NewSSEBridge(rc, channel, sseService.GetServerID())
			sseService.SetRedisBridge(bridge)
			logger.Get().Info("SSE Redis bridge enabled", "channel", channel)
		}
	}

	// Initialize auth middleware
	authMw := authMiddleware.New(tokenService)

	// Register auth routes (public + protected logout)
	authHandler.RegisterRoutes(api, authMw.Handle)

	// Register role routes (public GET + protected POST/PUT/DELETE)
	roleHandler.RegisterRoutes(app, authMw.Handle)

	// Register permission routes (protected with admin/system_admin role)
	permissionHandler.RegisterRoutes(app, authMw.Handle)

	// Register template routes (protected with auth middleware)
	templateHandler.RegisterRoutes(app, authMw.Handle)

	// Register 2FA routes (protected with auth middleware)
	twoFactorHandler.RegisterRoutes(api, authMw.Handle)

	// Register API key routes (protected with auth middleware)
	apiKeyHandler.RegisterRoutes(app, authMw.Handle)

	// Register policy routes (admin only — requires auth + admin role)
	policyHandler.RegisterRoutes(api, authMw.Handle, authMiddleware.RequireRoles("admin"))

	// Register SSE routes if SSE is enabled (protected with auth middleware)
	if sseHandler != nil {
		sseHandler.RegisterRoutes(api, authMw.Handle)
	}

	// Initialize storage service (graceful — nil if init fails)
	storageSvc, err := storage.NewStorageService(cfg)
	if err != nil {
		logger.Get().Error("Failed to initialize storage service", "error", err)
	} else {
		uploadHandler := identityAPI.NewUploadHandler(storageSvc, userRepo, cfg.Storage.MaxFileSize)
		uploadHandler.RegisterRoutes(api, authMw.Handle)

		// Serve local uploads as static files
		if cfg.Storage.Type == "local" {
			app.Static("/uploads", cfg.Storage.LocalPath)
		}
		logger.Get().Info("Storage service initialized", "type", cfg.Storage.Type)
	}

	// User profile routes (protected)
	api.Get("/users/profile", authMw.Handle, func(c *fiber.Ctx) error {
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

	// Admin routes (protected with role check + Casbin authorization)
	admin := api.Group("/admin")
	admin.Use(authMw.Handle)
	admin.Use(authMiddleware.RequireRoles("admin"))

	// Apply Casbin-based authorization enforcement on admin routes
	if casbinService != nil {
		admin.Use(authzMiddleware.AuthorizationMiddleware(casbinService))
		logger.Get().Info("Casbin authorization middleware enabled for admin routes")
	}

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

	// Register notification routes (protected with auth middleware)
	notificationHandler := notificationAPI.NewNotificationHandler(notificationSvc)
	notificationHandler.RegisterRoutes(api, authMw.Handle)
}

// setupHealthChecks configures health check endpoints
func setupHealthChecks(app *fiber.App, db *database.DB, rc *cache.RedisClient, rabbitmqSvc *rabbitmq.RabbitMQService) {
	// Liveness probe - simple check if service is alive
	app.Get("/livez", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().UTC(),
		})
	})

	// Readiness probe - check if service is ready to accept requests
	app.Get("/readyz", func(c *fiber.Ctx) error {
		checks := make(fiber.Map)

		// Check database connection (critical)
		dbOk := true
		if err := db.HealthCheck(); err != nil {
			dbOk = false
			logger.Get().Error("Database health check failed", "error", err)
			checks["database"] = fiber.Map{"status": "unhealthy", "error": err.Error()}
		} else {
			checks["database"] = fiber.Map{"status": "healthy"}
		}

		// Redis health check
		if rc != nil {
			if err := rc.HealthCheck(); err != nil {
				logger.Get().Error("Redis health check failed", "error", err)
				checks["redis"] = fiber.Map{"status": "unhealthy", "error": err.Error()}
			} else {
				checks["redis"] = fiber.Map{"status": "healthy"}
			}
		} else {
			checks["redis"] = fiber.Map{"status": "not_configured"}
		}

		// RabbitMQ health check
		if rabbitmqSvc != nil {
			if err := rabbitmqSvc.HealthCheck(); err != nil {
				checks["rabbitmq"] = fiber.Map{"status": "unhealthy", "error": err.Error()}
			} else {
				checks["rabbitmq"] = fiber.Map{"status": "healthy"}
			}
		} else {
			checks["rabbitmq"] = fiber.Map{"status": "not_configured"}
		}

		// Return not ready only if critical service (database) fails
		if !dbOk {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "not ready",
				"checks": checks,
				"time":   time.Now().UTC(),
			})
		}

		return c.JSON(fiber.Map{
			"status": "ready",
			"checks": checks,
			"time":   time.Now().UTC(),
		})
	})

	// Metrics endpoint - expose Prometheus metrics
	app.Get("/metrics", func(c *fiber.Ctx) error {
		// Create a buffer to capture Prometheus metrics output
		buf := &bytes.Buffer{}
		metricsHandler := promhttp.Handler()

		// Create a custom response writer to capture output
		respWriter := &metricsResponseWriter{
			header: make(http.Header),
			body:   buf,
		}

		// Create a dummy HTTP request for promhttp
		//nolint:noctx // dummy request for promhttp handler, no real HTTP context needed
		dummyReq, _ := http.NewRequest(http.MethodGet, "/metrics", http.NoBody)

		// Let Prometheus write to our buffer
		metricsHandler.ServeHTTP(respWriter, dummyReq)

		// Write captured content to Fiber response with proper headers
		c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
		return c.SendString(buf.String())
	})

	// Swagger UI endpoint - serve API documentation
	app.Get("/docs", func(c *fiber.Ctx) error {
		// Read swagger.json file
		swaggerData := readSwaggerJSON()

		// Serve Swagger UI HTML
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Go-Core API Documentation</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@3/swagger-ui.css">
    <style>
        html {
            box-sizing: border-box;
            overflow: -moz-scrollbars-vertical;
            overflow-y: scroll;
        }
        *, *:before, *:after {
            box-sizing: inherit;
        }
        body {
            margin: 0;
            padding: 0;
        }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@3/swagger-ui-bundle.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@3/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            window.ui = SwaggerUIBundle({
                spec: ` + string(swaggerData) + `,
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            })
        }
    </script>
</body>
</html>`

		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		return c.SendString(html)
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
		_ = problemDetail.WithTraceID(requestID)
		_ = problemDetail.WithInstance(c.Path())

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

// metricsResponseWriter implements http.ResponseWriter to capture Prometheus output
type metricsResponseWriter struct {
	header http.Header
	body   *bytes.Buffer
	status int
}

// Header returns the header map
func (mw *metricsResponseWriter) Header() http.Header {
	return mw.header
}

// Write writes data to the response
func (mw *metricsResponseWriter) Write(p []byte) (int, error) {
	if mw.status == 0 {
		mw.status = http.StatusOK
	}
	return mw.body.Write(p)
}

// WriteHeader writes the status code
func (mw *metricsResponseWriter) WriteHeader(statusCode int) {
	mw.status = statusCode
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

// readSwaggerJSON reads the swagger.json file from docs directory
func readSwaggerJSON() []byte {
	// Try to read from docs directory
	data, err := os.ReadFile("./docs/swagger.json")
	if err == nil {
		return data
	}

	// Fallback: try from current working directory
	data, err = os.ReadFile("docs/swagger.json")
	if err == nil {
		return data
	}

	// If file not found, return empty spec
	return []byte(`{"openapi":"3.0.0","info":{"title":"Go-Core API","version":"1.0.0"}}`)
}
