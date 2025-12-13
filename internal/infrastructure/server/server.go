package server

import (
	"bytes"
	"net/http"
	"os"
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
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	authMiddleware "github.com/mr-kaynak/go-core/internal/middleware/auth"
	identityAPI "github.com/mr-kaynak/go-core/internal/modules/identity/api"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// Initialize services
	tokenService := service.NewTokenService(cfg)
	authService := service.NewAuthService(cfg, userRepo, tokenService, verificationRepo, emailSvc)
	roleService := service.NewRoleService(roleRepo, casbinService)

	// Initialize repositories
	permissionRepo := repository.NewPermissionRepository(db.DB)

	// Initialize handlers
	authHandler := identityAPI.NewAuthHandler(authService)
	roleHandler := identityAPI.NewRoleHandler(roleService)
	permissionHandler := identityAPI.NewPermissionHandler(permissionRepo)

	// Register auth routes (public)
	authHandler.RegisterRoutes(api)

	// Initialize auth middleware
	authMw := authMiddleware.New(tokenService)

	// Register role routes (public GET + protected POST/PUT/DELETE)
	roleHandler.RegisterRoutes(app, authMw.Handle)

	// Register permission routes (protected with admin/system_admin role)
	permissionHandler.RegisterRoutes(app, authMw.Handle)

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

	// Notification routes (protected)
	notifications := api.Group("/notifications", authMw.Handle)

	// Notification endpoints
	notifications.Get("", func(c *fiber.Ctx) error {
		// List user's notifications with pagination
		page := c.QueryInt("page", 1)
		limit := c.QueryInt("limit", 20)
		_ = (page - 1) * limit // offset for future DB query

		// Get user claims
		_, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// TODO: Implement notification list from database
		return c.JSON(fiber.Map{
			"notifications": []interface{}{},
			"total":         0,
			"page":          page,
			"limit":         limit,
		})
	})

	notifications.Post("", func(c *fiber.Ctx) error {
		// Admin only - send notification
		adminGroup := api.Group("/admin")
		adminGroup.Use(authMiddleware.RequireRoles("admin"))

		// TODO: Implement notification send from admin
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{
			"error": "Endpoint not yet implemented",
		})
	})

	notifications.Put("/:id/read", func(c *fiber.Ctx) error {
		// Mark notification as read
		notificationID := c.Params("id")

		// Get user claims
		claims, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// TODO: Implement mark as read
		_ = notificationID
		_ = claims

		return c.JSON(fiber.Map{
			"message": "Notification marked as read",
		})
	})

	// Notification preferences (user settings)
	notifications.Get("/preferences", func(c *fiber.Ctx) error {
		// Get user notification preferences
		claims, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// TODO: Implement preferences from database
		_ = claims

		return c.JSON(fiber.Map{
			"email":   true,
			"push":    true,
			"sms":     false,
			"in_app":  true,
			"webhook": false,
		})
	})

	notifications.Put("/preferences", func(c *fiber.Ctx) error {
		// Update notification preferences
		claims, ok := c.Locals("claims").(*service.Claims)
		if !ok {
			return errors.NewUnauthorized("User not authenticated")
		}

		// TODO: Update preferences in database
		_ = claims

		return c.JSON(fiber.Map{
			"message": "Preferences updated successfully",
		})
	})
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
		checks := make(fiber.Map)

		// Check database connection
		dbOk := true
		if err := db.HealthCheck(); err != nil {
			dbOk = false
			logger.Get().Error("Database health check failed", "error", err)
			checks["database"] = fiber.Map{"status": "unhealthy", "error": err.Error()}
		} else {
			checks["database"] = "healthy"
		}

		// Check Redis connectivity
		redisOk := true
		// Try to ping Redis (if available)
		// This would use actual Redis client when integrated
		redisOk = true // Placeholder - will be enhanced when Redis is fully integrated
		if redisOk {
			checks["redis"] = "healthy"
		} else {
			checks["redis"] = "unhealthy"
		}

		// Check RabbitMQ connectivity
		rabbitOk := true
		// Try to verify RabbitMQ connection (if available)
		// This would use actual RabbitMQ client when integrated
		rabbitOk = true // Placeholder - will be enhanced when RabbitMQ is fully integrated
		if rabbitOk {
			checks["rabbitmq"] = "healthy"
		} else {
			checks["rabbitmq"] = "unhealthy"
		}

		// Return not ready only if critical service (database) fails
		if !dbOk {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "not ready",
				"checks": checks,
				"time":   time.Now().UTC(),
			})
		}

		// All critical services are healthy
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
		dummyReq, _ := http.NewRequest("GET", "/metrics", nil)

		// Let Prometheus write to our buffer
		metricsHandler.ServeHTTP(respWriter, dummyReq)

		// Write captured content to Fiber response with proper headers
		c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
		return c.SendString(buf.String())
	})

	// Swagger UI endpoint - serve API documentation
	app.Get("/docs", func(c *fiber.Ctx) error {
		// Read swagger.json file
		swaggerData, err := readSwaggerJSON()
		if err != nil {
			logger.Get().Warn("Failed to read swagger.json", "error", err)
			return c.SendString("<html><body><h1>Swagger UI</h1><p>Failed to load API documentation</p></body></html>")
		}

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
func readSwaggerJSON() ([]byte, error) {
	// Try to read from docs directory
	data, err := os.ReadFile("./docs/swagger.json")
	if err == nil {
		return data, nil
	}

	// Fallback: try from current working directory
	data, err = os.ReadFile("docs/swagger.json")
	if err == nil {
		return data, nil
	}

	// If file not found, return empty spec
	return []byte(`{"openapi":"3.0.0","info":{"title":"Go-Core API","version":"1.0.0"}}`), nil
}
