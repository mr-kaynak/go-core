package server

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3/extractors"

	fiberotel "github.com/gofiber/contrib/v3/otel"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	fiberlogger "github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/google/uuid"
	authzMiddleware "github.com/mr-kaynak/go-core/internal/api/middleware"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/captcha"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/push"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/infrastructure/webhook"
	authMiddleware "github.com/mr-kaynak/go-core/internal/middleware/auth"
	blogAPI "github.com/mr-kaynak/go-core/internal/modules/blog/api"
	blogDomain "github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	blogRepository "github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	blogService "github.com/mr-kaynak/go-core/internal/modules/blog/service"
	"github.com/mr-kaynak/go-core/internal/modules/identity"
	identityAPI "github.com/mr-kaynak/go-core/internal/modules/identity/api"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/modules/notification"
	notificationAPI "github.com/mr-kaynak/go-core/internal/modules/notification/api"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"github.com/yokeTH/gofiber-scalar/scalar/v3"
)

// AppServer wraps fiber.App and provides access to components that need
// graceful shutdown (e.g. SSE service, notification workers).
type AppServer struct {
	*fiber.App
	admin           *fiber.App
	adminPort       int
	sseService      *notificationService.SSEService
	notificationSvc *notificationService.NotificationService
}

// ListenAdmin starts the internal admin server for metrics and diagnostics.
func (s *AppServer) ListenAdmin() error {
	return s.admin.Listen(fmt.Sprintf(":%d", s.adminPort), fiber.ListenConfig{DisableStartupMessage: true})
}

// ShutdownAdmin gracefully shuts down the admin server.
func (s *AppServer) ShutdownAdmin() error {
	return s.admin.Shutdown()
}

// StopNotifications waits for in-flight notification workers to finish.
func (s *AppServer) StopNotifications(ctx context.Context) {
	if s.notificationSvc != nil {
		if err := s.notificationSvc.Shutdown(ctx); err != nil {
			logger.Get().Error("Notification workers did not finish in time", "error", err)
		}
	}
}

// StopSSE gracefully shuts down the SSE service if it was started.
func (s *AppServer) StopSSE(ctx context.Context) {
	if s.sseService != nil {
		if err := s.sseService.Stop(ctx); err != nil {
			logger.Get().Error("Failed to stop SSE service", "error", err)
		}
	}
}

// New creates a new Fiber server with all middleware and routes configured.
func New(
	cfg *config.Config,
	db *database.DB,
	redisClient *cache.RedisClient,
	rabbitmqService *rabbitmq.RabbitMQService,
	casbinSvc *authorization.CasbinService,
) (*AppServer, error) {
	// ── Casbin nil guard ─────────────────────────────────────────────
	// In production the normal code path (cmd/api/main.go) already aborts if
	// NewCasbinService fails, so a nil here indicates a misconfigured test
	// harness or an alternative entry point that skipped the authorization
	// setup.  We make the degraded security posture explicitly visible in all
	// environments and treat it as a fatal error in production.
	if casbinSvc == nil {
		logger.Get().Warn("Casbin service is nil — authorization middleware will be disabled; all routes run without RBAC enforcement")
		if cfg.IsProduction() {
			return nil, fmt.Errorf("casbinSvc must not be nil in production: authorization cannot be disabled")
		}
	}

	// Create Fiber app with configuration
	proxyHeader := cfg.App.ProxyHeader
	if proxyHeader == "" {
		proxyHeader = "X-Forwarded-For"
	}
	app := fiber.New(fiber.Config{
		AppName:      cfg.App.Name,
		ServerHeader: "", ErrorHandler: errorHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		BodyLimit:    cfg.App.BodyLimit,
		ProxyHeader:  proxyHeader,
	})

	// Initialize Prometheus metrics (namespace must be [a-zA-Z0-9_])
	metricsNs := strings.ReplaceAll(strings.ReplaceAll(cfg.App.Name, "-", "_"), " ", "_")
	metricsService := metrics.InitMetrics(metricsNs)
	metricsService.SetAppInfo(cfg.App.Version, cfg.App.Env, "api")

	// Scalar API docs — only available in development
	if cfg.IsDevelopment() {
		specJSON, err := os.ReadFile("docs/openapi.json")
		if err != nil {
			logger.Get().Debug("Could not read docs/openapi.json, trying docs/swagger.json", "error", err)
			var swaggerErr error
			specJSON, swaggerErr = os.ReadFile("docs/swagger.json")
			if swaggerErr != nil {
				logger.Get().Warn("No OpenAPI spec found; /docs will serve an empty spec",
					"openapi_error", err, "swagger_error", swaggerErr)
			}
		}
		app.Get("/docs/*", scalar.New(scalar.Config{
			Path:              "/docs",
			Title:             "Go-Core API",
			FileContentString: string(specJSON),
		}))
	}

	// Health checks registered BEFORE middleware so they are not subject
	// to rate limiting, metrics recording, or authentication — Kubernetes
	// probes must always succeed regardless of middleware state.
	setupHealthChecks(app, db, redisClient, rabbitmqService)

	// Setup middleware
	setupMiddleware(app, cfg, redisClient)

	// Setup routes
	sseService, notifSvc := setupRoutes(app, cfg, db, redisClient, rabbitmqService, casbinSvc)

	// Internal admin server for metrics and diagnostics
	admin := fiber.New(fiber.Config{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	})
	setupAdminEndpoints(admin, db, redisClient, rabbitmqService)

	return &AppServer{
		App:             app,
		admin:           admin,
		adminPort:       cfg.Metrics.Port,
		sseService:      sseService,
		notificationSvc: notifSvc,
	}, nil
}

