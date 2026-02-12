package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"github.com/mr-kaynak/go-core/internal/modules/notification/streaming"
)

var (
	// ErrBroadcastQueueFull is returned when broadcast queue is full
	ErrBroadcastQueueFull = errors.NewServiceUnavailable("Broadcast queue is full")
	// ErrBroadcasterStopped is returned when broadcaster is stopped
	ErrBroadcasterStopped = errors.NewServiceUnavailable("Event broadcaster is stopped")
)

// BroadcastJob represents a broadcast task
type BroadcastJob struct {
	Event       *domain.SSEEvent
	TargetUsers []uuid.UUID // Empty = broadcast to all
	Filter      *EventFilter
	Priority    int // Higher priority jobs are processed first
	RetryCount  int
	CreatedAt   time.Time
}

// EventFilter defines filtering criteria for events
type EventFilter struct {
	EventTypes []domain.SSEEventType
	Priorities []domain.NotificationPriority
	Channels   []string
	MinDelay   time.Duration
	Metadata   map[string]interface{}
}

// BroadcasterConfig contains configuration for event broadcaster
type BroadcasterConfig struct {
	MaxWorkers     int           `yaml:"max_workers" env:"SSE_BROADCAST_WORKERS" default:"10"`
	QueueSize      int           `yaml:"queue_size" env:"SSE_BROADCAST_QUEUE_SIZE" default:"1000"`
	MaxRetries     int           `yaml:"max_retries" env:"SSE_BROADCAST_MAX_RETRIES" default:"3"`
	RetryDelay     time.Duration `yaml:"retry_delay" env:"SSE_BROADCAST_RETRY_DELAY" default:"1s"`
	ProcessTimeout time.Duration `yaml:"process_timeout" env:"SSE_BROADCAST_PROCESS_TIMEOUT" default:"30s"`
	EnableBatching bool          `yaml:"enable_batching" env:"SSE_BROADCAST_ENABLE_BATCHING" default:"true"`
	BatchSize      int           `yaml:"batch_size" env:"SSE_BROADCAST_BATCH_SIZE" default:"100"`
	BatchInterval  time.Duration `yaml:"batch_interval" env:"SSE_BROADCAST_BATCH_INTERVAL" default:"100ms"`
}

// EventBroadcaster broadcasts events to connected clients
type EventBroadcaster struct {
	// Dependencies
	connManager *ConnectionManager
	logger      *logger.Logger

	// Configuration
	config BroadcasterConfig

	// Worker pool
	workerPool     chan struct{}
	workers        int32
	maxWorkers     int32
	broadcastQueue chan *BroadcastJob
	priorityQueue  chan *BroadcastJob // High priority queue

	// Batching
	batchBuffer []*BroadcastJob
	batchMu     sync.Mutex
	batchTimer  *time.Timer

	// Statistics
	totalBroadcasts int64
	successfulSends int64
	failedSends     int64
	droppedEvents   int64
	queuedJobs      int32
	processingJobs  int32
	averageLatency  int64 // in microseconds

	// Lifecycle
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stopped    int32
	shutdownCh chan struct{}
}

// NewEventBroadcaster creates a new event broadcaster
func NewEventBroadcaster(connManager *ConnectionManager, config BroadcasterConfig) *EventBroadcaster {
	ctx, cancel := context.WithCancel(context.Background())

	eb := &EventBroadcaster{
		connManager:    connManager,
		logger:         logger.Get().WithField("component", "event_broadcaster"),
		config:         config,
		workerPool:     make(chan struct{}, config.MaxWorkers),
		maxWorkers:     int32(config.MaxWorkers),
		broadcastQueue: make(chan *BroadcastJob, config.QueueSize),
		priorityQueue:  make(chan *BroadcastJob, config.QueueSize/10), // 10% for priority
		batchBuffer:    make([]*BroadcastJob, 0, config.BatchSize),
		ctx:            ctx,
		cancel:         cancel,
		shutdownCh:     make(chan struct{}),
	}

	// Initialize worker pool semaphore
	for i := 0; i < config.MaxWorkers; i++ {
		eb.workerPool <- struct{}{}
	}

	// Start workers
	eb.startWorkers()

	// Start batch processor if enabled
	if config.EnableBatching {
		eb.wg.Add(1)
		go eb.batchProcessor()
	}

	return eb
}

// BroadcastToUser broadcasts an event to a specific user
func (eb *EventBroadcaster) BroadcastToUser(ctx context.Context, userID uuid.UUID, event *domain.SSEEvent) error {
	return eb.broadcast(ctx, event, []uuid.UUID{userID}, nil, 0)
}

