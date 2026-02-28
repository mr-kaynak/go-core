package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

// SSEConfig contains all SSE-related configuration
type SSEConfig struct {
	Enabled             bool                    `yaml:"enabled" env:"SSE_ENABLED" default:"true"`
	ServerID            string                  `yaml:"server_id" env:"SSE_SERVER_ID"`
	Connection          ConnectionManagerConfig `yaml:"connection"`
	Broadcaster         BroadcasterConfig       `yaml:"broadcaster"`
	Heartbeat           HeartbeatConfig         `yaml:"heartbeat"`
	EnableRedis         bool                    `yaml:"enable_redis" env:"SSE_ENABLE_REDIS" default:"false"`
	RedisChannel        string                  `yaml:"redis_channel" env:"SSE_REDIS_CHANNEL" default:"notifications:sse"`
	EnableMetrics       bool                    `yaml:"enable_metrics" env:"SSE_ENABLE_METRICS" default:"true"`
	MetricsPushInterval time.Duration           `yaml:"metrics_push_interval" env:"SSE_METRICS_PUSH_INTERVAL" default:"5s"`
	CleanupOnStart      bool                    `yaml:"cleanup_on_start" env:"SSE_CLEANUP_ON_START" default:"true"`
}

// SSERedisBridge is an interface for the Redis pub/sub bridge.
// Defined here to avoid import cycles with the cache package.
type SSERedisBridge interface {
	Publish(ctx context.Context, event *domain.SSEEvent) error
	OnEvent(handler func(event *domain.SSEEvent))
	Subscribe(ctx context.Context) error
	Stop()
}

