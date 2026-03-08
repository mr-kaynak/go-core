package circuitbreaker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.MaxFailures = 2
	cfg.FailureThreshold = 0
	cfg.ResetTimeout = 40 * time.Millisecond
	cfg.HalfOpenMaxRequests = 1
	cfg.ObservationWindow = 500 * time.Millisecond
	cfg.Timeout = 100 * time.Millisecond
	return cfg
}

func TestCircuitBreakerClosedToOpenTransition(t *testing.T) {
	cb := New(testConfig())

	cb.RecordFailure()
	if cb.GetState() != StateClosed {
		t.Fatalf("expected state closed after first failure, got %s", cb.GetState())
	}

	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Fatalf("expected state open after threshold, got %s", cb.GetState())
	}
}

func TestCircuitBreakerOpenToHalfOpenAfterTimeout(t *testing.T) {
	cb := New(testConfig())
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Fatalf("expected state open before reset timeout")
	}

	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("expected allow in half-open after reset timeout")
	}
	if cb.GetState() != StateHalfOpen {
		t.Fatalf("expected half-open state, got %s", cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenToClosedOnSuccess(t *testing.T) {
	cb := New(testConfig())
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	if !cb.Allow() {
		t.Fatalf("expected half-open request allowance")
	}
	cb.RecordSuccess()

	if cb.GetState() != StateClosed {
		t.Fatalf("expected state closed after successful half-open requests, got %s", cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenToOpenOnFailure(t *testing.T) {
	cb := New(testConfig())
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	if !cb.Allow() {
		t.Fatalf("expected half-open request allowance")
	}
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Fatalf("expected state open after half-open failure, got %s", cb.GetState())
	}
}

func TestCircuitBreakerExecuteBehaviorByState(t *testing.T) {
	cb := New(testConfig())

	executed := false
	if err := cb.Execute(func() error {
		executed = true
		return nil
	}); err != nil {
		t.Fatalf("expected execute success in closed state, got %v", err)
	}
	if !executed {
		t.Fatalf("expected function execution in closed state")
	}

	cb.RecordFailure()
	cb.RecordFailure()

	err := cb.Execute(func() error {
		t.Fatalf("function should not run while circuit is open")
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreakerGetStatsAndReset(t *testing.T) {
	cb := New(testConfig())

	_ = cb.Execute(func() error { return nil })
	_ = cb.Execute(func() error { return errors.New("boom") })

	stats := cb.GetStats()
	if stats.Requests != 2 {
		t.Fatalf("expected 2 requests, got %d", stats.Requests)
	}
	if stats.Successes != 1 || stats.Failures != 1 {
		t.Fatalf("expected success/failure 1/1, got %d/%d", stats.Successes, stats.Failures)
	}

	cb.Reset()
	stats = cb.GetStats()
	if stats.State != StateClosed {
		t.Fatalf("expected closed after reset, got %s", stats.State)
	}
	if stats.Requests != 0 || stats.Failures != 0 || stats.Successes != 0 {
		t.Fatalf("expected zeroed stats after reset, got %+v", stats)
	}
}

func TestCircuitBreakerConcurrentAccess(t *testing.T) {
	cb := New(testConfig())
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = cb.Execute(func() error {
				if i%5 == 0 {
					return errors.New("intermittent")
				}
				return nil
			})
		}(i)
	}

	wg.Wait()
	stats := cb.GetStats()
	if stats.Requests == 0 {
		t.Fatalf("expected non-zero requests under concurrent load")
	}
}
