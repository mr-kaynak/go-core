package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/logger"
)

// WebhookService handles webhook notification delivery
type WebhookService struct {
	secret     string
	httpClient *http.Client
	maxRetries int
	logger     *logger.Logger
}

// WebhookConfig holds webhook service configuration
type WebhookConfig struct {
	Secret     string
	Timeout    time.Duration
	MaxRetries int
}

// WebhookPayload represents the webhook request body
type WebhookPayload struct {
	EventID   string      `json:"event_id"`
	EventType string      `json:"event_type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// NewWebhookService creates a new webhook service
func NewWebhookService(cfg WebhookConfig) *WebhookService {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	return &WebhookService{
		secret: cfg.Secret,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: ssrfSafeDialContext,
			},
		},
		maxRetries: maxRetries,
		logger:     logger.Get().WithFields(logger.Fields{"service": "webhook"}),
	}
}

// ssrfSafeDialContext resolves the host, checks that the target IP is not
// internal/private, and only then establishes the connection.
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("webhook: invalid address %q: %w", addr, err)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("webhook: DNS resolution failed for %q: %w", host, err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, fmt.Errorf("webhook: request to private/internal address %s is blocked (SSRF protection)", ip.IP)
		}
	}

	// All resolved IPs are public — dial the first one.
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// isPrivateIP returns true if the IP belongs to a private, loopback, link-local
// or otherwise non-routable range that should not be reached by webhooks.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		isCloudMetadata(ip)
}

// isCloudMetadata checks for well-known cloud metadata service IPs.
func isCloudMetadata(ip net.IP) bool {
	metadataIPs := []net.IP{
		net.ParseIP("169.254.169.254"), // AWS / GCP / Azure metadata
		net.ParseIP("fd00:ec2::254"),   // AWS IMDSv2 IPv6
	}
	for _, m := range metadataIPs {
		if ip.Equal(m) {
			return true
		}
	}
	return false
}

// Send sends a webhook POST request with HMAC-SHA256 signature and retry logic
func (s *WebhookService) Send(ctx context.Context, url string, payload WebhookPayload) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			s.logger.Debug("Retrying webhook", "attempt", attempt, "url", url)
		}

		lastErr = s.sendOnce(ctx, url, jsonData)
		if lastErr == nil {
			return nil
		}

		s.logger.WithError(lastErr).Warn("Webhook delivery failed",
			"attempt", attempt+1,
			"max_retries", s.maxRetries,
			"url", url,
		)
	}

	return fmt.Errorf("webhook delivery failed after %d attempts: %w", s.maxRetries+1, lastErr)
}

func (s *WebhookService) sendOnce(ctx context.Context, url string, jsonData []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "go-core-webhook/1.0")

	// Add HMAC-SHA256 signature
	if s.secret != "" {
		signature := s.computeSignature(jsonData)
		req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	}

	req.Header.Set("X-Webhook-Timestamp", time.Now().UTC().Format(time.RFC3339))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain the body
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

func (s *WebhookService) computeSignature(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
