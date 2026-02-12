package metrics

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Status label constants for Prometheus metrics
const (
	statusFailed  = "failed"
	statusSuccess = "success"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// HTTP metrics
	httpRequestsTotal     *prometheus.CounterVec
	httpRequestDuration   *prometheus.HistogramVec
	httpRequestSizeBytes  *prometheus.SummaryVec
	httpResponseSizeBytes *prometheus.SummaryVec
	httpActiveRequests    *prometheus.GaugeVec

	// Business metrics
	userRegistrations prometheus.Counter
	loginAttempts     *prometheus.CounterVec
	emailsSent        *prometheus.CounterVec
	notificationsSent *prometheus.CounterVec
	templatesRendered *prometheus.CounterVec

	// Database metrics
	dbQueriesTotal    *prometheus.CounterVec
	dbQueryDuration   *prometheus.HistogramVec
	dbConnectionsOpen prometheus.Gauge
	dbConnectionsIdle prometheus.Gauge

	// RabbitMQ metrics
	mqMessagesPublished *prometheus.CounterVec
	mqMessagesConsumed  *prometheus.CounterVec
	mqMessagesInOutbox  prometheus.Gauge
	mqMessagesDLQ       prometheus.Gauge
	mqConnectionStatus  prometheus.Gauge

	// Cache metrics
	cacheHits      prometheus.Counter
	cacheMisses    prometheus.Counter
	cacheEvictions prometheus.Counter
	cacheSize      prometheus.Gauge

	// Authorization metrics
	authzChecks  *prometheus.CounterVec
	authzLatency *prometheus.HistogramVec

	// Application info
	appInfo *prometheus.GaugeVec
}

var metrics *Metrics

// InitMetrics initializes all Prometheus metrics
func InitMetrics(namespace string) *Metrics {
	metrics = &Metrics{
		// HTTP metrics
		httpRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),

		httpRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   []float64{0.001, 0.01, 0.1, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "endpoint", "status"},
		),

		httpRequestSizeBytes: promauto.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "request_size_bytes",
				Help:      "HTTP request size in bytes",
				Objectives: map[float64]float64{
					0.5:  0.05,
					0.9:  0.01,
					0.99: 0.001,
				},
			},
			[]string{"method", "endpoint"},
		),

		httpResponseSizeBytes: promauto.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "response_size_bytes",
				Help:      "HTTP response size in bytes",
				Objectives: map[float64]float64{
					0.5:  0.05,
					0.9:  0.01,
					0.99: 0.001,
				},
			},
			[]string{"method", "endpoint"},
		),

		httpActiveRequests: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "active_requests",
				Help:      "Number of active HTTP requests",
			},
			[]string{"method", "endpoint"},
		),

		// Business metrics
		userRegistrations: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "business",
				Name:      "user_registrations_total",
				Help:      "Total number of user registrations",
			},
		),

		loginAttempts: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "business",
				Name:      "login_attempts_total",
				Help:      "Total number of login attempts",
			},
			[]string{"status", "method"},
		),

		emailsSent: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "business",
				Name:      "emails_sent_total",
				Help:      "Total number of emails sent",
			},
			[]string{"template", "status"},
		),

		notificationsSent: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "business",
				Name:      "notifications_sent_total",
				Help:      "Total number of notifications sent",
			},
			[]string{"type", "status"},
		),

		templatesRendered: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "business",
				Name:      "templates_rendered_total",
				Help:      "Total number of templates rendered",
			},
			[]string{"template", "language"},
		),

		// Database metrics
		dbQueriesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "database",
				Name:      "queries_total",
				Help:      "Total number of database queries",
			},
			[]string{"operation", "table", "status"},
		),

		dbQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "database",
				Name:      "query_duration_seconds",
				Help:      "Database query duration in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
			},
			[]string{"operation", "table"},
		),

		dbConnectionsOpen: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "database",
				Name:      "connections_open",
				Help:      "Number of open database connections",
			},
		),

		dbConnectionsIdle: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "database",
				Name:      "connections_idle",
				Help:      "Number of idle database connections",
			},
		),

		// RabbitMQ metrics
		mqMessagesPublished: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "rabbitmq",
				Name:      "messages_published_total",
				Help:      "Total number of messages published to RabbitMQ",
			},
			[]string{"exchange", "routing_key", "status"},
		),

		mqMessagesConsumed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "rabbitmq",
				Name:      "messages_consumed_total",
				Help:      "Total number of messages consumed from RabbitMQ",
			},
			[]string{"queue", "status"},
		),

		mqMessagesInOutbox: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "rabbitmq",
				Name:      "messages_in_outbox",
				Help:      "Number of messages in outbox waiting to be sent",
			},
		),

		mqMessagesDLQ: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "rabbitmq",
				Name:      "messages_in_dlq",
				Help:      "Number of messages in dead letter queue",
			},
		),

		mqConnectionStatus: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "rabbitmq",
				Name:      "connection_status",
				Help:      "RabbitMQ connection status (1 = connected, 0 = disconnected)",
			},
		),

		// Cache metrics
		cacheHits: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "cache",
				Name:      "hits_total",
				Help:      "Total number of cache hits",
			},
		),

		cacheMisses: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "cache",
				Name:      "misses_total",
				Help:      "Total number of cache misses",
			},
		),

		cacheEvictions: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "cache",
				Name:      "evictions_total",
				Help:      "Total number of cache evictions",
			},
		),

		cacheSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "cache",
				Name:      "size_bytes",
				Help:      "Current cache size in bytes",
			},
		),

		// Authorization metrics
		authzChecks: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "authorization",
				Name:      "checks_total",
				Help:      "Total number of authorization checks",
			},
			[]string{"resource", "action", "result"},
		),

		authzLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "authorization",
				Name:      "check_duration_seconds",
				Help:      "Authorization check duration in seconds",
				Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
			},
			[]string{"resource", "action"},
		),

		// Application info
		appInfo: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "app_info",
				Help:      "Application information",
			},
			[]string{"version", "environment", "commit"},
		),
	}

	return metrics
}

