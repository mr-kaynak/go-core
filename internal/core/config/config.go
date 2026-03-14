package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	App          AppConfig          `mapstructure:"app" validate:"required"`
	Database     DatabaseConfig     `mapstructure:"database" validate:"required"`
	Redis        RedisConfig        `mapstructure:"redis" validate:"required"`
	RabbitMQ     RabbitMQConfig     `mapstructure:"rabbitmq" validate:"required"`
	JWT          JWTConfig          `mapstructure:"jwt" validate:"required"`
	Email        EmailConfig        `mapstructure:"email" validate:"required"`
	Casbin       CasbinConfig       `mapstructure:"casbin"`
	OTEL         OTELConfig         `mapstructure:"otel"`
	Metrics      MetricsConfig      `mapstructure:"metrics"`
	Tracing      TracingConfig      `mapstructure:"tracing"`
	GRPC         GRPCConfig         `mapstructure:"grpc"`
	Log          LogConfig          `mapstructure:"log"`
	Storage      StorageConfig      `mapstructure:"storage"`
	Security     SecurityConfig     `mapstructure:"security"`
	CORS         CORSConfig         `mapstructure:"cors"`
	RateLimit    RateLimitConfig    `mapstructure:"rate_limit"`
	FCM          FCMConfig          `mapstructure:"fcm"`
	SMS          SMSConfig          `mapstructure:"sms"`
	Webhook      WebhookConfig      `mapstructure:"webhook"`
	Notification NotificationConfig `mapstructure:"notification"`
	Blog         BlogConfig         `mapstructure:"blog"`

	v *viper.Viper // local viper instance used by Get* helpers
}

// BlogConfig holds blog module configuration
type BlogConfig struct {
	PostsPerPage        int             `mapstructure:"posts_per_page"`
	ViewCooldown        time.Duration   `mapstructure:"view_cooldown"`
	MaxMediaSize        int64           `mapstructure:"max_media_size"`
	FeedItemLimit       int             `mapstructure:"feed_item_limit"`
	SiteURL             string          `mapstructure:"site_url"`
	ReadTimeWPM         int             `mapstructure:"read_time_wpm"`
	AutoApproveComments bool            `mapstructure:"auto_approve_comments"`
	TrendingWeights     TrendingWeights `mapstructure:"trending_weights"`
}

// TrendingWeights holds weights for trending score calculation
type TrendingWeights struct {
	View    int `mapstructure:"view"`
	Like    int `mapstructure:"like"`
	Comment int `mapstructure:"comment"`
	Share   int `mapstructure:"share"`
}

// NotificationConfig holds notification worker pool configuration
type NotificationConfig struct {
	MaxWorkers      int           `mapstructure:"max_workers"`
	UseRabbitMQ     bool          `mapstructure:"use_rabbitmq"`
	QueueName       string        `mapstructure:"queue_name"`
	PendingInterval time.Duration `mapstructure:"pending_interval"`
	RetryInterval   time.Duration `mapstructure:"retry_interval"`
}

// FCMConfig holds Firebase Cloud Messaging configuration
type FCMConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	CredentialsFile string `mapstructure:"credentials_file"`
	ProjectID       string `mapstructure:"project_id"`
}

// SMSConfig holds SMS provider configuration.
// Implement the SMSProvider interface with your preferred provider (Twilio, AWS SNS, etc.)
// and wire it via NotificationService.SetSMSProvider().
type SMSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Provider string `mapstructure:"provider"` // twilio, aws_sns, vonage, etc.
}

// WebhookConfig holds webhook notification delivery configuration
type WebhookConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	Secret     string        `mapstructure:"secret"`
	Timeout    time.Duration `mapstructure:"timeout"`
	MaxRetries int           `mapstructure:"max_retries"`
}

