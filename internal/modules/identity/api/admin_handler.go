package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/mail"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

// --- Admin Response DTOs ---

// DashboardResponse is the response for the admin dashboard endpoint.
type DashboardResponse struct {
	Users         UserStats         `json:"users"`
	Notifications NotificationStats `json:"notifications"`
	SSE           interface{}       `json:"sse"`
	System        SystemInfo        `json:"system"`
}

// UserStats holds user statistics for the dashboard.
type UserStats struct {
	Total              int64  `json:"total"`
	Active             int64  `json:"active"`
	Inactive           int64  `json:"inactive"`
	Locked             int64  `json:"locked"`
	TodayRegistrations int64  `json:"today_registrations"`
	Error              string `json:"error,omitempty"`
}

// NotificationStats holds notification statistics for the dashboard.
type NotificationStats struct {
	Pending int64  `json:"pending"`
	Sent    int64  `json:"sent"`
	Failed  int64  `json:"failed"`
	Error   string `json:"error,omitempty"`
}

// SystemInfo holds system information for the dashboard.
type SystemInfo struct {
	Uptime  string `json:"uptime"`
	Version string `json:"version"`
}

// SystemHealthResponse is the response for the system health endpoint.
type SystemHealthResponse struct {
	OverallStatus string                     `json:"overall_status"` // healthy, degraded, unhealthy
	Components    map[string]ComponentHealth `json:"components"`
}

// ComponentHealth represents the health status of a single component.
type ComponentHealth struct {
	Status  string      `json:"status"` // healthy, unhealthy
	Details interface{} `json:"details,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// APIKeySafeResponse is a safe representation of an API key that excludes the hash.
type APIKeySafeResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	UserID     uuid.UUID  `json:"user_id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	IsRevoked  bool       `json:"is_revoked"`
}
// SessionSafeResponse is a safe representation of a session that excludes the refresh token value.
type SessionSafeResponse struct {
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
}

// toSessionSafeResponse converts a domain RefreshToken to a safe response without the token value.
func toSessionSafeResponse(token *domain.RefreshToken) SessionSafeResponse {
	return SessionSafeResponse{
		SessionID: token.ID,
		UserID:    token.UserID,
		CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt,
		IPAddress: token.IPAddress,
		UserAgent: token.UserAgent,
	}
}

// toAPIKeySafeResponse converts a domain APIKey to a safe response without the hash.
func toAPIKeySafeResponse(key *domain.APIKey) APIKeySafeResponse {
	return APIKeySafeResponse{
		ID:         key.ID,
		Name:       key.Name,
		UserID:     key.UserID,
		CreatedAt:  key.CreatedAt,
		LastUsedAt: key.LastUsedAt,
		IsRevoked:  key.Revoked,
	}
}

// --- Admin Handler ---

// AdminHandler handles admin panel HTTP requests.
type AdminHandler struct {
	userRepo         repository.UserRepository
	notificationRepo notificationRepository.NotificationRepository
	notificationSvc  *notificationService.NotificationService
	templateSvc      *notificationService.TemplateService
	auditService     *service.AuditService
	apiKeyService    *service.APIKeyService
	apiKeyRepo       repository.APIKeyRepository
	userService      *service.UserService
	sseService       *notificationService.SSEService
	emailSvc         *email.EmailService
	db               *database.DB
	redisClient      *cache.RedisClient
	cfg              *config.Config
	startTime        time.Time
	logger           *logger.Logger
}

// NewAdminHandler creates a new admin handler with all dependencies.
func NewAdminHandler(
	userRepo repository.UserRepository,
	notificationRepo notificationRepository.NotificationRepository,
	notificationSvc *notificationService.NotificationService,
	templateSvc *notificationService.TemplateService,
	auditService *service.AuditService,
	apiKeyService *service.APIKeyService,
	apiKeyRepo repository.APIKeyRepository,
	userService *service.UserService,
	sseService *notificationService.SSEService,
	emailSvc *email.EmailService,
	db *database.DB,
	redisClient *cache.RedisClient,
	cfg *config.Config,
) *AdminHandler {
	return &AdminHandler{
		userRepo:         userRepo,
		notificationRepo: notificationRepo,
		notificationSvc:  notificationSvc,
		templateSvc:      templateSvc,
		auditService:     auditService,
		apiKeyService:    apiKeyService,
		apiKeyRepo:       apiKeyRepo,
		userService:      userService,
		sseService:       sseService,
		emailSvc:         emailSvc,
		db:               db,
		redisClient:      redisClient,
		cfg:              cfg,
		startTime:        time.Now(),
		logger:           logger.Get().WithField("handler", "admin"),
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
	admin.Get("/notifications/stats", h.NotificationStats)
	admin.Post("/notifications/retry-failed", h.RetryFailedNotifications)
	admin.Post("/notifications/process-pending", h.ProcessPendingNotifications)

	// Audit Log Export
	admin.Get("/audit-logs/export", h.ExportAuditLogs)
}

// audit logs an admin action to the audit service.
func (h *AdminHandler) audit(c *fiber.Ctx, action, resource, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID, _ := c.Locals("userID").(uuid.UUID)
		h.auditService.LogAction(&userID, action, resource, resourceID, c.IP(), c.Get("User-Agent"), meta)
	}
}

