package captcha

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
)

// Verifier validates a captcha token from the client.
type Verifier interface {
	// Verify checks the captcha response token. Returns nil on success.
	Verify(ctx context.Context, token, remoteIP string) error
}

// NewVerifier creates a Verifier for the configured provider.
func NewVerifier(cfg config.CaptchaConfig) (Verifier, error) {
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("captcha secret key is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	switch cfg.Provider {
	case "turnstile":
		return newTurnstileVerifier(cfg.SecretKey, timeout), nil
	case "recaptcha":
		return newRecaptchaVerifier(cfg.SecretKey, timeout), nil
	default:
		return nil, fmt.Errorf("unsupported captcha provider: %s", cfg.Provider)
	}
}
