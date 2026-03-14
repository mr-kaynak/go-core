package metrics

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func freshPromRegistry(t *testing.T) {
	t.Helper()
	oldReg := prometheus.DefaultRegisterer
	oldGather := prometheus.DefaultGatherer
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	metrics = nil
	metricsOnce = sync.Once{}

	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldReg
		prometheus.DefaultGatherer = oldGather
		metrics = nil
		metricsOnce = sync.Once{}
	})
}

func TestInitMetricsRegistersCollectors(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")
	if m == nil {
		t.Fatalf("expected non-nil metrics from InitMetrics")
	}

	// CounterVec/HistogramVec family appears after first observation.
	m.RecordHTTPRequest("GET", "/health", 200, 10*time.Millisecond, 1, 1)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	foundHTTP := false
	for _, mf := range families {
		if strings.HasPrefix(mf.GetName(), "testcore_http_requests_total") {
			foundHTTP = true
			break
		}
	}
	if !foundHTTP {
		t.Fatalf("expected testcore_http_requests_total to be registered")
	}
}

func TestMetricsCounterAndHistogramRecording(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordHTTPRequest("GET", "/health", 200, 25*time.Millisecond, 10, 20)
	m.RecordGRPCRequest("/gocore.v1.UserService/GetUser", "OK", 5*time.Millisecond)
	m.RecordEmailSent("welcome", true)
	m.RecordDBQuery("select", "users", true, 15*time.Millisecond)

	httpCount := testutil.ToFloat64(m.httpRequestsTotal.WithLabelValues("GET", "/health", "200"))
	if httpCount != 1 {
		t.Fatalf("expected http counter to be 1, got %v", httpCount)
	}

	emailCount := testutil.ToFloat64(m.emailsSent.WithLabelValues("welcome", statusSuccess))
	if emailCount != 1 {
		t.Fatalf("expected email counter to be 1, got %v", emailCount)
	}
	grpcCount := testutil.ToFloat64(m.grpcRequestsTotal.WithLabelValues("/gocore.v1.UserService/GetUser", "OK"))
	if grpcCount != 1 {
		t.Fatalf("expected grpc counter to be 1, got %v", grpcCount)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	var dbHistogramObserved bool
	for _, mf := range families {
		if mf.GetName() != "testcore_database_query_duration_seconds" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if metric.GetHistogram().GetSampleCount() > 0 {
				dbHistogramObserved = true
			}
		}
	}
	if !dbHistogramObserved {
		t.Fatalf("expected db histogram sample count to be > 0")
	}
}

