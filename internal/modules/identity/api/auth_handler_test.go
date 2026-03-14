package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

func newAuthTestApp(handler *AuthHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	api := app.Group("/api")
	handler.RegisterRoutes(api, func(c fiber.Ctx) error { return c.Next() })
	return app
}

func doRequest(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return string(data)
}

func TestParseAcceptLanguage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		// Defaults
		{name: "empty header", header: "", want: "en"},
		{name: "whitespace only", header: "   ", want: "en"},
		{name: "wildcard", header: "*", want: "en"},

		// Simple language tags
		{name: "simple en", header: "en", want: "en"},
		{name: "simple fr", header: "fr", want: "fr"},
		{name: "simple tr", header: "tr", want: "tr"},

		// With region subtag
		{name: "en-US", header: "en-US", want: "en"},
		{name: "fr-FR", header: "fr-FR", want: "fr"},
		{name: "pt-BR", header: "pt-BR", want: "pt"},
		{name: "zh-Hans-CN", header: "zh-Hans-CN", want: "zh"},

		// With quality values
		{name: "with quality", header: "fr;q=0.9", want: "fr"},
		{name: "multiple with quality", header: "fr-FR,fr;q=0.9,en;q=0.8", want: "fr"},
		{name: "en preferred", header: "en-US,en;q=0.9,de;q=0.7", want: "en"},

		// Case normalization
		{name: "uppercase", header: "FR", want: "fr"},
		{name: "mixed case", header: "De-AT", want: "de"},

		// Edge cases
		{name: "wildcard with quality", header: "*;q=0.5", want: "en"},
		{name: "spaces around tag", header: "  fr  ", want: "fr"},
		{name: "three-letter code", header: "ast", want: "ast"},

		// Invalid inputs → fallback to "en"
		{name: "numeric garbage", header: "123", want: "en"},
		{name: "mixed alphanumeric", header: "x1", want: "en"},
		{name: "too long code", header: "abcd", want: "en"},
		{name: "single char", header: "a", want: "en"},
		{name: "special chars", header: "@@", want: "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAcceptLanguage(tt.header)
			if got != tt.want {
				t.Errorf("parseAcceptLanguage(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestAuthHandlerRegister_InvalidJSONReturnsBadRequest(t *testing.T) {
	app := newAuthTestApp(NewAuthHandler(nil))
	resp := doRequest(t, app, http.MethodPost, "/api/auth/register", "{invalid")

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestAuthHandlerRegister_InvalidPayloadReturnsValidationError(t *testing.T) {
	app := newAuthTestApp(NewAuthHandler(nil))
	resp := doRequest(t, app, http.MethodPost, "/api/auth/register", `{"email":"bad","username":"!","password":"123"}`)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "email") {
		t.Fatalf("expected validation response to mention email, got: %s", body)
	}
}

func TestAuthHandlerRefresh_InvalidJSONReturnsBadRequest(t *testing.T) {
	app := newAuthTestApp(NewAuthHandler(nil))
	resp := doRequest(t, app, http.MethodPost, "/api/auth/refresh", "{invalid")

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestAuthHandlerLogout_NoUserInContextReturnsUnauthorized(t *testing.T) {
	app := newAuthTestApp(NewAuthHandler(nil))
	resp := doRequest(t, app, http.MethodPost, "/api/auth/logout", `{}`)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestAuthHandlerVerifyEmail_MissingTokenReturnsBadRequest(t *testing.T) {
	app := newAuthTestApp(NewAuthHandler(nil))
	resp := doRequest(t, app, http.MethodGet, "/api/auth/verify-email", "")

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestAuthHandlerValidateResetToken_MissingTokenReturnsBadRequest(t *testing.T) {
	app := newAuthTestApp(NewAuthHandler(nil))
	resp := doRequest(t, app, http.MethodGet, "/api/auth/validate-reset-token", "")

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestGetTokenFromHeader_ValidBearerToken(t *testing.T) {
	app := fiber.New()
	app.Get("/token", func(c fiber.Ctx) error {
		token, err := GetTokenFromHeader(c)
		if err != nil {
			return err
		}
		return c.SendString(token)
	})

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	req.Header.Set("Authorization", "Bearer abc.def.ghi")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if body != "abc.def.ghi" {
		t.Fatalf("expected token extraction, got %q", body)
	}
}

func TestGetTokenFromHeader_MissingHeader(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
	app.Get("/token", func(c fiber.Ctx) error {
		_, err := GetTokenFromHeader(c)
		return err
	})

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestGetUserFromContext_Success(t *testing.T) {
	app := fiber.New()
	app.Get("/me", func(c fiber.Ctx) error {
		expected := &service.Claims{UserID: uuid.New()}
		c.Locals("claims", expected)
		got, err := GetUserFromContext(c)
		if err != nil {
			return err
		}
		if got.UserID != expected.UserID {
			t.Fatalf("expected claims user id to match")
		}
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestGetUserFromContext_ReturnsUnauthorizedWhenMissing(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
	app.Get("/me", func(c fiber.Ctx) error {
		_, err := GetUserFromContext(c)
		return err
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestAuthHandlerLogout_WithUUIDLocalTypeSafety(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
	})
	h := NewAuthHandler(nil)
	app.Post("/logout", func(c fiber.Ctx) error {
		// Wrong type intentionally to validate strict UUID type assertion path.
		c.Locals("userID", uuid.NewString())
		return h.Logout(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}