// SSEService is the main service for Server-Sent Events
type SSEService struct {
	// Core components
	connManager *ConnectionManager
	broadcaster *EventBroadcaster
	heartbeat   *HeartbeatManager

	// Configuration
	config   SSEConfig
	serverID string

	// Dependencies
	logger      *logger.Logger
	redisBridge SSERedisBridge

	// State
	started bool
	mu      sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSSEService creates a new SSE service
func NewSSEService(cfg *config.Config) (*SSEService, error) {
	// Parse SSE configuration
	sseConfig := SSEConfig{
		Enabled: cfg.GetBool("sse.enabled"),
		Connection: ConnectionManagerConfig{
			MaxConnections:        cfg.GetInt("sse.max_connections"),
			MaxConnectionsPerUser: cfg.GetInt("sse.max_connections_per_user"),
			IdleTimeout:           cfg.GetDuration("sse.idle_timeout"),
			CleanupInterval:       cfg.GetDuration("sse.cleanup_interval"),
			EnableMetrics:         cfg.GetBool("sse.enable_metrics"),
		},
		Broadcaster: BroadcasterConfig{
			MaxWorkers:     cfg.GetInt("sse.broadcast_workers"),
			QueueSize:      cfg.GetInt("sse.broadcast_queue_size"),
			MaxRetries:     cfg.GetInt("sse.broadcast_max_retries"),
			RetryDelay:     cfg.GetDuration("sse.broadcast_retry_delay"),
			ProcessTimeout: cfg.GetDuration("sse.broadcast_process_timeout"),
			EnableBatching: cfg.GetBool("sse.broadcast_enable_batching"),
			BatchSize:      cfg.GetInt("sse.broadcast_batch_size"),
			BatchInterval:  cfg.GetDuration("sse.broadcast_batch_interval"),
		},
		Heartbeat: HeartbeatConfig{
			Interval:      cfg.GetDuration("sse.heartbeat_interval"),
			Timeout:       cfg.GetDuration("sse.heartbeat_timeout"),
			EnableMetrics: cfg.GetBool("sse.heartbeat_enable_metrics"),
			ServerID:      cfg.GetString("sse.server_id"),
		},
		EnableRedis:         cfg.GetBool("sse.enable_redis"),
		RedisChannel:        cfg.GetString("sse.redis_channel"),
		EnableMetrics:       cfg.GetBool("sse.enable_metrics"),
		MetricsPushInterval: cfg.GetDuration("sse.metrics_push_interval"),
		CleanupOnStart:      cfg.GetBool("sse.cleanup_on_start"),
	}

	// Set defaults if not configured
	if sseConfig.Connection.MaxConnections == 0 {
		sseConfig.Connection.MaxConnections = 10000
	}
	if sseConfig.Connection.MaxConnectionsPerUser == 0 {
		sseConfig.Connection.MaxConnectionsPerUser = 5
	}
	if sseConfig.Connection.IdleTimeout == 0 {
		sseConfig.Connection.IdleTimeout = 5 * time.Minute
	}
	if sseConfig.Connection.CleanupInterval == 0 {
		sseConfig.Connection.CleanupInterval = 1 * time.Minute
	}
	if sseConfig.Broadcaster.MaxWorkers == 0 {
		sseConfig.Broadcaster.MaxWorkers = 10
	}
	if sseConfig.Broadcaster.QueueSize == 0 {
		sseConfig.Broadcaster.QueueSize = 1000
	}
	if sseConfig.Broadcaster.ProcessTimeout == 0 {
		sseConfig.Broadcaster.ProcessTimeout = 30 * time.Second
	}
	if sseConfig.MetricsPushInterval == 0 {
		sseConfig.MetricsPushInterval = 5 * time.Second
	}
	if sseConfig.Heartbeat.Interval == 0 {
		sseConfig.Heartbeat.Interval = 30 * time.Second
	}
	if sseConfig.Heartbeat.Timeout == 0 {
		sseConfig.Heartbeat.Timeout = 60 * time.Second
	}

	// Generate server ID if not provided
	serverID := sseConfig.ServerID
	if serverID == "" {
		serverID = fmt.Sprintf("sse-%s", uuid.New().String()[:8])
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create core components
	connManager := NewConnectionManager(sseConfig.Connection)
	broadcaster := NewEventBroadcaster(connManager, sseConfig.Broadcaster)
	heartbeat := NewHeartbeatManager(connManager, broadcaster, sseConfig.Heartbeat)

	svc := &SSEService{
		connManager: connManager,
		broadcaster: broadcaster,
		heartbeat:   heartbeat,
		config:      sseConfig,
		serverID:    serverID,
		logger:      logger.Get().WithField("component", "sse_service"),
		ctx:         ctx,
		cancel:      cancel,
	}

	return svc, nil
}

// SetRedisBridge sets the Redis pub/sub bridge for cross-instance SSE broadcasting.
// Must be called before Start().
func (s *SSEService) SetRedisBridge(bridge SSERedisBridge) {
	s.redisBridge = bridge
}

// Start starts the SSE service
func (s *SSEService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	if !s.config.Enabled {
		s.logger.Info("SSE service is disabled")
		return nil
	}

	s.logger.Info("Starting SSE service",
		"server_id", s.serverID,
		"max_connections", s.config.Connection.MaxConnections,
		"max_per_user", s.config.Connection.MaxConnectionsPerUser,
		"broadcast_workers", s.config.Broadcaster.MaxWorkers,
		"heartbeat_interval", s.config.Heartbeat.Interval,
	)

	// Start heartbeat manager
	if err := s.heartbeat.Start(); err != nil {
		return fmt.Errorf("failed to start heartbeat manager: %w", err)
	}

	// Start Redis bridge if configured
	if s.redisBridge != nil {
		s.redisBridge.OnEvent(func(event *domain.SSEEvent) {
			// Handle events from other servers — broadcast locally
			if event.UserID != nil {
				_ = s.broadcaster.BroadcastToUser(s.ctx, *event.UserID, event)
			} else {
				_ = s.broadcaster.BroadcastToAll(s.ctx, event)
			}
		})
		if err := s.redisBridge.Subscribe(s.ctx); err != nil {
			s.logger.Error("Failed to start SSE Redis bridge", "error", err)
			// Non-fatal: continue without cross-instance broadcasting
		}
	}

	s.started = true

	// Start periodic metrics push to admin:metrics channel
	go s.startMetricsPush()

	s.logger.Info("SSE service started successfully")
	return nil
}

// Stop stops the SSE service
func (s *SSEService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.logger.Info("Stopping SSE service")

	// Stop accepting new connections
	s.started = false

	// Stop Redis bridge
	if s.redisBridge != nil {
		s.redisBridge.Stop()
	}

	// Stop heartbeat
	s.heartbeat.Stop()

	// Shutdown broadcaster
	if err := s.broadcaster.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to shutdown broadcaster", "error", err)
	}

	// Shutdown connection manager
	if err := s.connManager.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to shutdown connection manager", "error", err)
	}

	// Cancel context
	s.cancel()

	s.logger.Info("SSE service stopped")
	return nil
}

