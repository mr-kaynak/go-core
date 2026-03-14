package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/errors"
)

const recaptchaVerifyURL = "https://www.google.com/recaptcha/api/siteverify"

type recaptchaVerifier struct {
	secretKey  string
	httpClient *http.Client
}

type recaptchaResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

func newRecaptchaVerifier(secretKey string, timeout time.Duration) *recaptchaVerifier {
	return &recaptchaVerifier{
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (v *recaptchaVerifier) Verify(ctx context.Context, token, remoteIP string) error {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, recaptchaVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("captcha verification request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("captcha verification request failed: %w", err)
	}
	defer resp.Body.Close()

	var result recaptchaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("captcha verification response decode failed: %w", err)
	}

	if !result.Success {
		return errors.NewBadRequest("Captcha verification failed")
	}

	return nil
}
