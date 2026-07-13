package api

import (
	"html"

	"github.com/gofiber/fiber/v3"
	helpers "github.com/mr-kaynak/go-core/internal/api/helpers"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// AdminCommsHandler handles admin email and notification-queue operations.
type AdminCommsHandler struct {
	adminService    *service.AdminService
	notificationSvc AdminNotificationProcessor
	emailSvc        *email.EmailService
	logger          *logger.Logger
}

// SendTestEmailRequest is the request body for the send test email endpoint.
type SendTestEmailRequest struct {
	To      string `json:"to" validate:"required,email"`
	Subject string `json:"subject" validate:"required,min=1,max=200"`
	Body    string `json:"body" validate:"required,min=1,max=10000"`
}

// --- Email Handlers ---

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
func (h *AdminCommsHandler) ListEmailLogs(c fiber.Ctx) error {
	page, limit, offset := helpers.ParsePagination(c, 20)
	status := c.Query("status")

	logs, total, err := h.adminService.ListEmailLogs(c.Context(), offset, limit, status)
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
func (h *AdminCommsHandler) SendTestEmail(c fiber.Ctx) error {
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

// --- Notification Stats & Queue Management ---

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
func (h *AdminCommsHandler) NotificationStatsHandler(c fiber.Ctx) error {
	result, err := h.adminService.CollectNotificationStats(c.Context())
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
func (h *AdminCommsHandler) RetryFailedNotifications(c fiber.Ctx) error {
	if err := h.notificationSvc.RetryFailedNotifications(c.Context()); err != nil {
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
func (h *AdminCommsHandler) ProcessPendingNotifications(c fiber.Ctx) error {
	if err := h.notificationSvc.ProcessPendingNotifications(c.Context()); err != nil {
		h.logger.WithError(err).Error("Failed to process pending notifications")
		return errors.NewInternalError("Failed to process pending notifications")
	}

	return c.JSON(fiber.Map{
		"message": "Pending notifications queued for processing",
	})
}