// BroadcastToUsers broadcasts an event to multiple users
func (eb *EventBroadcaster) BroadcastToUsers(ctx context.Context, userIDs []uuid.UUID, event *domain.SSEEvent) error {
	return eb.broadcast(ctx, event, userIDs, nil, 0)
}

// BroadcastToAll broadcasts an event to all connected clients
func (eb *EventBroadcaster) BroadcastToAll(ctx context.Context, event *domain.SSEEvent) error {
	return eb.broadcast(ctx, event, nil, nil, 0)
}

// BroadcastWithFilter broadcasts an event with filtering
func (eb *EventBroadcaster) BroadcastWithFilter(ctx context.Context, event *domain.SSEEvent, filter *EventFilter) error {
	return eb.broadcast(ctx, event, nil, filter, 0)
}

// BroadcastPriority broadcasts a high-priority event
func (eb *EventBroadcaster) BroadcastPriority(ctx context.Context, event *domain.SSEEvent, priority int) error {
	return eb.broadcast(ctx, event, nil, nil, priority)
}

// broadcast internal method to queue a broadcast job
func (eb *EventBroadcaster) broadcast(
	ctx context.Context, event *domain.SSEEvent, targetUsers []uuid.UUID, filter *EventFilter, priority int,
) error {
	if atomic.LoadInt32(&eb.stopped) == 1 {
		return ErrBroadcasterStopped
	}

	job := &BroadcastJob{
		Event:       event,
		TargetUsers: targetUsers,
		Filter:      filter,
		Priority:    priority,
		CreatedAt:   time.Now(),
	}

	// Update queued jobs counter
	atomic.AddInt32(&eb.queuedJobs, 1)
	defer atomic.AddInt32(&eb.queuedJobs, -1)

	// Select appropriate queue based on priority
	queue := eb.broadcastQueue
	if priority > 0 {
		queue = eb.priorityQueue
	}

	// Try to queue the job
	select {
	case queue <- job:
		atomic.AddInt64(&eb.totalBroadcasts, 1)
		return nil

	case <-ctx.Done():
		atomic.AddInt64(&eb.droppedEvents, 1)
		return ctx.Err()

	case <-time.After(100 * time.Millisecond):
		// Queue is full, try batching if enabled
		if eb.config.EnableBatching {
			eb.addToBatch(job)
			return nil
		}
		atomic.AddInt64(&eb.droppedEvents, 1)
		return ErrBroadcastQueueFull
	}
}

// addToBatch adds a job to the batch buffer
func (eb *EventBroadcaster) addToBatch(job *BroadcastJob) {
	eb.batchMu.Lock()
	defer eb.batchMu.Unlock()

	eb.batchBuffer = append(eb.batchBuffer, job)

	// Process batch if it's full
	if len(eb.batchBuffer) >= eb.config.BatchSize {
		eb.processBatch()
	} else if eb.batchTimer == nil {
		// Start batch timer
		eb.batchTimer = time.AfterFunc(eb.config.BatchInterval, func() {
			eb.batchMu.Lock()
			defer eb.batchMu.Unlock()
			eb.processBatch()
		})
	}
}

// processBatch processes all jobs in the batch buffer
func (eb *EventBroadcaster) processBatch() {
	if len(eb.batchBuffer) == 0 {
		return
	}

	// Process all jobs in the batch
	for _, job := range eb.batchBuffer {
		select {
		case eb.broadcastQueue <- job:
			// Job queued successfully
		default:
			// Queue full, drop the job
			atomic.AddInt64(&eb.droppedEvents, 1)
		}
	}

	// Clear batch buffer
	eb.batchBuffer = eb.batchBuffer[:0]

	// Reset timer
	if eb.batchTimer != nil {
		eb.batchTimer.Stop()
		eb.batchTimer = nil
	}
}

// startWorkers starts the worker pool
func (eb *EventBroadcaster) startWorkers() {
	// Start initial workers
	initialWorkers := eb.config.MaxWorkers / 2
	if initialWorkers < 1 {
		initialWorkers = 1
	}

	for i := 0; i < initialWorkers; i++ {
		eb.wg.Add(1)
		go eb.worker(i)
		atomic.AddInt32(&eb.workers, 1)
	}

	// Start worker manager for dynamic scaling
	eb.wg.Add(1)
	go eb.workerManager()

	eb.logger.Info("Event broadcaster started",
		"initial_workers", initialWorkers,
		"max_workers", eb.config.MaxWorkers,
		"queue_size", eb.config.QueueSize,
	)
}

// workerManager dynamically manages the worker pool
func (eb *EventBroadcaster) workerManager() {
	defer eb.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			eb.adjustWorkerPool()

		case <-eb.ctx.Done():
			return

		case <-eb.shutdownCh:
			return
		}
	}
}

