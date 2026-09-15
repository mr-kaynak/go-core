package server

import (
	"errors"
	"testing"
)

// TestReadinessEntryHidesRawError: /readyz is unauthenticated, so a failing
// dependency must be reported as unhealthy without echoing the driver error
// (which carries hosts, ports and sometimes credentials) to the caller.
func TestReadinessEntryHidesRawError(t *testing.T) {
	entry := readinessEntry(errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"))

	if entry["status"] != "unhealthy" {
		t.Fatalf("expected status unhealthy, got %v", entry["status"])
	}
	if _, leaked := entry["error"]; leaked {
		t.Fatalf("readiness entry must not expose the raw error, got %v", entry)
	}
}

func TestReadinessEntryHealthy(t *testing.T) {
	entry := readinessEntry(nil)
	if entry["status"] != "healthy" {
		t.Fatalf("expected status healthy, got %v", entry["status"])
	}
}
