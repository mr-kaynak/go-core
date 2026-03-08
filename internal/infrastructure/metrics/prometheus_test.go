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
