package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the circuit breaker state
type State int32

const (
	// StateClosed allows requests through
	StateClosed State = iota
	// StateOpen rejects all requests
	StateOpen
	// StateHalfOpen allows limited requests for testing
	StateHalfOpen
)

// String returns the string representation of the state
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Errors
var (
	ErrCircuitOpen     = errors.New("circuit breaker is open")
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// Config contains circuit breaker configuration
type Config struct {
	// MaxFailures is the maximum number of failures allowed before opening the circuit
	MaxFailures int
	// FailureThreshold is the failure rate threshold (0.0 to 1.0)
	FailureThreshold float64
	// Timeout is the timeout for each request
	Timeout time.Duration
	// ResetTimeout is the time to wait before transitioning from open to half-open
	ResetTimeout time.Duration
	// HalfOpenMaxRequests is the max requests allowed in half-open state
	HalfOpenMaxRequests int
	// ObservationWindow is the time window for tracking failures
	ObservationWindow time.Duration
	// OnStateChange is called when the state changes
	OnStateChange func(from, to State)
}

// DefaultConfig returns default circuit breaker configuration
func DefaultConfig() Config {
	return Config{
		MaxFailures:         5,
		FailureThreshold:    0.5,
		Timeout:             30 * time.Second,
		ResetTimeout:        60 * time.Second,
		HalfOpenMaxRequests: 3,
		ObservationWindow:   10 * time.Second,
		OnStateChange:       nil,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config Config

	// State management
	state         int32 // atomic
	lastFailTime  int64 // atomic, unix nano
	nextResetTime int64 // atomic, unix nano

	// Statistics
	requests          int64 // atomic
	failures          int64 // atomic
	successes         int64 // atomic
	consecutiveFailures int32 // atomic
	halfOpenRequests    int32 // atomic

	// Sliding window for failure rate calculation
	window        *slidingWindow
	mu            sync.RWMutex
	lastStateTime time.Time
}

// New creates a new circuit breaker with the given configuration
func New(config Config) *CircuitBreaker {
	cb := &CircuitBreaker{
		config:        config,
		state:         int32(StateClosed),
		window:        newSlidingWindow(config.ObservationWindow),
		lastStateTime: time.Now(),
	}
	return cb
}

// Execute executes the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	return cb.ExecuteContext(context.Background(), fn)
}

// ExecuteContext executes the given function with circuit breaker protection and context
func (cb *CircuitBreaker) ExecuteContext(ctx context.Context, fn func() error) error {
	if !cb.Allow() {
		atomic.AddInt64(&cb.failures, 1)
		return ErrCircuitOpen
	}

	// Create timeout context if configured
	if cb.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cb.config.Timeout)
		defer cancel()
	}

	// Execute function
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		if err != nil {
			cb.RecordFailure()
			return err
		}
		cb.RecordSuccess()
		return nil

	case <-ctx.Done():
		cb.RecordFailure()
		return ctx.Err()
	}
}

