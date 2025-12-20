package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

var (
	// ErrMaxConnectionsReached is returned when max connections limit is reached
	ErrMaxConnectionsReached = errors.NewServiceUnavailable("Maximum connections limit reached")
	// ErrMaxUserConnectionsReached is returned when user's max connections limit is reached
	ErrMaxUserConnectionsReached = errors.NewBadRequest("Maximum connections per user limit reached")
	// ErrClientNotFound is returned when client is not found
	ErrClientNotFound = errors.NewNotFound("Client", "connection")
)

// ConnectionManagerConfig contains configuration for connection manager
type ConnectionManagerConfig struct {
	MaxConnections        int           `yaml:"max_connections" env:"SSE_MAX_CONNECTIONS" default:"10000"`
	MaxConnectionsPerUser int           `yaml:"max_connections_per_user" env:"SSE_MAX_CONNECTIONS_PER_USER" default:"5"`
	IdleTimeout           time.Duration `yaml:"idle_timeout" env:"SSE_IDLE_TIMEOUT" default:"5m"`
	CleanupInterval       time.Duration `yaml:"cleanup_interval" env:"SSE_CLEANUP_INTERVAL" default:"1m"`
	EnableMetrics         bool          `yaml:"enable_metrics" env:"SSE_ENABLE_METRICS" default:"true"`
}

// ConnectionManager manages all active SSE connections
type ConnectionManager struct {
	// Connection storage
	clients   map[uuid.UUID]*streaming.Client // clientID -> Client
	userIndex map[uuid.UUID][]uuid.UUID       // userID -> []clientID
	mu        sync.RWMutex

	// Statistics
	totalConnections   int64
	totalDisconnects   int64
	totalMessagesSent  int64
	totalBytesTransferred int64

	// Configuration
	config ConnectionManagerConfig

	// Dependencies
	logger *logger.Logger

	// Lifecycle
	ctx        context.Context
	cancel     context.CancelFunc
	cleanupWg  sync.WaitGroup
	shutdownCh chan struct{}
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(config ConnectionManagerConfig) *ConnectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	cm := &ConnectionManager{
		clients:    make(map[uuid.UUID]*streaming.Client),
		userIndex:  make(map[uuid.UUID][]uuid.UUID),
		config:     config,
		logger:     logger.Get().WithField("component", "connection_manager"),
		ctx:        ctx,
		cancel:     cancel,
		shutdownCh: make(chan struct{}),
	}

	// Start cleanup routine
	cm.cleanupWg.Add(1)
	go cm.cleanupRoutine()

	return cm
}

// Register registers a new client connection
func (cm *ConnectionManager) Register(client *streaming.Client) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check global connection limit
	if len(cm.clients) >= cm.config.MaxConnections {
		cm.logger.Warn("Max connections reached",
			"current", len(cm.clients),
			"max", cm.config.MaxConnections,
		)
		return ErrMaxConnectionsReached
	}

	// Check per-user connection limit
	userClients := cm.userIndex[client.UserID]
	if len(userClients) >= cm.config.MaxConnectionsPerUser {
		cm.logger.Warn("Max user connections reached",
			"user_id", client.UserID,
			"current", len(userClients),
			"max", cm.config.MaxConnectionsPerUser,
		)
		return ErrMaxUserConnectionsReached
	}

	// Register client
	cm.clients[client.ID] = client
	cm.userIndex[client.UserID] = append(cm.userIndex[client.UserID], client.ID)

	// Update statistics
	atomic.AddInt64(&cm.totalConnections, 1)

	cm.logger.Info("Client connected",
		"client_id", client.ID,
		"user_id", client.UserID,
		"ip", client.IPAddress,
		"user_agent", client.UserAgent,
		"total_connections", len(cm.clients),
		"user_connections", len(cm.userIndex[client.UserID]),
	)

	return nil
}

// Unregister unregisters a client connection
func (cm *ConnectionManager) Unregister(clientID uuid.UUID) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	client, exists := cm.clients[clientID]
	if !exists {
		return ErrClientNotFound
	}

	// Remove from clients map
	delete(cm.clients, clientID)

	// Remove from user index
	userClients := cm.userIndex[client.UserID]
	for i, cid := range userClients {
		if cid == clientID {
			cm.userIndex[client.UserID] = append(userClients[:i], userClients[i+1:]...)
			break
		}
	}

	// Clean up user index if no more connections
	if len(cm.userIndex[client.UserID]) == 0 {
		delete(cm.userIndex, client.UserID)
	}

	// Get client stats before closing
	stats := client.GetStats()

	// Close client connection
	client.Close()

	// Update statistics
	atomic.AddInt64(&cm.totalDisconnects, 1)
	atomic.AddInt64(&cm.totalMessagesSent, int64(stats.MessagesSent))
	atomic.AddInt64(&cm.totalBytesTransferred, int64(stats.BytesTransferred))

	cm.logger.Info("Client disconnected",
		"client_id", clientID,
		"user_id", client.UserID,
		"duration", time.Since(client.ConnectedAt),
		"messages_sent", stats.MessagesSent,
		"messages_dropped", stats.MessagesDropped,
		"bytes_transferred", stats.BytesTransferred,
		"total_connections", len(cm.clients),
	)

	return nil
}

