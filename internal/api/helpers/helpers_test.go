package helpers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
)

// --- ParseUUIDParam ---

func TestParseUUIDParam_Valid(t *testing.T) {
	want := uuid.New()

	app := fiber.New()
	app.Get("/items/:id", func(c fiber.Ctx) error {
		got, err := ParseUUIDParam(c, "id", "Invalid id format")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("ParseUUIDParam() = %v, want %v", got, want)
		}
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/items/"+want.String(), nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestParseUUIDParam_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		param   string
		message string
	}{
		{"malformed uuid, param 'id'", "id", "Invalid id format"},
		{"malformed uuid, param 'userId'", "userId", "Invalid user ID"},
		{"malformed uuid, arbitrary message", "id", "boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramName := tt.param
			wantMessage := tt.message

			app := fiber.New(fiber.Config{
				ErrorHandler: func(c fiber.Ctx, err error) error {
					pd, ok := err.(*errors.ProblemDetail)
					if !ok {
						t.Fatalf("expected *errors.ProblemDetail, got %T", err)
					}
					if pd.Status != http.StatusBadRequest {
						t.Errorf("expected 400 status, got %d", pd.Status)
					}
					if pd.Detail != wantMessage {
						t.Errorf("expected detail %q, got %q", wantMessage, pd.Detail)
					}
					return c.Status(pd.Status).JSON(pd)
				},
			})
			app.Get("/items/:"+paramName, func(c fiber.Ctx) error {
				_, err := ParseUUIDParam(c, paramName, wantMessage)
				return err
			})

			req := httptest.NewRequest(http.MethodGet, "/items/not-a-uuid", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestParseUUIDParam_NilOnFailure(t *testing.T) {
	app := fiber.New()
	app.Get("/items/:id", func(c fiber.Ctx) error {
		got, err := ParseUUIDParam(c, "id", "Invalid id format")
		if err == nil {
			t.Error("expected error for invalid uuid")
		}
		if got != uuid.Nil {
			t.Errorf("expected uuid.Nil on failure, got %v", got)
		}
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/items/garbage", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
}

// --- ParsePagination ---

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		defaultLimit int
		wantPage     int
		wantLimit    int
		wantOffset   int
	}{
		{"defaults when no params", "", 20, 1, 20, 0},
		{"explicit page and limit", "?page=3&limit=10", 20, 3, 10, 20},
		{"page below 1 clamps to 1", "?page=0", 20, 1, 20, 0},
		{"negative page clamps to 1", "?page=-5", 20, 1, 20, 0},
		{"limit above max clamps to 100", "?limit=500", 20, 1, 100, 0},
		{"limit below 1 falls back to default", "?limit=0", 20, 1, 20, 0},
		{"page 2 offset computed from clamped limit", "?page=2&limit=500", 20, 2, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPage, gotLimit, gotOffset int

			app := fiber.New()
			app.Get("/list", func(c fiber.Ctx) error {
				gotPage, gotLimit, gotOffset = ParsePagination(c, tt.defaultLimit)
				return c.SendString("ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/list"+tt.query, nil)
			if _, err := app.Test(req); err != nil {
				t.Fatal(err)
			}

			if gotPage != tt.wantPage {
				t.Errorf("page = %d, want %d", gotPage, tt.wantPage)
			}
			if gotLimit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", gotLimit, tt.wantLimit)
			}
			if gotOffset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", gotOffset, tt.wantOffset)
			}
		})
	}
}

// --- ValidateSort ---

func TestValidateSort(t *testing.T) {
	allowed := map[string]bool{
		"created_at": true,
		"updated_at": true,
	}

	tests := []struct {
		name      string
		sortBy    string
		order     string
		wantErr   bool
		wantSort  string
		wantOrder string
	}{
		{"empty values accepted", "", "", false, "", ""},
		{"allowed sort field", "created_at", "asc", false, "created_at", "asc"},
		{"allowed sort field desc", "updated_at", "desc", false, "updated_at", "desc"},
		{"disallowed sort field errors", "password", "asc", true, "", ""},
		{"case mismatch errors (case-sensitive)", "Created_At", "asc", true, "", ""},
		{"invalid order errors", "created_at", "sideways", true, "", ""},
		{"empty order with valid sort", "created_at", "", false, "created_at", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSort, gotOrder, err := ValidateSort(tt.sortBy, tt.order, allowed)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pd, ok := err.(*errors.ProblemDetail)
				if !ok {
					t.Fatalf("expected *errors.ProblemDetail, got %T", err)
				}
				if pd.Status != http.StatusBadRequest {
					t.Errorf("expected 400 status, got %d", pd.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotSort != tt.wantSort {
				t.Errorf("sortBy = %q, want %q", gotSort, tt.wantSort)
			}
			if gotOrder != tt.wantOrder {
				t.Errorf("order = %q, want %q", gotOrder, tt.wantOrder)
			}
		})
	}
}
