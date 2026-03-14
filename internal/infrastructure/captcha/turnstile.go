package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/errors"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileVerifier struct {
	secretKey  string
	httpClient *http.Client
}

type turnstileResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

func newTurnstileVerifier(secretKey string, timeout time.Duration) *turnstileVerifier {
	return &turnstileVerifier{
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (v *turnstileVerifier) Verify(ctx context.Context, token, remoteIP string) error {
	if token == "" {
		return errors.NewBadRequest("Captcha token is required")
	}

	form := url.Values{
		"secret":   {v.secretKey},
		"response": {token},
	}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	resp, err := v.httpClient.PostForm(turnstileVerifyURL, form)
	if err != nil {
		return fmt.Errorf("captcha verification request failed: %w", err)
	}
	defer resp.Body.Close()

	var result turnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("captcha verification response decode failed: %w", err)
	}

	if !result.Success {
		return errors.NewBadRequest("Captcha verification failed")
	}

	return nil
}
