package streaming

import (
	"time"

	"github.com/google/uuid"
)

// MessageType represents the type of control message
type MessageType string

const (
	// Control messages
	MessageTypeSubscribe   MessageType = "subscribe"
	MessageTypeUnsubscribe MessageType = "unsubscribe"
	MessageTypePing        MessageType = "ping"
	MessageTypePong        MessageType = "pong"
	MessageTypeAck         MessageType = "ack"
	MessageTypeClose       MessageType = "close"
	MessageTypeAuth        MessageType = "auth"
	MessageTypeConfig      MessageType = "config"
)

// ControlMessage represents a control message between client and server
type ControlMessage struct {
	Type      MessageType            `json:"type"`
	ID        string                 `json:"id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// SubscribeMessage represents a subscription request
type SubscribeMessage struct {
	Channels   []string   `json:"channels"`
	EventTypes []string   `json:"event_types,omitempty"`
	Priorities []string   `json:"priorities,omitempty"`
	Since      *time.Time `json:"since,omitempty"` // Get missed events since timestamp
}

// UnsubscribeMessage represents an unsubscription request
type UnsubscribeMessage struct {
	Channels []string `json:"channels"`
}

// AckMessage represents an acknowledgment message
type AckMessage struct {
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// ConfigMessage represents client configuration
type ConfigMessage struct {
	BufferSize     int      `json:"buffer_size,omitempty"`
	EventTypes     []string `json:"event_types,omitempty"`
	Priorities     []string `json:"priorities,omitempty"`
	Locale         string   `json:"locale,omitempty"`
	Timezone       string   `json:"timezone,omitempty"`
	EnablePresence bool     `json:"enable_presence,omitempty"`
}

// AuthMessage represents authentication data
type AuthMessage struct {
	Token        string    `json:"token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"` //nolint:gosec // G117: auth message DTO field, intentional API design
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// Channel represents a subscription channel
type Channel struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // user, group, broadcast, system
	Subscribers map[uuid.UUID]bool     `json:"-"`
	CreatedAt   time.Time              `json:"created_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ChannelType defines different channel types
type ChannelType string

const (
	ChannelTypeUser      ChannelType = "user"      // User-specific channel
	ChannelTypeGroup     ChannelType = "group"     // Group/team channel
	ChannelTypeBroadcast ChannelType = "broadcast" // Broadcast to all
	ChannelTypeSystem    ChannelType = "system"    // System messages
	ChannelTypeTenant    ChannelType = "tenant"    // Tenant-specific
	ChannelTypeRole      ChannelType = "role"      // Role-based channel
)

// EventFilter defines filtering criteria for events
type EventFilter struct {
	EventTypes   []string               `json:"event_types,omitempty"`
	Priorities   []string               `json:"priorities,omitempty"`
	Channels     []string               `json:"channels,omitempty"`
	UserIDs      []uuid.UUID            `json:"user_ids,omitempty"`
	TenantIDs    []uuid.UUID            `json:"tenant_ids,omitempty"`
	MinTimestamp *time.Time             `json:"min_timestamp,omitempty"`
	MaxTimestamp *time.Time             `json:"max_timestamp,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Matches checks if an event matches the filter criteria
//
//nolint:gocyclo // filter matching requires multiple condition checks
func (f *EventFilter) Matches(eventType string, priority string, userID *uuid.UUID, tenantID *uuid.UUID, timestamp time.Time) bool {
	// Check event type filter
	if len(f.EventTypes) > 0 {
		found := false
		for _, et := range f.EventTypes {
			if et == eventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check priority filter
	if len(f.Priorities) > 0 && priority != "" {
		found := false
		for _, p := range f.Priorities {
			if p == priority {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check user ID filter
	if len(f.UserIDs) > 0 && userID != nil {
		found := false
		for _, uid := range f.UserIDs {
			if uid == *userID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check tenant ID filter
	if len(f.TenantIDs) > 0 && tenantID != nil {
		found := false
		for _, tid := range f.TenantIDs {
			if tid == *tenantID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check timestamp range
	if f.MinTimestamp != nil && timestamp.Before(*f.MinTimestamp) {
		return false
	}
	if f.MaxTimestamp != nil && timestamp.After(*f.MaxTimestamp) {
		return false
	}

	return true
}

// ConnectionState represents the state of a client connection
type ConnectionState string

const (
	ConnectionStateConnecting ConnectionState = "connecting"
	ConnectionStateConnected  ConnectionState = "connected"
	ConnectionStateReady      ConnectionState = "ready"
	ConnectionStateClosing    ConnectionState = "closing"
	ConnectionStateClosed     ConnectionState = "closed"
	ConnectionStateError      ConnectionState = "error"
)

// ConnectionInfo contains information about a connection
type ConnectionInfo struct {
	ClientID      uuid.UUID       `json:"client_id"`
	UserID        uuid.UUID       `json:"user_id"`
	State         ConnectionState `json:"state"`
	ConnectedAt   time.Time       `json:"connected_at"`
	LastActivity  time.Time       `json:"last_activity"`
	IPAddress     string          `json:"ip_address,omitempty"`
	UserAgent     string          `json:"user_agent,omitempty"`
	Subscriptions []string        `json:"subscriptions,omitempty"`
	MessageCount  uint64          `json:"message_count"`
	ErrorCount    uint64          `json:"error_count"`
}
