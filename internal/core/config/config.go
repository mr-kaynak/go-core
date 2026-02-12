package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	App       AppConfig       `mapstructure:"app" validate:"required"`
	Database  DatabaseConfig  `mapstructure:"database" validate:"required"`
	Redis     RedisConfig     `mapstructure:"redis" validate:"required"`
	RabbitMQ  RabbitMQConfig  `mapstructure:"rabbitmq" validate:"required"`
	JWT       JWTConfig       `mapstructure:"jwt" validate:"required"`
	Email     EmailConfig     `mapstructure:"email" validate:"required"`
	Casbin    CasbinConfig    `mapstructure:"casbin"`
	OTEL      OTELConfig      `mapstructure:"otel"`
	Metrics   MetricsConfig   `mapstructure:"metrics"`
	Tracing   TracingConfig   `mapstructure:"tracing"`
	GRPC      GRPCConfig      `mapstructure:"grpc"`
	Log       LogConfig       `mapstructure:"log"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Security  SecurityConfig  `mapstructure:"security"`
	CORS      CORSConfig      `mapstructure:"cors"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
}

// AppConfig holds application-specific configuration
type AppConfig struct {
	Name         string `mapstructure:"name" validate:"required"`
	Env          string `mapstructure:"env" validate:"required,oneof=development staging production"`
	Port         int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Version      string `mapstructure:"version" validate:"required"`
	Debug        bool   `mapstructure:"debug"`
	ErrorBaseURL string `mapstructure:"error_base_url"` // Base URL for error documentation
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host            string        `mapstructure:"host" validate:"required"`
	Port            int           `mapstructure:"port" validate:"required,min=1,max=65535"`
	Name            string        `mapstructure:"name" validate:"required"`
	User            string        `mapstructure:"user" validate:"required"`
	Password        string        `mapstructure:"password"`
	SSLMode         string        `mapstructure:"ssl_mode" validate:"required,oneof=disable require verify-ca verify-full"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" validate:"min=1"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" validate:"min=1"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db" validate:"min=0"`
	PoolSize int    `mapstructure:"pool_size" validate:"min=1"`
}

// RabbitMQConfig holds RabbitMQ configuration
type RabbitMQConfig struct {
	URL         string `mapstructure:"url" validate:"required,url"`
	Exchange    string `mapstructure:"exchange" validate:"required"`
	QueuePrefix string `mapstructure:"queue_prefix" validate:"required"`
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret        string        `mapstructure:"secret" validate:"required,min=32"`
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
	Endpoint       string `mapstructure:"endpoint"`
	ServiceName    string `mapstructure:"service_name"`
	TracesEnabled  bool   `mapstructure:"traces_enabled"`
	MetricsEnabled bool   `mapstructure:"metrics_enabled"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port" validate:"min=1,max=65535"`
	Path    string `mapstructure:"path"`
}