// Allow checks if a request is allowed
func (cb *CircuitBreaker) Allow() bool {
	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if we should transition to half-open
		now := time.Now().UnixNano()
		nextReset := atomic.LoadInt64(&cb.nextResetTime)

		if now >= nextReset {
			// Try to transition to half-open
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateOpen), int32(StateHalfOpen)) {
				atomic.StoreInt32(&cb.halfOpenRequests, 0)
				cb.onStateChange(StateOpen, StateHalfOpen)
			}
			return cb.Allow() // Re-check with new state
		}
		return false

	case StateHalfOpen:
		// Allow limited requests
		current := atomic.AddInt32(&cb.halfOpenRequests, 1)
		if current <= int32(cb.config.HalfOpenMaxRequests) {
			return true
		}
		atomic.AddInt32(&cb.halfOpenRequests, -1)
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	atomic.AddInt64(&cb.requests, 1)
	atomic.AddInt64(&cb.successes, 1)
	atomic.StoreInt32(&cb.consecutiveFailures, 0)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.window.Record(true)

	state := State(atomic.LoadInt32(&cb.state))

	// If in half-open state and successful, consider closing
	if state == StateHalfOpen {
		halfOpenRequests := atomic.LoadInt32(&cb.halfOpenRequests)
		if halfOpenRequests >= int32(cb.config.HalfOpenMaxRequests) {
			// All half-open requests succeeded, close the circuit
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateHalfOpen), int32(StateClosed)) {
				cb.onStateChange(StateHalfOpen, StateClosed)
				atomic.StoreInt32(&cb.consecutiveFailures, 0)
				atomic.StoreInt64(&cb.failures, 0)
			}
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	atomic.AddInt64(&cb.requests, 1)
	atomic.AddInt64(&cb.failures, 1)
	consecutiveFailures := atomic.AddInt32(&cb.consecutiveFailures, 1)
	atomic.StoreInt64(&cb.lastFailTime, time.Now().UnixNano())

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.window.Record(false)

	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		// Check if we should open the circuit
		failureRate := cb.window.FailureRate()
		shouldOpen := false

		// Check consecutive failures
		if cb.config.MaxFailures > 0 && int(consecutiveFailures) >= cb.config.MaxFailures {
			shouldOpen = true
		}

		// Check failure rate
		if cb.config.FailureThreshold > 0 && failureRate >= cb.config.FailureThreshold {
			shouldOpen = true
		}

		if shouldOpen {
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateClosed), int32(StateOpen)) {
				cb.onStateChange(StateClosed, StateOpen)
				nextReset := time.Now().Add(cb.config.ResetTimeout).UnixNano()
				atomic.StoreInt64(&cb.nextResetTime, nextReset)
			}
		}

	case StateHalfOpen:
		// Any failure in half-open state reopens the circuit
		if atomic.CompareAndSwapInt32(&cb.state, int32(StateHalfOpen), int32(StateOpen)) {
			cb.onStateChange(StateHalfOpen, StateOpen)
			nextReset := time.Now().Add(cb.config.ResetTimeout).UnixNano()
			atomic.StoreInt64(&cb.nextResetTime, nextReset)
		}
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() State {
	return State(atomic.LoadInt32(&cb.state))
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state := State(atomic.LoadInt32(&cb.state))

	return Stats{
		State:               state,
		Requests:            atomic.LoadInt64(&cb.requests),
		Failures:            atomic.LoadInt64(&cb.failures),
		Successes:           atomic.LoadInt64(&cb.successes),
		ConsecutiveFailures: atomic.LoadInt32(&cb.consecutiveFailures),
		FailureRate:         cb.window.FailureRate(),
		LastFailure:         time.Unix(0, atomic.LoadInt64(&cb.lastFailTime)),
		NextReset:           time.Unix(0, atomic.LoadInt64(&cb.nextResetTime)),
		LastStateChange:     cb.lastStateTime,
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := State(atomic.LoadInt32(&cb.state))

	atomic.StoreInt32(&cb.state, int32(StateClosed))
	atomic.StoreInt64(&cb.failures, 0)
	atomic.StoreInt64(&cb.successes, 0)
	atomic.StoreInt64(&cb.requests, 0)
	atomic.StoreInt32(&cb.consecutiveFailures, 0)
	atomic.StoreInt32(&cb.halfOpenRequests, 0)

	cb.window.Reset()
	cb.lastStateTime = time.Now()

	if oldState != StateClosed {
		cb.onStateChange(oldState, StateClosed)
	}
}

// onStateChange calls the state change callback if configured
func (cb *CircuitBreaker) onStateChange(from, to State) {
	cb.lastStateTime = time.Now()

	if cb.config.OnStateChange != nil {
		// Call in goroutine to prevent blocking
		go cb.config.OnStateChange(from, to)
	}
}

// Stats contains circuit breaker statistics
type Stats struct {
	State               State
	Requests            int64
	Failures            int64
	Successes           int64
	ConsecutiveFailures int32
	FailureRate         float64
	LastFailure         time.Time
	NextReset           time.Time
	LastStateChange     time.Time
}

// slidingWindow tracks success/failure rate over a time window
type slidingWindow struct {
	window   time.Duration
	buckets  []bucket
	current  int
	mu       sync.Mutex
}

type bucket struct {
	timestamp time.Time
	successes int64
	failures  int64
}

func newSlidingWindow(window time.Duration) *slidingWindow {
	// Create 10 buckets for the window
	buckets := make([]bucket, 10)
	now := time.Now()
	for i := range buckets {
		buckets[i].timestamp = now
	}

	return &slidingWindow{
		window:  window,
		buckets: buckets,
		current: 0,
	}
}

func (sw *slidingWindow) Record(success bool) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	bucketDuration := sw.window / time.Duration(len(sw.buckets))

	// Rotate to current bucket if needed
	if now.Sub(sw.buckets[sw.current].timestamp) > bucketDuration {
		sw.current = (sw.current + 1) % len(sw.buckets)
		sw.buckets[sw.current] = bucket{
			timestamp: now,
			successes: 0,
			failures:  0,
		}
	}

	if success {
		sw.buckets[sw.current].successes++
	} else {
		sw.buckets[sw.current].failures++
	}
}

func (sw *slidingWindow) FailureRate() float64 {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	var totalSuccesses, totalFailures int64

	for _, b := range sw.buckets {
		if now.Sub(b.timestamp) <= sw.window {
			totalSuccesses += b.successes
			totalFailures += b.failures
		}
	}

	total := totalSuccesses + totalFailures
	if total == 0 {
		return 0
	}

	return float64(totalFailures) / float64(total)
}

func (sw *slidingWindow) Reset() {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	for i := range sw.buckets {
		sw.buckets[i] = bucket{
			timestamp: now,
			successes: 0,
			failures:  0,
		}
	}
	sw.current = 0
}