// adjustWorkerPool dynamically adjusts the number of workers
func (eb *EventBroadcaster) adjustWorkerPool() {
	queuedJobs := atomic.LoadInt32(&eb.queuedJobs)
	currentWorkers := atomic.LoadInt32(&eb.workers)

	// Calculate desired workers based on queue size
	desiredWorkers := int32(1)
	if queuedJobs > 10 {
		desiredWorkers = queuedJobs / 10
	}
	if desiredWorkers > eb.maxWorkers {
		desiredWorkers = eb.maxWorkers
	}

	// Scale up if needed
	if desiredWorkers > currentWorkers && currentWorkers < eb.maxWorkers {
		workersToAdd := desiredWorkers - currentWorkers
		if workersToAdd > 5 {
			workersToAdd = 5 // Add max 5 workers at a time
		}

		for i := int32(0); i < workersToAdd; i++ {
			eb.wg.Add(1)
			go eb.worker(int(currentWorkers + i))
			atomic.AddInt32(&eb.workers, 1)
		}

		eb.logger.Debug("Scaled up workers",
			"added", workersToAdd,
			"total", atomic.LoadInt32(&eb.workers),
		)
	}
}

// worker processes broadcast jobs
func (eb *EventBroadcaster) worker(id int) {
	defer eb.wg.Done()
	defer atomic.AddInt32(&eb.workers, -1)

	eb.logger.Debug("Worker started", "worker_id", id)

	for {
		select {
		// Check priority queue first
		case job := <-eb.priorityQueue:
			eb.processJob(job)

		// Then check normal queue
		case job := <-eb.broadcastQueue:
			eb.processJob(job)

		case <-eb.ctx.Done():
			eb.logger.Debug("Worker stopped", "worker_id", id)
			return

		case <-eb.shutdownCh:
			eb.logger.Debug("Worker shutdown", "worker_id", id)
			return
		}
	}
}

// processJob processes a single broadcast job
func (eb *EventBroadcaster) processJob(job *BroadcastJob) {
	// Acquire worker pool semaphore
	<-eb.workerPool
	defer func() { eb.workerPool <- struct{}{} }()

	// Update processing counter
	atomic.AddInt32(&eb.processingJobs, 1)
	defer atomic.AddInt32(&eb.processingJobs, -1)

	// Track processing time
	startTime := time.Now()
	defer func() {
		latency := time.Since(startTime).Microseconds()
		// Update average latency (simplified moving average)
		oldAvg := atomic.LoadInt64(&eb.averageLatency)
		newAvg := (oldAvg*9 + latency) / 10
		atomic.StoreInt64(&eb.averageLatency, newAvg)
	}()

	// Process with timeout
	ctx, cancel := context.WithTimeout(eb.ctx, eb.config.ProcessTimeout)
	defer cancel()

	// Get target clients
	var clients []*streaming.Client
	if len(job.TargetUsers) > 0 {
		// Specific users
		for _, userID := range job.TargetUsers {
			userClients := eb.connManager.GetUserClients(userID)
			clients = append(clients, userClients...)
		}
	} else {
		// All clients
		clients = eb.connManager.GetAllClients()
	}

	if len(clients) == 0 {
		eb.logger.Debug("No clients to broadcast to",
			"event_type", job.Event.Type,
			"target_users", len(job.TargetUsers),
		)
		return
	}

	// Apply filters and send
	successCount := 0
	failCount := 0

	for _, client := range clients {
		// Check context
		select {
		case <-ctx.Done():
			eb.logger.Warn("Broadcast timeout",
				"event_type", job.Event.Type,
				"sent", successCount,
				"failed", failCount,
			)
			return
		default:
		}

		// Apply filter
		if job.Filter != nil && !eb.shouldSendToClient(client, job.Event, job.Filter) {
			continue
		}

		// Check if client should receive this event
		if !client.ShouldReceiveEvent(job.Event) {
			continue
		}

		// Send event
		if err := client.Send(job.Event); err != nil {
			failCount++
			atomic.AddInt64(&eb.failedSends, 1)

			// Handle specific errors
			if err == streaming.ErrClientClosed {
				// Client is closed, unregister it
				_ = eb.connManager.Unregister(client.ID)
			} else if err == streaming.ErrBufferFull && job.RetryCount < eb.config.MaxRetries {
				// Retry if buffer is full
				job.RetryCount++
				go func() {
					time.Sleep(eb.config.RetryDelay * time.Duration(job.RetryCount))
					eb.broadcastQueue <- job
				}()
			}
		} else {
			successCount++
			atomic.AddInt64(&eb.successfulSends, 1)
		}
	}

	// Log results
	if failCount > 0 || eb.logger.IsDebugEnabled() {
		eb.logger.Debug("Broadcast completed",
			"event_type", job.Event.Type,
			"event_id", job.Event.ID,
			"target_clients", len(clients),
			"success", successCount,
			"failed", failCount,
			"latency", time.Since(job.CreatedAt),
		)
	}
}

