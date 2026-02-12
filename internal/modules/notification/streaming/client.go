package streaming

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

var (
	// ErrClientClosed is returned when trying to send to a closed client
	ErrClientClosed = errors.New("client connection is closed")
	// ErrSendTimeout is returned when send operation times out
	ErrSendTimeout = errors.New("send operation timed out")
	// ErrBufferFull is returned when client buffer is full
	ErrBufferFull = errors.New("client buffer is full")
)

// ClientOptions contains options for creating a new client
type ClientOptions struct {
	BufferSize     int
	SendTimeout    time.Duration
	MaxMessageSize int
	EnableMetrics  bool
}

// DefaultClientOptions returns default client options
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		BufferSize:     100,
		SendTimeout:    5 * time.Second,
		MaxMessageSize: 1024 * 1024, // 1MB
		EnableMetrics:  true,
	}
}

// Client represents a single SSE client connection
type Client struct {
	// Identification
	ID       uuid.UUID
	UserID   uuid.UUID
	TenantID *uuid.UUID // Multi-tenant support

	// Connection management
	Channel chan *domain.SSEEvent
	Context context.Context
	Cancel  context.CancelFunc

	// Timestamps
	ConnectedAt time.Time
	LastPing    time.Time
	LastMessage time.Time

	// Filtering and preferences
	EventTypes    []domain.SSEEventType
	Priorities    []domain.NotificationPriority
	Subscriptions map[string]bool // Channel subscriptions

	// Connection metadata
	UserAgent string
	IPAddress string
	DeviceID  string
	SessionID string
	AuthToken string

	// Statistics
	messagesSent     uint64
	messagesDropped  uint64
	bytesTransferred uint64

	// Options
	options ClientOptions

	// State management
	mu     sync.RWMutex
	closed bool
	ready  bool
}

// NewClient creates a new SSE client with default options
func NewClient(ctx context.Context, userID uuid.UUID) *Client {
	return NewClientWithOptions(ctx, userID, DefaultClientOptions())
}

// NewClientWithOptions creates a new SSE client with custom options
func NewClientWithOptions(ctx context.Context, userID uuid.UUID, opts ClientOptions) *Client {
	clientCtx, cancel := context.WithCancel(ctx)

	return &Client{
		ID:            uuid.New(),
		UserID:        userID,
		Channel:       make(chan *domain.SSEEvent, opts.BufferSize),
		Context:       clientCtx,
		Cancel:        cancel,
		ConnectedAt:   time.Now(),
		LastPing:      time.Now(),
		LastMessage:   time.Now(),
		Subscriptions: make(map[string]bool),
		options:       opts,
		closed:        false,
		ready:         true,
	}
}

// Send sends an event to the client
func (c *Client) Send(event *domain.SSEEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		atomic.AddUint64(&c.messagesDropped, 1)
		return ErrClientClosed
	}

	// Update last message time
	c.updateLastMessage()

	// Non-blocking send with timeout
	timer := time.NewTimer(c.options.SendTimeout)
	defer timer.Stop()

	select {
	case c.Channel <- event:
		atomic.AddUint64(&c.messagesSent, 1)
		// Estimate bytes (simplified)
		atomic.AddUint64(&c.bytesTransferred, uint64(len(event.Format())))
		return nil

	case <-timer.C:
		atomic.AddUint64(&c.messagesDropped, 1)
		return ErrSendTimeout

	case <-c.Context.Done():
		return c.Context.Err()
	}
}

// TrySend attempts to send an event without blocking
func (c *Client) TrySend(event *domain.SSEEvent) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return false
	}

	select {
	case c.Channel <- event:
		atomic.AddUint64(&c.messagesSent, 1)
		c.updateLastMessage()
		return true
	default:
		atomic.AddUint64(&c.messagesDropped, 1)
		return false
	}
}

// Close closes the client connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.ready = false
	c.Cancel()

	// Don't close channel immediately to allow draining
	go func() {
		time.Sleep(100 * time.Millisecond) // Allow time for final messages
		close(c.Channel)
	}()

	return nil
}

// IsClosed returns whether the client is closed
func (c *Client) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// IsReady returns whether the client is ready to receive messages
func (c *Client) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready && !c.closed
}

// SetReady sets the client ready state
func (c *Client) SetReady(ready bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = ready
}

