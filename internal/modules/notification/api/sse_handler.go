package api

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

const (
	sseBufferSize      = 100
	sseSendTimeout     = 5 * time.Second
	sseMaxMessageSize  = 1024 * 1024 // 1MB
	sseHeartbeatPeriod = 30 * time.Second
)

// SSEHandler handles Server-Sent Events endpoints
type SSEHandler struct {
	sseService      *service.SSEService
	notificationSvc *service.NotificationService
	logger          *logger.Logger
}

// NewSSEHandler creates a new SSE handler
func NewSSEHandler(
	sseService *service.SSEService,
	notificationSvc *service.NotificationService,
) *SSEHandler {
	return &SSEHandler{
		sseService:      sseService,
		notificationSvc: notificationSvc,
		logger:          logger.Get().WithField("component", "sse_handler"),
	}
}

// RegisterRoutes registers SSE routes
func (h *SSEHandler) RegisterRoutes(router fiber.Router, authMiddleware fiber.Handler, authzMiddleware ...fiber.Handler) {
	// SSE endpoints (protected — require authentication)
	sse := router.Group("/notifications")
	sse.Use(authMiddleware)

	// Main SSE streaming endpoint
	sse.Get("/stream", h.StreamNotifications)

	// SSE control endpoints
	sse.Post("/stream/subscribe", h.Subscribe)
	sse.Post("/stream/unsubscribe", h.Unsubscribe)
	sse.Post("/stream/ack", h.Acknowledge)

	// SSE admin endpoints (protected — require authentication + authorization)
	admin := router.Group("/admin/sse")
	admin.Use(authMiddleware)
	for _, mw := range authzMiddleware {
		admin.Use(mw)
	}
	admin.Get("/stats", h.GetStats)
	admin.Get("/connections", h.GetConnections)
	admin.Post("/broadcast", h.BroadcastMessage)
	admin.Delete("/connections/:clientId", h.DisconnectClient)
}