// GetClient returns a specific client by ID
func (cm *ConnectionManager) GetClient(clientID uuid.UUID) (*streaming.Client, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.clients[clientID]
	if !exists {
		return nil, ErrClientNotFound
	}

	return client, nil
}

// GetUserClients returns all clients for a specific user
func (cm *ConnectionManager) GetUserClients(userID uuid.UUID) []*streaming.Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clientIDs := cm.userIndex[userID]
	clients := make([]*streaming.Client, 0, len(clientIDs))

	for _, cid := range clientIDs {
		if client, exists := cm.clients[cid]; exists && client.IsReady() {
			clients = append(clients, client)
		}
	}

	return clients
}

// GetAllClients returns all active clients
func (cm *ConnectionManager) GetAllClients() []*streaming.Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clients := make([]*streaming.Client, 0, len(cm.clients))
	for _, client := range cm.clients {
		if client.IsReady() {
			clients = append(clients, client)
		}
	}

	return clients
}

// GetClientsByFilter returns clients matching the filter
func (cm *ConnectionManager) GetClientsByFilter(filter func(*streaming.Client) bool) []*streaming.Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var clients []*streaming.Client
	for _, client := range cm.clients {
		if filter(client) {
			clients = append(clients, client)
		}
	}

	return clients
}

// IsUserConnected checks if a user has any active connections
func (cm *ConnectionManager) IsUserConnected(userID uuid.UUID) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clientIDs := cm.userIndex[userID]
	for _, cid := range clientIDs {
		if client, exists := cm.clients[cid]; exists && client.IsReady() {
			return true
		}
	}

	return false
}

// GetConnectedUsers returns all users with active connections
func (cm *ConnectionManager) GetConnectedUsers() []uuid.UUID {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	users := make([]uuid.UUID, 0, len(cm.userIndex))
	for userID, clientIDs := range cm.userIndex {
		// Check if user has at least one ready client
		for _, cid := range clientIDs {
			if client, exists := cm.clients[cid]; exists && client.IsReady() {
				users = append(users, userID)
				break
			}
		}
	}

	return users
}

// GetStats returns connection manager statistics
func (cm *ConnectionManager) GetStats() ConnectionManagerStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Calculate active connections
	activeConnections := 0
	for _, client := range cm.clients {
		if client.IsReady() {
			activeConnections++
		}
	}

	// Calculate average connection duration
	var totalDuration time.Duration
	now := time.Now()
	for _, client := range cm.clients {
		totalDuration += now.Sub(client.ConnectedAt)
	}

	avgDuration := time.Duration(0)
	if len(cm.clients) > 0 {
		avgDuration = totalDuration / time.Duration(len(cm.clients))
	}

	return ConnectionManagerStats{
		TotalConnections:      len(cm.clients),
		ActiveConnections:     activeConnections,
		UniqueUsers:           len(cm.userIndex),
		TotalConnectionsMade:  atomic.LoadInt64(&cm.totalConnections),
		TotalDisconnects:      atomic.LoadInt64(&cm.totalDisconnects),
		TotalMessagesSent:     atomic.LoadInt64(&cm.totalMessagesSent),
		TotalBytesTransferred: atomic.LoadInt64(&cm.totalBytesTransferred),
		MaxConnections:        cm.config.MaxConnections,
		MaxConnectionsPerUser: cm.config.MaxConnectionsPerUser,
		IdleTimeout:           cm.config.IdleTimeout,
		CleanupInterval:       cm.config.CleanupInterval,
		AverageConnectionTime: avgDuration,
		Uptime:                time.Since(cm.getStartTime()),
	}
}

// ConnectionManagerStats contains connection manager statistics
type ConnectionManagerStats struct {
	TotalConnections      int           `json:"total_connections"`
	ActiveConnections     int           `json:"active_connections"`
	UniqueUsers           int           `json:"unique_users"`
	TotalConnectionsMade  int64         `json:"total_connections_made"`
	TotalDisconnects      int64         `json:"total_disconnects"`
	TotalMessagesSent     int64         `json:"total_messages_sent"`
	TotalBytesTransferred int64         `json:"total_bytes_transferred"`
	MaxConnections        int           `json:"max_connections"`
	MaxConnectionsPerUser int           `json:"max_connections_per_user"`
	IdleTimeout           time.Duration `json:"idle_timeout"`
	CleanupInterval       time.Duration `json:"cleanup_interval"`
	AverageConnectionTime time.Duration `json:"average_connection_time"`
	Uptime                time.Duration `json:"uptime"`
}