// RegisterClient registers a new SSE client
func (s *SSEService) RegisterClient(client *streaming.Client) error {
	if !s.isRunning() {
		return errors.NewServiceUnavailable("SSE service is not running")
	}

	return s.connManager.Register(client)
}

// UnregisterClient unregisters an SSE client
func (s *SSEService) UnregisterClient(clientID uuid.UUID) error {
	return s.connManager.Unregister(clientID)
}

// GetClient returns a specific client
func (s *SSEService) GetClient(clientID uuid.UUID) (*streaming.Client, error) {
	return s.connManager.GetClient(clientID)
}

// DisconnectClient forcefully disconnects a client
func (s *SSEService) DisconnectClient(clientID uuid.UUID) error {
	return s.connManager.Unregister(clientID)
}

// BroadcastToUser broadcasts an event to a specific user
func (s *SSEService) BroadcastToUser(ctx context.Context, userID uuid.UUID, event *domain.SSEEvent) error {
	if !s.isRunning() {
		return errors.NewServiceUnavailable("SSE service is not running")
	}

	// Publish to Redis for cross-instance broadcasting
	if s.redisBridge != nil && s.config.EnableRedis {
		if err := s.redisBridge.Publish(ctx, event); err != nil {
			s.logger.Debug("Failed to publish SSE event to Redis", "error", err)
		}
	}

	return s.broadcaster.BroadcastToUser(ctx, userID, event)
}

// BroadcastToUsers broadcasts an event to multiple users
func (s *SSEService) BroadcastToUsers(ctx context.Context, userIDs []uuid.UUID, event *domain.SSEEvent) error {
	if !s.isRunning() {
		return errors.NewServiceUnavailable("SSE service is not running")
	}

	return s.broadcaster.BroadcastToUsers(ctx, userIDs, event)
}

// BroadcastToAll broadcasts an event to all connected clients
func (s *SSEService) BroadcastToAll(ctx context.Context, event *domain.SSEEvent) error {
	if !s.isRunning() {
		return errors.NewServiceUnavailable("SSE service is not running")
	}

	// Publish to Redis for cross-instance broadcasting
	if s.redisBridge != nil && s.config.EnableRedis {
		if err := s.redisBridge.Publish(ctx, event); err != nil {
			s.logger.Debug("Failed to publish SSE event to Redis", "error", err)
		}
	}

	return s.broadcaster.BroadcastToAll(ctx, event)
}

// BroadcastToChannel broadcasts an event to all clients subscribed to a channel
func (s *SSEService) BroadcastToChannel(ctx context.Context, channel string, event *domain.SSEEvent) error {
	if !s.isRunning() {
		return errors.NewServiceUnavailable("SSE service is not running")
	}

	// Get clients subscribed to the channel
	clients := s.connManager.GetClientsByFilter(func(client *streaming.Client) bool {
		return client.IsSubscribed(channel)
	})

	// Send event to each client
	for _, client := range clients {
		if err := client.Send(event); err != nil {
			s.logger.Debug("Failed to send to client",
				"client_id", client.ID,
				"channel", channel,
				"error", err,
			)
		}
	}

	return nil
}