// --- API Key Management Handlers ---

// ListAllAPIKeys returns all API keys paginated, with hash stripped from the response.
// @Summary      List all API keys
// @Description  Returns a paginated list of all API keys with sensitive hash stripped. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page  query int false "Page number"    default(1)
// @Param        limit query int false "Items per page" default(20)
// @Success      200 {object} apiresponse.PaginatedResponse[APIKeySafeResponse]
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/api-keys [get]
func (h *AdminHandler) ListAllAPIKeys(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	offset := (page - 1) * limit

	keys, total, err := h.apiKeyService.ListAll(offset, limit)
	if err != nil {
		return err
	}

	safeKeys := make([]APIKeySafeResponse, 0, len(keys))
	for _, key := range keys {
		safeKeys = append(safeKeys, toAPIKeySafeResponse(key))
	}

	return c.JSON(apiresponse.NewPaginatedResponse(safeKeys, page, limit, total))
}

// RevokeAPIKey revokes an API key by ID and logs an audit event.
// @Summary      Revoke an API key
// @Description  Revokes an API key by its ID. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "API Key UUID"
// @Success      200 {object} MessageResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/api-keys/{id} [delete]
func (h *AdminHandler) RevokeAPIKey(c *fiber.Ctx) error {
	keyID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid API key ID format")
	}

	if err := h.apiKeyService.AdminRevoke(keyID); err != nil {
		return err
	}

	h.audit(c, service.ActionAPIKeyRevoked, "api_key", keyID.String(), nil)

	return c.JSON(fiber.Map{
		"message": "API key revoked successfully",
	})
}
// --- Session Management Handlers ---

// ListActiveSessions returns all active sessions paginated, with token values stripped from the response.
// @Summary      List active sessions
// @Description  Returns a paginated list of all active sessions with token values stripped. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page  query int false "Page number"    default(1)
// @Param        limit query int false "Items per page" default(20)
// @Success      200 {object} apiresponse.PaginatedResponse[SessionSafeResponse]
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/sessions [get]
func (h *AdminHandler) ListActiveSessions(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	offset := (page - 1) * limit

	tokens, err := h.userRepo.GetAllActiveSessions(offset, limit)
	if err != nil {
		return err
	}

	total, err := h.userRepo.CountActiveSessions()
	if err != nil {
		return err
	}

	safeSessions := make([]SessionSafeResponse, 0, len(tokens))
	for _, token := range tokens {
		safeSessions = append(safeSessions, toSessionSafeResponse(token))
	}

	return c.JSON(apiresponse.NewPaginatedResponse(safeSessions, page, limit, total))
}

// ForceLogoutUser revokes all refresh tokens for a user and logs an audit event.
// @Summary      Force logout user
// @Description  Revokes all active sessions for a specific user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        userId path string true "User UUID"
// @Success      200 {object} MessageResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/sessions/user/{userId} [delete]
func (h *AdminHandler) ForceLogoutUser(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID format")
	}

	if err := h.userRepo.RevokeAllUserRefreshTokens(userID); err != nil {
		return err
	}

	h.audit(c, service.ActionAdminSessionRevokeAll, "user", userID.String(), map[string]interface{}{
		"target_user_id": userID.String(),
	})

	return c.JSON(fiber.Map{
		"message": "All sessions revoked successfully",
	})
}

// --- Dashboard Handler ---