// shouldSendToClient checks if an event should be sent to a client based on filter
func (eb *EventBroadcaster) shouldSendToClient(client *streaming.Client, event *domain.SSEEvent, filter *EventFilter) bool {
	// Check event types filter
	if len(filter.EventTypes) > 0 {
		found := false
		for _, et := range filter.EventTypes {
			if et == event.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check channels filter
	if len(filter.Channels) > 0 {
		hasChannel := false
		for _, channel := range filter.Channels {
			if client.IsSubscribed(channel) {
				hasChannel = true
				break
			}
		}
		if !hasChannel {
			return false
		}
	}

	// Check minimum delay
	if filter.MinDelay > 0 {
		if time.Since(client.GetLastMessage()) < filter.MinDelay {
			return false
		}
	}

	return true
}

// batchProcessor processes batched jobs
func (eb *EventBroadcaster) batchProcessor() {
	defer eb.wg.Done()

	ticker := time.NewTicker(eb.config.BatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			eb.batchMu.Lock()
			if len(eb.batchBuffer) > 0 {
				eb.processBatch()
			}
			eb.batchMu.Unlock()

		case <-eb.ctx.Done():
			return

		case <-eb.shutdownCh:
			// Process remaining batch before shutdown
			eb.batchMu.Lock()
			eb.processBatch()
			eb.batchMu.Unlock()
			return
		}
	}
}

// GetStats returns broadcaster statistics
func (eb *EventBroadcaster) GetStats() BroadcasterStats {
	return BroadcasterStats{
		TotalBroadcasts:   atomic.LoadInt64(&eb.totalBroadcasts),
		SuccessfulSends:   atomic.LoadInt64(&eb.successfulSends),
		FailedSends:       atomic.LoadInt64(&eb.failedSends),
		DroppedEvents:     atomic.LoadInt64(&eb.droppedEvents),
		QueuedJobs:        atomic.LoadInt32(&eb.queuedJobs),
		ProcessingJobs:    atomic.LoadInt32(&eb.processingJobs),
		ActiveWorkers:     atomic.LoadInt32(&eb.workers),
		MaxWorkers:        eb.maxWorkers,
		QueueSize:         len(eb.broadcastQueue),
		QueueCapacity:     eb.config.QueueSize,
		PriorityQueueSize: len(eb.priorityQueue),
		AverageLatency:    time.Duration(atomic.LoadInt64(&eb.averageLatency)) * time.Microsecond,
		BatchBufferSize:   len(eb.batchBuffer),
		IsStopped:         atomic.LoadInt32(&eb.stopped) == 1,
	}
}

// BroadcasterStats contains broadcaster statistics
type BroadcasterStats struct {
	TotalBroadcasts   int64         `json:"total_broadcasts"`
	SuccessfulSends   int64         `json:"successful_sends"`
	FailedSends       int64         `json:"failed_sends"`
	DroppedEvents     int64         `json:"dropped_events"`
	QueuedJobs        int32         `json:"queued_jobs"`
	ProcessingJobs    int32         `json:"processing_jobs"`
	ActiveWorkers     int32         `json:"active_workers"`
	MaxWorkers        int32         `json:"max_workers"`
	QueueSize         int           `json:"queue_size"`
	QueueCapacity     int           `json:"queue_capacity"`
	PriorityQueueSize int           `json:"priority_queue_size"`
	AverageLatency    time.Duration `json:"average_latency"`
	BatchBufferSize   int           `json:"batch_buffer_size"`
	IsStopped         bool          `json:"is_stopped"`
}

// Shutdown gracefully shuts down the broadcaster
func (eb *EventBroadcaster) Shutdown(ctx context.Context) error {
	eb.logger.Info("Shutting down event broadcaster")

	// Mark as stopped
	atomic.StoreInt32(&eb.stopped, 1)

	// Signal shutdown
	close(eb.shutdownCh)
	eb.cancel()

	// Process remaining jobs
	done := make(chan struct{})
	go func() {
		// Process remaining priority jobs
		for len(eb.priorityQueue) > 0 {
			select {
			case job := <-eb.priorityQueue:
				eb.processJob(job)
			default:
			}
		}

		// Process remaining normal jobs
		for len(eb.broadcastQueue) > 0 {
			select {
			case job := <-eb.broadcastQueue:
				eb.processJob(job)
			default:
			}
		}

		eb.wg.Wait()
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		eb.logger.Info("Event broadcaster shutdown complete")
		return nil
	case <-ctx.Done():
		eb.logger.Warn("Event broadcaster shutdown timed out")
		return ctx.Err()
	}
}
