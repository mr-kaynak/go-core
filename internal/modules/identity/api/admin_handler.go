package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// --- Admin Handler (facade) ---

// AdminHandler handles admin panel HTTP requests.
//
// It is a facade that embeds focused sub-handlers by value:
//   - AdminUserOpsHandler: session, API key, bulk and export user operations
//   - AdminCommsHandler:   email and notification-queue operations
//
// System/dashboard/health handlers are defined directly on AdminHandler (see
// admin_system_handler.go) because they read the facade's own fields and are
// exercised directly in tests via the AdminHandler receiver.
type AdminHandler struct {
	AdminUserOpsHandler
	AdminCommsHandler

	adminService *service.AdminService
	sseService   AdminSSEMonitor
	emailSvc     *email.EmailService
	cfg          *config.Config
	startTime    time.Time
	logger       *logger.Logger
}

// NewAdminHandler creates a new admin handler with all dependencies.
func NewAdminHandler(
	adminService *service.AdminService,
	notificationSvc AdminNotificationProcessor,
	auditService *service.AuditService,
	apiKeyService *service.APIKeyService,
	userService *service.UserService,
	sseService AdminSSEMonitor,
	emailSvc *email.EmailService,
	cfg *config.Config,
) *AdminHandler {
	log := logger.Get().WithField("handler", "admin")
	return &AdminHandler{
		AdminUserOpsHandler: AdminUserOpsHandler{
			adminService:  adminService,
			apiKeyService: apiKeyService,
			userService:   userService,
			auditService:  auditService,
			logger:        log,
		},
		AdminCommsHandler: AdminCommsHandler{
			adminService:    adminService,
			notificationSvc: notificationSvc,
			emailSvc:        emailSvc,
			logger:          log,
		},
		adminService: adminService,
		sseService:   sseService,
		emailSvc:     emailSvc,
		cfg:          cfg,
		startTime:    time.Now(),
		logger:       log,
	}
}

// RegisterRoutes registers all admin handler routes on the admin group.
func (h *AdminHandler) RegisterRoutes(admin fiber.Router) {
	// Dashboard & Health
	admin.Get("/dashboard", h.Dashboard)
	admin.Get("/system/health", h.SystemHealth)

	// API Key Management
	admin.Get("/api-keys", h.ListAllAPIKeys)
	admin.Delete("/api-keys/:id", h.RevokeAPIKey)

	// Session Management
	admin.Get("/sessions", h.ListActiveSessions)
	admin.Delete("/sessions/user/:userId", h.ForceLogoutUser)

	// Email
	admin.Get("/email-logs", h.ListEmailLogs)
	admin.Post("/email/test", h.SendTestEmail)

	// User Export & Bulk Operations
	admin.Get("/users/export", h.ExportUsers)
	admin.Post("/users/bulk-status", h.BulkUpdateStatus)
	admin.Post("/users/bulk-role", h.BulkAssignRole)

	// Notification Stats & Queue Management
	admin.Get("/notifications/stats", h.NotificationStatsHandler)
	admin.Post("/notifications/retry-failed", h.RetryFailedNotifications)
	admin.Post("/notifications/process-pending", h.ProcessPendingNotifications)

	// Audit Log Export
	admin.Get("/audit-logs/export", h.ExportAuditLogs)
}

// fiber:context-methods migrated