// AppConfig holds application-specific configuration
type AppConfig struct {
	Name         string `mapstructure:"name" validate:"required"`
	Env          string `mapstructure:"env" validate:"required,oneof=development staging production"`
	Port         int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Version      string `mapstructure:"version" validate:"required"`
	Debug        bool   `mapstructure:"debug"`
	FrontendURL  string `mapstructure:"frontend_url"`   // Base URL for frontend (email links, etc.)
	ErrorDocsURL string `mapstructure:"error_docs_url"` // Base URL for error documentation (RFC 7807 type field)
	BodyLimit    int    `mapstructure:"body_limit"`     // HTTP request body limit in bytes
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host               string        `mapstructure:"host" validate:"required"`
	Port               int           `mapstructure:"port" validate:"required,min=1,max=65535"`
	Name               string        `mapstructure:"name" validate:"required"`
	User               string        `mapstructure:"user" validate:"required"`
	Password           string        `mapstructure:"password"`
	SSLMode            string        `mapstructure:"ssl_mode" validate:"required,oneof=disable require verify-ca verify-full"`
	MaxOpenConns       int           `mapstructure:"max_open_conns" validate:"min=1"`
	MaxIdleConns       int           `mapstructure:"max_idle_conns" validate:"min=1"`
	ConnMaxLifetime    time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime    time.Duration `mapstructure:"conn_max_idle_time"`
	SlowQueryThreshold time.Duration `mapstructure:"slow_query_threshold"`
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host            string        `mapstructure:"host" validate:"required"`
	Port            int           `mapstructure:"port" validate:"required,min=1,max=65535"`
	Password        string        `mapstructure:"password"`
	DB              int           `mapstructure:"db" validate:"min=0"`
	PoolSize        int           `mapstructure:"pool_size" validate:"min=1"`
	MinIdleConns    int           `mapstructure:"min_idle_conns"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	CBThreshold     int           `mapstructure:"cb_failure_threshold"` // Circuit breaker failure threshold
	CBResetTimeout  time.Duration `mapstructure:"cb_reset_timeout"`     // Circuit breaker reset timeout
}

// RabbitMQConfig holds RabbitMQ configuration
type RabbitMQConfig struct {
	URL                       string        `mapstructure:"url" validate:"required,url"`
	Exchange                  string        `mapstructure:"exchange" validate:"required"`
	QueuePrefix               string        `mapstructure:"queue_prefix" validate:"required"`
	PrefetchCount             int           `mapstructure:"prefetch_count"`
	ProcessedMessageRetention time.Duration `mapstructure:"processed_message_retention"`
	OutboxBatchSize           int           `mapstructure:"outbox_batch_size"`
	OutboxMaxRetry            int           `mapstructure:"outbox_max_retry"`
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret        string        `mapstructure:"secret" validate:"required,min=32"`
	RefreshSecret string        `mapstructure:"refresh_secret" validate:"required,min=32"`
	Expiry        time.Duration `mapstructure:"expiry" validate:"required"`
	RefreshExpiry time.Duration `mapstructure:"refresh_expiry" validate:"required"`
	Issuer        string        `mapstructure:"issuer" validate:"required"`
}

// EmailConfig holds email configuration
type EmailConfig struct {
	SMTPHost     string `mapstructure:"smtp_host" validate:"required"`
	SMTPPort     int    `mapstructure:"smtp_port" validate:"required,min=1,max=65535"`
	SMTPUser     string `mapstructure:"smtp_user"`
	SMTPPassword string `mapstructure:"smtp_password"`
	FromEmail    string `mapstructure:"from_email" validate:"required,email"`
	FromName     string `mapstructure:"from_name" validate:"required"`
}

// CasbinConfig holds Casbin configuration
type CasbinConfig struct {
	ModelPath  string `mapstructure:"model_path"`
	PolicyPath string `mapstructure:"policy_path"`
}

// OTELConfig holds OpenTelemetry configuration
type OTELConfig struct {
	Endpoint       string  `mapstructure:"endpoint"`
	ServiceName    string  `mapstructure:"service_name"`
	TracesEnabled  bool    `mapstructure:"traces_enabled"`
	MetricsEnabled bool    `mapstructure:"metrics_enabled"`
	Insecure       bool    `mapstructure:"insecure"`    // Use plaintext gRPC; must be explicitly enabled
	SampleRate     float64 `mapstructure:"sample_rate"` // Trace sampling rate (0.0-1.0) for non-dev environments
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port" validate:"min=1,max=65535"`
	Path    string `mapstructure:"path"`
}

// GRPCConfig holds gRPC configuration
type GRPCConfig struct {
	Port              int    `mapstructure:"port" validate:"min=1,max=65535"`
	ReflectionEnabled bool   `mapstructure:"reflection_enabled"`
	TLSCertFile       string `mapstructure:"tls_cert_file"`
	TLSKeyFile        string `mapstructure:"tls_key_file"`
}

