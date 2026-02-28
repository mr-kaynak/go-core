package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/logger"
)

// FCMService handles Firebase Cloud Messaging push notifications
type FCMService struct {
	serverKey  string
	projectID  string
	httpClient *http.Client
	logger     *logger.Logger
}

// FCMConfig holds FCM configuration
type FCMConfig struct {
	ServerKey string
	ProjectID string
}

// FCMMessage represents an FCM push notification message
type FCMMessage struct {
	Token        string            `json:"token,omitempty"`
	Topic        string            `json:"topic,omitempty"`
	Notification *FCMNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      *FCMAndroid       `json:"android,omitempty"`
	APNS         *FCMAPNS          `json:"apns,omitempty"`
}

// FCMNotification represents the notification payload
type FCMNotification struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	ImageURL string `json:"image,omitempty"`
}

// FCMAndroid represents Android-specific configuration
type FCMAndroid struct {
	Priority string `json:"priority,omitempty"` // "high" or "normal"
	TTL      string `json:"ttl,omitempty"`
}

// FCMAPNS represents iOS-specific configuration
type FCMAPNS struct {
	Headers map[string]string `json:"headers,omitempty"`
}

// NewFCMService creates a new FCM service
func NewFCMService(cfg FCMConfig) *FCMService {
	return &FCMService{
		serverKey: cfg.ServerKey,
		projectID: cfg.ProjectID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger.Get().WithFields(logger.Fields{"service": "fcm"}),
	}
}

// Send sends a push notification to a single device
func (s *FCMService) Send(ctx context.Context, deviceToken, title, body string, data map[string]string) error {
	msg := FCMMessage{
		Token: deviceToken,
		Notification: &FCMNotification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &FCMAndroid{
			Priority: "high",
		},
	}

	return s.sendMessage(ctx, msg)
}

// SendMulticast sends a push notification to multiple devices
func (s *FCMService) SendMulticast(ctx context.Context, tokens []string, title, body string, data map[string]string) error {
	var lastErr error
	for _, token := range tokens {
		if err := s.Send(ctx, token, title, body, data); err != nil {
			s.logger.WithError(err).Warn("Failed to send push to device", "token", safePrefix(token, 10))
			lastErr = err
		}
	}
	return lastErr
}

// SendToTopic sends a push notification to a topic
func (s *FCMService) SendToTopic(ctx context.Context, topic, title, body string, data map[string]string) error {
	msg := FCMMessage{
		Topic: topic,
		Notification: &FCMNotification{
			Title: title,
			Body:  body,
		},
		Data: data,
	}

	return s.sendMessage(ctx, msg)
}

func (s *FCMService) sendMessage(ctx context.Context, msg FCMMessage) error {
	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", s.projectID)

	payload := map[string]interface{}{
		"message": msg,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal FCM message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create FCM request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.serverKey)

	resp, err := s.httpClient.Do(req) //nolint:gosec // G107: URL is constructed from trusted FCM API base + configured project ID
	if err != nil {
		return fmt.Errorf("failed to send FCM request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("FCM returned status %d", resp.StatusCode)
	}

	s.logger.Debug("Push notification sent successfully")
	return nil
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
