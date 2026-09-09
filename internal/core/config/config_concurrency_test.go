package config

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestLoadConcurrentIsRaceFree asserts that concurrent Load calls do not race
// on the package globals. The -race runtime is the real assertion; the checks
// on each returned config prove that every caller got its own fully populated
// instance instead of a half-built one shared through the global.
func TestLoadConcurrentIsRaceFree(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SECURITY_ENCRYPTION_KEY", "test-encryption-key-that-is-at-least-32-chars")

	// An explicit non-existent path keeps every goroutine on the same
	// defaults + env inputs, independent of the working directory.
	configPath := filepath.Join(t.TempDir(), "missing.yaml")

	const goroutines = 8
	loaded := make([]*Config, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			loaded[i], errs[i] = Load(configPath)
		}(i)
	}

	close(start)
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: expected Load to succeed, got %v", i, errs[i])
		}
		c := loaded[i]
		if c == nil {
			t.Fatalf("goroutine %d: expected a non-nil config", i)
		}
		if c.App.Name != "go-core" {
			t.Fatalf("goroutine %d: expected default app name go-core, got %q", i, c.App.Name)
		}
		// Durations are parsed after unmarshal; a zero value here means the
		// parse landed on another goroutine's config.
		if c.JWT.Expiry == 0 {
			t.Fatalf("goroutine %d: expected jwt.expiry to be parsed", i)
		}
		if c.Database.ConnMaxLifetime == 0 {
			t.Fatalf("goroutine %d: expected database.conn_max_lifetime to be parsed", i)
		}
		if c.Security.AccountLockDuration == 0 {
			t.Fatalf("goroutine %d: expected security.account_lock_duration to be parsed", i)
		}
	}

	global := Get()
	if global == nil {
		t.Fatalf("expected a non-nil global config after concurrent loads")
	}
	if global.JWT.Expiry == 0 {
		t.Fatalf("expected the published global config to have parsed durations")
	}
}

// TestLoadFailureDoesNotPublishGlobal asserts that a Load that fails
// validation never publishes its half-built config to the process-global slot.
func TestLoadFailureDoesNotPublishGlobal(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SECURITY_ENCRYPTION_KEY", "test-encryption-key-that-is-at-least-32-chars")

	good, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("expected Load to succeed with required envs, got %v", err)
	}
	if Get() != good {
		t.Fatalf("expected an explicit successful Load to publish the global config")
	}

	// Drop a required env var so the next Load fails validation.
	t.Setenv("SECURITY_ENCRYPTION_KEY", "")
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected Load to fail without a valid encryption key")
	}

	if Get() != good {
		t.Fatalf("expected the failed Load to leave the previously published global config in place")
	}
}