// TracingConfig holds tracing configuration
type TracingConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	ServiceName    string `mapstructure:"service_name"`
	Exporter       string `mapstructure:"exporter"`
	JaegerEndpoint string `mapstructure:"jaeger_endpoint"`
	OTLPEndpoint   string `mapstructure:"otlp_endpoint"`
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level  string `mapstructure:"level" validate:"oneof=debug info warn error"`
	Format string `mapstructure:"format" validate:"oneof=json text"`
	Output string `mapstructure:"output"`
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	Type             string        `mapstructure:"type" validate:"oneof=local s3"`
	LocalPath        string        `mapstructure:"local_path"`
	MaxFileSize      int64         `mapstructure:"max_file_size" validate:"min=1"`
	S3Endpoint       string        `mapstructure:"s3_endpoint"`
	S3Bucket         string        `mapstructure:"s3_bucket"`
	S3Region         string        `mapstructure:"s3_region"`
	S3AccessKey      string        `mapstructure:"s3_access_key"`
	S3SecretKey      string        `mapstructure:"s3_secret_key"`
	S3UseSSL         bool          `mapstructure:"s3_use_ssl"`
	S3PresignTTL     time.Duration `mapstructure:"s3_presign_ttl"`
	S3PublicEndpoint string        `mapstructure:"s3_public_endpoint"`
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	BCryptCost          int           `mapstructure:"bcrypt_cost" validate:"min=4,max=31"`
	APIKeyHeader        string        `mapstructure:"api_key_header"`
	EncryptionKey       string        `mapstructure:"encryption_key" validate:"required,min=32"`
	MaxLoginAttempts    int           `mapstructure:"max_login_attempts"`
	AccountLockDuration time.Duration `mapstructure:"account_lock_duration"`
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowedMethods   []string `mapstructure:"allowed_methods"`
	AllowedHeaders   []string `mapstructure:"allowed_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	PerMinute int `mapstructure:"per_minute" validate:"min=1"`
	Burst     int `mapstructure:"burst" validate:"min=1"`
}

var (
	// cfg holds the global configuration
	cfg *Config
	// validate is used for configuration validation
	validate *validator.Validate
	// cfgOnce guards lazy initialization of the global config
	cfgOnce sync.Once
)

// Load loads configuration from environment variables and config files
func Load(configPath ...string) (*Config, error) {
	v := viper.New()
	validate = validator.New()

	// Set default values
	setDefaults(v)

	// Load from environment variables
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// mustBindEnv panics if BindEnv fails (runs at startup, fail fast)
	mustBindEnv := func(input ...string) {
		if err := v.BindEnv(input...); err != nil {
			panic(fmt.Sprintf("config: failed to bind env %v: %v", input, err))
		}
	}

	// Bind specific environment variables
	mustBindEnv("database.name", "DB_NAME")
	mustBindEnv("database.user", "DB_USER")
	mustBindEnv("database.password", "DB_PASSWORD")
	mustBindEnv("rabbitmq.url", "RABBITMQ_URL")
	mustBindEnv("rabbitmq.exchange", "RABBITMQ_EXCHANGE")
	mustBindEnv("rabbitmq.queue_prefix", "RABBITMQ_QUEUE_PREFIX")
	mustBindEnv("jwt.secret", "JWT_SECRET")
	mustBindEnv("jwt.issuer", "JWT_ISSUER")
	mustBindEnv("jwt.expiry", "JWT_EXPIRY")
	mustBindEnv("jwt.refresh_secret", "JWT_REFRESH_SECRET")
	mustBindEnv("jwt.refresh_expiry", "JWT_REFRESH_EXPIRY")
	mustBindEnv("email.smtp_host", "SMTP_HOST")
	mustBindEnv("email.smtp_port", "SMTP_PORT")
	mustBindEnv("email.from_email", "SMTP_FROM_EMAIL")
	mustBindEnv("email.from_name", "SMTP_FROM_NAME")
	mustBindEnv("storage.s3_endpoint", "STORAGE_S3_ENDPOINT")
	mustBindEnv("storage.s3_bucket", "STORAGE_S3_BUCKET")
	mustBindEnv("storage.s3_region", "STORAGE_S3_REGION")
	mustBindEnv("storage.s3_access_key", "STORAGE_S3_ACCESS_KEY")
	mustBindEnv("storage.s3_secret_key", "STORAGE_S3_SECRET_KEY")
	mustBindEnv("storage.s3_use_ssl", "STORAGE_S3_USE_SSL")
	mustBindEnv("storage.s3_presign_ttl", "STORAGE_S3_PRESIGN_TTL")
	mustBindEnv("storage.s3_public_endpoint", "STORAGE_S3_PUBLIC_ENDPOINT")
	mustBindEnv("security.encryption_key", "SECURITY_ENCRYPTION_KEY")

	// OpenTelemetry bindings
	mustBindEnv("otel.endpoint", "OTEL_ENDPOINT")
	mustBindEnv("otel.service_name", "OTEL_SERVICE_NAME")
	mustBindEnv("otel.traces_enabled", "OTEL_TRACES_ENABLED")
	mustBindEnv("otel.metrics_enabled", "OTEL_METRICS_ENABLED")
	mustBindEnv("otel.sample_rate", "OTEL_SAMPLE_RATE")
	mustBindEnv("otel.insecure", "OTEL_INSECURE")

	// FCM bindings
	mustBindEnv("fcm.enabled", "FCM_ENABLED")
	mustBindEnv("fcm.credentials_file", "FCM_CREDENTIALS_FILE")
	mustBindEnv("fcm.project_id", "FCM_PROJECT_ID")

	// SMS bindings
	mustBindEnv("sms.enabled", "SMS_ENABLED")
	mustBindEnv("sms.provider", "SMS_PROVIDER")

	// Webhook bindings
	mustBindEnv("webhook.enabled", "WEBHOOK_ENABLED")
	mustBindEnv("webhook.secret", "WEBHOOK_SECRET")
	mustBindEnv("webhook.timeout", "WEBHOOK_TIMEOUT")
	mustBindEnv("webhook.max_retries", "WEBHOOK_MAX_RETRIES")

	// Notification bindings
	mustBindEnv("notification.use_rabbitmq", "NOTIFICATION_USE_RABBITMQ")
	mustBindEnv("notification.queue_name", "NOTIFICATION_QUEUE_NAME")
	mustBindEnv("notification.pending_interval", "NOTIFICATION_PENDING_INTERVAL")
	mustBindEnv("notification.retry_interval", "NOTIFICATION_RETRY_INTERVAL")

	// SSE bindings
	mustBindEnv("sse.enabled", "SSE_ENABLED")
	mustBindEnv("sse.enable_redis", "SSE_ENABLE_REDIS")
	mustBindEnv("sse.enable_metrics", "SSE_ENABLE_METRICS")

	// Load from config file if provided
	if len(configPath) > 0 && configPath[0] != "" {
		dir := filepath.Dir(configPath[0])
		file := filepath.Base(configPath[0])
		ext := filepath.Ext(file)
		name := strings.TrimSuffix(file, ext)

		v.SetConfigName(name)
		v.SetConfigType(strings.TrimPrefix(ext, "."))
		v.AddConfigPath(dir)

		if err := v.ReadInConfig(); err != nil {
			// It's okay if the config file doesn't exist
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("failed to read config file: %w", err)
			}
		}
	} else {
		// Try to load from default locations
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")

		// Check environment-specific config
		env := os.Getenv("APP_ENV")
		if env != "" {
			v.SetConfigName(fmt.Sprintf("config.%s", env))
		}

		// Read config file (ignore error if not found)
		_ = v.ReadInConfig()
	}

	// Unmarshal configuration
	cfg = &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Store viper instance for Get* helpers
	cfg.v = v

	// Parse durations
	if err := parseDurations(v); err != nil {
		return nil, fmt.Errorf("failed to parse duration config: %w", err)
	}

	// Validate configuration
	if err := validate.Struct(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Reject known placeholder secrets in all environments
	if cfg.Security.EncryptionKey == "change-me-in-production-this-is-minimum-32-chars" {
		return nil, fmt.Errorf("SECURITY_ENCRYPTION_KEY must be changed from placeholder value")
	}
	if strings.HasPrefix(cfg.JWT.Secret, "your-super-secret") {
		return nil, fmt.Errorf("JWT_SECRET must be changed from placeholder value")
	}
	if strings.HasPrefix(cfg.JWT.RefreshSecret, "your-super-secret") {
		return nil, fmt.Errorf("JWT_REFRESH_SECRET must be changed from placeholder value")
	}

	// Production/staging guards
	if cfg.IsProduction() || cfg.IsStaging() {
		if cfg.Database.SSLMode == "disable" {
			return nil, fmt.Errorf("database.ssl_mode must not be 'disable' in %s environment", cfg.App.Env)
		}
	}

	return cfg, nil
}

// Get returns the global configuration.
// Panics if Load() was not called explicitly and auto-loading fails.
func Get() *Config {
	cfgOnce.Do(func() {
		if cfg != nil {
			return // already set by an explicit Load() call
		}
		c, err := Load()
		if err != nil {
			panic(fmt.Sprintf("config: failed to load: %v", err))
		}
		cfg = c
	})
	return cfg
}

// Default configuration values
const (
	defaultAppPort         = 3000
	defaultDBPort          = 5432
	defaultDBMaxOpenConns  = 25
	defaultRedisPort       = 6379
	defaultMetricsPort     = 9090
	defaultGRPCPort        = 50051
	defaultMaxFileSize     = 10485760 // 10MB
	defaultBcryptCost      = 12
	defaultRateLimitPerMin = 60
)

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// App defaults
	v.SetDefault("app.name", "go-core")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.port", defaultAppPort)
	v.SetDefault("app.version", "1.0.0")
	v.SetDefault("app.debug", false)
	v.SetDefault("app.body_limit", 4*1024*1024) // 4MB

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", defaultDBPort)
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.max_open_conns", defaultDBMaxOpenConns)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")
	v.SetDefault("database.conn_max_idle_time", "5m")
	v.SetDefault("database.slow_query_threshold", "200ms")

	// Redis defaults
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", defaultRedisPort)
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 10)
	v.SetDefault("redis.min_idle_conns", 5)
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.write_timeout", "3s")
	v.SetDefault("redis.conn_max_idle_time", "5m")
	v.SetDefault("redis.conn_max_lifetime", "30m")
	v.SetDefault("redis.cb_failure_threshold", 5)
	v.SetDefault("redis.cb_reset_timeout", "30s")

	// RabbitMQ defaults
	v.SetDefault("rabbitmq.prefetch_count", 10)
	v.SetDefault("rabbitmq.processed_message_retention", "24h")
	v.SetDefault("rabbitmq.outbox_batch_size", 10)
	v.SetDefault("rabbitmq.outbox_max_retry", 5)

	// JWT defaults
	v.SetDefault("jwt.expiry", "15m")
	v.SetDefault("jwt.refresh_expiry", "168h")

	// OTEL defaults
	v.SetDefault("otel.sample_rate", 0.1)

	// Metrics defaults
	v.SetDefault("metrics.port", defaultMetricsPort)
	v.SetDefault("metrics.path", "/metrics")

	// gRPC defaults
	v.SetDefault("grpc.port", defaultGRPCPort)
	v.SetDefault("grpc.reflection_enabled", false)

	// Logging defaults
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.output", "stdout")

	// Storage defaults
	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.local_path", "./uploads")
	v.SetDefault("storage.max_file_size", defaultMaxFileSize)
	v.SetDefault("storage.s3_endpoint", "localhost:9000")
	v.SetDefault("storage.s3_bucket", "go-core")
	v.SetDefault("storage.s3_region", "us-east-1")
	v.SetDefault("storage.s3_use_ssl", false)
	v.SetDefault("storage.s3_presign_ttl", "15m")

	// Security defaults
	v.SetDefault("security.bcrypt_cost", defaultBcryptCost)
	v.SetDefault("security.api_key_header", "X-API-Key")
	v.SetDefault("security.max_login_attempts", 5)
	v.SetDefault("security.account_lock_duration", "15m")
	// No default for encryption_key — must be explicitly provided via SECURITY_ENCRYPTION_KEY

	// CORS defaults
	v.SetDefault("cors.allowed_origins", []string{"http://localhost:3000"})
	v.SetDefault("cors.allowed_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allowed_headers", []string{"Origin", "Content-Type", "Accept", "Authorization"})
	v.SetDefault("cors.allow_credentials", true)

	// Rate limit defaults
	v.SetDefault("rate_limit.per_minute", defaultRateLimitPerMin)
	v.SetDefault("rate_limit.burst", 10)

	// FCM defaults
	v.SetDefault("fcm.enabled", false)

	// SMS defaults
	v.SetDefault("sms.enabled", false)

	// Webhook defaults
	v.SetDefault("webhook.enabled", false)
	v.SetDefault("webhook.timeout", "10s")
	v.SetDefault("webhook.max_retries", 3)

	// SSE defaults
	v.SetDefault("sse.enabled", false)
	v.SetDefault("sse.enable_redis", false)
	v.SetDefault("sse.enable_metrics", false)

	// Notification worker pool defaults
	v.SetDefault("notification.max_workers", 50)
	v.SetDefault("notification.use_rabbitmq", false)
	v.SetDefault("notification.queue_name", "notifications.process")
	v.SetDefault("notification.pending_interval", "60s")
	v.SetDefault("notification.retry_interval", "120s")

	// Blog defaults
	v.SetDefault("blog.posts_per_page", 20)
	v.SetDefault("blog.view_cooldown", "30m")
	const defaultMaxMediaSize = 10 * 1024 * 1024 // 10MB
	const defaultReadTimeWPM = 200

	v.SetDefault("blog.max_media_size", defaultMaxMediaSize)
	v.SetDefault("blog.feed_item_limit", 50)
	v.SetDefault("blog.site_url", "http://localhost:3000")
	v.SetDefault("blog.read_time_wpm", defaultReadTimeWPM)
	v.SetDefault("blog.auto_approve_comments", false)
	v.SetDefault("blog.trending_weights.view", 1)
	v.SetDefault("blog.trending_weights.like", 3)
	v.SetDefault("blog.trending_weights.comment", 5)
	v.SetDefault("blog.trending_weights.share", 2)
}

// parseDuration reads a duration string from viper by key and stores it in dest.
func parseDuration(v *viper.Viper, key string, dest *time.Duration) error {
	if s := v.GetString(key); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid %s %q: %w", key, s, err)
		}
		*dest = d
	}
	return nil
}

// parseDurations parses duration strings from configuration
func parseDurations(v *viper.Viper) error {
	// Simple key→dest mappings
	durations := []struct {
		key  string
		dest *time.Duration
	}{
		{"jwt.expiry", &cfg.JWT.Expiry},
		{"jwt.refresh_expiry", &cfg.JWT.RefreshExpiry},
		{"database.conn_max_lifetime", &cfg.Database.ConnMaxLifetime},
		{"database.conn_max_idle_time", &cfg.Database.ConnMaxIdleTime},
		{"database.slow_query_threshold", &cfg.Database.SlowQueryThreshold},
		{"storage.s3_presign_ttl", &cfg.Storage.S3PresignTTL},
		{"webhook.timeout", &cfg.Webhook.Timeout},
		{"notification.pending_interval", &cfg.Notification.PendingInterval},
		{"notification.retry_interval", &cfg.Notification.RetryInterval},
		{"blog.view_cooldown", &cfg.Blog.ViewCooldown},
		{"redis.read_timeout", &cfg.Redis.ReadTimeout},
		{"redis.write_timeout", &cfg.Redis.WriteTimeout},
		{"redis.conn_max_idle_time", &cfg.Redis.ConnMaxIdleTime},
		{"redis.conn_max_lifetime", &cfg.Redis.ConnMaxLifetime},
		{"redis.cb_reset_timeout", &cfg.Redis.CBResetTimeout},
		{"rabbitmq.processed_message_retention", &cfg.RabbitMQ.ProcessedMessageRetention},
		{"security.account_lock_duration", &cfg.Security.AccountLockDuration},
	}
	for _, d := range durations {
		if err := parseDuration(v, d.key, d.dest); err != nil {
			return err
		}
	}

	return nil
}

// IsDevelopment returns true if the environment is development
func (c *Config) IsDevelopment() bool {
	return strings.EqualFold(c.App.Env, "development")
}

// IsProduction returns true if the environment is production
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.App.Env, "production")
}

// IsStaging returns true if the environment is staging
func (c *Config) IsStaging() bool {
	return strings.EqualFold(c.App.Env, "staging")
}

// GetDSN returns the database connection string
func (c *Config) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Database.Host,
		c.Database.Port,
		c.Database.User,
		c.Database.Password,
		c.Database.Name,
		c.Database.SSLMode,
	)
}

// GetRedisAddr returns the Redis address
func (c *Config) GetRedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Redis.Host, c.Redis.Port)
}

// GetBool returns a boolean value from viper by key
func (c *Config) GetBool(key string) bool {
	if c.v != nil {
		return c.v.GetBool(key)
	}
	return viper.GetBool(key)
}

// GetInt returns an integer value from viper by key
func (c *Config) GetInt(key string) int {
	if c.v != nil {
		return c.v.GetInt(key)
	}
	return viper.GetInt(key)
}

// GetString returns a string value from viper by key
func (c *Config) GetString(key string) string {
	if c.v != nil {
		return c.v.GetString(key)
	}
	return viper.GetString(key)
}

// GetDuration returns a duration value from viper by key
func (c *Config) GetDuration(key string) time.Duration {
	if c.v != nil {
		return c.v.GetDuration(key)
	}
	return viper.GetDuration(key)
}
