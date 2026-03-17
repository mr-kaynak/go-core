package push

import (
	"context"
	"errors"
	"testing"

	"firebase.google.com/go/v4/messaging"
)

// mockMessagingClient implements messagingClient for testing.
type mockMessagingClient struct {
	sendFn                 func(ctx context.Context, msg *messaging.Message) (string, error)
	sendEachForMulticastFn func(ctx context.Context, msg *messaging.MulticastMessage) (*messaging.BatchResponse, error)
}

func (m *mockMessagingClient) Send(ctx context.Context, msg *messaging.Message) (string, error) {
	if m.sendFn != nil {
		return m.sendFn(ctx, msg)
	}
	return "projects/test/messages/123", nil
}

func (m *mockMessagingClient) SendEachForMulticast(ctx context.Context, msg *messaging.MulticastMessage) (*messaging.BatchResponse, error) {
	if m.sendEachForMulticastFn != nil {
		return m.sendEachForMulticastFn(ctx, msg)
	}
	responses := make([]*messaging.SendResponse, len(msg.Tokens))
	for i := range msg.Tokens {
		responses[i] = &messaging.SendResponse{Success: true, MessageID: "msg-" + msg.Tokens[i]}
	}
	return &messaging.BatchResponse{
		SuccessCount: len(msg.Tokens),
		Responses:    responses,
	}, nil
}

func TestFCMSendSuccess(t *testing.T) {
	var capturedMsg *messaging.Message

	mock := &mockMessagingClient{
		sendFn: func(_ context.Context, msg *messaging.Message) (string, error) {
			capturedMsg = msg
			return "projects/test/messages/123", nil
		},
	}

	svc := newFCMServiceWithClient(mock)
	err := svc.Send(context.Background(), "device-token-abc", "Hello", "World", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if capturedMsg.Token != "device-token-abc" {
		t.Errorf("expected token 'device-token-abc', got %q", capturedMsg.Token)
	}
	if capturedMsg.Notification.Title != "Hello" {
		t.Errorf("expected title 'Hello', got %q", capturedMsg.Notification.Title)
	}
	if capturedMsg.Notification.Body != "World" {
		t.Errorf("expected body 'World', got %q", capturedMsg.Notification.Body)
	}
	if capturedMsg.Data["key"] != "val" {
		t.Errorf("expected data[key]='val', got %q", capturedMsg.Data["key"])
	}
	if capturedMsg.Android == nil || capturedMsg.Android.Priority != "high" {
		t.Error("expected android priority 'high'")
	}
}

func TestFCMSendError(t *testing.T) {
	mock := &mockMessagingClient{
		sendFn: func(_ context.Context, _ *messaging.Message) (string, error) {
			return "", errors.New("auth error")
		},
	}

	svc := newFCMServiceWithClient(mock)
	err := svc.Send(context.Background(), "device-token", "Title", "Body", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, err) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFCMSendMulticastAllSuccess(t *testing.T) {
	mock := &mockMessagingClient{}
	svc := newFCMServiceWithClient(mock)

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
	mock := &mockMessagingClient{
		sendEachForMulticastFn: func(_ context.Context, msg *messaging.MulticastMessage) (*messaging.BatchResponse, error) {
			responses := make([]*messaging.SendResponse, len(msg.Tokens))
			for i := range msg.Tokens {
				if i == 1 { // Second token fails
					responses[i] = &messaging.SendResponse{Error: errors.New("not registered")}
				} else {
					responses[i] = &messaging.SendResponse{Success: true, MessageID: "msg-" + msg.Tokens[i]}
				}
			}
			return &messaging.BatchResponse{
				SuccessCount: 2,
				FailureCount: 1,
				Responses:    responses,
			}, nil
		},
	}

	svc := newFCMServiceWithClient(mock)
	tokens := []string{"token-1", "token-2", "token-3"}
	result, err := svc.SendMulticast(context.Background(), tokens, "Title", "Body", nil)
	if err != nil {
		t.Fatalf("expected no error (partial failure is not a top-level error), got: %v", err)
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

func TestFCMSendMulticastTotalFailure(t *testing.T) {
	mock := &mockMessagingClient{
		sendEachForMulticastFn: func(_ context.Context, _ *messaging.MulticastMessage) (*messaging.BatchResponse, error) {
			return nil, errors.New("service unavailable")
		},
	}

	svc := newFCMServiceWithClient(mock)
	_, err := svc.SendMulticast(context.Background(), []string{"t1"}, "Title", "Body", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFCMSendToTopic(t *testing.T) {
	var capturedMsg *messaging.Message

	mock := &mockMessagingClient{
		sendFn: func(_ context.Context, msg *messaging.Message) (string, error) {
			capturedMsg = msg
			return "projects/test/messages/456", nil
		},
	}

	svc := newFCMServiceWithClient(mock)
	err := svc.SendToTopic(context.Background(), "news", "Breaking", "Something happened", map[string]string{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if capturedMsg.Topic != "news" {
		t.Errorf("expected topic 'news', got %q", capturedMsg.Topic)
	}
	if capturedMsg.Token != "" {
		t.Errorf("expected empty token for topic message, got %q", capturedMsg.Token)
	}
	if capturedMsg.Notification.Title != "Breaking" {
		t.Errorf("expected title 'Breaking', got %q", capturedMsg.Notification.Title)
	}
	if capturedMsg.Data["url"] != "https://example.com" {
		t.Errorf("expected data[url]='https://example.com', got %q", capturedMsg.Data["url"])
	}
}

func TestFCMSendNilData(t *testing.T) {
	mock := &mockMessagingClient{}
	svc := newFCMServiceWithClient(mock)

	err := svc.Send(context.Background(), "token", "Title", "Body", nil)
	if err != nil {
		t.Fatalf("expected no error with nil data, got: %v", err)
	}
}

func TestSafePrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		n        int
		expected string
	}{
		{name: "longer than n", input: "abcdefghij", n: 5, expected: "abcde..."},
		{name: "shorter than n", input: "short", n: 10, expected: "short"},
		{name: "exact length", input: "exact6", n: 6, expected: "exact6"},
		{name: "empty string", input: "", n: 5, expected: ""},
		{name: "n is zero", input: "hello", n: 0, expected: "..."},
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