// Dashboard returns aggregated system statistics.
// @Summary      Admin dashboard
// @Description  Returns aggregated system statistics including user counts, notification stats, SSE info and system info. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Success      200 {object} DashboardResponse
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/dashboard [get]
func (h *AdminHandler) Dashboard(c *fiber.Ctx) error {
	resp := DashboardResponse{}

	// Collect user stats with partial failure tolerance
	resp.Users = h.collectUserStats()

	// Collect notification stats with partial failure tolerance
	resp.Notifications = h.collectNotificationStats()

	// Collect SSE stats with partial failure tolerance
	resp.SSE = h.collectSSEStats()

	// System info (always available)
	resp.System = SystemInfo{
		Uptime:  time.Since(h.startTime).String(),
		Version: h.cfg.App.Version,
	}

	return c.JSON(resp)
}

func (h *AdminHandler) collectUserStats() UserStats {
	stats := UserStats{}

	total, err := h.userRepo.Count()
	if err != nil {
		h.logger.Error("Failed to get user count", "error", err)
		stats.Error = "unavailable"
		return stats
	}
	stats.Total = total

	active, err := h.userRepo.CountByStatus("active")
	if err != nil {
		h.logger.Error("Failed to get active user count", "error", err)
		stats.Error = "unavailable"
		return stats
	}
	stats.Active = active

	inactive, err := h.userRepo.CountByStatus("inactive")
	if err != nil {
		h.logger.Error("Failed to get inactive user count", "error", err)
		stats.Error = "unavailable"
		return stats
	}
	stats.Inactive = inactive

	locked, err := h.userRepo.CountByStatus("locked")
	if err != nil {
		h.logger.Error("Failed to get locked user count", "error", err)
		stats.Error = "unavailable"
		return stats
	}
	stats.Locked = locked

	todayStart := time.Now().Truncate(24 * time.Hour)
	todayRegs, err := h.userRepo.CountCreatedAfter(todayStart)
	if err != nil {
		h.logger.Error("Failed to get today's registration count", "error", err)
		stats.Error = "unavailable"
		return stats
	}
	stats.TodayRegistrations = todayRegs

	return stats
}

func (h *AdminHandler) collectNotificationStats() NotificationStats {
	stats := NotificationStats{}

	counts, err := h.notificationRepo.CountByStatus()
	if err != nil {
		h.logger.Error("Failed to get notification counts", "error", err)
		stats.Error = "unavailable"
		return stats
	}

	stats.Pending = counts["pending"]
	stats.Sent = counts["sent"]
	stats.Failed = counts["failed"]

	return stats
}

func (h *AdminHandler) collectSSEStats() interface{} {
	if h.sseService == nil {
		return map[string]interface{}{
			"error": "unavailable",
		}
	}
	return h.sseService.GetStats()
}

// --- System Health Handler ---

// SystemHealth returns detailed health status of all system components.
// @Summary      System health check
// @Description  Returns detailed health status of all system components (database, redis, SSE, email, storage). Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Success      200 {object} SystemHealthResponse
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/system/health [get]
func (h *AdminHandler) SystemHealth(c *fiber.Ctx) error {
	components := make(map[string]ComponentHealth)

	// Database health
	components["database"] = h.checkDatabaseHealth()

	// Redis health
	components["redis"] = h.checkRedisHealth()

	// SSE health
	components["sse"] = h.checkSSEHealth()

	// Email configuration status
	components["email"] = h.checkEmailHealth()

	// Storage configuration status
	components["storage"] = h.checkStorageHealth()

	// Calculate overall status
	overallStatus := calculateOverallStatus(components)

	return c.JSON(SystemHealthResponse{
		OverallStatus: overallStatus,
		Components:    components,
	})
}

func (h *AdminHandler) checkDatabaseHealth() ComponentHealth {
	sqlDB, err := h.db.DB.DB()
	if err != nil {
		return ComponentHealth{
			Status: "unhealthy",
			Error:  fmt.Sprintf("failed to get database instance: %v", err),
		}
	}

	stats := sqlDB.Stats()
	return ComponentHealth{
		Status: "healthy",
		Details: map[string]interface{}{
			"open_connections": stats.OpenConnections,
			"in_use":           stats.InUse,
			"idle":             stats.Idle,
			"max_open":         stats.MaxOpenConnections,
			"wait_count":       stats.WaitCount,
			"wait_duration":    stats.WaitDuration.String(),
		},
	}
}