// StreamNotifications handles SSE streaming endpoint
// @Summary Stream notifications via Server-Sent Events
// @Description Establishes an SSE connection for real-time notifications
// @Tags SSE
// @Security Bearer
// @Param types query string false "Event types to filter (comma-separated)"
// @Param priorities query string false "Priority levels to filter (comma-separated)"
// @Param channels query string false "Channels to subscribe (comma-separated)"
// @Param since query string false "Get missed events since timestamp (RFC3339)"
// @Produce text/event-stream
// @Success 200 {string} string "SSE stream established"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 503 {object} errors.ProblemDetail "Service unavailable"
// @Router /notifications/stream [get]
func (h *SSEHandler) StreamNotifications(c fiber.Ctx) error {
	// Get user claims from context
	claims, ok := c.Locals("claims").(*identityService.Claims)
	if !ok || claims == nil {
		return errors.NewUnauthorized("User not authenticated")
	}

	// Parse query parameters
	eventTypes := h.parseCommaSeparated(c.Query("types"))
	priorities := h.parseCommaSeparated(c.Query("priorities"))
	channels := h.parseCommaSeparated(c.Query("channels"))
	sinceStr := c.Query("since")

	// Create SSE client with options
	clientOptions := streaming.ClientOptions{
		BufferSize:     sseBufferSize,
		SendTimeout:    sseSendTimeout,
		MaxMessageSize: sseMaxMessageSize,
		EnableMetrics:  true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := streaming.NewClientWithOptions(ctx, claims.UserID, clientOptions)

	// Set client metadata
	client.UserAgent = c.Get("User-Agent")
	client.IPAddress = c.IP()
	client.SessionID = c.Get("X-Session-ID", "")
	client.DeviceID = c.Get("X-Device-ID", "")
	client.AuthToken = c.Get("Authorization", "")

	// Set filters
	if len(eventTypes) > 0 {
		client.SetEventTypes(h.convertEventTypes(eventTypes))
	}
	if len(priorities) > 0 {
		client.SetPriorities(h.convertPriorities(priorities))
	}
	for _, channel := range channels {
		if strings.HasPrefix(channel, "admin:") && !h.isAdmin(claims) {
			continue
		}
		client.Subscribe(channel)
	}

	// Register client
	if err := h.sseService.RegisterClient(client); err != nil {
		cancel()
		h.logger.Error("Failed to register client",
			"user_id", claims.UserID,
			"error", err,
		)
		return errors.NewServiceUnavailable("Too many connections")
	}

	h.logger.Info("SSE connection established",
		"client_id", client.ID,
		"user_id", claims.UserID,
		"ip", client.IPAddress,
		"channels", channels,
	)

	// Capture values for the streaming closure
	clientID := client.ID
	userID := claims.UserID
	sseService := h.sseService
	log := h.logger

	// Set SSE response headers — Fiber sends these before streaming starts
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	// Use Fiber v3's first-class streaming API.
	// SendStreamWriter manages the response lifecycle: headers are sent
	// automatically, flush errors signal client disconnect, and cleanup
	// runs when the function returns.
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		defer func() { _ = sseService.UnregisterClient(clientID) }()

		// Send connection info event
		connectionEvent := domain.NewSSEConnectionInfoEvent(clientID, userID, "1.0.0")
		if _, err := w.Write(connectionEvent.Format()); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}

		// Send missed notifications if requested
		if sinceStr != "" {
			if since, err := time.Parse(time.RFC3339, sinceStr); err == nil {
				h.sendMissedNotifications(client, userID, since)
			}
		}

		ticker := time.NewTicker(sseHeartbeatPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-client.Context.Done():
				return

			case event, ok := <-client.Channel:
				if !ok {
					return
				}
				if _, err := w.Write(event.Format()); err != nil {
					log.Debug("Failed to write event",
						"client_id", clientID,
						"error", err,
					)
					return
				}
				if err := w.Flush(); err != nil {
					return
				}

			case <-ticker.C:
				heartbeat := domain.NewSSEHeartbeatEvent(0, sseService.GetServerID())
				if _, err := w.Write(heartbeat.Format()); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
				client.UpdatePing()
			}
		}
	})
}

// Subscribe handles channel subscription requests
// @Summary Subscribe to notification channels
// @Description Subscribe to specific notification channels
// @Tags SSE
// @Security Bearer
// @Accept json
// @Param request body streaming.SubscribeMessage true "Subscribe request"
// @Success 200 {object} map[string]interface{} "Subscription successful"
// @Failure 400 {object} errors.ProblemDetail "Bad request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /notifications/stream/subscribe [post]
func (h *SSEHandler) Subscribe(c fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*identityService.Claims)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var req streaming.SubscribeMessage
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Filter out admin channels for non-admin users
	allowed := make([]string, 0, len(req.Channels))
	for _, ch := range req.Channels {
		if strings.HasPrefix(ch, "admin:") && !h.isAdmin(claims) {
			continue
		}
		allowed = append(allowed, ch)
	}

	// Subscribe to channels
	subscribed := h.sseService.SubscribeUserToChannels(claims.UserID, allowed)

	return c.JSON(fiber.Map{
		"subscribed": subscribed,
		"channels":   req.Channels,
	})
}

// Unsubscribe handles channel unsubscription requests
// @Summary Unsubscribe from notification channels
// @Description Unsubscribe from specific notification channels
// @Tags SSE
// @Security Bearer
// @Accept json
// @Param request body streaming.UnsubscribeMessage true "Unsubscribe request"
// @Success 200 {object} map[string]interface{} "Unsubscription successful"
// @Failure 400 {object} errors.ProblemDetail "Bad request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /notifications/stream/unsubscribe [post]
func (h *SSEHandler) Unsubscribe(c fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*identityService.Claims)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var req streaming.UnsubscribeMessage
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Unsubscribe from channels
	unsubscribed := h.sseService.UnsubscribeUserFromChannels(claims.UserID, req.Channels)

	return c.JSON(fiber.Map{
		"unsubscribed": unsubscribed,
		"channels":     req.Channels,
	})
}

