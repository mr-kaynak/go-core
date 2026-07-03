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
	// LastMessage is stored as a UnixNano int64 accessed via atomics so that
	// Send can update it without taking the mutex. Read/write it through
	// setLastMessage/GetLastMessage, never directly.
	lastMessageNano int64

	// Filtering and preferences
	EventTypes    []domain.SSEEventType
	Priorities    []domain.NotificationPriority
	Subscriptions map[string]bool // Channel subscriptions

	// Connection metadata
	UserAgent string
	IPAddress string
	DeviceID  string
	SessionID string

	// Statistics
	messagesSent     uint64
	messagesDropped  uint64
	bytesTransferred uint64

	// Options
	options ClientOptions

	// State management
	//
	// closed is an atomic flag toggled once by Close. done is closed by Close
	// to unblock any in-flight Send that is parked on a full channel. The
	// channel itself is deliberately never closed so that a blocking send can
	// run without the mutex and without risking a send-on-closed-channel panic.
	mu        sync.RWMutex
	ready     bool
	closed    atomic.Bool
	done      chan struct{}
	closeOnce sync.Once
}

// NewClient creates a new SSE client with default options
func NewClient(ctx context.Context, userID uuid.UUID) *Client {
	return NewClientWithOptions(ctx, userID, DefaultClientOptions())
}

// NewClientWithOptions creates a new SSE client with custom options
func NewClientWithOptions(ctx context.Context, userID uuid.UUID, opts ClientOptions) *Client {
	clientCtx, cancel := context.WithCancel(ctx)

	c := &Client{
		ID:            uuid.New(),
		UserID:        userID,
		Channel:       make(chan *domain.SSEEvent, opts.BufferSize),
		Context:       clientCtx,
		Cancel:        cancel,
		ConnectedAt:   time.Now(),
		LastPing:      time.Now(),
		Subscriptions: make(map[string]bool),
		options:       opts,
		done:          make(chan struct{}),
		ready:         true,
	}
	c.setLastMessage(time.Now())
	return c
}

// Send sends an event to the client.
//
// Send never holds the mutex across the blocking channel send: a slow consumer
// would otherwise serialize every other client operation (Close, GetLastPing,
// IsReady, heartbeat updates) behind it. The closed state is read via an atomic
// flag, and the select races the send against c.done (closed by Close), the send
// timeout, and the client context so a parked send is always unblocked promptly.
// Because Close never closes c.Channel, the send below can never panic.
func (c *Client) Send(event *domain.SSEEvent) error {
	if c.closed.Load() {
		atomic.AddUint64(&c.messagesDropped, 1)
		return ErrClientClosed
	}

	// Update last message time (atomic — no lock required).
	c.setLastMessage(time.Now())

	// Non-blocking send with timeout.
	timer := time.NewTimer(c.options.SendTimeout)
	defer timer.Stop()

	select {
	case c.Channel <- event:
		atomic.AddUint64(&c.messagesSent, 1)
		// Estimate bytes (simplified)
		atomic.AddUint64(&c.bytesTransferred, uint64(len(event.Format())))
		return nil

	case <-c.done:
		// Close happened while we were parked on a full channel.
		atomic.AddUint64(&c.messagesDropped, 1)
		return ErrClientClosed

	case <-timer.C:
		atomic.AddUint64(&c.messagesDropped, 1)
		return ErrSendTimeout

	case <-c.Context.Done():
		return c.Context.Err()
	}
}

// TrySend attempts to send an event without blocking
func (c *Client) TrySend(event *domain.SSEEvent) bool {
	if c.closed.Load() {
		return false
	}

	select {
	case c.Channel <- event:
		atomic.AddUint64(&c.messagesSent, 1)
		c.setLastMessage(time.Now())
		return true
	case <-c.done:
		return false
	default:
		atomic.AddUint64(&c.messagesDropped, 1)
		return false
	}
}

// Close closes the client connection.
//
// Close does not hold the mutex across any blocking operation and never closes
// c.Channel. It flips the atomic closed flag, closes the done channel to
// unblock any in-flight Send, and cancels the context so the consumer loop
// exits. Not closing c.Channel is what makes the lock-free Send race-safe: a
// concurrent send can never hit a closed channel. Pending buffered events are
// abandoned; callers that need them drain via Drain before Close.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)

		c.mu.Lock()
		c.ready = false
		c.mu.Unlock()

		c.Cancel()
	})

	return nil
}

// IsClosed returns whether the client is closed
func (c *Client) IsClosed() bool {
	return c.closed.Load()
}

// IsReady returns whether the client is ready to receive messages
func (c *Client) IsReady() bool {
	if c.closed.Load() {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
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

// setLastMessage stores the last message time atomically.
func (c *Client) setLastMessage(t time.Time) {
	atomic.StoreInt64(&c.lastMessageNano, t.UnixNano())
}

// GetLastPing returns the last ping time
func (c *Client) GetLastPing() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LastPing
}

// GetLastMessage returns the last message time
func (c *Client) GetLastMessage() time.Time {
	return time.Unix(0, atomic.LoadInt64(&c.lastMessageNano))
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
	c.mu.RLock()
	subscriptions := len(c.Subscriptions)
	lastPing := c.LastPing
	ready := c.ready
	c.mu.RUnlock()

	lastMessage := c.GetLastMessage()
	isReady := ready && !c.closed.Load()

	return ClientStats{
		ClientID:         c.ID,
		UserID:           c.UserID,
		ConnectedAt:      c.ConnectedAt,
		LastPing:         lastPing,
		LastMessage:      lastMessage,
		MessagesSent:     atomic.LoadUint64(&c.messagesSent),
		MessagesDropped:  atomic.LoadUint64(&c.messagesDropped),
		BytesTransferred: atomic.LoadUint64(&c.bytesTransferred),
		BufferSize:       len(c.Channel),
		BufferCapacity:   c.options.BufferSize,
		Subscriptions:    subscriptions,
		IsReady:          isReady,
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