func (h *AdminHandler) checkRedisHealth() ComponentHealth {
	if h.redisClient == nil {
		return ComponentHealth{
			Status: "unhealthy",
			Error:  "redis client not configured",
		}
	}

	start := time.Now()
	err := h.redisClient.HealthCheck()
	pingDuration := time.Since(start)

	if err != nil {
		return ComponentHealth{
			Status: "unhealthy",
			Error:  fmt.Sprintf("redis health check failed: %v", err),
		}
	}

	return ComponentHealth{
		Status: "healthy",
		Details: map[string]interface{}{
			"ping_duration": pingDuration.String(),
			"connected":     true,
		},
	}
}

func (h *AdminHandler) checkSSEHealth() ComponentHealth {
	if h.sseService == nil {
		return ComponentHealth{
			Status: "unhealthy",
			Error:  "SSE service not configured",
		}
	}

	healthy := h.sseService.IsHealthy()
	stats := h.sseService.GetStats()

	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	return ComponentHealth{
		Status: status,
		Details: map[string]interface{}{
			"healthy":            healthy,
			"active_connections": stats["connection_manager"],
		},
	}
}

func (h *AdminHandler) checkEmailHealth() ComponentHealth {
	configured := h.emailSvc != nil
	details := map[string]interface{}{
		"configured": configured,
	}
	if configured {
		details["service_type"] = "smtp"
	}

	return ComponentHealth{
		Status:  "healthy",
		Details: details,
	}
}

func (h *AdminHandler) checkStorageHealth() ComponentHealth {
	configured := h.cfg.Storage.Type != ""
	details := map[string]interface{}{
		"configured": configured,
	}
	if configured {
		details["service_type"] = h.cfg.Storage.Type
	}

	return ComponentHealth{
		Status:  "healthy",
		Details: details,
	}
}

// calculateOverallStatus determines the overall system health status.
// All healthy → "healthy", some unhealthy → "degraded", all unhealthy → "unhealthy".
func calculateOverallStatus(components map[string]ComponentHealth) string {
	totalComponents := len(components)
	if totalComponents == 0 {
		return "healthy"
	}

	unhealthyCount := 0
	for _, comp := range components {
		if comp.Status == "unhealthy" {
			unhealthyCount++
		}
	}

	switch {
	case unhealthyCount == 0:
		return "healthy"
	case unhealthyCount == totalComponents:
		return "unhealthy"
	default:
		return "degraded"
	}
}

// --- Email Handlers ---

// SendTestEmailRequest is the request body for the send test email endpoint.
type SendTestEmailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
// BulkUpdateStatusRequest is the request body for bulk user status update.
type BulkUpdateStatusRequest struct {
	UserIDs []uuid.UUID `json:"user_ids"`
	Status  string      `json:"status"`
}

// BulkAssignRoleRequest is the request body for bulk user role assignment.
type BulkAssignRoleRequest struct {
	UserIDs []uuid.UUID `json:"user_ids"`
	RoleID  uuid.UUID   `json:"role_id"`
}

