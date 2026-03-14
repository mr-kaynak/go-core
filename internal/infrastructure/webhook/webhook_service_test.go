package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/logger"
)

func init() {
	_ = logger.Initialize("error", "text", "stdout")
}

// newTestWebhookService creates a WebhookService suitable for tests.
// It uses a plain http.Client (no SSRF dialer) so httptest servers on
// localhost are reachable, and wires in the global logger.
func newTestWebhookService(secret string, maxRetries int) *WebhookService {
	return &WebhookService{
		secret:     secret,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		maxRetries: maxRetries,
		logger:     logger.Get().WithFields(logger.Fields{"service": "webhook-test"}),
	}
}

// ---------------------------------------------------------------------------
// computeSignature
// ---------------------------------------------------------------------------

func TestComputeSignature(t *testing.T) {
	secret := "test-webhook-secret"
	svc := newTestWebhookService(secret, 0)

	payload := []byte(`{"event_type":"test","data":"hello"}`)
	got := svc.computeSignature(payload)

	// Compute expected independently.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("computeSignature mismatch: got %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// Successful delivery
// ---------------------------------------------------------------------------

func TestSendSuccessful(t *testing.T) {
	secret := "my-secret"
	var receivedBody []byte
	var receivedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		receivedBody = buf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestWebhookService(secret, 0)
	svc.httpClient = srv.Client()

	payload := WebhookPayload{
		EventID:   "evt-123",
		EventType: "order.created",
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Data:      map[string]string{"order_id": "abc"},
	}

	err := svc.Send(context.Background(), srv.URL, payload)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	// Content-Type
	if ct := receivedHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// User-Agent
	if ua := receivedHeaders.Get("User-Agent"); ua != "go-core-webhook/1.0" {
		t.Errorf("User-Agent = %q, want go-core-webhook/1.0", ua)
	}

	// Signature present and valid
	sig := receivedHeaders.Get("X-Webhook-Signature")
	if sig == "" {
		t.Fatal("X-Webhook-Signature header missing")
	}
	if len(sig) < 8 || sig[:7] != "sha256=" {
		t.Fatalf("signature does not start with sha256=: %s", sig)
	}

	// Verify HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if sig[7:] != expectedSig {
		t.Errorf("HMAC mismatch: got %s, want %s", sig[7:], expectedSig)
	}

	// Verify body structure
	var decoded WebhookPayload
	if err := json.Unmarshal(receivedBody, &decoded); err != nil {
		t.Fatalf("failed to unmarshal received body: %v", err)
	}
	if decoded.EventType != "order.created" {
		t.Errorf("EventType = %q, want order.created", decoded.EventType)
	}
}

// ---------------------------------------------------------------------------
// Auto-wrap raw payload
// ---------------------------------------------------------------------------

func TestSendAutoWrapsRawPayload(t *testing.T) {
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestWebhookService("secret", 0)
	svc.httpClient = srv.Client()

	rawPayload := map[string]string{"key": "value"}
	err := svc.Send(context.Background(), srv.URL, rawPayload)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var decoded WebhookPayload
	if err := json.Unmarshal(receivedBody, &decoded); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if decoded.EventType != "notification" {
		t.Errorf("EventType = %q, want notification", decoded.EventType)
	}
	if decoded.Timestamp.IsZero() {
		t.Error("Timestamp should be set automatically")
	}
}

// ---------------------------------------------------------------------------
// No signature when secret is empty
// ---------------------------------------------------------------------------

func TestSendNoSignatureWhenNoSecret(t *testing.T) {
	var receivedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestWebhookService("", 0)
	svc.httpClient = srv.Client()

	err := svc.Send(context.Background(), srv.URL, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if sig := receivedHeaders.Get("X-Webhook-Signature"); sig != "" {
		t.Errorf("X-Webhook-Signature should be empty when no secret, got %q", sig)
	}
}

// ---------------------------------------------------------------------------
// Retry on server error
// ---------------------------------------------------------------------------

func TestSendRetryOnServerError(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestWebhookService("secret", 3)
	svc.httpClient = srv.Client()

	err := svc.Send(context.Background(), srv.URL, map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Exhaust retries
// ---------------------------------------------------------------------------

func TestSendExhaustsRetries(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	svc := newTestWebhookService("secret", 1)
	svc.httpClient = srv.Client()

	err := svc.Send(context.Background(), srv.URL, map[string]string{"x": "y"})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts (1 initial + 1 retry), got %d", got)
	}
	if want := "failed after"; !containsSubstring(err.Error(), want) {
		t.Errorf("error %q should contain %q", err.Error(), want)
	}
}

// ---------------------------------------------------------------------------
// Retry on 429
// ---------------------------------------------------------------------------

func TestSendRetryOn429(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestWebhookService("secret", 3)
	svc.httpClient = srv.Client()

	err := svc.Send(context.Background(), srv.URL, map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("expected success after retry on 429, got: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Permanent errors (4xx except 429) — no retry
// ---------------------------------------------------------------------------

func TestSendPermanentErrorOn4xx(t *testing.T) {
	codes := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
	}

	for _, code := range codes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var attempts atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			svc := newTestWebhookService("secret", 3)
			svc.httpClient = srv.Client()

			err := svc.Send(context.Background(), srv.URL, map[string]string{"x": "y"})
			if err == nil {
				t.Fatalf("expected permanent error for status %d", code)
			}
			if got := attempts.Load(); got != 1 {
				t.Errorf("expected exactly 1 attempt (no retry) for %d, got %d", code, got)
			}

			// Verify it's a permanent error
			var permErr *permanentError
			if !errors.As(err, &permErr) {
				t.Errorf("expected permanentError for status %d, got %T: %v", code, err, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestSendContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestWebhookService("secret", 2)
	svc.httpClient = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := svc.Send(ctx, srv.URL, map[string]string{"x": "y"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// ---------------------------------------------------------------------------
// isPrivateIP — table-driven
// ---------------------------------------------------------------------------

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fd00:ec2::254", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isCloudMetadata
// ---------------------------------------------------------------------------

func TestIsCloudMetadata(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"169.254.169.254", true},
		{"fd00:ec2::254", true},
		{"8.8.8.8", false},
		{"10.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isCloudMetadata(ip)
			if got != tt.want {
				t.Errorf("isCloudMetadata(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// permanentError type
// ---------------------------------------------------------------------------

func TestPermanentErrorUnwrap(t *testing.T) {
	inner := errors.New("something went wrong")
	pe := &permanentError{err: inner}

	if pe.Error() != inner.Error() {
		t.Errorf("Error() = %q, want %q", pe.Error(), inner.Error())
	}
	if pe.Unwrap() != inner {
		t.Error("Unwrap() should return the inner error")
	}
}

// ---------------------------------------------------------------------------
// NewWebhookService defaults
// ---------------------------------------------------------------------------

func TestNewWebhookServiceDefaults(t *testing.T) {
	svc := NewWebhookService(WebhookConfig{Secret: "s"})
	if svc.maxRetries != 3 {
		t.Errorf("default maxRetries = %d, want 3", svc.maxRetries)
	}
	if svc.secret != "s" {
		t.Errorf("secret = %q, want %q", svc.secret, "s")
	}
	if svc.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
	if svc.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestNewWebhookServiceCustomValues(t *testing.T) {
	svc := NewWebhookService(WebhookConfig{
		Secret:     "custom-secret",
		Timeout:    30 * time.Second,
		MaxRetries: 5,
	})
	if svc.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", svc.maxRetries)
	}
	if svc.secret != "custom-secret" {
		t.Errorf("secret = %q, want custom-secret", svc.secret)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
