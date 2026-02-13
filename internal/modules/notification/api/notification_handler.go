package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

// NotificationHandler handles notification-related HTTP requests.
type NotificationHandler struct {
	notificationService *service.NotificationService
}

// NewNotificationHandler creates a new notification handler.
func NewNotificationHandler(notificationService *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{
		notificationService: notificationService,
	}
}

// RegisterRoutes registers notification routes on the given router group.
// All routes require authentication (authMw).
func (h *NotificationHandler) RegisterRoutes(api fiber.Router, authMw fiber.Handler) {
	notifications := api.Group("/notifications", authMw)

	notifications.Get("", h.ListNotifications)
	notifications.Post("", h.CreateNotification)
	notifications.Put("/:id/read", h.MarkAsRead)
	notifications.Get("/preferences", h.GetPreferences)
	notifications.Put("/preferences", h.UpdatePreferences)
}

// ListNotifications returns paginated notifications for the authenticated user.
// @Summary List user notifications
// @Description Get paginated notifications for the authenticated user
// @Tags Notifications
// @Security Bearer
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Success 200 {object} fiber.Map "List of notifications"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /notifications [get]
func (h *NotificationHandler) ListNotifications(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	offset := (page - 1) * limit

	items, err := h.notificationService.GetUserNotifications(userID, limit, offset)
	if err != nil {
		return errors.NewInternalError("Failed to fetch notifications")
	}

	return c.JSON(fiber.Map{
		"notifications": items,
		"page":          page,
		"limit":         limit,
	})
}

// CreateNotification is a placeholder; system-wide notifications should use the SSE broadcast endpoint.
// @Summary Create notification (placeholder)
// @Description Placeholder - use POST /admin/sse/broadcast for system notifications
// @Tags Notifications
// @Security Bearer
// @Accept json
// @Produce json
// @Success 501 {object} fiber.Map "Not implemented"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /notifications [post]
func (h *NotificationHandler) CreateNotification(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{
		"error":   "Please use POST /api/v1/admin/sse/broadcast for system-wide notifications",
		"message": "Individual notifications should be triggered by business events",
	})
}

// MarkAsRead marks a single notification as read.
// @Summary Mark notification as read
// @Description Mark a single notification as read
// @Tags Notifications
// @Security Bearer
// @Produce json
// @Param id path string true "Notification UUID"
// @Success 200 {object} fiber.Map "Notification marked as read"
// @Failure 400 {object} errors.ProblemDetail "Invalid notification ID"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /notifications/{id}/read [put]
func (h *NotificationHandler) MarkAsRead(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	notificationID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid notification ID")
	}

	if err := h.notificationService.MarkAsRead(notificationID, userID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Notification marked as read",
		"id":      notificationID,
	})
}

// GetPreferences returns the notification preferences for the authenticated user.
// @Summary Get notification preferences
// @Description Get notification preferences for the authenticated user
// @Tags Notifications
// @Security Bearer
// @Produce json
// @Success 200 {object} domain.NotificationPreference "User preferences"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /notifications/preferences [get]
func (h *NotificationHandler) GetPreferences(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	prefs, err := h.notificationService.GetUserPreferences(userID)
	if err != nil {
		return errors.NewInternalError("Failed to fetch preferences")
	}

	return c.JSON(prefs)
}

// UpdatePreferences updates the notification preferences for the authenticated user.
// @Summary Update notification preferences
// @Description Update notification preferences for the authenticated user
// @Tags Notifications
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body domain.NotificationPreference true "Notification preferences"
// @Success 200 {object} fiber.Map "Preferences updated"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /notifications/preferences [put]
func (h *NotificationHandler) UpdatePreferences(c *fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var prefs domain.NotificationPreference
	if err := c.BodyParser(&prefs); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.notificationService.UpdateUserPreferences(userID, &prefs); err != nil {
		return errors.NewInternalError("Failed to update preferences")
	}

	return c.JSON(fiber.Map{
		"message": "Preferences updated successfully",
	})
}