// ExportAuditLogs exports audit logs as a JSON file with Content-Disposition header.
// Supports filtering by start_date, end_date, action, and user_id query parameters.
// @Summary      Export audit logs
// @Description  Exports audit logs as a downloadable JSON file. Supports filtering by date range, action and user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        start_date query string false "Start date (YYYY-MM-DD)"
// @Param        end_date   query string false "End date (YYYY-MM-DD)"
// @Param        action     query string false "Filter by action type"
// @Param        user_id    query string false "Filter by user UUID"
// @Success      200 {file} file "JSON file download"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/audit-logs/export [get]
func (h *AdminHandler) ExportAuditLogs(c *fiber.Ctx) error {
	filter := repository.AuditLogListFilter{
		Action: c.Query("action"),
		Offset: 0,
		Limit:  100000,
	}

	if userIDStr := c.Query("user_id"); userIDStr != "" {
		uid, err := uuid.Parse(userIDStr)
		if err != nil {
			return errors.NewBadRequest("Invalid user_id parameter")
		}
		filter.UserID = &uid
	}

	const dateLayout = "2006-01-02"

	if startDateStr := c.Query("start_date"); startDateStr != "" {
		t, err := time.Parse(dateLayout, startDateStr)
		if err != nil {
			return errors.NewBadRequest("Invalid start_date format, expected YYYY-MM-DD")
		}
		filter.StartDate = &t
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		t, err := time.Parse(dateLayout, endDateStr)
		if err != nil {
			return errors.NewBadRequest("Invalid end_date format, expected YYYY-MM-DD")
		}
		filter.EndDate = &t
	}

	logs, _, err := h.auditService.ListAllLogs(filter)
	if err != nil {
		return errors.NewInternalError("Failed to fetch audit logs for export")
	}

	data, err := json.Marshal(logs)
	if err != nil {
		return errors.NewInternalError("Failed to marshal audit logs to JSON")
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=audit_logs_export.json")
	return c.Send(data)
}


// NotificationStats returns notification statistics grouped by status and type.
// @Summary      Notification statistics
// @Description  Returns notification counts grouped by status (pending/sent/failed) and by type. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Success      200 {object} map[string]interface{}
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/notifications/stats [get]
func (h *AdminHandler) NotificationStats(c *fiber.Ctx) error {
	byStatus, err := h.notificationRepo.CountByStatus()
	if err != nil {
		return errors.NewInternalError("failed to get notification stats by status: " + err.Error())
	}

	byType, err := h.notificationRepo.CountByType()
	if err != nil {
		return errors.NewInternalError("failed to get notification stats by type: " + err.Error())
	}

	return c.JSON(fiber.Map{
		"by_status": byStatus,
		"by_type":   byType,
	})
}

// RetryFailedNotifications triggers retry of failed notifications.
// @Summary      Retry failed notifications
// @Description  Queues all failed notifications for retry. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Success      200 {object} MessageResponse
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/notifications/retry-failed [post]
func (h *AdminHandler) RetryFailedNotifications(c *fiber.Ctx) error {
	if err := h.notificationSvc.RetryFailedNotifications(); err != nil {
		return errors.NewInternalError("failed to retry failed notifications: " + err.Error())
	}

	return c.JSON(fiber.Map{
		"message": "Failed notifications queued for retry",
	})
}

// ProcessPendingNotifications triggers processing of pending notifications.
// @Summary      Process pending notifications
// @Description  Queues all pending notifications for processing. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Success      200 {object} MessageResponse
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/notifications/process-pending [post]
func (h *AdminHandler) ProcessPendingNotifications(c *fiber.Ctx) error {
	if err := h.notificationSvc.ProcessPendingNotifications(); err != nil {
		return errors.NewInternalError("failed to process pending notifications: " + err.Error())
	}

	return c.JSON(fiber.Map{
		"message": "Pending notifications queued for processing",
	})
}


// BulkOperationResult represents the result of a bulk operation.
type BulkOperationResult struct {
	SuccessCount int                   `json:"success_count"`
	FailureCount int                   `json:"failure_count"`
	Failures     []BulkOperationError  `json:"failures,omitempty"`
}

// BulkOperationError represents a single failure in a bulk operation.
type BulkOperationError struct {
	UserID uuid.UUID `json:"user_id"`
	Error  string    `json:"error"`
}

// ExportUsers exports all users in JSON or CSV format.
// @Summary      Export users
// @Description  Exports all users as a downloadable JSON or CSV file. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        format query string false "Export format (json or csv)" default(json)
// @Success      200 {file} file "JSON or CSV file download"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/users/export [get]
func (h *AdminHandler) ExportUsers(c *fiber.Ctx) error {
	format := c.Query("format", "json")
	if format != "json" && format != "csv" {
		return errors.NewBadRequest("Invalid format. Supported formats: json, csv")
	}

	filter := repository.UserListFilter{
		Offset: 0,
		Limit:  100000,
	}
	users, _, err := h.userService.AdminListUsers(filter)
	if err != nil {
		return err
	}

	if format == "csv" {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)

		header := []string{"id", "email", "username", "first_name", "last_name", "status", "created_at"}
		if err := writer.Write(header); err != nil {
			return errors.NewInternalError("Failed to write CSV header")
		}

		for _, u := range users {
			row := []string{
				u.ID.String(),
				u.Email,
				u.Username,
				u.FirstName,
				u.LastName,
				string(u.Status),
				u.CreatedAt.Format(time.RFC3339),
			}
			if err := writer.Write(row); err != nil {
				return errors.NewInternalError("Failed to write CSV row")
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return errors.NewInternalError("Failed to flush CSV writer")
		}

		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", "attachment; filename=users_export.csv")
		return c.Send(buf.Bytes())
	}

	// JSON format (default)
	data, err := json.Marshal(users)
	if err != nil {
		return errors.NewInternalError("Failed to marshal users to JSON")
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=users_export.json")
	return c.Send(data)
}


// ListEmailLogs returns paginated email logs with optional status filtering.
// @Summary      List email logs
// @Description  Returns a paginated list of email logs with optional status filtering. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page   query int    false "Page number"    default(1)
// @Param        limit  query int    false "Items per page" default(20)
// @Param        status query string false "Filter by status (sent/failed/pending)"
// @Success      200 {object} object "Paginated email logs"
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/email-logs [get]
func (h *AdminHandler) ListEmailLogs(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	offset := (page - 1) * limit

	logs, total, err := h.notificationRepo.ListEmailLogs(offset, limit, status)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(logs, page, limit, total))
}

// SendTestEmail sends a test email to verify email configuration.
// @Summary      Send test email
// @Description  Sends a test email to verify email configuration is working correctly. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body SendTestEmailRequest true "Test email details"
// @Success      200 {object} MessageResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      503 {object} errors.ProblemDetail "Email service unavailable"
// @Router       /admin/email/test [post]
func (h *AdminHandler) SendTestEmail(c *fiber.Ctx) error {
	if h.emailSvc == nil {
		return errors.NewServiceUnavailable("email service")
	}

	var req SendTestEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if req.To == "" || req.Subject == "" || req.Body == "" {
		return errors.NewBadRequest("to, subject and body are required")
	}

	if _, err := mail.ParseAddress(req.To); err != nil {
		return errors.NewBadRequest("Invalid email address")
	}

	err := h.emailSvc.Send(context.Background(), email.EmailData{
		To:       []string{req.To},
		Subject:  req.Subject,
		Template: "notification",
		Data: map[string]interface{}{
			"Subject": req.Subject,
			"Message": req.Body,
			"AppName": h.cfg.App.Name,
			"Year":    time.Now().Year(),
		},
	})
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Test email sent successfully",
	})
}
// --- Bulk User Operations ---

