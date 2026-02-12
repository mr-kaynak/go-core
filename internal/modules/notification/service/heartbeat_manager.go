package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

// HeartbeatConfig contains configuration for heartbeat manager
type HeartbeatConfig struct {
	Interval      time.Duration `yaml:"interval" env:"SSE_HEARTBEAT_INTERVAL" default:"30s"`
	Timeout       time.Duration `yaml:"timeout" env:"SSE_HEARTBEAT_TIMEOUT" default:"60s"`
	EnableMetrics bool          `yaml:"enable_metrics" env:"SSE_HEARTBEAT_METRICS" default:"true"`
	ServerID      string        `yaml:"server_id" env:"SSE_SERVER_ID" default:""`
}

// HeartbeatManager manages heartbeat for all connections
type HeartbeatManager struct {
	// Dependencies
	connManager *ConnectionManager
	broadcaster *EventBroadcaster
	logger      *logger.Logger

	// Configuration
	config HeartbeatConfig

	// State
	sequence         int64
	serverID         string
	lastHeartbeat    time.Time
	heartbeatsSent   int64
	heartbeatsFailed int64

	// Lifecycle
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	running    int32
	shutdownCh chan struct{}
}

// NewHeartbeatManager creates a new heartbeat manager
func NewHeartbeatManager(
	connManager *ConnectionManager,
	broadcaster *EventBroadcaster,
	config HeartbeatConfig,
) *HeartbeatManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Generate server ID if not provided
	serverID := config.ServerID
	if serverID == "" {
		serverID = "server-" + uuid.New().String()[:8]
	}

	hm := &HeartbeatManager{
		connManager: connManager,
		broadcaster: broadcaster,
		logger:      logger.Get().WithField("component", "heartbeat_manager"),
		config:      config,
		serverID:    serverID,
		ctx:         ctx,
		cancel:      cancel,
		shutdownCh:  make(chan struct{}),
	}

	return hm
}

// Start starts the heartbeat manager
func (hm *HeartbeatManager) Start() error {
	if !atomic.CompareAndSwapInt32(&hm.running, 0, 1) {
		return nil // Already running
	}

	hm.wg.Add(1)
	go hm.heartbeatRoutine()

	hm.logger.Info("Heartbeat manager started",
		"interval", hm.config.Interval,
		"timeout", hm.config.Timeout,
		"server_id", hm.serverID,
	)

	return nil
}

// Stop stops the heartbeat manager
func (hm *HeartbeatManager) Stop() {
	if !atomic.CompareAndSwapInt32(&hm.running, 1, 0) {
		return // Not running
	}

	close(hm.shutdownCh)
	hm.cancel()
	hm.wg.Wait()

	hm.logger.Info("Heartbeat manager stopped",
		"total_heartbeats_sent", atomic.LoadInt64(&hm.heartbeatsSent),
		"total_heartbeats_failed", atomic.LoadInt64(&hm.heartbeatsFailed),
	)
}

// heartbeatRoutine sends periodic heartbeats
func (hm *HeartbeatManager) heartbeatRoutine() {
	defer hm.wg.Done()

	ticker := time.NewTicker(hm.config.Interval)
	defer ticker.Stop()

	// Send initial heartbeat
	hm.sendHeartbeat()

	for {
		select {
		case <-ticker.C:
			hm.sendHeartbeat()
			hm.checkClientHealth()

		case <-hm.ctx.Done():
			return

		case <-hm.shutdownCh:
			return
		}
	}
}

// sendHeartbeat sends heartbeat to all clients
func (hm *HeartbeatManager) sendHeartbeat() {
	sequence := atomic.AddInt64(&hm.sequence, 1)

	// Create heartbeat event
	event := domain.NewSSEHeartbeatEvent(sequence, hm.serverID)

	// Broadcast to all clients
	ctx, cancel := context.WithTimeout(hm.ctx, 5*time.Second)
	defer cancel()

	err := hm.broadcaster.BroadcastToAll(ctx, event)
	if err != nil {
		atomic.AddInt64(&hm.heartbeatsFailed, 1)
		hm.logger.Warn("Failed to broadcast heartbeat",
			"error", err,
			"sequence", sequence,
		)
	} else {
		atomic.AddInt64(&hm.heartbeatsSent, 1)
		hm.updateLastHeartbeat()

		if hm.logger.IsDebugEnabled() {
			hm.logger.Debug("Heartbeat sent",
				"sequence", sequence,
				"connected_clients", len(hm.connManager.GetAllClients()),
			)
		}
	}
}