// SendNotificationEvent sends a notification as an SSE event
func (s *SSEService) SendNotificationEvent(notification *domain.Notification) error {
	if !s.isRunning() {
		return nil // Silently ignore if service is not running
	}

	// Create SSE event from notification
	event := domain.NewSSENotificationEvent(notification)

	// Broadcast to the user
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	return s.broadcaster.BroadcastToUser(ctx, notification.UserID, event)
}

// SubscribeUserToChannels subscribes a user's clients to channels
func (s *SSEService) SubscribeUserToChannels(userID uuid.UUID, channels []string) int {
	clients := s.connManager.GetUserClients(userID)
	subscribed := 0

	for _, client := range clients {
		for _, channel := range channels {
			client.Subscribe(channel)
			subscribed++
		}
	}

	return subscribed
}

// UnsubscribeUserFromChannels unsubscribes a user's clients from channels
func (s *SSEService) UnsubscribeUserFromChannels(userID uuid.UUID, channels []string) int {
	clients := s.connManager.GetUserClients(userID)
	unsubscribed := 0

	for _, client := range clients {
		for _, channel := range channels {
			client.Unsubscribe(channel)
			unsubscribed++
		}
	}

	return unsubscribed
}

// ProcessAcknowledgment processes an event acknowledgment
func (s *SSEService) ProcessAcknowledgment(userID uuid.UUID, eventID string) {
	// Log acknowledgment
	s.logger.Debug("Event acknowledged",
		"user_id", userID,
		"event_id", eventID,
	)

	// Could track acknowledgments for analytics or retry logic
}

// GetUserConnections returns connection info for a user
func (s *SSEService) GetUserConnections(userID uuid.UUID) []streaming.ConnectionInfo {
	clients := s.connManager.GetUserClients(userID)
	connections := make([]streaming.ConnectionInfo, 0, len(clients))

	for _, client := range clients {
		stats := client.GetStats()
		connections = append(connections, streaming.ConnectionInfo{
			ClientID:      client.ID,
			UserID:        client.UserID,
			State:         s.getClientState(client),
			ConnectedAt:   client.ConnectedAt,
			LastActivity:  client.GetLastMessage(),
			IPAddress:     client.IPAddress,
			UserAgent:     client.UserAgent,
			Subscriptions: client.GetSubscriptions(),
			MessageCount:  stats.MessagesSent,
			ErrorCount:    stats.MessagesDropped,
		})
	}

	return connections
}

// GetAllConnections returns all active connections
func (s *SSEService) GetAllConnections(limit int) []streaming.ConnectionInfo {
	clients := s.connManager.GetAllClients()
	if limit > 0 && len(clients) > limit {
		clients = clients[:limit]
	}

	connections := make([]streaming.ConnectionInfo, 0, len(clients))
	for _, client := range clients {
		stats := client.GetStats()
		connections = append(connections, streaming.ConnectionInfo{
			ClientID:      client.ID,
			UserID:        client.UserID,
			State:         s.getClientState(client),
			ConnectedAt:   client.ConnectedAt,
			LastActivity:  client.GetLastMessage(),
			IPAddress:     client.IPAddress,
			UserAgent:     client.UserAgent,
			Subscriptions: client.GetSubscriptions(),
			MessageCount:  stats.MessagesSent,
			ErrorCount:    stats.MessagesDropped,
		})
	}

	return connections
}