// BulkUpdateStatus updates the status of multiple users at once.
// Returns 200 if all succeed, 207 Multi-Status if partial or all fail, 400 for invalid input.
// @Summary      Bulk update user status
// @Description  Updates the status of multiple users at once. Returns 207 Multi-Status on partial failure. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body BulkUpdateStatusRequest true "User IDs and target status"
// @Success      200 {object} BulkOperationResult
// @Success      207 {object} BulkOperationResult "Partial failure"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/users/bulk-status [post]
func (h *AdminHandler) BulkUpdateStatus(c *fiber.Ctx) error {
	var req BulkUpdateStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.UserIDs) == 0 {
		return errors.NewBadRequest("user_ids is required and cannot be empty")
	}

	validStatuses := map[string]bool{"active": true, "inactive": true, "locked": true}
	if !validStatuses[req.Status] {
		return errors.NewBadRequest("Invalid status. Allowed values: active, inactive, locked")
	}

	result := BulkOperationResult{}
	for _, userID := range req.UserIDs {
		_, err := h.userService.AdminUpdateStatus(userID, req.Status)
		if err != nil {
			result.FailureCount++
			result.Failures = append(result.Failures, BulkOperationError{
				UserID: userID,
				Error:  err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	if result.FailureCount > 0 {
		return c.Status(207).JSON(result)
	}
	return c.JSON(result)
}

// BulkAssignRole assigns a role to multiple users at once.
// Returns 200 if all succeed, 207 Multi-Status if partial or all fail, 400 for invalid input.
// @Summary      Bulk assign role
// @Description  Assigns a role to multiple users at once. Returns 207 Multi-Status on partial failure. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body BulkAssignRoleRequest true "User IDs and target role"
// @Success      200 {object} BulkOperationResult
// @Success      207 {object} BulkOperationResult "Partial failure"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/users/bulk-role [post]
func (h *AdminHandler) BulkAssignRole(c *fiber.Ctx) error {
	var req BulkAssignRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.UserIDs) == 0 {
		return errors.NewBadRequest("user_ids is required and cannot be empty")
	}

	// Validate role exists before iterating over users
	if _, err := h.userRepo.GetRoleByID(req.RoleID); err != nil {
		return errors.NewBadRequest("Invalid role_id: role not found")
	}

	result := BulkOperationResult{}
	for _, userID := range req.UserIDs {
		err := h.userService.AdminAssignRole(userID, req.RoleID)
		if err != nil {
			result.FailureCount++
			result.Failures = append(result.Failures, BulkOperationError{
				UserID: userID,
				Error:  err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	if result.FailureCount > 0 {
		return c.Status(207).JSON(result)
	}
	return c.JSON(result)
}
