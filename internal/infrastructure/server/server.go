package server

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/google/uuid"
	authzMiddleware "github.com/mr-kaynak/go-core/internal/api/middleware"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/push"
	"github.com/mr-kaynak/go-core/internal/infrastructure/storage"
	"github.com/mr-kaynak/go-core/internal/infrastructure/webhook"
	authMiddleware "github.com/mr-kaynak/go-core/internal/middleware/auth"
	"github.com/mr-kaynak/go-core/internal/modules/identity"
	blogAPI "github.com/mr-kaynak/go-core/internal/modules/blog/api"
	blogDomain "github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	blogRepository "github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	blogService "github.com/mr-kaynak/go-core/internal/modules/blog/service"
	identityAPI "github.com/mr-kaynak/go-core/internal/modules/identity/api"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationAPI "github.com/mr-kaynak/go-core/internal/modules/notification/api"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yokeTH/gofiber-scalar/scalar/v2"
)

// AppServer wraps fiber.App and provides access to components that need
// graceful shutdown (e.g. SSE service, notification workers).
type AppServer struct {
	*fiber.App
	sseService      *notificationService.SSEService
	notificationSvc *notificationService.NotificationService
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

	// Initialize Prometheus metrics
	metricsService := metrics.InitMetrics("go_core")
	metricsService.SetAppInfo(cfg.App.Version, cfg.App.Env, "api")

	// Scalar API docs — registered before middleware so helmet CSP won't block inline JS
	specJSON, _ := os.ReadFile("docs/swagger.json")
	app.Get("/docs/*", scalar.New(scalar.Config{
		Path:              "/docs",
		Title:             "Go-Core API",
		FileContentString: string(specJSON),
	}))

	// Setup middleware
	setupMiddleware(app, cfg, redisClient)

	// Setup routes
	sseService, notifSvc := setupRoutes(app, cfg, db, redisClient, rabbitmqService, casbinSvc)

	// Setup health checks
	setupHealthChecks(app, db, redisClient, rabbitmqService)

	return &AppServer{App: app, sseService: sseService, notificationSvc: notifSvc}, nil
}