// setupMiddleware configures all middleware for the application
func setupMiddleware(app *fiber.App, cfg *config.Config, rc *cache.RedisClient) {
	// Cache-Control: prevent caching of sensitive API responses
	app.Use(func(c fiber.Ctx) error {
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Set("Pragma", "no-cache")
		return c.Next()
	})

	// Request ID middleware (should be first)
	app.Use(requestid.New())

	// OpenTelemetry tracing middleware
	app.Use(fiberotel.Middleware())

	// Prometheus metrics middleware
	app.Use(metrics.PrometheusMiddleware())

	// Logger middleware
	app.Use(fiberlogger.New(fiberlogger.Config{
		Format: "${time} | ${status} | ${latency} | ${ip} | ${method} | ${path} | ${error}\n",
	}))

	// Recovery middleware with stack trace logging
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c fiber.Ctx, e interface{}) {
			logger.Get().Error("PANIC STACK TRACE",
				"error", e,
				"method", c.Method(),
				"path", c.Path(),
				"stack", string(debug.Stack()),
			)
		},
	}))

	// Security headers
	app.Use(helmet.New())

	// HSTS header for production
	if cfg.IsProduction() {
		app.Use(func(c fiber.Ctx) error {
			c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			return c.Next()
		})
	}

	// CORS middleware
	corsOrigins := cfg.CORS.AllowedOrigins
	if len(corsOrigins) == 0 {
		logger.Get().Warn("CORS allowed_origins is empty; defaulting to localhost only")
		corsOrigins = []string{"http://localhost:3000"}
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     cfg.CORS.AllowedMethods,
		AllowHeaders:     cfg.CORS.AllowedHeaders,
		AllowCredentials: cfg.CORS.AllowCredentials,
		MaxAge:           86400,
	}))

	// Compression middleware (skip SSE streaming endpoints)
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
		Next: func(c fiber.Ctx) bool {
			return c.Get("Accept") == "text/event-stream" ||
				c.Path() == "/api/v1/notifications/stream"
		},
	}))

	// CSRF protection (optional, active when cookie-based auth is used)
	if cfg.GetBool("security.csrf_enabled") {
		app.Use(csrf.New(csrf.Config{
			Extractor:   extractors.FromHeader("X-CSRF-Token"),
			IdleTimeout: 1 * time.Hour,
		}))
	}

	// Rate limiting middleware
	limiterCfg := limiter.Config{
		Max:          cfg.RateLimit.PerMinute,
		Expiration:   1 * time.Minute,
		KeyGenerator: rateLimitClientIP,
		LimitReached: func(c fiber.Ctx) error {
			return errors.NewRateLimitExceeded(cfg.RateLimit.PerMinute)
		},
		Next: func(c fiber.Ctx) bool {
			// Skip rate limiting for long-lived SSE streaming connections
			return c.Path() == "/api/v1/notifications/stream"
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

// rateLimitClientIP returns the client IP for rate limiting.
// When ProxyHeader is configured (e.g. CF-Connecting-IP), Fiber's c.IP()
// automatically extracts the real client IP from that header.
func rateLimitClientIP(c fiber.Ctx) string {
	return c.IP()
}

// identityModule holds identity module services exposed to other modules.
type identityModule struct {
	tokenService   *service.TokenService
	authService    *service.AuthService
	auditService   *service.AuditService
	userService    *service.UserService
	apiKeyService  *service.APIKeyService
	userRepo       repository.UserRepository
	apiKeyRepo     repository.APIKeyRepository
	authMw         fiber.Handler
	optionalAuthMw fiber.Handler
	userHandler    *identityAPI.UserHandler
}

// notificationModule holds notification module services exposed to other modules.
type notificationModule struct {
	sseService       *notificationService.SSEService
	notificationSvc  *notificationService.NotificationService
	notificationRepo notificationRepository.NotificationRepository
}

// setupRoutes configures all application routes and returns the SSE service (if enabled)
// so the caller can shut it down gracefully.
func setupRoutes(
	app *fiber.App, cfg *config.Config, db *database.DB, rc *cache.RedisClient,
	rabbitmqSvc *rabbitmq.RabbitMQService, casbinSvc *authorization.CasbinService,
) (*notificationService.SSEService, *notificationService.NotificationService) {
	api := app.Group("/api/v1")
	api.Get("/", getAPIStatus(cfg))

	// ── Shared Infrastructure ───────────────────────────────────────
	var emailSvc *email.EmailService
	if svc, err := email.NewEmailService(cfg); err != nil {
		logger.Get().Warn("Email service unavailable; email-dependent features will be disabled", "error", err)
	} else {
		emailSvc = svc
	}

	storageSvc, err := storage.NewStorageService(cfg)
	if err != nil {
		logger.Get().Error("Failed to initialize storage service", "error", err)
	} else {
		if cfg.Storage.Type == "local" {
			app.Get("/uploads/*", static.New(cfg.Storage.LocalPath))
		}
		logger.Get().Info("Storage service initialized", "type", cfg.Storage.Type)
	}

	templateSvc, enhancedEmailSvc := notification.WireEnhancedEmail(cfg, db.DB)

	// ── Event Dispatcher ─────────────────────────────────────────────
	eventDispatcher := events.NewEventDispatcher(rabbitmqSvc)

	// ── Casbin Authorization Middleware ──────────────────────────────
	var authzMw fiber.Handler
	if casbinSvc != nil {
		authzMw = authzMiddleware.AuthorizationMiddleware(casbinSvc)
		logger.Get().Info("Casbin authorization middleware enabled")
	}

	// ── Captcha ─────────────────────────────────────────────────────
	var captchaVerifier captcha.Verifier
	if cfg.Captcha.Enabled {
		var err error
		captchaVerifier, err = captcha.NewVerifier(cfg.Captcha)
		if err != nil {
			logger.Get().Error("Failed to initialize captcha verifier", "error", err)
		} else {
			logger.Get().Info("Captcha verification enabled", "provider", cfg.Captcha.Provider)
		}
	}

	// ── Identity Module ──────────────────────────────────────────────
	identitySvcs := identity.WireServices(cfg, db.DB, emailSvc, enhancedEmailSvc)
	identitySvcs.SetBlacklist(rc)
	identitySvcs.SetSessionCacheWithTTL(rc, cfg)
	identitySvcs.SetEventPublisher(eventDispatcher)
	identityMod := setupIdentityRoutes(app, api, cfg, db, rc, identitySvcs, casbinSvc, storageSvc, authzMw, captchaVerifier)

	// ── Notification Module ──────────────────────────────────────────
	notification := setupNotificationRoutes(app, api, cfg, db, rc, emailSvc, templateSvc, enhancedEmailSvc, identityMod, rabbitmqSvc, authzMw)

	// ── Email Consumer (RabbitMQ → SMTP) ─────────────────────────────
	if rabbitmqSvc != nil {
		emailConsumer := notificationService.NewEmailConsumerService(cfg, emailSvc, enhancedEmailSvc)
		emailConsumer.SetRabbitMQ(rabbitmqSvc)
		if err := emailConsumer.StartConsumer(); err != nil {
			logger.Get().Error("Failed to start email RabbitMQ consumer", "error", err)
		}
	}

	// ── Cross-module: user language resolver + notification preference creator ──
	identityMod.authService.SetLanguageResolver(&userLanguageResolverAdapter{notifRepo: notification.notificationRepo})
	identityMod.authService.SetNotificationPreferenceCreator(&notificationPrefCreatorAdapter{notifRepo: notification.notificationRepo})

	// ── Cross-module: audit log → SSE broadcast ──────────────────────
	wireAuditSSEBridge(identityMod.auditService, notification.sseService)

	// ── Admin Routes ─────────────────────────────────────────────────
	admin := api.Group("/admin")
	admin.Use(identityMod.authMw)
	if authzMw != nil {
		admin.Use(authzMw)
	}
	setupAdminRoutes(admin, cfg, db, rc, emailSvc, identityMod, notification)

	// ── Blog Module ──────────────────────────────────────────────────
	setupBlogRoutes(
		api, admin, cfg, db, rc, storageSvc, notification.sseService,
		identityMod.authMw, identityMod.optionalAuthMw, identityMod.userRepo,
		authzMw, captchaVerifier,
	)

	return notification.sseService, notification.notificationSvc
}

// setupIdentityRoutes initializes identity module handlers and routes using
// pre-built core services from the shared identity.WireServices factory.
func setupIdentityRoutes(
	_ *fiber.App,
	api fiber.Router,
	cfg *config.Config,
	db *database.DB,
	rc *cache.RedisClient,
	identitySvcs *identity.Services,
	casbinSvc *authorization.CasbinService,
	storageSvc storage.StorageService,
	authzMw fiber.Handler,
	captchaVerifier captcha.Verifier,
) identityModule {
	// HTTP-specific repositories
	roleRepo := repository.NewRoleRepository(db.DB)
	permissionRepo := repository.NewPermissionRepository(db.DB)
	apiKeyRepo := repository.NewAPIKeyRepository(db.DB)
	auditLogRepo := repository.NewAuditLogRepository(db.DB)

	// References from shared factory
	userRepo := identitySvcs.UserRepo
	tokenService := identitySvcs.TokenService
	authService := identitySvcs.AuthService

	// HTTP-specific services
	roleService := service.NewRoleService(roleRepo, casbinSvc)
	apiKeyService := service.NewAPIKeyService(apiKeyRepo, roleRepo, userRepo)
	auditService := service.NewAuditService(auditLogRepo)

	// Handlers
	authHandler := identityAPI.NewAuthHandler(authService)
	authHandler.SetAuditService(auditService)
	if captchaVerifier != nil {
		authHandler.SetCaptchaVerifier(captchaVerifier)
	}

	roleHandler := identityAPI.NewRoleHandler(roleService)
	roleHandler.SetAuditService(auditService)

	permissionService := service.NewPermissionService(permissionRepo, roleRepo, casbinSvc)
	permissionHandler := identityAPI.NewPermissionHandler(permissionService)
	permissionHandler.SetAuditService(auditService)

	twoFactorHandler := identityAPI.NewTwoFactorHandler(authService)
	twoFactorHandler.SetAuditService(auditService)

	apiKeyHandler := identityAPI.NewAPIKeyHandler(apiKeyService)
	apiKeyHandler.SetAuditService(auditService)

	policyHandler := identityAPI.NewPolicyHandler(casbinSvc)
	policyHandler.SetAuditService(auditService)

	// Auth middleware
	authMw := authMiddleware.New(tokenService, apiKeyService, userRepo)

	// Register routes
	authHandler.RegisterRoutes(api, authMw.Handle, authzMw)
	roleHandler.RegisterRoutes(api, authMw.Handle, authzMw)
	permissionHandler.RegisterRoutes(api, authMw.Handle, authzMw)
	twoFactorHandler.RegisterRoutes(api, authMw.Handle, authzMw)
	apiKeyHandler.RegisterRoutes(api, authMw.Handle, authzMw)
	policyHandler.RegisterRoutes(api, authMw.Handle, authzMw)

	// User service & handler
	userService := identitySvcs.UserService
	if storageSvc != nil {
		userService.SetStorage(storageSvc)
	}

	// Storage & uploads
	var uploadHandler *identityAPI.UploadHandler
	if storageSvc != nil {
		uploadHandler = identityAPI.NewUploadHandler(storageSvc, userService, cfg.Storage.MaxFileSize)
		uploadHandler.RegisterRoutes(api, authMw.Handle, authzMw)
	}

	if rc != nil && storageSvc != nil && cfg.Storage.Type == "s3" {
		presignCache := cache.NewPresignCache(rc, cfg.Storage.S3PresignTTL)
		userService.SetPresignCache(presignCache)
		if uploadHandler != nil {
			uploadHandler.SetPresignCache(presignCache)
		}
		logger.Get().Info("Presigned URL caching enabled (Redis)")
	}

	userHandler := identityAPI.NewUserHandler(userService, authService)
	userHandler.SetAuditService(auditService)
	userHandler.RegisterSelfServiceRoutes(api, authMw.Handle, authzMw)

	return identityModule{
		tokenService:   tokenService,
		authService:    authService,
		auditService:   auditService,
		userService:    userService,
		apiKeyService:  apiKeyService,
		userRepo:       userRepo,
		apiKeyRepo:     apiKeyRepo,
		authMw:         authMw.Handle,
		optionalAuthMw: authMw.OptionalHandle,
		userHandler:    userHandler,
	}
}

// setupNotificationRoutes initializes notification module repositories, services, handlers and routes.
func setupNotificationRoutes(
	_ *fiber.App,
	api fiber.Router,
	cfg *config.Config,
	db *database.DB,
	rc *cache.RedisClient,
	emailSvc *email.EmailService,
	templateSvc *notificationService.TemplateService,
	enhancedEmailSvc *notificationService.EnhancedEmailService,
	identity identityModule,
	rabbitmqSvc *rabbitmq.RabbitMQService,
	authzMw fiber.Handler,
) notificationModule {
	// Repositories
	notifRepo := notificationRepository.NewNotificationRepository(db.DB)

	// Services
	notifSvc := notificationService.NewNotificationService(cfg, notifRepo, emailSvc)

	// Wire enhanced email service for DB template support
	if enhancedEmailSvc != nil {
		notifSvc.SetEnhancedEmailService(enhancedEmailSvc)
	}

	// Wire user email resolver for recipient resolution
	notifSvc.SetUserEmailResolver(&userEmailResolverAdapter{userRepo: identity.userRepo})

	// Wire FCM push provider
	if cfg.FCM.Enabled {
		if cfg.FCM.CredentialsFile == "" || cfg.FCM.ProjectID == "" {
			logger.Get().Error("FCM enabled but credentials_file or project_id not set")
		} else {
			fcmSvc, fcmErr := push.NewFCMService(context.Background(), push.FCMConfig{
				CredentialsFile: cfg.FCM.CredentialsFile,
				ProjectID:       cfg.FCM.ProjectID,
			})
			if fcmErr != nil {
				logger.Get().Error("Failed to initialize FCM service", "error", fcmErr)
			} else {
				notifSvc.SetPushProvider(fcmSvc)
				logger.Get().Info("FCM push provider enabled", "project_id", cfg.FCM.ProjectID)
			}
		}
	}

	// Wire webhook provider
	if cfg.Webhook.Enabled {
		webhookSvc := webhook.NewWebhookService(webhook.WebhookConfig{
			Secret:     cfg.Webhook.Secret,
			Timeout:    cfg.Webhook.Timeout,
			MaxRetries: cfg.Webhook.MaxRetries,
		})
		notifSvc.SetWebhookProvider(webhookSvc)
		logger.Get().Info("Webhook notification provider enabled")
	}

	// SMS provider is pluggable — implement SMSProvider interface and call SetSMSProvider().
	// Example: notifSvc.SetSMSProvider(twilioSvc)

	// Wire RabbitMQ dispatch and start consumer
	if rabbitmqSvc != nil && cfg.Notification.UseRabbitMQ {
		notifSvc.SetRabbitMQ(rabbitmqSvc)
		if err := notifSvc.StartConsumer(); err != nil {
			logger.Get().Error("Failed to start notification RabbitMQ consumer", "error", err)
		}
	}

	// Start background scheduler for pending/retry processing
	notifSvc.StartScheduler()

	// SSE setup — create externally and inject into NotificationService
	var sseSvc *notificationService.SSEService
	var sseHandler *notificationAPI.SSEHandler
	if cfg.GetBool("sse.enabled") {
		svc, err := notificationService.NewSSEService(cfg)
		if err != nil {
			logger.Get().Error("Failed to create SSE service", "error", err)
		} else {
			sseSvc = svc
			if startErr := sseSvc.Start(); startErr != nil {
				logger.Get().Error("Failed to start SSE service", "error", startErr)
			}
			notifSvc.SetSSEService(sseSvc)
			sseHandler = notificationAPI.NewSSEHandler(sseSvc, notifSvc)

			// Wire Redis SSE bridge for cross-instance broadcasting
			if rc != nil && cfg.GetBool("sse.enable_redis") {
				channel := cfg.GetString("sse.redis_channel")
				if channel == "" {
					channel = "notifications:sse"
				}
				bridge := cache.NewSSEBridge(rc, channel, sseSvc.GetServerID())
				sseSvc.SetRedisBridge(bridge)
				logger.Get().Info("SSE Redis bridge enabled", "channel", channel)
			}
		}
	}

	// Handlers & routes
	templateHandler := notificationAPI.NewTemplateHandler(templateSvc)
	templateHandler.RegisterRoutes(api, identity.authMw, authzMw)

	notifHandler := notificationAPI.NewNotificationHandler(notifSvc)
	notifHandler.RegisterRoutes(api, identity.authMw, authzMw)

	if sseHandler != nil {
		sseHandler.RegisterRoutes(api, identity.authMw, authzMw)
	}

	return notificationModule{
		sseService:       sseSvc,
		notificationSvc:  notifSvc,
		notificationRepo: notifRepo,
	}
}

// setupAdminRoutes registers admin-only routes that span multiple modules.
func setupAdminRoutes(
	admin fiber.Router,
	cfg *config.Config,
	db *database.DB,
	rc *cache.RedisClient,
	emailSvc *email.EmailService,
	identity identityModule,
	notification notificationModule,
) {
	identity.userHandler.RegisterAdminRoutes(admin)

	// Build HealthChecker from infrastructure components
	sqlDB, _ := db.DB.DB()
	healthChecker := service.NewHealthChecker(sqlDB, rc)

	adminService := service.NewAdminService(
		identity.userRepo,
		&notificationReaderAdapter{notifRepo: notification.notificationRepo},
		identity.tokenService,
		cfg,
		healthChecker,
	)

	// Pass the concrete services as the admin handler's narrow interfaces.
	// Guard nil concrete pointers so they arrive as nil interfaces — the
	// handler relies on `== nil` checks for optional SSE support.
	var adminNotifProcessor identityAPI.AdminNotificationProcessor
	if notification.notificationSvc != nil {
		adminNotifProcessor = notification.notificationSvc
	}
	var adminSSEMonitor identityAPI.AdminSSEMonitor
	if notification.sseService != nil {
		adminSSEMonitor = notification.sseService
	}

	adminHandler := identityAPI.NewAdminHandler(
		adminService,
		adminNotifProcessor,
		identity.auditService,
		identity.apiKeyService,
		identity.userService,
		adminSSEMonitor,
		emailSvc,
		cfg,
	)
	adminHandler.RegisterRoutes(admin)
}

// wireAuditSSEBridge connects audit log events to SSE broadcasting.
func wireAuditSSEBridge(auditSvc *service.AuditService, sseSvc *notificationService.SSEService) {
	if sseSvc == nil {
		return
	}
	auditSvc.SetOnLogCreated(func(
		id uuid.UUID, userID *uuid.UUID,
		action, resource, resourceID, ipAddress, userAgent string,
		metadata map[string]interface{}, createdAt time.Time,
	) {
		event := notificationDomain.NewSSEAuditLogEvent(notificationDomain.SSEAuditLogData{
			ID:         id,
			UserID:     userID,
			Action:     action,
			Resource:   resource,
			ResourceID: resourceID,
			IPAddress:  ipAddress,
			UserAgent:  userAgent,
			Metadata:   metadata,
			CreatedAt:  createdAt,
		})
		if err := sseSvc.BroadcastToChannel(context.Background(), "admin:audit", event); err != nil {
			logger.Get().Debug("Failed to broadcast audit log via SSE", "error", err)
		}
	})
}

// setupBlogRoutes initializes and wires all blog module components.
func setupBlogRoutes(
	api, admin fiber.Router,
	cfg *config.Config,
	db *database.DB,
	rc *cache.RedisClient,
	storageSvc storage.StorageService,
	sseSvc *notificationService.SSEService,
	authMw fiber.Handler,
	optionalAuthMw fiber.Handler,
	userRepo repository.UserReader,
	authzMw fiber.Handler,
	captchaVerifier captcha.Verifier,
) {
	// Repositories
	postRepo := blogRepository.NewPostRepository(db.DB)
	categoryRepo := blogRepository.NewCategoryRepository(db.DB)
	tagRepo := blogRepository.NewTagRepository(db.DB)
	commentRepo := blogRepository.NewCommentRepository(db.DB)
	engagementRepo := blogRepository.NewEngagementRepository(db.DB)

	// Adapt the notification SSE service to the blog module's broadcaster
	// interface, decoupling blog from the notification module.
	var sseBroadcaster blogService.SSEBroadcaster
	if sseSvc != nil {
		sseBroadcaster = &blogSSEBroadcasterAdapter{sseSvc: sseSvc}
	}

	// Utility services
	contentSvc := blogService.NewContentService()
	slugSvc := blogService.NewSlugService()
	readTimeSvc := blogService.NewReadTimeService(cfg.Blog.ReadTimeWPM)
	seoSvc := blogService.NewSEOService(cfg)
	feedSvc := blogService.NewFeedService(postRepo, cfg)

	// Core services
	postSvc := blogService.NewPostService(db.DB, postRepo, categoryRepo, tagRepo, contentSvc, slugSvc, readTimeSvc)
	postSvc.SetEngagementRepo(engagementRepo)
	if sseBroadcaster != nil {
		postSvc.SetSSEService(sseBroadcaster)
	}

	categorySvc := blogService.NewCategoryService(categoryRepo, slugSvc)
	tagSvc := blogService.NewTagService(tagRepo, slugSvc)

	// Settings service
	settingsRepo := blogRepository.NewSettingsRepository(db.DB)
	settingsSvc := blogService.NewSettingsService(cfg, settingsRepo)
	if rc != nil {
		settingsSvc.SetRedisClient(rc)
	}

	commentSvc := blogService.NewCommentService(cfg, db.DB, commentRepo, postRepo)
	commentSvc.SetEngagementRepo(engagementRepo)
	commentSvc.SetSettingsService(settingsSvc)
	if sseBroadcaster != nil {
		commentSvc.SetSSEService(sseBroadcaster)
	}

	engagementSvc := blogService.NewEngagementService(db.DB, cfg, engagementRepo, postRepo)
	if sseBroadcaster != nil {
		engagementSvc.SetSSEService(sseBroadcaster)
	}
	if rc != nil {
		engagementSvc.SetRedisClient(rc)
	}

	// Blog API group
	blog := api.Group("/blog")

	// Handlers
	// userDisplayName builds the display name from a user's profile fields.
	userDisplayName := func(username, firstName, lastName string) string {
		if firstName == "" {
			return username
		}
		if lastName == "" {
			return firstName
		}
		return firstName + " " + lastName
	}

	// Single-ID lookup for single-post endpoints (GetBySlug, GetForEdit, etc.).
	userLookupFn := blogAPI.UserLookupFunc(func(_ context.Context, userID uuid.UUID) (*blogDomain.PostAuthor, error) {
		user, err := userRepo.GetByID(userID)
		if err != nil {
			return nil, err
		}
		return &blogDomain.PostAuthor{
			ID:        user.ID,
			Name:      userDisplayName(user.Username, user.FirstName, user.LastName),
			AvatarURL: user.AvatarURL,
		}, nil
	})

	// Batch lookup for list endpoints: one WHERE id IN (...) query per page.
	userBatchLookupFn := blogAPI.UserBatchLookupFunc(func(
		_ context.Context, userIDs []uuid.UUID,
	) (map[uuid.UUID]*blogDomain.PostAuthor, error) {
		users, err := userRepo.GetByIDs(userIDs)
		if err != nil {
			return nil, err
		}
		result := make(map[uuid.UUID]*blogDomain.PostAuthor, len(users))
		for _, u := range users {
			result[u.ID] = &blogDomain.PostAuthor{
				ID:        u.ID,
				Name:      userDisplayName(u.Username, u.FirstName, u.LastName),
				AvatarURL: u.AvatarURL,
			}
		}
		return result, nil
	})

	postHandler := blogAPI.NewPostHandler(postSvc, cfg.Blog.PostsPerPage)
	postHandler.SetEngagementService(engagementSvc)
	postHandler.SetUserLookup(userLookupFn)
	postHandler.SetUserBatchLookup(userBatchLookupFn)
	postHandler.RegisterRoutes(blog, authMw, authzMw)

	categoryHandler := blogAPI.NewCategoryHandler(categorySvc)
	categoryHandler.RegisterRoutes(blog, authMw, authzMw)

	tagHandler := blogAPI.NewTagHandler(tagSvc)
	tagHandler.RegisterRoutes(blog)

	commentHandler := blogAPI.NewCommentHandler(commentSvc)
	commentHandler.SetUserLookup(userLookupFn)
	if captchaVerifier != nil {
		commentHandler.SetCaptchaVerifier(captchaVerifier)
	}
	commentHandler.RegisterRoutes(blog, authMw, authzMw)

	engagementHandler := blogAPI.NewEngagementHandler(engagementSvc)
	engagementHandler.RegisterRoutes(blog, authMw, authzMw)

	feedHandler := blogAPI.NewFeedHandler(feedSvc)
	feedHandler.RegisterRoutes(blog)

	seoHandler := blogAPI.NewSEOHandler(seoSvc, postSvc)
	seoHandler.SetUserLookup(userLookupFn)
	seoHandler.RegisterRoutes(blog)

	// Media handler (requires storage service)
	if storageSvc != nil {
		mediaSvc := blogService.NewMediaService(postRepo, storageSvc, cfg)
		mediaHandler := blogAPI.NewMediaHandler(mediaSvc, storageSvc)
		mediaHandler.RegisterRoutes(blog, authMw, optionalAuthMw, authzMw)
	}

	// Blog admin routes (already under admin group with auth+role middleware)
	blogAdminHandler := blogAPI.NewAdminHandler(postSvc, commentSvc, engagementSvc, settingsSvc, postRepo, cfg.Blog.PostsPerPage)
	blogAdminHandler.RegisterRoutes(admin)

	logger.Get().Info("Blog module initialized")
}

// setupHealthChecks configures health check endpoints
func setupHealthChecks(app *fiber.App, db *database.DB, rc *cache.RedisClient, rabbitmqSvc *rabbitmq.RabbitMQService) {
	// Liveness probe - simple check if service is alive
	app.Get("/livez", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().UTC(),
		})
	})

	// Readiness probe - check if service is ready to accept requests
	app.Get("/readyz", func(c fiber.Ctx) error {
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
				logger.Get().Error("RabbitMQ health check failed", "error", err)
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
}

// setupAdminEndpoints configures the internal admin server with metrics and
// health endpoints. This server runs on a separate port that should only be
// accessible within the internal network (e.g. via k8s ClusterIP service).
func setupAdminEndpoints(admin *fiber.App, db *database.DB, _ *cache.RedisClient, _ *rabbitmq.RabbitMQService) {
	metricsHandler := fasthttpadaptor.NewFastHTTPHandler(promhttp.Handler())
	admin.Get("/metrics", func(c fiber.Ctx) error {
		metricsHandler(c.RequestCtx())
		return nil
	})

	admin.Get("/healthz", func(c fiber.Ctx) error {
		if err := db.HealthCheck(); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "unhealthy",
				"time":   time.Now().UTC(),
			})
		}
		return c.JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().UTC(),
		})
	})
}

