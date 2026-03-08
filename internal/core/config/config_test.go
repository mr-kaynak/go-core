package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_NAME", "go_core_test")
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("RABBITMQ_EXCHANGE", "go-core")
	t.Setenv("RABBITMQ_QUEUE_PREFIX", "go-core")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
	t.Setenv("JWT_REFRESH_SECRET", "refresh-secret-key-at-least-32-chars!")
	t.Setenv("JWT_ISSUER", "go-core-tests")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM_EMAIL", "noreply@example.com")
	t.Setenv("SMTP_FROM_NAME", "Go Core")
}

func TestLoadDefaultsAndEnvOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_PORT", "8088")
	t.Setenv("SECURITY_ENCRYPTION_KEY", "test-production-key-that-is-at-least-32-chars-long")
	t.Setenv("DATABASE_SSL_MODE", "require")

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("expected Load to succeed with required envs, got %v", err)
	}

	loaded := Get()
	if loaded.App.Name != "go-core" {
		t.Fatalf("expected default app name go-core, got %s", loaded.App.Name)
	}
	if loaded.App.Env != "production" {
		t.Fatalf("expected APP_ENV override to production, got %s", loaded.App.Env)
	}
	if loaded.App.Port != 8088 {
		t.Fatalf("expected APP_PORT override to 8088, got %d", loaded.App.Port)
	}
	if loaded.Redis.Host != "localhost" || loaded.Redis.Port != 6379 {
		t.Fatalf("expected default redis localhost:6379, got %s:%d", loaded.Redis.Host, loaded.Redis.Port)
	}
}

func TestConfigHelpersDSNAndEnvironmentChecks(t *testing.T) {
	c := &Config{
		App: AppConfig{Env: "staging"},
		Database: DatabaseConfig{
			Host:     "db.local",
			Port:     5432,
			User:     "tester",
			Password: "pwd",
			Name:     "core",
			SSLMode:  "disable",
		},
		Redis: RedisConfig{Host: "redis.local", Port: 6379},
	}

	dsn := c.GetDSN()
	if !strings.Contains(dsn, "host=db.local") || !strings.Contains(dsn, "dbname=core") {
		t.Fatalf("expected dsn to include host and dbname, got %s", dsn)
	}
	if c.GetRedisAddr() != "redis.local:6379" {
		t.Fatalf("expected redis address redis.local:6379, got %s", c.GetRedisAddr())
	}
	if c.IsDevelopment() {
		t.Fatalf("expected IsDevelopment=false for staging")
	}
	if c.IsProduction() {
		t.Fatalf("expected IsProduction=false for staging")
	}
	if !c.IsStaging() {
		t.Fatalf("expected IsStaging=true for staging")
	}
}

func TestLoadValidationFailureOnMissingRequiredFields(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatalf("expected validation error for missing required config fields")
	}
	if !strings.Contains(err.Error(), "configuration validation failed") {
		t.Fatalf("expected configuration validation failure, got %v", err)
	}
}
