package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
)

// --- Admin System/Health/Dashboard Response DTOs ---

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
	Status  string      `json:"status"` // healthy, unhealthy, unavailable
	Details interface{} `json:"details,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// DBHealthDetails holds database connection pool statistics for the health response.
type DBHealthDetails struct {
	OpenConnections int    `json:"open_connections"`
	InUse           int    `json:"in_use"`
	Idle            int    `json:"idle"`
	MaxOpen         int    `json:"max_open"`
	WaitCount       int64  `json:"wait_count"`
	WaitDuration    string `json:"wait_duration"`
}

// RedisHealthDetails holds Redis health details for the health response.
type RedisHealthDetails struct {
	PingDuration string `json:"ping_duration"`
	Connected    bool   `json:"connected"`
}

// SSEHealthDetails holds SSE health details for the health response.
// ActiveConnections mirrors the untyped "connection_manager" value from the SSE
// service's stats map, whose concrete shape is owned by the notification module.
type SSEHealthDetails struct {
	Healthy           bool        `json:"healthy"`
	ActiveConnections interface{} `json:"active_connections"`
}

// ServiceConfigDetails holds configuration status for services that expose no
// runtime probe on the health path (email, storage).
type ServiceConfigDetails struct {
	Configured  bool   `json:"configured"`
	ServiceType string `json:"service_type,omitempty"`
}

// SSEStatsError is the error payload returned when the SSE service is unavailable.
type SSEStatsError struct {
	Error string `json:"error"`
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
	resp.Users = h.collectUserStats(c.Context())

	// Collect notification stats with partial failure tolerance
	resp.Notifications = h.collectNotificationStats(c.Context())

	// Collect SSE stats with partial failure tolerance
	resp.SSE = h.collectSSEStats()

	// System info (always available)
	resp.System = SystemInfo{
		Uptime:  time.Since(h.startTime).String(),
		Version: h.cfg.App.Version,
	}

	return c.JSON(resp)
}

func (h *AdminHandler) collectUserStats(ctx context.Context) UserStats {
	result, err := h.adminService.CollectUserStats(ctx)
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

func (h *AdminHandler) collectNotificationStats(ctx context.Context) NotificationStats {
	result, err := h.adminService.CollectNotificationStats(ctx)
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
		return SSEStatsError{Error: statusUnavailable}
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
		Details: DBHealthDetails{
			OpenConnections: dbHealth.OpenConnections,
			InUse:           dbHealth.InUse,
			Idle:            dbHealth.Idle,
			MaxOpen:         dbHealth.MaxOpen,
			WaitCount:       dbHealth.WaitCount,
			WaitDuration:    dbHealth.WaitDuration.String(),
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
		Details: RedisHealthDetails{
			PingDuration: redisHealth.PingDuration.String(),
			Connected:    true,
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
		Details: SSEHealthDetails{
			Healthy:           healthy,
			ActiveConnections: stats["connection_manager"],
		},
	}
}

func (h *AdminHandler) checkEmailHealth() ComponentHealth {
	// We do NOT open an SMTP connection on the health path. When the email service
	// is unconfigured, report "unavailable" rather than lying with "healthy"; when
	// it is wired, report "healthy" (config presence, not a live probe).
	configured := h.emailSvc != nil
	if !configured {
		return ComponentHealth{
			Status:  statusUnavailable,
			Details: ServiceConfigDetails{Configured: false},
		}
	}

	return ComponentHealth{
		Status:  statusHealthy,
		Details: ServiceConfigDetails{Configured: true, ServiceType: "smtp"},
	}
}

func (h *AdminHandler) checkStorageHealth() ComponentHealth {
	configured := h.cfg.Storage.Type != ""
	details := ServiceConfigDetails{Configured: configured}
	if configured {
		details.ServiceType = h.cfg.Storage.Type
	}

	return ComponentHealth{
		Status:  statusHealthy,
		Details: details,
	}
}