// GetMetrics returns the global metrics instance
func GetMetrics() *Metrics {
	if metrics == nil {
		InitMetrics("go_core")
	}
	return metrics
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, endpoint string, status int, duration time.Duration, reqSize, respSize int) {
	statusStr := strconv.Itoa(status)

	m.httpRequestsTotal.WithLabelValues(method, endpoint, statusStr).Inc()
	m.httpRequestDuration.WithLabelValues(method, endpoint, statusStr).Observe(duration.Seconds())
	m.httpRequestSizeBytes.WithLabelValues(method, endpoint).Observe(float64(reqSize))
	m.httpResponseSizeBytes.WithLabelValues(method, endpoint).Observe(float64(respSize))
}

// IncrementActiveRequests increments active requests counter
func (m *Metrics) IncrementActiveRequests(method, endpoint string) {
	m.httpActiveRequests.WithLabelValues(method, endpoint).Inc()
}

// DecrementActiveRequests decrements active requests counter
func (m *Metrics) DecrementActiveRequests(method, endpoint string) {
	m.httpActiveRequests.WithLabelValues(method, endpoint).Dec()
}

// RecordUserRegistration records a user registration
func (m *Metrics) RecordUserRegistration() {
	m.userRegistrations.Inc()
}

// RecordLoginAttempt records a login attempt
func (m *Metrics) RecordLoginAttempt(success bool, method string) {
	status := statusFailed
	if success {
		status = statusSuccess
	}
	m.loginAttempts.WithLabelValues(status, method).Inc()
}

// RecordEmailSent records an email sent
func (m *Metrics) RecordEmailSent(template string, success bool) {
	status := statusFailed
	if success {
		status = statusSuccess
	}
	m.emailsSent.WithLabelValues(template, status).Inc()
}

