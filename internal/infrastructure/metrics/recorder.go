package metrics

import "time"

// MetricsRecorder defines the interface for recording application metrics.
// Services should depend on this interface rather than the concrete Metrics
// struct or the GetMetrics() global singleton.
type MetricsRecorder interface {
	// Auth & user metrics
	RecordLoginAttempt(success bool, method string)
	RecordUserRegistration()

	// Notification metrics
	RecordNotificationSent(notificationType string, success bool)

	// Blog metrics
	RecordBlogPostCreated(status string)
	RecordBlogPostPublished()
	RecordBlogCommentCreated(commentType string)
	RecordBlogLikeToggled(action string)
	RecordBlogViewRecorded()
	RecordBlogShareRecorded(platform string)

	// Infrastructure metrics (used by DB, cache, MQ wrappers)
	RecordDBQuery(operation, table string, success bool, duration time.Duration)
	UpdateDBConnections(open, idle int)
	RecordCacheHit()
	RecordCacheMiss()
	RecordMQMessagePublished(exchange, routingKey string, success bool)
	RecordMQMessageConsumed(queue string, success bool)
	UpdateMQMetrics(outboxCount, dlqCount int, connected bool)
}

// Compile-time check: *Metrics satisfies MetricsRecorder.
var _ MetricsRecorder = (*Metrics)(nil)

// NoOpMetrics is a no-op implementation of MetricsRecorder for tests
// and degraded mode where metrics collection is not needed.
type NoOpMetrics struct{}

func (NoOpMetrics) RecordLoginAttempt(bool, string)                   {}
func (NoOpMetrics) RecordUserRegistration()                           {}
func (NoOpMetrics) RecordNotificationSent(string, bool)               {}
func (NoOpMetrics) RecordBlogPostCreated(string)                      {}
func (NoOpMetrics) RecordBlogPostPublished()                          {}
func (NoOpMetrics) RecordBlogCommentCreated(string)                   {}
func (NoOpMetrics) RecordBlogLikeToggled(string)                      {}
func (NoOpMetrics) RecordBlogViewRecorded()                           {}
func (NoOpMetrics) RecordBlogShareRecorded(string)                    {}
func (NoOpMetrics) RecordDBQuery(string, string, bool, time.Duration) {}
func (NoOpMetrics) UpdateDBConnections(int, int)                      {}
func (NoOpMetrics) RecordCacheHit()                                   {}
func (NoOpMetrics) RecordCacheMiss()                                  {}
func (NoOpMetrics) RecordMQMessagePublished(string, string, bool)     {}
func (NoOpMetrics) RecordMQMessageConsumed(string, bool)              {}
func (NoOpMetrics) UpdateMQMetrics(int, int, bool)                    {}