// setupMiddleware configures all middleware for the application
func setupMiddleware(app *fiber.App, cfg *config.Config, rc *cache.RedisClient) {
	// Request ID middleware (should be first)
	app.Use(requestid.New())

	// OpenTelemetry tracing middleware
	app.Use(otelfiber.Middleware())

	// Prometheus metrics middleware
	app.Use(metrics.PrometheusMiddleware())

	// Logger middleware
	app.Use(fiberlogger.New(fiberlogger.Config{
		Format: "${time} | ${status} | ${latency} | ${ip} | ${method} | ${path} | ${error}\n",
	}))

	// Recovery middleware with stack trace logging
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
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

	// Compression middleware (skip SSE streaming endpoints)
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
		Next: func(c *fiber.Ctx) bool {
			return c.Get("Accept") == "text/event-stream" ||
				c.Path() == "/api/v1/notifications/stream"
		},
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
		Max:          cfg.RateLimit.PerMinute,
		Expiration:   1 * time.Minute,
		KeyGenerator: rateLimitClientIP,
		LimitReached: func(c *fiber.Ctx) error {
			return errors.NewRateLimitExceeded(cfg.RateLimit.PerMinute)
		},
		Next: func(c *fiber.Ctx) bool {
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

func rateLimitClientIP(c *fiber.Ctx) string {
	if remoteIP := c.Context().RemoteIP(); remoteIP != nil {
		if ip := remoteIP.String(); ip != "" {
			return ip
		}
	}
	return c.IP()
}

// identityModule holds identity module services exposed to other modules.
type identityModule struct {
	tokenService  *service.TokenService
	authService   *service.AuthService
	auditService  *service.AuditService
	userService   *service.UserService
	apiKeyService *service.APIKeyService
	userRepo      repository.UserRepository
	apiKeyRepo    repository.APIKeyRepository
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
	emailSvc, err := email.NewEmailService(cfg)
	if err != nil {
		logger.Get().Error("Failed to initialize email service", "error", err)
	}

	storageSvc, err := storage.NewStorageService(cfg)
	if err != nil {
		logger.Get().Error("Failed to initialize storage service", "error", err)
	} else {
		if cfg.Storage.Type == "local" {
			app.Static("/uploads", cfg.Storage.LocalPath)
		}
		logger.Get().Info("Storage service initialized", "type", cfg.Storage.Type)
	}

	templateSvc, enhancedEmailSvc := identity.WireEnhancedEmail(cfg, db.DB)

	// ── Event Dispatcher ─────────────────────────────────────────────
	eventDispatcher := events.NewEventDispatcher(rabbitmqSvc)

	// ── Identity Module ──────────────────────────────────────────────
	identitySvcs := identity.WireServices(cfg, db.DB, emailSvc, enhancedEmailSvc)
	identitySvcs.SetBlacklist(rc)
	identitySvcs.SetSessionCacheWithTTL(rc, cfg)
	identitySvcs.SetEventPublisher(eventDispatcher)
	identityMod := setupIdentityRoutes(app, api, cfg, db, rc, identitySvcs, casbinSvc, storageSvc)

	// ── Notification Module ──────────────────────────────────────────
	notification := setupNotificationRoutes(app, api, cfg, db, rc, emailSvc, templateSvc, enhancedEmailSvc, identityMod, rabbitmqSvc)

	// ── Email Consumer (RabbitMQ → SMTP) ─────────────────────────────
	if rabbitmqSvc != nil {
		emailConsumer := notificationService.NewEmailConsumerService(cfg, emailSvc, enhancedEmailSvc)
		emailConsumer.SetRabbitMQ(rabbitmqSvc)
		if err := emailConsumer.StartConsumer(); err != nil {
			logger.Get().Error("Failed to start email RabbitMQ consumer", "error", err)
		}
	}

	// ── Cross-module: audit log → SSE broadcast ──────────────────────
	wireAuditSSEBridge(identityMod.auditService, notification.sseService)

	// ── Admin Routes ─────────────────────────────────────────────────
	admin := api.Group("/admin")
	admin.Use(identityMod.authMw)
	admin.Use(authMiddleware.RequireRoles("admin", "system_admin"))
	if casbinSvc != nil {
		admin.Use(authzMiddleware.AuthorizationMiddleware(casbinSvc))
		logger.Get().Info("Casbin authorization middleware enabled for admin routes")
	}
	setupAdminRoutes(admin, cfg, db, rc, emailSvc, identityMod, notification)

	// ── Blog Module ──────────────────────────────────────────────────
	setupBlogRoutes(api, admin, cfg, db, rc, storageSvc, notification.sseService, identityMod.authMw, identityMod.optionalAuthMw, identityMod.userRepo)

	return notification.sseService, notification.notificationSvc
}

// setupIdentityRoutes initializes identity module handlers and routes using
// pre-built core services from the shared identity.WireServices factory.
func setupIdentityRoutes(
	app *fiber.App,
	api fiber.Router,
	cfg *config.Config,
	db *database.DB,
	rc *cache.RedisClient,
	identitySvcs *identity.Services,
	casbinSvc *authorization.CasbinService,
	storageSvc storage.StorageService,
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

	roleHandler := identityAPI.NewRoleHandler(roleService)
	roleHandler.SetAuditService(auditService)

	permissionHandler := identityAPI.NewPermissionHandler(permissionRepo, roleRepo, casbinSvc)
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
	authHandler.RegisterRoutes(api, authMw.Handle)
	roleHandler.RegisterRoutes(app, authMw.Handle)
	permissionHandler.RegisterRoutes(app, authMw.Handle)
	twoFactorHandler.RegisterRoutes(api, authMw.Handle)
	apiKeyHandler.RegisterRoutes(app, authMw.Handle)
	policyHandler.RegisterRoutes(api, authMw.Handle, authMiddleware.RequireRoles("admin", "system_admin"))

	// Storage & uploads
	var uploadHandler *identityAPI.UploadHandler
	if storageSvc != nil {
		uploadHandler = identityAPI.NewUploadHandler(storageSvc, userRepo, cfg.Storage.MaxFileSize)
		uploadHandler.RegisterRoutes(api, authMw.Handle)
	}

	// User service & handler
	userService := identitySvcs.UserService
	if storageSvc != nil {
		userService.SetStorage(storageSvc)
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
	userHandler.RegisterSelfServiceRoutes(api, authMw.Handle)

	return identityModule{
		tokenService:  tokenService,
		authService:   authService,
		auditService:  auditService,
		userService:   userService,
		apiKeyService: apiKeyService,
		userRepo:      userRepo,
		apiKeyRepo:    apiKeyRepo,
		authMw:         authMw.Handle,
		optionalAuthMw: authMw.OptionalHandle,
		userHandler:    userHandler,
	}
}

// setupNotificationRoutes initializes notification module repositories, services, handlers and routes.
func setupNotificationRoutes(
	app *fiber.App,
	api fiber.Router,
	cfg *config.Config,
	db *database.DB,
	rc *cache.RedisClient,
	emailSvc *email.EmailService,
	templateSvc *notificationService.TemplateService,
	enhancedEmailSvc *notificationService.EnhancedEmailService,
	identity identityModule,
	rabbitmqSvc *rabbitmq.RabbitMQService,
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
		if cfg.FCM.ServerKey == "" || cfg.FCM.ProjectID == "" {
			logger.Get().Error("FCM enabled but server_key or project_id not set")
		} else {
			fcmSvc := push.NewFCMService(push.FCMConfig{
				ServerKey: cfg.FCM.ServerKey,
				ProjectID: cfg.FCM.ProjectID,
			})
			notifSvc.SetPushProvider(fcmSvc)
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
	templateHandler.RegisterRoutes(app, identity.authMw)

	notifHandler := notificationAPI.NewNotificationHandler(notifSvc)
	notifHandler.RegisterRoutes(api, identity.authMw)

	if sseHandler != nil {
		sseHandler.RegisterRoutes(api, identity.authMw)
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
		notification.notificationRepo,
		identity.tokenService,
		cfg,
		healthChecker,
	)

	adminHandler := identityAPI.NewAdminHandler(
		adminService,
		notification.notificationSvc,
		identity.auditService,
		identity.apiKeyService,
		identity.userService,
		notification.sseService,
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
) {
	// Repositories
	postRepo := blogRepository.NewPostRepository(db.DB)
	categoryRepo := blogRepository.NewCategoryRepository(db.DB)
	tagRepo := blogRepository.NewTagRepository(db.DB)
	commentRepo := blogRepository.NewCommentRepository(db.DB)
	engagementRepo := blogRepository.NewEngagementRepository(db.DB)

	// Utility services
	contentSvc := blogService.NewContentService()
	slugSvc := blogService.NewSlugService()
	readTimeSvc := blogService.NewReadTimeService(cfg.Blog.ReadTimeWPM)
	seoSvc := blogService.NewSEOService(cfg)
	feedSvc := blogService.NewFeedService(postRepo, cfg)

	// Core services
	postSvc := blogService.NewPostService(db.DB, postRepo, categoryRepo, tagRepo, contentSvc, slugSvc, readTimeSvc)
	postSvc.SetEngagementRepo(engagementRepo)
	if sseSvc != nil {
		postSvc.SetSSEService(sseSvc)
	}

	categorySvc := blogService.NewCategoryService(categoryRepo, slugSvc)
	tagSvc := blogService.NewTagService(tagRepo, slugSvc)

	// Settings service
	settingsRepo := blogRepository.NewSettingsRepository(db.DB)
	settingsSvc := blogService.NewSettingsService(cfg, settingsRepo)
	if rc != nil {
		settingsSvc.SetRedisClient(rc)
	}

	commentSvc := blogService.NewCommentService(cfg, commentRepo, postRepo)
	commentSvc.SetEngagementRepo(engagementRepo)
	commentSvc.SetSettingsService(settingsSvc)
	if sseSvc != nil {
		commentSvc.SetSSEService(sseSvc)
	}

	engagementSvc := blogService.NewEngagementService(db.DB, cfg, engagementRepo, postRepo)
	if sseSvc != nil {
		engagementSvc.SetSSEService(sseSvc)
	}
	if rc != nil {
		engagementSvc.SetRedisClient(rc)
	}

	// Blog API group
	blog := api.Group("/blog")

	// Handlers
	// Build user lookup function for author enrichment
	userLookupFn := blogAPI.UserLookupFunc(func(ctx context.Context, userID uuid.UUID) (*blogDomain.PostAuthor, error) {
		user, err := userRepo.GetByID(userID)
		if err != nil {
			return nil, err
		}
		name := user.Username
		if user.FirstName != "" {
			name = user.FirstName
			if user.LastName != "" {
				name += " " + user.LastName
			}
		}
		return &blogDomain.PostAuthor{
			ID:        user.ID,
			Name:      name,
			AvatarURL: user.AvatarURL,
		}, nil
	})

	postHandler := blogAPI.NewPostHandler(postSvc, cfg.Blog.PostsPerPage)
	postHandler.SetEngagementService(engagementSvc)
	postHandler.SetUserLookup(userLookupFn)
	postHandler.RegisterRoutes(blog, authMw)

	categoryHandler := blogAPI.NewCategoryHandler(categorySvc)
	categoryHandler.RegisterRoutes(blog, authMw)

	tagHandler := blogAPI.NewTagHandler(tagSvc)
	tagHandler.RegisterRoutes(blog)

	commentHandler := blogAPI.NewCommentHandler(commentSvc)
	commentHandler.SetUserLookup(userLookupFn)
	commentHandler.RegisterRoutes(blog, authMw)

	engagementHandler := blogAPI.NewEngagementHandler(engagementSvc)
	engagementHandler.RegisterRoutes(blog, authMw)

	feedHandler := blogAPI.NewFeedHandler(feedSvc)
	feedHandler.RegisterRoutes(blog)

	seoHandler := blogAPI.NewSEOHandler(seoSvc, postSvc)
	seoHandler.RegisterRoutes(blog)

	// Media handler (requires storage service)
	if storageSvc != nil {
		mediaSvc := blogService.NewMediaService(postRepo, storageSvc, cfg)
		mediaHandler := blogAPI.NewMediaHandler(mediaSvc, storageSvc)
		mediaHandler.RegisterRoutes(blog, authMw, optionalAuthMw)
	}

	// Blog admin routes (already under admin group with auth+role middleware)
	blogAdminHandler := blogAPI.NewAdminHandler(postSvc, commentSvc, engagementSvc, settingsSvc, postRepo, cfg.Blog.PostsPerPage)
	blogAdminHandler.RegisterRoutes(admin)

	logger.Get().Info("Blog module initialized")
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
			checks["database"] = fiber.Map{"status": "unhealthy"}
		} else {
			checks["database"] = fiber.Map{"status": "healthy"}
		}

		// Redis health check
		if rc != nil {
			if err := rc.HealthCheck(); err != nil {
				logger.Get().Error("Redis health check failed", "error", err)
				checks["redis"] = fiber.Map{"status": "unhealthy"}
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
				checks["rabbitmq"] = fiber.Map{"status": "unhealthy"}
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

// getAPIStatus godoc
// @Summary      Get API status
// @Description  Returns current API status information including name, version and uptime
// @Tags         Health
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Router       / [get]
func getAPIStatus(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
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