// RecordNotificationSent records a notification sent
func (m *Metrics) RecordNotificationSent(notificationType string, success bool) {
	status := statusFailed
	if success {
		status = statusSuccess
	}
	m.notificationsSent.WithLabelValues(notificationType, status).Inc()
}

// RecordTemplateRendered records a template render
func (m *Metrics) RecordTemplateRendered(template, language string) {
	m.templatesRendered.WithLabelValues(template, language).Inc()
}

// RecordDBQuery records a database query
func (m *Metrics) RecordDBQuery(operation, table string, success bool, duration time.Duration) {
	status := statusFailed
	if success {
		status = statusSuccess
	}
	m.dbQueriesTotal.WithLabelValues(operation, table, status).Inc()
	m.dbQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}

// UpdateDBConnections updates database connection metrics
func (m *Metrics) UpdateDBConnections(open, idle int) {
	m.dbConnectionsOpen.Set(float64(open))
	m.dbConnectionsIdle.Set(float64(idle))
}

// RecordMQMessagePublished records a message published to RabbitMQ
func (m *Metrics) RecordMQMessagePublished(exchange, routingKey string, success bool) {
	status := statusFailed
	if success {
		status = statusSuccess
	}
	m.mqMessagesPublished.WithLabelValues(exchange, routingKey, status).Inc()
}

// RecordMQMessageConsumed records a message consumed from RabbitMQ
func (m *Metrics) RecordMQMessageConsumed(queue string, success bool) {
	status := statusFailed
	if success {
		status = statusSuccess
	}
	m.mqMessagesConsumed.WithLabelValues(queue, status).Inc()
}

// UpdateMQMetrics updates RabbitMQ metrics
func (m *Metrics) UpdateMQMetrics(outboxCount, dlqCount int, connected bool) {
	m.mqMessagesInOutbox.Set(float64(outboxCount))
	m.mqMessagesDLQ.Set(float64(dlqCount))
	if connected {
		m.mqConnectionStatus.Set(1)
	} else {
		m.mqConnectionStatus.Set(0)
	}
}

// RecordCacheHit records a cache hit
func (m *Metrics) RecordCacheHit() {
	m.cacheHits.Inc()
}

// RecordCacheMiss records a cache miss
func (m *Metrics) RecordCacheMiss() {
	m.cacheMisses.Inc()
}

// RecordCacheEviction records a cache eviction
func (m *Metrics) RecordCacheEviction() {
	m.cacheEvictions.Inc()
}

// UpdateCacheSize updates the cache size
func (m *Metrics) UpdateCacheSize(size int64) {
	m.cacheSize.Set(float64(size))
}

// RecordAuthzCheck records an authorization check
func (m *Metrics) RecordAuthzCheck(resource, action string, allowed bool, duration time.Duration) {
	result := "denied"
	if allowed {
		result = "allowed"
	}
	m.authzChecks.WithLabelValues(resource, action, result).Inc()
	m.authzLatency.WithLabelValues(resource, action).Observe(duration.Seconds())
}

// SetAppInfo sets application information
func (m *Metrics) SetAppInfo(version, environment, commit string) {
	m.appInfo.WithLabelValues(version, environment, commit).Set(1)
}

// PrometheusMiddleware creates a Fiber middleware for Prometheus metrics
func PrometheusMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip metrics endpoint itself
		if c.Path() == "/metrics" {
			return c.Next()
		}

		start := time.Now()
		method := c.Method()
		path := c.Path()

		// Increment active requests
		metrics.IncrementActiveRequests(method, path)
		defer metrics.DecrementActiveRequests(method, path)

		// Continue processing
		err := c.Next()

		// Record metrics
		duration := time.Since(start)
		status := c.Response().StatusCode()
		reqSize := len(c.Request().Body())
		respSize := len(c.Response().Body())

		metrics.RecordHTTPRequest(method, path, status, duration, reqSize, respSize)

		return err
	}
}