// Acknowledge handles event acknowledgment
// @Summary Acknowledge notification receipt
// @Description Acknowledge that a notification has been received
// @Tags SSE
// @Security Bearer
// @Accept json
// @Param request body streaming.AckMessage true "Acknowledgment"
// @Success 200 {object} map[string]interface{} "Acknowledgment received"
// @Failure 400 {object} errors.ProblemDetail "Bad request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Router /notifications/stream/ack [post]
func (h *SSEHandler) Acknowledge(c fiber.Ctx) error {
	claims, ok := c.Locals("claims").(*identityService.Claims)
	if !ok {
		return errors.NewUnauthorized("User not authenticated")
	}

	var req streaming.AckMessage
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	// Process acknowledgment
	h.sseService.ProcessAcknowledgment(claims.UserID, req.EventID)

	// Mark notification as read if it's a notification event
	if notificationID, parseErr := uuid.Parse(req.EventID); parseErr == nil {
		_ = h.notificationSvc.MarkAsRead(notificationID, claims.UserID)
	}

	return c.JSON(fiber.Map{
		"acknowledged": true,
		"event_id":     req.EventID,
	})
}

// GetStats returns SSE statistics
// @Summary Get SSE statistics
// @Description Returns current SSE connection and broadcast statistics
// @Tags SSE Admin
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{} "SSE statistics"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /admin/sse/stats [get]
func (h *SSEHandler) GetStats(c fiber.Ctx) error {
	stats := h.sseService.GetStats()
	return c.JSON(stats)
}

// GetConnections returns active connections
// @Summary Get active SSE connections
// @Description Returns list of active SSE connections with details
// @Tags SSE Admin
// @Security Bearer
// @Produce json
// @Param user_id query string false "Filter by user ID"
// @Param limit query int false "Limit results" default(100)
// @Success 200 {object} map[string]interface{} "Active connections"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /admin/sse/connections [get]
func (h *SSEHandler) GetConnections(c fiber.Ctx) error {
	userIDStr := c.Query("user_id")
	limit := fiber.Query[int](c, "limit", 100)

	var connections []streaming.ConnectionInfo

	if userIDStr != "" {
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			return errors.NewBadRequest("Invalid user ID")
		}
		connections = h.sseService.GetUserConnections(userID)
	} else {
		connections = h.sseService.GetAllConnections(limit)
	}

	return c.JSON(fiber.Map{
		"connections": connections,
		"total":       len(connections),
	})
}

// BroadcastMessage broadcasts a message to all or specific users
// @Summary Broadcast message
// @Description Broadcast a message to all connected users or specific users
// @Tags SSE Admin
// @Security Bearer
// @Accept json
// @Param request body BroadcastRequest true "Broadcast request"
// @Success 200 {object} map[string]interface{} "Broadcast result"
// @Failure 400 {object} errors.ProblemDetail "Bad request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /admin/sse/broadcast [post]
func (h *SSEHandler) BroadcastMessage(c fiber.Ctx) error {
	var req BroadcastRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	// Create SSE event
	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeSystemMessage,
		Timestamp: time.Now(),
		Data: domain.SSESystemMessage{
			ID:          uuid.New(),
			Title:       req.Title,
			Message:     req.Message,
			Type:        req.Type,
			AffectsUser: true,
		},
	}

	// Broadcast
	ctx := c.Context()
	var err error

	if len(req.UserIDs) > 0 {
		err = h.sseService.BroadcastToUsers(ctx, req.UserIDs, event)
	} else {
		err = h.sseService.BroadcastToAll(ctx, event)
	}

	if err != nil {
		h.logger.Error("Broadcast failed", "error", err)
		return errors.NewInternalError("Failed to broadcast message")
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"event_id":   event.ID,
		"recipients": len(req.UserIDs),
	})
}

