package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
)

const (
	roleAdmin       = "admin"
	roleSystemAdmin = "system_admin"
)

// NotificationHandler handles notification-related HTTP requests.
type NotificationHandler struct {
	notificationService *service.NotificationService
}

// ListNotificationsResponse is the standardized paginated response for notifications.
type ListNotificationsResponse struct {
	Items      []*domain.Notification `json:"items"`
	Pagination apiresponse.Pagination `json:"pagination"`
}

// MessageResponse is a simple response containing only a message.
type MessageResponse struct {
	Message string `json:"message"`
}

// AdminCreateNotificationRequest represents the request body for admin notification creation.
type AdminCreateNotificationRequest struct {
	UserID       uuid.UUID              `json:"user_id" validate:"required"`
	Title        string                 `json:"title" validate:"required,min=1,max=255"`
	Content      string                 `json:"content" validate:"required,min=1"`
	Type         string                 `json:"type" validate:"required,oneof=email push in_app webhook sms"`
	Template     string                 `json:"template"`
	LanguageCode string                 `json:"language_code"`
	Data         map[string]interface{} `json:"data"`
}

// NewNotificationHandler creates a new notification handler.
func NewNotificationHandler(notificationService *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{
		notificationService: notificationService,
	}
}

// RegisterRoutes registers notification routes on the given router group.
// All routes require authentication (authMw). authzMw is the Casbin authorization
// middleware; it may be nil when Casbin is not configured.
func (h *NotificationHandler) RegisterRoutes(api fiber.Router, authMw fiber.Handler, authzMw fiber.Handler) {
	middlewares := []any{authMw}
	if authzMw != nil {
		middlewares = append(middlewares, authzMw)
	}
	notifications := api.Group("/notifications", middlewares...)

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
// @Success 200 {object} ListNotificationsResponse "List of notifications"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /notifications [get]
func (h *NotificationHandler) ListNotifications(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	page := fiber.Query[int](c, "page", 1)
	limit := fiber.Query[int](c, "limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	items, err := h.notificationService.GetUserNotifications(userID, limit, offset)
	if err != nil {
		return errors.NewInternalError("Failed to fetch notifications")
	}

	total, err := h.notificationService.CountUserNotifications(userID)
	if err != nil {
		return errors.NewInternalError("Failed to fetch notifications")
	}

	return c.JSON(apiresponse.NewPaginatedResponse(items, page, limit, total))
}

// CreateNotification creates and sends a notification to a specific user (admin only).
// @Summary Create notification
// @Description Admin endpoint to send a notification to a specific user
// @Tags Notifications
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body AdminCreateNotificationRequest true "Notification data"
// @Success 201 {object} domain.Notification "Created notification"
// @Failure 400 {object} errors.ProblemDetail "Validation error"
// @Failure 403 {object} errors.ProblemDetail "Forbidden - admin only"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /notifications [post]
func (h *NotificationHandler) CreateNotification(c fiber.Ctx) error {
	// Parse request body
	var req AdminCreateNotificationRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Validate request
	if err := validation.Struct(req); err != nil {
		return err
	}

	// Build SendNotificationRequest — recipients are resolved by the service layer
	// based on notification type (e.g. email → user's email address from DB)
	metadata := req.Data
	if req.LanguageCode != "" {
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		metadata["language_code"] = req.LanguageCode
	}

	sendReq := &service.SendNotificationRequest{
		UserID:   req.UserID,
		Type:     domain.NotificationType(req.Type),
		Priority: domain.NotificationPriorityNormal,
		Subject:  req.Title,
		Content:  req.Content,
		Template: req.Template,
		Metadata: metadata,
	}

	// Send notification
	notification, err := h.notificationService.SendNotification(sendReq)
	if err != nil {
		return errors.NewInternalError("Failed to send notification")
	}

	return c.Status(fiber.StatusCreated).JSON(notification)
}

// MarkAsRead marks a single notification as read.
// @Summary Mark notification as read
// @Description Mark a single notification as read
// @Tags Notifications
// @Security Bearer
// @Produce json
// @Param id path string true "Notification UUID"
// @Success 200 {object} MessageResponse "Notification marked as read"
// @Failure 400 {object} errors.ProblemDetail "Invalid notification ID"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Router /notifications/{id}/read [put]
func (h *NotificationHandler) MarkAsRead(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
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
func (h *NotificationHandler) GetPreferences(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
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
// @Success 200 {object} MessageResponse "Preferences updated"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Not authenticated"
// @Failure 500 {object} errors.ProblemDetail "Internal server error"
// @Router /notifications/preferences [put]
func (h *NotificationHandler) UpdatePreferences(c fiber.Ctx) error {
	userID := fiber.Locals[uuid.UUID](c, "userID")
	if userID == uuid.Nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	var prefs domain.NotificationPreference
	if err := c.Bind().Body(&prefs); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	// Ensure the preference is bound to the authenticated user regardless of body content
	prefs.UserID = userID

	if err := h.notificationService.UpdateUserPreferences(userID, &prefs); err != nil {
		return errors.NewInternalError("Failed to update preferences")
	}

	return c.JSON(fiber.Map{
		"message": "Preferences updated successfully",
	})
}