func TestMetricsGaugeSetters(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.UpdateDBConnections(12, 5)
	m.UpdateCacheSize(4096)
	m.UpdateMQMetrics(7, 2, true)
	m.IncrementActiveRequests("POST", "/login")
	m.DecrementActiveRequests("POST", "/login")

	if got := testutil.ToFloat64(m.dbConnectionsOpen); got != 12 {
		t.Fatalf("expected db open gauge 12, got %v", got)
	}
	if got := testutil.ToFloat64(m.dbConnectionsIdle); got != 5 {
		t.Fatalf("expected db idle gauge 5, got %v", got)
	}
	if got := testutil.ToFloat64(m.cacheSize); got != 4096 {
		t.Fatalf("expected cache size gauge 4096, got %v", got)
	}
	if got := testutil.ToFloat64(m.mqConnectionStatus); got != 1 {
		t.Fatalf("expected mq connection status 1, got %v", got)
	}
	if got := testutil.ToFloat64(m.httpActiveRequests.WithLabelValues("POST", "/login")); got != 0 {
		t.Fatalf("expected active requests gauge back to 0, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Business metrics
// ---------------------------------------------------------------------------

func TestRecordLoginAttempt(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordLoginAttempt(true, "password")
	m.RecordLoginAttempt(false, "password")
	m.RecordLoginAttempt(true, "2fa")

	if got := testutil.ToFloat64(m.loginAttempts.WithLabelValues(statusSuccess, "password")); got != 1 {
		t.Fatalf("expected password success=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.loginAttempts.WithLabelValues(statusFailed, "password")); got != 1 {
		t.Fatalf("expected password failed=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.loginAttempts.WithLabelValues(statusSuccess, "2fa")); got != 1 {
		t.Fatalf("expected 2fa success=1, got %v", got)
	}
}

func TestRecordUserRegistration(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordUserRegistration()
	m.RecordUserRegistration()

	if got := testutil.ToFloat64(m.userRegistrations); got != 2 {
		t.Fatalf("expected registrations=2, got %v", got)
	}
}

func TestRecordNotificationSent(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordNotificationSent("email", true)
	m.RecordNotificationSent("push", false)

	if got := testutil.ToFloat64(m.notificationsSent.WithLabelValues("email", statusSuccess)); got != 1 {
		t.Fatalf("expected email/success=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.notificationsSent.WithLabelValues("push", statusFailed)); got != 1 {
		t.Fatalf("expected push/failed=1, got %v", got)
	}
}

func TestRecordTemplateRendered(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordTemplateRendered("welcome", "en")
	m.RecordTemplateRendered("welcome", "tr")

	if got := testutil.ToFloat64(m.templatesRendered.WithLabelValues("welcome", "en")); got != 1 {
		t.Fatalf("expected welcome/en=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.templatesRendered.WithLabelValues("welcome", "tr")); got != 1 {
		t.Fatalf("expected welcome/tr=1, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Infrastructure metrics: RabbitMQ
// ---------------------------------------------------------------------------

func TestRecordMQMessages(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordMQMessagePublished("events", "user.created", true)
	m.RecordMQMessagePublished("events", "user.created", false)
	m.RecordMQMessageConsumed("notifications", true)
	m.RecordMQMessageConsumed("notifications", false)

	if got := testutil.ToFloat64(m.mqMessagesPublished.WithLabelValues("events", "user.created", statusSuccess)); got != 1 {
		t.Fatalf("expected published success=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.mqMessagesPublished.WithLabelValues("events", "user.created", statusFailed)); got != 1 {
		t.Fatalf("expected published failed=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.mqMessagesConsumed.WithLabelValues("notifications", statusSuccess)); got != 1 {
		t.Fatalf("expected consumed success=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.mqMessagesConsumed.WithLabelValues("notifications", statusFailed)); got != 1 {
		t.Fatalf("expected consumed failed=1, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Infrastructure metrics: Cache
// ---------------------------------------------------------------------------

func TestRecordCacheOperations(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordCacheHit()
	m.RecordCacheHit()
	m.RecordCacheMiss()
	m.RecordCacheEviction()

	if got := testutil.ToFloat64(m.cacheHits); got != 2 {
		t.Fatalf("expected cache hits=2, got %v", got)
	}
	if got := testutil.ToFloat64(m.cacheMisses); got != 1 {
		t.Fatalf("expected cache misses=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.cacheEvictions); got != 1 {
		t.Fatalf("expected cache evictions=1, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Infrastructure metrics: Authorization
// ---------------------------------------------------------------------------

func TestRecordAuthzCheck(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordAuthzCheck("posts", "read", true, 1*time.Millisecond)
	m.RecordAuthzCheck("posts", "delete", false, 2*time.Millisecond)

	if got := testutil.ToFloat64(m.authzChecks.WithLabelValues("posts", "read", "allowed")); got != 1 {
		t.Fatalf("expected posts/read/allowed=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.authzChecks.WithLabelValues("posts", "delete", "denied")); got != 1 {
		t.Fatalf("expected posts/delete/denied=1, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Infrastructure metrics: MQ disconnected state
// ---------------------------------------------------------------------------

func TestUpdateMQMetricsDisconnected(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.UpdateMQMetrics(5, 3, false)

	if got := testutil.ToFloat64(m.mqMessagesInOutbox); got != 5 {
		t.Fatalf("expected outbox=5, got %v", got)
	}
	if got := testutil.ToFloat64(m.mqMessagesDLQ); got != 3 {
		t.Fatalf("expected dlq=3, got %v", got)
	}
	if got := testutil.ToFloat64(m.mqConnectionStatus); got != 0 {
		t.Fatalf("expected connection status=0, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Blog metrics
// ---------------------------------------------------------------------------

func TestRecordBlogMetrics(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.RecordBlogPostCreated("draft")
	m.RecordBlogPostPublished()
	m.RecordBlogCommentCreated("reply")
	m.RecordBlogLikeToggled("like")
	m.RecordBlogLikeToggled("unlike")
	m.RecordBlogViewRecorded()
	m.RecordBlogShareRecorded("twitter")

	if got := testutil.ToFloat64(m.blogPostsCreated.WithLabelValues("draft")); got != 1 {
		t.Fatalf("expected posts created draft=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.blogPostsPublished); got != 1 {
		t.Fatalf("expected posts published=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.blogCommentsCreated.WithLabelValues("reply")); got != 1 {
		t.Fatalf("expected comments reply=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.blogLikesToggled.WithLabelValues("like")); got != 1 {
		t.Fatalf("expected likes like=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.blogLikesToggled.WithLabelValues("unlike")); got != 1 {
		t.Fatalf("expected likes unlike=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.blogViewsRecorded); got != 1 {
		t.Fatalf("expected views=1, got %v", got)
	}
	if got := testutil.ToFloat64(m.blogSharesRecorded.WithLabelValues("twitter")); got != 1 {
		t.Fatalf("expected shares twitter=1, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// App info
// ---------------------------------------------------------------------------

func TestSetAppInfo(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")

	m.SetAppInfo("1.0.0", "testing", "abc123")

	if got := testutil.ToFloat64(m.appInfo.WithLabelValues("1.0.0", "testing", "abc123")); got != 1 {
		t.Fatalf("expected app info gauge=1, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Singleton behaviour
// ---------------------------------------------------------------------------

func TestGetMetricsSingleton(t *testing.T) {
	freshPromRegistry(t)
	m := InitMetrics("testcore")
	m2 := GetMetrics()

	if m != m2 {
		t.Fatalf("expected GetMetrics to return the same pointer as InitMetrics")
	}
}