// DisconnectClient forcefully disconnects a client
// @Summary Disconnect client
// @Description Forcefully disconnect a specific SSE client
// @Tags SSE Admin
// @Security Bearer
// @Param clientId path string true "Client ID"
// @Success 200 {object} map[string]interface{} "Disconnection result"
// @Failure 400 {object} errors.ProblemDetail "Bad request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Failure 404 {object} errors.ProblemDetail "Client not found"
// @Router /admin/sse/connections/{clientId} [delete]
func (h *SSEHandler) DisconnectClient(c fiber.Ctx) error {
	clientIDStr := c.Params("clientId")
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		return errors.NewBadRequest("Invalid client ID")
	}

	// Disconnect client
	if err := h.sseService.DisconnectClient(clientID); err != nil {
		return errors.NewNotFound("Client", clientIDStr)
	}

	return c.JSON(fiber.Map{
		"success":   true,
		"client_id": clientID,
	})
}

// Helper methods

func (h *SSEHandler) sendMissedNotifications(client *streaming.Client, userID uuid.UUID, since time.Time) {
	// Get missed notifications
	notifications, hasMore, err := h.notificationSvc.GetNotificationsSince(userID, since)
	if err != nil {
		h.logger.Error("Failed to get missed notifications", "error", err)
		return
	}

	if len(notifications) == 0 {
		return
	}

	// Create bulk notification event
	notifData := make([]domain.SSENotificationData, 0, len(notifications))
	for _, n := range notifications {
		var metadata map[string]interface{}
		if len(n.Metadata) > 0 {
			_ = json.Unmarshal(n.Metadata, &metadata)
		}
		notifData = append(notifData, domain.SSENotificationData{
			NotificationID: n.ID,
			UserID:         n.UserID,
			Type:           n.Type,
			Priority:       n.Priority,
			Subject:        n.Subject,
			Content:        n.Content,
			CreatedAt:      n.CreatedAt,
			Metadata:       metadata,
			Unread:         n.Status != domain.NotificationStatusRead,
		})
	}

	// Send bulk event
	bulkEvent := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeBulk,
		Timestamp: time.Now(),
		UserID:    &userID,
		Data: domain.SSEBulkNotificationData{
			Notifications: notifData,
			TotalCount:    len(notifData),
			UnreadCount:   h.countUnread(notifications),
			HasMore:       hasMore,
		},
	}

	_ = client.Send(bulkEvent)
}

func (h *SSEHandler) countUnread(notifications []*domain.Notification) int {
	count := 0
	for _, n := range notifications {
		if n.Status != domain.NotificationStatusRead {
			count++
		}
	}
	return count
}

func (h *SSEHandler) parseCommaSeparated(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (h *SSEHandler) convertEventTypes(types []string) []domain.SSEEventType {
	result := make([]domain.SSEEventType, 0, len(types))
	for _, t := range types {
		result = append(result, domain.SSEEventType(t))
	}
	return result
}

func (h *SSEHandler) convertPriorities(priorities []string) []domain.NotificationPriority {
	result := make([]domain.NotificationPriority, 0, len(priorities))
	for _, p := range priorities {
		result = append(result, domain.NotificationPriority(p))
	}
	return result
}

func (h *SSEHandler) isAdmin(claims *identityService.Claims) bool {
	// Check if user has admin role
	for _, role := range claims.Roles {
		if role == roleAdmin || role == roleSystemAdmin {
			return true
		}
	}
	return false
}

// BroadcastRequest represents a broadcast request
type BroadcastRequest struct {
	Title   string      `json:"title" validate:"required"`
	Message string      `json:"message" validate:"required"`
	Type    string      `json:"type" validate:"required,oneof=info warning error maintenance"`
	UserIDs []uuid.UUID `json:"user_ids,omitempty"`
}

// fiber:context-methods migrated
