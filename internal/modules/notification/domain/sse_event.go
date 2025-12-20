package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SSEEventType defines types of SSE events
type SSEEventType string

const (
	SSEEventTypeNotification   SSEEventType = "notification"
	SSEEventTypeHeartbeat      SSEEventType = "heartbeat"
	SSEEventTypeConnectionInfo SSEEventType = "connection_info"
	SSEEventTypeError          SSEEventType = "error"
	SSEEventTypeSystemMessage  SSEEventType = "system_message"
	SSEEventTypePresence       SSEEventType = "presence"
	SSEEventTypeBulk           SSEEventType = "bulk_notification"
)

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	ID        string       `json:"id"`
	Type      SSEEventType `json:"type"`
	Data      interface{}  `json:"data"`
	Timestamp time.Time    `json:"timestamp"`
	Retry     int          `json:"retry,omitempty"`     // Milliseconds before reconnect
	UserID    *uuid.UUID   `json:"user_id,omitempty"`   // Target user (nil = broadcast)
	TenantID  *uuid.UUID   `json:"tenant_id,omitempty"` // Multi-tenant support
}

// SSENotificationData represents notification-specific event data
type SSENotificationData struct {
	NotificationID uuid.UUID              `json:"notification_id"`
	UserID         uuid.UUID              `json:"user_id"`
	Type           NotificationType       `json:"type"`
	Priority       NotificationPriority   `json:"priority"`
	Subject        string                 `json:"subject"`
	Content        string                 `json:"content"`
	CreatedAt      time.Time              `json:"created_at"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	ActionURL      string                 `json:"action_url,omitempty"`
	Icon           string                 `json:"icon,omitempty"`
	Unread         bool                   `json:"unread"`
}

// SSEConnectionInfo represents connection establishment data
type SSEConnectionInfo struct {
	ClientID      uuid.UUID `json:"client_id"`
	UserID        uuid.UUID `json:"user_id"`
	ConnectedAt   time.Time `json:"connected_at"`
	ServerTime    time.Time `json:"server_time"`
	ServerVersion string    `json:"server_version"`
	Features      []string  `json:"features"` // Available features for this connection
}

// SSEHeartbeat represents heartbeat event data
type SSEHeartbeat struct {
	Timestamp   time.Time `json:"timestamp"`
	ServerTime  int64     `json:"server_time"` // Unix timestamp
	ServerID    string    `json:"server_id,omitempty"`
	Sequence    int64     `json:"sequence,omitempty"` // Sequence number for ordering
	HealthCheck bool      `json:"health_check"`
}

// SSEErrorData represents error event data
type SSEErrorData struct {
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	Details     string    `json:"details,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Recoverable bool      `json:"recoverable"`
	RetryAfter  int       `json:"retry_after,omitempty"` // Seconds to wait before retry
}

// SSESystemMessage represents system-wide messages
type SSESystemMessage struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	Type        string    `json:"type"` // info, warning, error, maintenance
	StartTime   time.Time `json:"start_time,omitempty"`
	EndTime     time.Time `json:"end_time,omitempty"`
	AffectsUser bool      `json:"affects_user"`
}