// cleanupRoutine periodically cleans up idle connections
func (cm *ConnectionManager) cleanupRoutine() {
	defer cm.cleanupWg.Done()

	ticker := time.NewTicker(cm.config.CleanupInterval)
	defer ticker.Stop()

	cm.logger.Info("Cleanup routine started",
		"interval", cm.config.CleanupInterval,
		"idle_timeout", cm.config.IdleTimeout,
	)

	for {
		select {
		case <-ticker.C:
			cm.cleanupIdleConnections()

		case <-cm.ctx.Done():
			cm.logger.Info("Cleanup routine stopped")
			return

		case <-cm.shutdownCh:
			cm.logger.Info("Cleanup routine shutdown")
			return
		}
	}
}

// cleanupIdleConnections removes idle connections
func (cm *ConnectionManager) cleanupIdleConnections() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()
	toRemove := []uuid.UUID{}
	idleCount := 0

	for clientID, client := range cm.clients {
		// Check if connection is idle
		if client.IsIdle(cm.config.IdleTimeout) {
			toRemove = append(toRemove, clientID)
			idleCount++
		}

		// Also remove closed connections
		if client.IsClosed() {
			toRemove = append(toRemove, clientID)
		}
	}

	// Remove idle connections
	for _, clientID := range toRemove {
		client := cm.clients[clientID]
		if client != nil {
			cm.logger.Debug("Removing idle connection",
				"client_id", clientID,
				"user_id", client.UserID,
				"idle_duration", now.Sub(client.GetLastPing()),
			)

			// Unregister without lock (we already have it)
			cm.unregisterInternal(clientID)
		}
	}

	if len(toRemove) > 0 {
		cm.logger.Info("Cleanup completed",
			"removed_connections", len(toRemove),
			"idle_connections", idleCount,
			"remaining_connections", len(cm.clients),
		)
	}
}

// unregisterInternal unregisters a client without locking (must be called with lock held)
func (cm *ConnectionManager) unregisterInternal(clientID uuid.UUID) {
	client, exists := cm.clients[clientID]
	if !exists {
		return
	}

	// Remove from clients map
	delete(cm.clients, clientID)

	// Remove from user index
	userClients := cm.userIndex[client.UserID]
	for i, cid := range userClients {
		if cid == clientID {
			cm.userIndex[client.UserID] = append(userClients[:i], userClients[i+1:]...)
			break
		}
	}

	// Clean up user index if no more connections
	if len(cm.userIndex[client.UserID]) == 0 {
		delete(cm.userIndex, client.UserID)
	}

	// Close client
	client.Close()

	// Update statistics
	atomic.AddInt64(&cm.totalDisconnects, 1)
}

// BroadcastToUser sends an event to all connections of a specific user
func (cm *ConnectionManager) BroadcastToUser(userID uuid.UUID, event interface{}) (int, int) {
	clients := cm.GetUserClients(userID)
	successCount := 0
	failCount := 0

	for _, client := range clients {
		if sseEvent, ok := event.(*streaming.SSEEvent); ok {
			if err := client.Send(sseEvent); err != nil {
				failCount++
				cm.logger.Debug("Failed to send to client",
					"client_id", client.ID,
					"error", err,
				)
			} else {
				successCount++
			}
		}
	}

	return successCount, failCount
}

// BroadcastToAll sends an event to all connected clients
func (cm *ConnectionManager) BroadcastToAll(event interface{}) (int, int) {
	clients := cm.GetAllClients()
	successCount := 0
	failCount := 0

	for _, client := range clients {
		if sseEvent, ok := event.(*streaming.SSEEvent); ok {
			if err := client.Send(sseEvent); err != nil {
				failCount++
			} else {
				successCount++
			}
		}
	}

	return successCount, failCount
}

// Shutdown gracefully shuts down the connection manager
func (cm *ConnectionManager) Shutdown(ctx context.Context) error {
	cm.logger.Info("Shutting down connection manager",
		"active_connections", len(cm.clients),
	)

	// Signal shutdown
	close(cm.shutdownCh)
	cm.cancel()

	// Wait for cleanup routine to finish
	done := make(chan struct{})
	go func() {
		cm.cleanupWg.Wait()
		close(done)
	}()

	// Wait for cleanup or timeout
	select {
	case <-done:
		// Cleanup finished
	case <-ctx.Done():
		cm.logger.Warn("Shutdown timeout, forcing close")
	}

	// Close all remaining connections
	cm.mu.Lock()
	for clientID, client := range cm.clients {
		cm.logger.Debug("Closing client on shutdown", "client_id", clientID)
		client.Close()
	}
	cm.clients = make(map[uuid.UUID]*streaming.Client)
	cm.userIndex = make(map[uuid.UUID][]uuid.UUID)
	cm.mu.Unlock()

	cm.logger.Info("Connection manager shutdown complete")
	return nil
}

// getStartTime returns the approximate start time (for uptime calculation)
func (cm *ConnectionManager) getStartTime() time.Time {
	// This is a simplified approach - in production, store actual start time
	return time.Now().Add(-time.Duration(atomic.LoadInt64(&cm.totalConnections)) * time.Minute)
}