// errorHandler is the global error handler for the application
func errorHandler(c fiber.Ctx, err error) error {
	// Get request ID for tracing
	requestID := requestid.FromContext(c)

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

// getAPIStatus godoc
// @Summary      Get API status
// @Description  Returns current API status information including name, version and uptime
// @Tags         Health
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/ [get]
func getAPIStatus(cfg *config.Config) fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name":    cfg.App.Name,
			"version": cfg.App.Version,
			"status":  "running",
			"time":    time.Now().UTC(),
		})
	}
}

// userEmailResolverAdapter adapts the identity UserReader to the notification
// service's UserEmailResolver interface, avoiding a direct module dependency.
type userEmailResolverAdapter struct {
	userRepo repository.UserReader
}

func (a *userEmailResolverAdapter) GetEmailByUserID(userID uuid.UUID) (string, error) {
	user, err := a.userRepo.GetByID(userID)
	if err != nil {
		return "", err
	}
	return user.Email, nil
}

// userLanguageResolverAdapter resolves a user's preferred language from
// notification preferences, avoiding a direct identity→notification dependency.
type userLanguageResolverAdapter struct {
	notifRepo notificationRepository.NotificationRepository
}

func (a *userLanguageResolverAdapter) GetLanguageByUserID(userID uuid.UUID) (string, error) {
	pref, err := a.notifRepo.GetUserPreferences(userID)
	if err != nil {
		return "", err
	}
	if pref == nil {
		return "en", nil
	}
	return pref.Language, nil
}

