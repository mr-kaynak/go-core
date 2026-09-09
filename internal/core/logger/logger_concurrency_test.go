package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestInitializeConcurrentIsRaceFree asserts that concurrent initializers and
// concurrent loggers do not race on the process-global logger. The -race
// runtime is the real assertion here; the checks after the wait group only
// prove the global ended up in a single consistent state.
func TestInitializeConcurrentIsRaceFree(t *testing.T) {
	resetDefaultLogger(t)

	dir := t.TempDir()
	levels := []string{"debug", "info", "warn", "error"}
	formats := []string{"json", "text"}

	const goroutines = 16
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			// Every goroutine tries to own the global logger with its own settings.
			output := filepath.Join(dir, fmt.Sprintf("app-%d.log", i))
			if err := Initialize(levels[i%len(levels)], formats[i%len(formats)], output); err != nil {
				t.Errorf("goroutine %d: initialize failed: %v", i, err)
				return
			}

			// And logs through both the instance and the package-level helpers.
			Get().Info("concurrent instance log", "goroutine", i)
			Info("concurrent package log", "goroutine", i)
		}(i)
	}

	close(start)
	wg.Wait()

	l := Get()
	if l == nil {
		t.Fatalf("expected a non-nil logger after concurrent initialization")
	}
	if l != Get() {
		t.Fatalf("expected Get to return a single consistent instance")
	}
	l.Info("logging still works after concurrent initialization")
}

// TestInitializeFirstConfigurationWins asserts first-configuration-wins
// ownership: a later Initialize succeeds but reuses the installed logger
// instead of replacing it.
func TestInitializeFirstConfigurationWins(t *testing.T) {
	resetDefaultLogger(t)

	dir := t.TempDir()
	firstOutput := filepath.Join(dir, "first.log")
	secondOutput := filepath.Join(dir, "second.log")

	if err := Initialize("debug", "json", firstOutput); err != nil {
		t.Fatalf("first initialize failed: %v", err)
	}
	first := Get()
	if !first.IsDebugEnabled() {
		t.Fatalf("expected debug level from the first configuration")
	}

	if err := Initialize("error", "text", secondOutput); err != nil {
		t.Fatalf("expected second initialize to reuse the existing logger, got %v", err)
	}

	second := Get()
	if first != second {
		t.Fatalf("expected the first configuration to keep ownership of the global logger")
	}
	if !second.IsDebugEnabled() {
		t.Fatalf("expected the first configuration's debug level to survive a second initialize")
	}

	second.Debug("still writing to the first output")

	firstData, err := os.ReadFile(firstOutput)
	if err != nil {
		t.Fatalf("failed reading first log output: %v", err)
	}
	if len(firstData) == 0 {
		t.Fatalf("expected log output in the first configuration's file")
	}

	secondData, err := os.ReadFile(secondOutput)
	if err != nil {
		t.Fatalf("failed reading second log output: %v", err)
	}
	if len(secondData) != 0 {
		t.Fatalf("expected the losing configuration's file to stay empty, got: %s", secondData)
	}
}
