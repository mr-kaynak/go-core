package projects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/app"
	"gorm.io/gorm"
)

type eventStub struct{ err error }

func (e *eventStub) Dispatch(context.Context, string, string, map[string]any) error { return e.err }

// This harness tests handler ownership/rollback, not core authentication or
// PostgreSQL migrations. The auth seam supplies an already verified principal.
func harness(t *testing.T) (*fiber.App, *gorm.DB, *eventStub) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&Project{}); err != nil {
		t.Fatal(err)
	}
	api := fiber.New()
	events := &eventStub{}
	m := New()
	err = m.Register(&app.ModuleContext{
		DB: db, Events: events, Router: api.Group("/api/v1"),
		Auth: func(c fiber.Ctx) error {
			id, err := uuid.Parse(c.Get("X-Test-User"))
			if err != nil {
				return fiber.ErrUnauthorized
			}
			c.Locals("userID", id)
			return c.Next()
		},
		Authz: func(c fiber.Ctx) error { return c.Next() },
	})
	if err != nil {
		t.Fatal(err)
	}
	return api, db, events
}

func request(t *testing.T, api *fiber.App, method, path, body, user string, want int, result any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", user)
	res, err := api.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		t.Fatalf("%s %s: got %d, want %d", method, path, res.StatusCode, want)
	}
	if result != nil {
		if err := json.NewDecoder(res.Body).Decode(result); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectsOwnershipAndValidation(t *testing.T) {
	api, _, _ := harness(t)
	owner, other := uuid.NewString(), uuid.NewString()
	request(t, api, "GET", "/api/v1/projects", "", "", 401, nil)
	for _, body := range []string{`{`, `{"name":"  "}`, `{"name":"` + strings.Repeat("a", 121) + `"}`} {
		request(t, api, "POST", "/api/v1/projects", body, owner, 400, nil)
	}
	var row Project
	request(t, api, "POST", "/api/v1/projects", `{"name":"  Acme  ","owner_id":"`+other+`"}`, owner, 201, &row)
	if row.Name != "Acme" || row.OwnerID.String() != owner {
		t.Fatalf("unexpected project: %+v", row)
	}
	request(t, api, "GET", "/api/v1/projects/"+row.ID.String(), "", owner, 200, nil)
	request(t, api, "GET", "/api/v1/projects/"+row.ID.String(), "", other, 404, nil)
	request(t, api, "GET", "/api/v1/projects/not-a-uuid", "", owner, 400, nil)
	var list struct {
		Projects []Project `json:"projects"`
		Total    int64     `json:"total"`
	}
	request(t, api, "GET", "/api/v1/projects", "", other, 200, &list)
	if list.Total != 0 || len(list.Projects) != 0 {
		t.Fatal("another owner can see the project")
	}
	request(t, api, "GET", "/api/v1/projects", "", owner, 200, &list)
	if list.Total != 1 || len(list.Projects) != 1 {
		t.Fatal("owner cannot see the project")
	}
}

func TestDispatchFailureRollsBackProject(t *testing.T) {
	api, db, events := harness(t)
	events.err = errors.New("outbox unavailable")
	request(t, api, "POST", "/api/v1/projects", `{"name":"Must roll back"}`, uuid.NewString(), 500, nil)
	var count int64
	if err := db.Model(&Project{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed publication left %d business rows", count)
	}
}