// notificationPrefCreatorAdapter creates initial notification preferences for
// newly registered users, wired via setter to avoid import cycles.
type notificationPrefCreatorAdapter struct {
	notifRepo notificationRepository.NotificationRepository
}

func (a *notificationPrefCreatorAdapter) CreateInitialPreferences(userID uuid.UUID, language string) error {
	if language == "" {
		language = "en"
	}
	return a.notifRepo.CreateUserPreferences(&notificationDomain.NotificationPreference{
		UserID:       userID,
		EmailEnabled: true,
		InAppEnabled: true,
		Language:     language,
	})
}

// notificationReaderAdapter adapts the notification repository to the identity
// admin service's NotificationReader interface, converting email logs to the
// identity module's neutral view type. This keeps identity free of a
// compile-time dependency on the notification module.
type notificationReaderAdapter struct {
	notifRepo notificationRepository.NotificationRepository
}

func (a *notificationReaderAdapter) CountByStatus() (map[string]int64, error) {
	return a.notifRepo.CountByStatus()
}

func (a *notificationReaderAdapter) CountByType() (map[string]int64, error) {
	return a.notifRepo.CountByType()
}

func (a *notificationReaderAdapter) ListEmailLogs(offset, limit int, status string) ([]*service.EmailLogView, int64, error) {
	logs, total, err := a.notifRepo.ListEmailLogs(offset, limit, status)
	if err != nil {
		return nil, 0, err
	}
	views := make([]*service.EmailLogView, 0, len(logs))
	for _, log := range logs {
		views = append(views, toEmailLogView(log))
	}
	return views, total, nil
}

