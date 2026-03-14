package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rewriteTransport redirects all outbound requests to a local test server,
// regardless of the original scheme/host in the URL.
type rewriteTransport struct {
	base    http.RoundTripper
	baseURL string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(rt.baseURL, "http://")
	if rt.base != nil {
		return rt.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// newTestFCMService creates an FCMService whose HTTP client is wired to the
// given handler via httptest, so no real network calls are made.
func newTestFCMService(t *testing.T, handler http.HandlerFunc) *FCMService {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	svc := NewFCMService(FCMConfig{ServerKey: "test-key", ProjectID: "test-project"})
	svc.httpClient = server.Client()
	svc.httpClient.Transport = &rewriteTransport{
		base:    server.Client().Transport,
		baseURL: server.URL,
	}
	return svc
}

// fcmRequestBody is the top-level JSON envelope sent to the FCM API.
type fcmRequestBody struct {
	Message FCMMessage `json:"message"`
}

func TestFCMSendSuccess(t *testing.T) {
	var capturedReq fcmRequestBody
	var capturedAuth string

	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedReq); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	})

	err := svc.Send(context.Background(), "device-token-abc", "Hello", "World", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify authorization header
	if capturedAuth != "Bearer test-key" {
		t.Errorf("expected Authorization 'Bearer test-key', got %q", capturedAuth)
	}

	// Verify token in payload
	if capturedReq.Message.Token != "device-token-abc" {
		t.Errorf("expected token 'device-token-abc', got %q", capturedReq.Message.Token)
	}

	// Verify notification
	if capturedReq.Message.Notification == nil {
		t.Fatal("expected notification to be non-nil")
	}
	if capturedReq.Message.Notification.Title != "Hello" {
		t.Errorf("expected title 'Hello', got %q", capturedReq.Message.Notification.Title)
	}
	if capturedReq.Message.Notification.Body != "World" {
		t.Errorf("expected body 'World', got %q", capturedReq.Message.Notification.Body)
	}

	// Verify data
	if capturedReq.Message.Data["key"] != "val" {
		t.Errorf("expected data[key]='val', got %q", capturedReq.Message.Data["key"])
	}

	// Verify android priority is set
	if capturedReq.Message.Android == nil || capturedReq.Message.Android.Priority != "high" {
		t.Error("expected android priority 'high'")
	}
}

func TestFCMSendServerError(t *testing.T) {
	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	})

	err := svc.Send(context.Background(), "device-token", "Title", "Body", nil)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got: %v", err)
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Errorf("expected error to contain response body, got: %v", err)
	}
}

func TestFCMSendMulticastAllSuccess(t *testing.T) {
	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tokens := []string{"token-1", "token-2", "token-3"}
	result, err := svc.SendMulticast(context.Background(), tokens, "Title", "Body", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.SuccessCount != 3 {
		t.Errorf("expected SuccessCount=3, got %d", result.SuccessCount)
	}
	if len(result.FailedTokens) != 0 {
		t.Errorf("expected no failed tokens, got %v", result.FailedTokens)
	}
}

func TestFCMSendMulticastPartialFailure(t *testing.T) {
	callCount := 0
	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 2 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	tokens := []string{"token-1", "token-2", "token-3"}
	result, err := svc.SendMulticast(context.Background(), tokens, "Title", "Body", nil)

	// Error should be non-nil because one token failed
	if err == nil {
		t.Fatal("expected non-nil error for partial failure")
	}

	if result.SuccessCount != 2 {
		t.Errorf("expected SuccessCount=2, got %d", result.SuccessCount)
	}
	if len(result.FailedTokens) != 1 {
		t.Errorf("expected 1 failed token, got %d", len(result.FailedTokens))
	}
	if len(result.FailedTokens) > 0 && result.FailedTokens[0] != "token-2" {
		t.Errorf("expected failed token 'token-2', got %q", result.FailedTokens[0])
	}
}

func TestFCMSendToTopic(t *testing.T) {
	var capturedReq fcmRequestBody

	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedReq); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	err := svc.SendToTopic(context.Background(), "news", "Breaking", "Something happened", map[string]string{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if capturedReq.Message.Topic != "news" {
		t.Errorf("expected topic 'news', got %q", capturedReq.Message.Topic)
	}
	if capturedReq.Message.Token != "" {
		t.Errorf("expected empty token for topic message, got %q", capturedReq.Message.Token)
	}
	if capturedReq.Message.Notification == nil {
		t.Fatal("expected notification to be non-nil")
	}
	if capturedReq.Message.Notification.Title != "Breaking" {
		t.Errorf("expected title 'Breaking', got %q", capturedReq.Message.Notification.Title)
	}
	if capturedReq.Message.Data["url"] != "https://example.com" {
		t.Errorf("expected data[url]='https://example.com', got %q", capturedReq.Message.Data["url"])
	}
}

func TestSafePrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		n        int
		expected string
	}{
		{
			name:     "longer than n",
			input:    "abcdefghij",
			n:        5,
			expected: "abcde...",
		},
		{
			name:     "shorter than n",
			input:    "short",
			n:        10,
			expected: "short",
		},
		{
			name:     "exact length",
			input:    "exact6",
			n:        6,
			expected: "exact6",
		},
		{
			name:     "empty string",
			input:    "",
			n:        5,
			expected: "",
		},
		{
			name:     "n is zero",
			input:    "hello",
			n:        0,
			expected: "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safePrefix(tt.input, tt.n)
			if got != tt.expected {
				t.Errorf("safePrefix(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.expected)
			}
		})
	}
}

func TestFCMSendNilData(t *testing.T) {
	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Verify Send works with nil data map
	err := svc.Send(context.Background(), "token", "Title", "Body", nil)
	if err != nil {
		t.Fatalf("expected no error with nil data, got: %v", err)
	}
}

func TestFCMSendURLContainsProjectID(t *testing.T) {
	var capturedPath string

	svc := newTestFCMService(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	err := svc.Send(context.Background(), "token", "Title", "Body", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	expectedPath := "/v1/projects/test-project/messages:send"
	if capturedPath != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, capturedPath)
	}
}

func TestNewFCMService(t *testing.T) {
	svc := NewFCMService(FCMConfig{
		ServerKey: "my-key",
		ProjectID: "my-project",
	})

	if svc.serverKey != "my-key" {
		t.Errorf("expected serverKey 'my-key', got %q", svc.serverKey)
	}
	if svc.projectID != "my-project" {
		t.Errorf("expected projectID 'my-project', got %q", svc.projectID)
	}
	if svc.httpClient == nil {
		t.Error("expected httpClient to be non-nil")
	}
	if svc.logger == nil {
		t.Error("expected logger to be non-nil")
	}
}