// SSEPresenceData represents user presence information
type SSEPresenceData struct {
	UserID    uuid.UUID `json:"user_id"`
	Status    string    `json:"status"` // online, away, busy, offline
	LastSeen  time.Time `json:"last_seen,omitempty"`
	Device    string    `json:"device,omitempty"`
	Location  string    `json:"location,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SSEBulkNotificationData represents multiple notifications in one event
type SSEBulkNotificationData struct {
	Notifications []SSENotificationData `json:"notifications"`
	TotalCount    int                   `json:"total_count"`
	UnreadCount   int                   `json:"unread_count"`
	HasMore       bool                  `json:"has_more"`
}

// Format formats the SSE event according to SSE protocol
// Returns byte array ready to write to response writer
func (e *SSEEvent) Format() []byte {
	var buf bytes.Buffer

	// Write event ID if present
	if e.ID != "" {
		buf.WriteString(fmt.Sprintf("id: %s\n", e.ID))
	}

	// Write event type if present
	if e.Type != "" {
		buf.WriteString(fmt.Sprintf("event: %s\n", string(e.Type)))
	}

	// Write retry interval if present
	if e.Retry > 0 {
		buf.WriteString(fmt.Sprintf("retry: %d\n", e.Retry))
	}

	// Marshal data to JSON
	data, err := json.Marshal(e.Data)
	if err != nil {
		// If marshaling fails, send error data
		errorData := SSEErrorData{
			Code:        "MARSHAL_ERROR",
			Message:     "Failed to marshal event data",
			Details:     err.Error(),
			Timestamp:   time.Now(),
			Recoverable: true,
		}
		data, _ = json.Marshal(errorData)
		buf.WriteString(fmt.Sprintf("event: %s\n", SSEEventTypeError))
	}

	// Write data field
	buf.WriteString(fmt.Sprintf("data: %s\n", string(data)))

	// End with double newline
	buf.WriteString("\n")

	return buf.Bytes()
}

// NewSSENotificationEvent creates a new notification SSE event
func NewSSENotificationEvent(notification *Notification) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeNotification,
		Timestamp: time.Now(),
		UserID:    &notification.UserID,
		Data: SSENotificationData{
			NotificationID: notification.ID,
			UserID:         notification.UserID,
			Type:           notification.Type,
			Priority:       notification.Priority,
			Subject:        notification.Subject,
			Content:        notification.Body,
			CreatedAt:      notification.CreatedAt,
			Metadata:       notification.Data,
			Unread:         notification.Status != NotificationStatusRead,
		},
	}
}

// NewSSEHeartbeatEvent creates a new heartbeat SSE event
func NewSSEHeartbeatEvent(sequence int64, serverID string) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeHeartbeat,
		Timestamp: time.Now(),
		Retry:     5000, // Retry after 5 seconds
		Data: SSEHeartbeat{
			Timestamp:   time.Now(),
			ServerTime:  time.Now().Unix(),
			ServerID:    serverID,
			Sequence:    sequence,
			HealthCheck: true,
		},
	}
}

// NewSSEConnectionInfoEvent creates a new connection info event
func NewSSEConnectionInfoEvent(clientID, userID uuid.UUID, serverVersion string) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeConnectionInfo,
		Timestamp: time.Now(),
		UserID:    &userID,
		Data: SSEConnectionInfo{
			ClientID:      clientID,
			UserID:        userID,
			ConnectedAt:   time.Now(),
			ServerTime:    time.Now(),
			ServerVersion: serverVersion,
			Features: []string{
				"notification",
				"heartbeat",
				"presence",
				"bulk_notification",
				"system_message",
			},
		},
	}
}

// NewSSEErrorEvent creates a new error event
func NewSSEErrorEvent(code, message, details string, recoverable bool) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeError,
		Timestamp: time.Now(),
		Data: SSEErrorData{
			Code:        code,
			Message:     message,
			Details:     details,
			Timestamp:   time.Now(),
			Recoverable: recoverable,
			RetryAfter:  5, // Default 5 seconds
		},
	}
}

// NewSSESystemMessageEvent creates a new system message event
func NewSSESystemMessageEvent(title, message, msgType string) *SSEEvent {
	return &SSEEvent{
		ID:        uuid.New().String(),
		Type:      SSEEventTypeSystemMessage,
		Timestamp: time.Now(),
		Data: SSESystemMessage{
			ID:          uuid.New(),
			Title:       title,
			Message:     message,
			Type:        msgType,
			AffectsUser: false,
		},
	}
}

// IsValid validates the SSE event
func (e *SSEEvent) IsValid() bool {
	return e.Type != "" && e.Data != nil
}

// ShouldRetry determines if client should retry connection
func (e *SSEEvent) ShouldRetry() bool {
	if e.Type != SSEEventTypeError {
		return true
	}

	// Check error data
	if errorData, ok := e.Data.(SSEErrorData); ok {
		return errorData.Recoverable
	}

	return true
}