func toEmailLogView(log *notificationDomain.EmailLog) *service.EmailLogView {
	view := &service.EmailLogView{
		ID:             log.ID.String(),
		From:           log.From,
		To:             log.To,
		CC:             log.CC,
		BCC:            log.BCC,
		Subject:        log.Subject,
		Body:           log.Body,
		Template:       log.Template,
		Status:         log.Status,
		SMTPResponse:   log.SMTPResponse,
		MessageID:      log.MessageID,
		Error:          log.Error,
		OpenedAt:       log.OpenedAt,
		ClickedAt:      log.ClickedAt,
		BouncedAt:      log.BouncedAt,
		UnsubscribedAt: log.UnsubscribedAt,
		CreatedAt:      log.CreatedAt,
		UpdatedAt:      log.UpdatedAt,
	}
	if log.NotificationID != nil {
		id := log.NotificationID.String()
		view.NotificationID = &id
	}
	return view
}

// blogSSEBroadcasterAdapter adapts the notification SSE service to the blog
// module's SSEBroadcaster interface, converting blog-local SSE events to the
// notification module's event type without coupling blog to notification.
type blogSSEBroadcasterAdapter struct {
	sseSvc *notificationService.SSEService
}

func (a *blogSSEBroadcasterAdapter) BroadcastToChannel(ctx context.Context, channel string, event *blogDomain.SSEEvent) error {
	return a.sseSvc.BroadcastToChannel(ctx, channel, &notificationDomain.SSEEvent{
		ID:        event.ID,
		Type:      notificationDomain.SSEEventType(event.Type),
		Data:      event.Data,
		Timestamp: event.Timestamp,
	})
}

// fiber:context-methods migrated