// UpdatePing updates the last ping time
func (c *Client) UpdatePing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastPing = time.Now()
}

// updateLastMessage updates the last message time
func (c *Client) updateLastMessage() {
	c.LastMessage = time.Now()
}

// GetLastPing returns the last ping time
func (c *Client) GetLastPing() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LastPing
}

// GetLastMessage returns the last message time
func (c *Client) GetLastMessage() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LastMessage
}

// IsIdle checks if the client has been idle for the given duration
func (c *Client) IsIdle(duration time.Duration) bool {
	return time.Since(c.GetLastPing()) > duration
}

// ShouldReceiveEvent checks if client should receive this event
func (c *Client) ShouldReceiveEvent(event *domain.SSEEvent) bool {
	// Check if event is targeted to specific user
	if event.UserID != nil && *event.UserID != c.UserID {
		return false
	}

	// Check tenant ID in multi-tenant setup
	if event.TenantID != nil && c.TenantID != nil && *event.TenantID != *c.TenantID {
		return false
	}

	// Filter by event type if specified
	if len(c.EventTypes) > 0 {
		found := false
		for _, et := range c.EventTypes {
			if et == event.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check priority filter for notification events
	if event.Type == domain.SSEEventTypeNotification && len(c.Priorities) > 0 {
		if notifData, ok := event.Data.(domain.SSENotificationData); ok {
			found := false
			for _, p := range c.Priorities {
				if p == notifData.Priority {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// Subscribe adds a channel subscription
func (c *Client) Subscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Subscriptions[channel] = true
}

// Unsubscribe removes a channel subscription
func (c *Client) Unsubscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Subscriptions, channel)
}

// IsSubscribed checks if client is subscribed to a channel
func (c *Client) IsSubscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Subscriptions[channel]
}

// GetSubscriptions returns all channel subscriptions
func (c *Client) GetSubscriptions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	channels := make([]string, 0, len(c.Subscriptions))
	for channel := range c.Subscriptions {
		channels = append(channels, channel)
	}
	return channels
}

// SetEventTypes sets the event type filter
func (c *Client) SetEventTypes(types []domain.SSEEventType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.EventTypes = types
}

// SetPriorities sets the priority filter
func (c *Client) SetPriorities(priorities []domain.NotificationPriority) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Priorities = priorities
}

// GetStats returns client statistics
func (c *Client) GetStats() ClientStats {
	return ClientStats{
		ClientID:         c.ID,
		UserID:           c.UserID,
		ConnectedAt:      c.ConnectedAt,
		LastPing:         c.GetLastPing(),
		LastMessage:      c.GetLastMessage(),
		MessagesSent:     atomic.LoadUint64(&c.messagesSent),
		MessagesDropped:  atomic.LoadUint64(&c.messagesDropped),
		BytesTransferred: atomic.LoadUint64(&c.bytesTransferred),
		BufferSize:       len(c.Channel),
		BufferCapacity:   c.options.BufferSize,
		Subscriptions:    len(c.Subscriptions),
		IsReady:          c.IsReady(),
	}
}

// ClientStats contains client statistics
type ClientStats struct {
	ClientID         uuid.UUID `json:"client_id"`
	UserID           uuid.UUID `json:"user_id"`
	ConnectedAt      time.Time `json:"connected_at"`
	LastPing         time.Time `json:"last_ping"`
	LastMessage      time.Time `json:"last_message"`
	MessagesSent     uint64    `json:"messages_sent"`
	MessagesDropped  uint64    `json:"messages_dropped"`
	BytesTransferred uint64    `json:"bytes_transferred"`
	BufferSize       int       `json:"buffer_size"`
	BufferCapacity   int       `json:"buffer_capacity"`
	Subscriptions    int       `json:"subscriptions"`
	IsReady          bool      `json:"is_ready"`
}

// Drain drains all pending messages from the channel
func (c *Client) Drain() []*domain.SSEEvent {
	var events []*domain.SSEEvent

	for {
		select {
		case event := <-c.Channel:
			if event != nil {
				events = append(events, event)
			}
		default:
			return events
		}
	}
}

// Flush sends all pending messages and waits for completion
func (c *Client) Flush(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for len(c.Channel) > 0 {
		select {
		case <-timer.C:
			return ErrSendTimeout
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// String returns a string representation of the client
func (c *Client) String() string {
	return c.ID.String()
}