// GRPCConfig holds gRPC configuration
type GRPCConfig struct {
	Port              int  `mapstructure:"port" validate:"min=1,max=65535"`
	ReflectionEnabled bool `mapstructure:"reflection_enabled"`
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
	Type        string `mapstructure:"type" validate:"oneof=local s3"`
	LocalPath   string `mapstructure:"local_path"`
	MaxFileSize int64  `mapstructure:"max_file_size" validate:"min=1"`
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	BCryptCost   int    `mapstructure:"bcrypt_cost" validate:"min=4,max=31"`
	APIKeyHeader string `mapstructure:"api_key_header"`
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

	// Bind specific environment variables
	v.BindEnv("database.name", "DB_NAME")
	v.BindEnv("database.user", "DB_USER")
	v.BindEnv("database.password", "DB_PASSWORD")
	v.BindEnv("rabbitmq.url", "RABBITMQ_URL")
	v.BindEnv("rabbitmq.exchange", "RABBITMQ_EXCHANGE")
	v.BindEnv("rabbitmq.queue_prefix", "RABBITMQ_QUEUE_PREFIX")
	v.BindEnv("jwt.secret", "JWT_SECRET")
	v.BindEnv("jwt.issuer", "JWT_ISSUER")
	v.BindEnv("jwt.expiry", "JWT_EXPIRY")
	v.BindEnv("jwt.refresh_expiry", "JWT_REFRESH_EXPIRY")
	v.BindEnv("email.smtp_host", "SMTP_HOST")
	v.BindEnv("email.smtp_port", "SMTP_PORT")
	v.BindEnv("email.from_email", "SMTP_FROM_EMAIL")
	v.BindEnv("email.from_name", "SMTP_FROM_NAME")

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

	// Parse durations
	parseDurations(v)

	// Validate configuration
	if err := validate.Struct(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// Get returns the global configuration
func Get() *Config {
	if cfg == nil {
		// Lazy init with defaults instead of panicking
		c, err := Load()
		if err != nil {
			// Return minimal default config to avoid nil pointer panics
			cfg = &Config{
				App: AppConfig{
					Name:    "go-core",
					Env:     "development",
					Port:    3000,
					Version: "1.0.0",
				},
			}
		} else {
			cfg = c
		}
	}
	return cfg
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// App defaults
	v.SetDefault("app.name", "go-core")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.port", 3000)
	v.SetDefault("app.version", "1.0.0")
	v.SetDefault("app.debug", false)

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")

	// Redis defaults
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 10)

	// JWT defaults
	v.SetDefault("jwt.expiry", "15m")
	v.SetDefault("jwt.refresh_expiry", "168h")

	// Metrics defaults
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")

	// gRPC defaults
	v.SetDefault("grpc.port", 50051)
	v.SetDefault("grpc.reflection_enabled", true)

	// Logging defaults
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.output", "stdout")

	// Storage defaults
	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.local_path", "./uploads")
	v.SetDefault("storage.max_file_size", 10485760) // 10MB

	// Security defaults
	v.SetDefault("security.bcrypt_cost", 12)
	v.SetDefault("security.api_key_header", "X-API-Key")

	// CORS defaults
	v.SetDefault("cors.allowed_origins", []string{"http://localhost:3000"})
	v.SetDefault("cors.allowed_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allowed_headers", []string{"Origin", "Content-Type", "Accept", "Authorization"})
	v.SetDefault("cors.allow_credentials", true)

	// Rate limit defaults
	v.SetDefault("rate_limit.per_minute", 60)
	v.SetDefault("rate_limit.burst", 10)
}

// parseDurations parses duration strings from configuration
func parseDurations(v *viper.Viper) {
	// Parse JWT durations
	if expiryStr := v.GetString("jwt.expiry"); expiryStr != "" {
		if expiry, err := time.ParseDuration(expiryStr); err == nil {
			cfg.JWT.Expiry = expiry
		}
	}
	if refreshStr := v.GetString("jwt.refresh_expiry"); refreshStr != "" {
		if refresh, err := time.ParseDuration(refreshStr); err == nil {
			cfg.JWT.RefreshExpiry = refresh
		}
	}

	// Parse database connection lifetime
	if lifetimeStr := v.GetString("database.conn_max_lifetime"); lifetimeStr != "" {
		if lifetime, err := time.ParseDuration(lifetimeStr); err == nil {
			cfg.Database.ConnMaxLifetime = lifetime
		}
	}
}

// IsDevelopment returns true if the environment is development
func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.App.Env) == "development"
}

// IsProduction returns true if the environment is production
func (c *Config) IsProduction() bool {
	return strings.ToLower(c.App.Env) == "production"
}

// IsStaging returns true if the environment is staging
func (c *Config) IsStaging() bool {
	return strings.ToLower(c.App.Env) == "staging"
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
	return viper.GetBool(key)
}

// GetInt returns an integer value from viper by key
func (c *Config) GetInt(key string) int {
	return viper.GetInt(key)
}

// GetString returns a string value from viper by key
func (c *Config) GetString(key string) string {
	return viper.GetString(key)
}

// GetDuration returns a duration value from viper by key
func (c *Config) GetDuration(key string) time.Duration {
	return viper.GetDuration(key)
}