// checkClientHealth checks health of all connected clients
func (hm *HeartbeatManager) checkClientHealth() {
	clients := hm.connManager.GetAllClients()
	unhealthyCount := 0
	now := time.Now()

	for _, client := range clients {
		// Check if client has been idle for too long
		if now.Sub(client.GetLastPing()) > hm.config.Timeout {
			unhealthyCount++

			// Mark client as unhealthy
			client.SetReady(false)

			// Send a ping request to the client
			hm.sendPingRequest(client)
		}
	}

	if unhealthyCount > 0 {
		hm.logger.Warn("Unhealthy clients detected",
			"unhealthy_count", unhealthyCount,
			"total_clients", len(clients),
		)
	}
}

// sendPingRequest sends a ping request to a specific client
func (hm *HeartbeatManager) sendPingRequest(client *streaming.Client) {
	event := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      "ping",
		Timestamp: time.Now(),
		UserID:    &client.UserID,
		Data: map[string]interface{}{
			"type":      "ping_request",
			"client_id": client.ID,
			"timestamp": time.Now().Unix(),
		},
	}

	if err := client.Send(event); err != nil {
		hm.logger.Debug("Failed to send ping request",
			"client_id", client.ID,
			"error", err,
		)

		// Client might be dead, let connection manager handle cleanup
		if err == streaming.ErrClientClosed {
			_ = hm.connManager.Unregister(client.ID)
		}
	}
}

// HandlePong handles pong response from a client
func (hm *HeartbeatManager) HandlePong(clientID uuid.UUID) {
	client, err := hm.connManager.GetClient(clientID)
	if err != nil {
		return
	}

	// Update client's last ping time
	client.UpdatePing()

	// Mark client as healthy
	if !client.IsReady() {
		client.SetReady(true)
		hm.logger.Debug("Client recovered",
			"client_id", clientID,
		)
	}
}

// updateLastHeartbeat updates the last heartbeat timestamp
func (hm *HeartbeatManager) updateLastHeartbeat() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.lastHeartbeat = time.Now()
}

// GetLastHeartbeat returns the last heartbeat timestamp
func (hm *HeartbeatManager) GetLastHeartbeat() time.Time {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.lastHeartbeat
}

// IsHealthy checks if heartbeat manager is healthy
func (hm *HeartbeatManager) IsHealthy() bool {
	if atomic.LoadInt32(&hm.running) != 1 {
		return false
	}

	// Check if last heartbeat was recent
	lastHeartbeat := hm.GetLastHeartbeat()
	return time.Since(lastHeartbeat) <= hm.config.Interval*2
}

// GetStats returns heartbeat manager statistics
func (hm *HeartbeatManager) GetStats() HeartbeatStats {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	return HeartbeatStats{
		ServerID:         hm.serverID,
		Sequence:         atomic.LoadInt64(&hm.sequence),
		HeartbeatsSent:   atomic.LoadInt64(&hm.heartbeatsSent),
		HeartbeatsFailed: atomic.LoadInt64(&hm.heartbeatsFailed),
		LastHeartbeat:    hm.lastHeartbeat,
		Interval:         hm.config.Interval,
		IsRunning:        atomic.LoadInt32(&hm.running) == 1,
		IsHealthy:        hm.IsHealthy(),
	}
}

// HeartbeatStats contains heartbeat manager statistics
type HeartbeatStats struct {
	ServerID         string        `json:"server_id"`
	Sequence         int64         `json:"sequence"`
	HeartbeatsSent   int64         `json:"heartbeats_sent"`
	HeartbeatsFailed int64         `json:"heartbeats_failed"`
	LastHeartbeat    time.Time     `json:"last_heartbeat"`
	Interval         time.Duration `json:"interval"`
	IsRunning        bool          `json:"is_running"`
	IsHealthy        bool          `json:"is_healthy"`
}

// Shutdown gracefully shuts down the heartbeat manager
func (hm *HeartbeatManager) Shutdown(ctx context.Context) error {
	hm.logger.Info("Shutting down heartbeat manager")

	// Stop heartbeat routine
	hm.Stop()

	// Send final heartbeat with shutdown message
	finalEvent := &domain.SSEEvent{
		ID:        uuid.New().String(),
		Type:      domain.SSEEventTypeSystemMessage,
		Timestamp: time.Now(),
		Data: domain.SSESystemMessage{
			ID:          uuid.New(),
			Title:       "Server Shutdown",
			Message:     "This server is shutting down. Please reconnect.",
			Type:        "warning",
			AffectsUser: true,
		},
	}

	// Try to broadcast shutdown message
	broadcastCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := hm.broadcaster.BroadcastToAll(broadcastCtx, finalEvent); err != nil {
		hm.logger.Warn("Failed to send shutdown message", "error", err)
	}

	hm.logger.Info("Heartbeat manager shutdown complete")
	return nil
}
