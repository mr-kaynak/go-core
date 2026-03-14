package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"html"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

// Health/status string constants used across admin responses.
const (
	statusHealthy     = "healthy"
	statusUnhealthy   = "unhealthy"
	statusUnavailable = "unavailable"

	maxExportLimit    = 10000
	maxBulkOperations = 1000
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
	adminService    *service.AdminService
	notificationSvc *notificationService.NotificationService
	auditService    *service.AuditService
	apiKeyService   *service.APIKeyService
	userService     *service.UserService
	sseService      *notificationService.SSEService
	emailSvc        *email.EmailService
	cfg             *config.Config
	startTime       time.Time
	logger          *logger.Logger
}

// NewAdminHandler creates a new admin handler with all dependencies.
func NewAdminHandler(
	adminService *service.AdminService,
	notificationSvc *notificationService.NotificationService,
	auditService *service.AuditService,
	apiKeyService *service.APIKeyService,
	userService *service.UserService,
	sseService *notificationService.SSEService,
	emailSvc *email.EmailService,
	cfg *config.Config,
) *AdminHandler {
	return &AdminHandler{
		adminService:    adminService,
		notificationSvc: notificationSvc,
		auditService:    auditService,
		apiKeyService:   apiKeyService,
		userService:     userService,
		sseService:      sseService,
		emailSvc:        emailSvc,
		cfg:             cfg,
		startTime:       time.Now(),
		logger:          logger.Get().WithField("handler", "admin"),
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

// audit logs an admin action to the audit service.
func (h *AdminHandler) audit(c fiber.Ctx, action, resource, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID := fiber.Locals[uuid.UUID](c, "userID")
		h.auditService.LogAction(&userID, action, resource, resourceID, c.IP(), c.UserAgent(), meta)
	}
}

func parsePagination(c fiber.Ctx) (page, limit, offset int) {
	page = fiber.Query[int](c, "page", 1)
	limit = apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", 20), 20)
	if page < 1 {
		page = 1
	}
	offset = (page - 1) * limit
	return
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
func (h *AdminHandler) ListAllAPIKeys(c fiber.Ctx) error {
	page, limit, offset := parsePagination(c)

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
func (h *AdminHandler) RevokeAPIKey(c fiber.Ctx) error {
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
func (h *AdminHandler) ListActiveSessions(c fiber.Ctx) error {
	page, limit, offset := parsePagination(c)

	tokens, total, err := h.adminService.ListActiveSessions(offset, limit)
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
func (h *AdminHandler) ForceLogoutUser(c fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID format")
	}

	ctx, cancel := context.WithTimeout(c, 3*time.Second)
	defer cancel()

	if err := h.adminService.ForceLogoutUser(ctx, userID); err != nil {
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
func (h *AdminHandler) Dashboard(c fiber.Ctx) error {
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
	result, err := h.adminService.CollectUserStats()
	if err != nil {
		h.logger.Error("Failed to collect user stats", "error", err)
		return UserStats{Error: statusUnavailable}
	}
	return UserStats{
		Total:              result.Total,
		Active:             result.Active,
		Inactive:           result.Inactive,
		Locked:             result.Locked,
		TodayRegistrations: result.TodayRegistrations,
	}
}

func (h *AdminHandler) collectNotificationStats() NotificationStats {
	result, err := h.adminService.CollectNotificationStats()
	if err != nil {
		h.logger.Error("Failed to collect notification stats", "error", err)
		return NotificationStats{Error: statusUnavailable}
	}
	return NotificationStats{
		Pending: result.ByStatus["pending"],
		Sent:    result.ByStatus["sent"],
		Failed:  result.ByStatus["failed"],
	}
}

func (h *AdminHandler) collectSSEStats() interface{} {
	if h.sseService == nil {
		return map[string]interface{}{
			"error": statusUnavailable,
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
func (h *AdminHandler) SystemHealth(c fiber.Ctx) error {
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
	dbHealth, err := h.adminService.CheckDatabaseHealth()
	if err != nil {
		h.logger.WithError(err).Error("Failed to check database health")
		return ComponentHealth{
			Status: statusUnhealthy,
			Error:  "failed to get database instance",
		}
	}
	if dbHealth == nil {
		return ComponentHealth{
			Status: statusUnhealthy,
			Error:  "database not configured",
		}
	}

	return ComponentHealth{
		Status: statusHealthy,
		Details: map[string]interface{}{
			"open_connections": dbHealth.OpenConnections,
			"in_use":           dbHealth.InUse,
			"idle":             dbHealth.Idle,
			"max_open":         dbHealth.MaxOpen,
			"wait_count":       dbHealth.WaitCount,
			"wait_duration":    dbHealth.WaitDuration.String(),
		},
	}
}

func (h *AdminHandler) checkRedisHealth() ComponentHealth {
	redisHealth, err := h.adminService.CheckRedisHealth()
	if redisHealth == nil && err == nil {
		return ComponentHealth{
			Status: statusUnhealthy,
			Error:  "redis client not configured",
		}
	}

	if err != nil {
		h.logger.WithError(err).Error("Redis health check failed")
		return ComponentHealth{
			Status: statusUnhealthy,
			Error:  "redis health check failed",
		}
	}

	return ComponentHealth{
		Status: statusHealthy,
		Details: map[string]interface{}{
			"ping_duration": redisHealth.PingDuration.String(),
			"connected":     true,
		},
	}
}

func (h *AdminHandler) checkSSEHealth() ComponentHealth {
	if h.sseService == nil {
		return ComponentHealth{
			Status: statusUnhealthy,
			Error:  "SSE service not configured",
		}
	}

	healthy := h.sseService.IsHealthy()
	stats := h.sseService.GetStats()

	status := statusHealthy
	if !healthy {
		status = statusUnhealthy
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
		return statusHealthy
	}

	unhealthyCount := 0
	for _, comp := range components {
		if comp.Status == statusUnhealthy {
			unhealthyCount++
		}
	}

	switch unhealthyCount {
	case 0:
		return statusHealthy
	case totalComponents:
		return statusUnhealthy
	default:
		return "degraded"
	}
}

// --- Email Handlers ---

// SendTestEmailRequest is the request body for the send test email endpoint.
type SendTestEmailRequest struct {
	To      string `json:"to" validate:"required,email"`
	Subject string `json:"subject" validate:"required,min=1,max=200"`
	Body    string `json:"body" validate:"required,min=1,max=10000"`
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
func (h *AdminHandler) ExportAuditLogs(c fiber.Ctx) error {
	filter := domain.AuditLogListFilter{
		Action: c.Query("action"),
		Offset: 0,
		Limit:  maxExportLimit,
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

// NotificationStatsHandler returns notification statistics grouped by status and type.
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
func (h *AdminHandler) NotificationStatsHandler(c fiber.Ctx) error {
	result, err := h.adminService.CollectNotificationStats()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get notification stats")
		return errors.NewInternalError("Failed to get notification statistics")
	}

	return c.JSON(fiber.Map{
		"by_status": result.ByStatus,
		"by_type":   result.ByType,
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
func (h *AdminHandler) RetryFailedNotifications(c fiber.Ctx) error {
	if err := h.notificationSvc.RetryFailedNotifications(); err != nil {
		h.logger.WithError(err).Error("Failed to retry failed notifications")
		return errors.NewInternalError("Failed to retry failed notifications")
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
func (h *AdminHandler) ProcessPendingNotifications(c fiber.Ctx) error {
	if err := h.notificationSvc.ProcessPendingNotifications(); err != nil {
		h.logger.WithError(err).Error("Failed to process pending notifications")
		return errors.NewInternalError("Failed to process pending notifications")
	}

	return c.JSON(fiber.Map{
		"message": "Pending notifications queued for processing",
	})
}

// --- Bulk User Operations ---

// BulkOperationResult represents the result of a bulk operation.
type BulkOperationResult struct {
	SuccessCount int                  `json:"success_count"`
	FailureCount int                  `json:"failure_count"`
	Failures     []BulkOperationError `json:"failures,omitempty"`
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
func (h *AdminHandler) ExportUsers(c fiber.Ctx) error {
	format := c.Query("format", "json")
	if format != "json" && format != "csv" {
		return errors.NewBadRequest("Invalid format. Supported formats: json, csv")
	}

	filter := domain.UserListFilter{
		Offset: 0,
		Limit:  maxExportLimit,
	}
	users, _, err := h.userService.AdminListUsers(c, filter)
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
func (h *AdminHandler) ListEmailLogs(c fiber.Ctx) error {
	page := fiber.Query[int](c, "page", 1)
	limit := apiresponse.SanitizeLimit(fiber.Query[int](c, "limit", 20), 20)
	status := c.Query("status")

	if page < 1 {
		page = 1
	}

	offset := (page - 1) * limit

	logs, total, err := h.adminService.ListEmailLogs(offset, limit, status)
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
func (h *AdminHandler) SendTestEmail(c fiber.Ctx) error {
	if h.emailSvc == nil {
		return errors.NewServiceUnavailable("email service")
	}

	var req SendTestEmailRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	// Sanitize body to prevent HTML injection / phishing via admin endpoint.
	// Wrap plain text in a minimal HTML envelope so go-mail sends it correctly.
	sanitizedBody := "<pre>" + html.EscapeString(req.Body) + "</pre>"

	err := h.emailSvc.SendRaw(c, []string{req.To}, req.Subject, sanitizedBody)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Test email sent successfully",
	})
}

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
func (h *AdminHandler) BulkUpdateStatus(c fiber.Ctx) error {
	var req BulkUpdateStatusRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.UserIDs) == 0 {
		return errors.NewBadRequest("user_ids is required and cannot be empty")
	}
	if len(req.UserIDs) > maxBulkOperations {
		return errors.NewBadRequest("user_ids exceeds maximum batch size of 1000")
	}

	validStatuses := map[string]bool{"active": true, "inactive": true, "locked": true}
	if !validStatuses[req.Status] {
		return errors.NewBadRequest("Invalid status. Allowed values: active, inactive, locked")
	}

	result := BulkOperationResult{}
	for _, userID := range req.UserIDs {
		_, err := h.userService.AdminUpdateStatus(c, userID, req.Status)
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
		return c.Status(http.StatusMultiStatus).JSON(result)
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
func (h *AdminHandler) BulkAssignRole(c fiber.Ctx) error {
	var req BulkAssignRoleRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.UserIDs) == 0 {
		return errors.NewBadRequest("user_ids is required and cannot be empty")
	}
	if len(req.UserIDs) > maxBulkOperations {
		return errors.NewBadRequest("user_ids exceeds maximum batch size of 1000")
	}

	// Validate role exists before iterating over users
	if err := h.adminService.ValidateRoleExists(req.RoleID); err != nil {
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
		return c.Status(http.StatusMultiStatus).JSON(result)
	}
	return c.JSON(result)
}

// fiber:context-methods migrated
