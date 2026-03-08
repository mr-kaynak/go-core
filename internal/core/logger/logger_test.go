package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetDefaultLogger(t *testing.T) {
	t.Helper()
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })
}

func TestInitializeLevelFormatOutput(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "app.log")

	if err := Initialize("debug", "text", logFile); err != nil {
		t.Fatalf("expected initialize success, got %v", err)
	}

	l := Get()
	if !l.IsDebugEnabled() {
		t.Fatalf("expected debug logging to be enabled")
	}

	l.Debug("debug message", "k", "v")
	l.Info("info message", "k", "v")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log output: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "debug message") {
		t.Fatalf("expected debug message in log output")
	}
	if !strings.Contains(content, "info message") {
		t.Fatalf("expected info message in log output")
	}
}

func TestGetReturnsSingleton(t *testing.T) {
	resetDefaultLogger(t)
	if err := Initialize("info", "json", "stdout"); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l1 := Get()
	l2 := Get()
	if l1 != l2 {
		t.Fatalf("expected Get to return singleton instance")
	}
}

func TestLogLevelFilteringInfoDropsDebug(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "level.log")

	if err := Initialize("info", "text", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get()
	l.Debug("should be filtered")
	l.Info("should stay")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log output: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "should be filtered") {
		t.Fatalf("expected debug log to be filtered at info level")
	}
	if !strings.Contains(content, "should stay") {
		t.Fatalf("expected info log to be present")
	}
}
