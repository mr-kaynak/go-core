package push

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"

	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

// FCMService handles Firebase Cloud Messaging push notifications
// using the official Firebase Admin SDK.
type FCMService struct {
	client    messagingClient
	projectID string
	logger    *logger.Logger
}

// messagingClient abstracts the Firebase messaging.Client for testability.
type messagingClient interface {
	Send(ctx context.Context, message *messaging.Message) (string, error)
	SendEachForMulticast(ctx context.Context, message *messaging.MulticastMessage) (*messaging.BatchResponse, error)
}

// FCMConfig holds FCM configuration
type FCMConfig struct {
	CredentialsFile string
	ProjectID       string
}

// NewFCMService creates a new FCM service backed by the Firebase Admin SDK.
// The SDK handles OAuth2 token management automatically via the service account credentials.
func NewFCMService(ctx context.Context, cfg FCMConfig) (*FCMService, error) {
	// Firebase SDK uses GOOGLE_APPLICATION_CREDENTIALS env var for auth.
	// Set it from config if not already present in the environment.
	if cfg.CredentialsFile != "" {
		if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
			if err := os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.CredentialsFile); err != nil {
				return nil, fmt.Errorf("failed to set GOOGLE_APPLICATION_CREDENTIALS: %w", err)
			}
		}
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: cfg.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get messaging client: %w", err)
	}

	return &FCMService{
		client:    client,
		projectID: cfg.ProjectID,
		logger:    logger.Get().WithFields(logger.Fields{"service": "fcm"}),
	}, nil
}

// newFCMServiceWithClient creates an FCMService with an injected client (for testing).
func newFCMServiceWithClient(client messagingClient) *FCMService {
	return &FCMService{
		client:    client,
		projectID: "test-project",
		logger:    logger.Get().WithFields(logger.Fields{"service": "fcm"}),
	}
}

// Send sends a push notification to a single device.
func (s *FCMService) Send(ctx context.Context, deviceToken, title, body string, data map[string]string) error {
	msg := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
	}

	_, err := s.client.Send(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to send FCM message: %w", err)
	}

	s.logger.Debug("Push notification sent successfully")
	return nil
}

// SendMulticast sends a push notification to multiple devices concurrently
// via the Firebase SDK's SendEachForMulticast (handles parallelism internally).
func (s *FCMService) SendMulticast(
	ctx context.Context, tokens []string, title, body string, data map[string]string,
) (*domain.MulticastResult, error) {
	msg := &messaging.MulticastMessage{
		Tokens: tokens,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
	}

	resp, err := s.client.SendEachForMulticast(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send FCM multicast: %w", err)
	}

	result := &domain.MulticastResult{
		SuccessCount: resp.SuccessCount,
	}

	for i, sr := range resp.Responses {
		if sr.Error != nil && i < len(tokens) {
			s.logger.Warn("Failed to send push to device",
				"token", safePrefix(tokens[i], 10),
				"error", sr.Error,
			)
			result.FailedTokens = append(result.FailedTokens, tokens[i])
		}
	}

	return result, nil
}

// SendToTopic sends a push notification to a topic.
func (s *FCMService) SendToTopic(ctx context.Context, topic, title, body string, data map[string]string) error {
	msg := &messaging.Message{
		Topic: topic,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
	}

	_, err := s.client.Send(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to send FCM topic message: %w", err)
	}

	s.logger.Debug("Topic notification sent successfully", "topic", topic)
	return nil
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
