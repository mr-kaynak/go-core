package cache

import (
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/config"
)

// unreachableRedisConfig returns a config pointing at a port nothing listens on,
// so connection attempts fail immediately with "connection refused".
func unreachableRedisConfig(mode string) *config.Config {
	return &config.Config{
		Redis: config.RedisConfig{
			Host: "127.0.0.1",
			Port: 1,
			Mode: mode,
		},
	}
}

func TestNewRedisClientWithPolicy_DisabledReturnsNilWithoutError(t *testing.T) {
	client, err := NewRedisClientWithPolicy(unreachableRedisConfig("disabled"))
	if err != nil {
		t.Fatalf("disabled mode must not error, got: %v", err)
	}
	if client != nil {
		t.Fatal("disabled mode must not create a client")
	}
}

func TestNewRedisClientWithPolicy_RequiredFailsStartupWhenUnreachable(t *testing.T) {
	client, err := NewRedisClientWithPolicy(unreachableRedisConfig("required"))
	if err == nil {
		t.Fatal("required mode with unreachable Redis must return an error")
	}
	if client != nil {
		t.Fatal("required mode with unreachable Redis must not return a client")
	}
}

func TestNewRedisClientWithPolicy_EmptyModeDefaultsToRequired(t *testing.T) {
	_, err := NewRedisClientWithPolicy(unreachableRedisConfig(""))
	if err == nil {
		t.Fatal("empty mode must default to required (fail-closed) and error when unreachable")
	}
}

func TestNewRedisClientWithPolicy_OptionalDegradesWithoutError(t *testing.T) {
	client, err := NewRedisClientWithPolicy(unreachableRedisConfig("optional"))
	if err != nil {
		t.Fatalf("optional mode must degrade gracefully, got: %v", err)
	}
	if client != nil {
		t.Fatal("optional mode with unreachable Redis must return nil client")
	}
}

func TestNewRedisClientWithPolicy_UnknownModeIsRejected(t *testing.T) {
	_, err := NewRedisClientWithPolicy(unreachableRedisConfig("sometimes"))
	if err == nil {
		t.Fatal("unknown mode must be rejected")
	}
	if !strings.Contains(err.Error(), "sometimes") {
		t.Fatalf("error should name the invalid mode, got: %v", err)
	}
}