// GetStats returns SSE service statistics
func (s *SSEService) GetStats() map[string]interface{} {
	connStats := s.connManager.GetStats()
	broadcastStats := s.broadcaster.GetStats()
	heartbeatStats := s.heartbeat.GetStats()

	return map[string]interface{}{
		"server_id": s.serverID,
		"started":   s.started,
		"uptime":    connStats.Uptime,
		"connection_manager": map[string]interface{}{
			"total_connections":        connStats.TotalConnections,
			"active_connections":       connStats.ActiveConnections,
			"unique_users":             connStats.UniqueUsers,
			"total_connections_made":   connStats.TotalConnectionsMade,
			"total_disconnects":        connStats.TotalDisconnects,
			"total_messages_sent":      connStats.TotalMessagesSent,
			"total_bytes_transferred":  connStats.TotalBytesTransferred,
			"max_connections":          connStats.MaxConnections,
			"max_connections_per_user": connStats.MaxConnectionsPerUser,
			"average_connection_time":  connStats.AverageConnectionTime,
		},
		"event_broadcaster": map[string]interface{}{
			"total_broadcasts":    broadcastStats.TotalBroadcasts,
			"successful_sends":    broadcastStats.SuccessfulSends,
			"failed_sends":        broadcastStats.FailedSends,
			"dropped_events":      broadcastStats.DroppedEvents,
			"queued_jobs":         broadcastStats.QueuedJobs,
			"processing_jobs":     broadcastStats.ProcessingJobs,
			"active_workers":      broadcastStats.ActiveWorkers,
			"max_workers":         broadcastStats.MaxWorkers,
			"queue_size":          broadcastStats.QueueSize,
			"queue_capacity":      broadcastStats.QueueCapacity,
			"priority_queue_size": broadcastStats.PriorityQueueSize,
			"average_latency":     broadcastStats.AverageLatency,
		},
		"heartbeat_manager": map[string]interface{}{
			"sequence":          heartbeatStats.Sequence,
			"heartbeats_sent":   heartbeatStats.HeartbeatsSent,
			"heartbeats_failed": heartbeatStats.HeartbeatsFailed,
			"last_heartbeat":    heartbeatStats.LastHeartbeat,
			"interval":          heartbeatStats.Interval,
			"is_healthy":        heartbeatStats.IsHealthy,
		},
		"configuration": map[string]interface{}{
			"enabled":        s.config.Enabled,
			"enable_redis":   s.config.EnableRedis,
			"enable_metrics": s.config.EnableMetrics,
			"redis_channel":  s.config.RedisChannel,
		},
	}
}

// GetServerID returns the server ID
func (s *SSEService) GetServerID() string {
	return s.serverID
}

// IsHealthy checks if the SSE service is healthy
func (s *SSEService) IsHealthy() bool {
	if !s.isRunning() {
		return false
	}

	// Check heartbeat health
	if !s.heartbeat.IsHealthy() {
		return false
	}

	// Check if we can accept connections
	stats := s.connManager.GetStats()
	return stats.TotalConnections < stats.MaxConnections
}

// Helper methods

func (s *SSEService) isRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started && s.config.Enabled
}

func (s *SSEService) startMetricsPush() {
	ticker := time.NewTicker(s.config.MetricsPushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !s.isRunning() {
				return
			}

			connStats := s.connManager.GetStats()
			broadcastStats := s.broadcaster.GetStats()

			event := domain.NewSSEMetricsEvent(domain.SSEMetricsData{
				ServerID:          s.serverID,
				Timestamp:         time.Now(),
				ActiveConnections: connStats.ActiveConnections,
				UniqueUsers:       connStats.UniqueUsers,
				TotalMessagesSent: connStats.TotalMessagesSent,
				TotalBroadcasts:   broadcastStats.TotalBroadcasts,
				SuccessfulSends:   broadcastStats.SuccessfulSends,
				FailedSends:       broadcastStats.FailedSends,
				DroppedEvents:     broadcastStats.DroppedEvents,
				ActiveWorkers:     broadcastStats.ActiveWorkers,
				QueuedJobs:        broadcastStats.QueuedJobs,
				Uptime:            connStats.Uptime,
				IsHealthy:         s.heartbeat.IsHealthy(),
			})

			if err := s.BroadcastToChannel(s.ctx, "admin:metrics", event); err != nil {
				s.logger.Debug("Failed to push metrics via SSE", "error", err)
			}
		}
	}
}

func (s *SSEService) getClientState(client *streaming.Client) streaming.ConnectionState {
	if client.IsClosed() {
		return streaming.ConnectionStateClosed
	}
	if !client.IsReady() {
		return streaming.ConnectionStateConnecting
	}
	if client.IsIdle(s.config.Heartbeat.Timeout) {
		return streaming.ConnectionStateError
	}
	return streaming.ConnectionStateReady
}
