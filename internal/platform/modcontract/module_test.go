package modcontract_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
)

// ordersModule is the recipe from the plan: one permission name carrying both
// the collection and the item Casbin pattern.
type ordersModule struct{}

func (ordersModule) Name() string { return "orders" }

func (ordersModule) Permissions() []modcontract.Permission {
	return []modcontract.Permission{
		{
			Name:    "orders.view",
			Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
			Action:  authorization.ActionRead,
		},
	}
}

func (ordersModule) Register(mctx *modcontract.ModuleContext) error {
	mctx.Router.Get("/orders", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	return nil
}

var _ modcontract.Module = ordersModule{}

// noopPublisher asserts the EventPublisher shape is implementable outside the
// package.
type noopPublisher struct{}

func (noopPublisher) Dispatch(context.Context, string, string, map[string]any) error { return nil }

var _ modcontract.EventPublisher = noopPublisher{}

// TestModuleContract keeps the contract honest at compile time: Permission
// must stay assignable from authorization.PermissionDef, and ModuleContext
// must keep the field set consumers are promised.
func TestModuleContract(t *testing.T) {
	//nolint:staticcheck // The explicit type verifies that the public permission alias remains assignable.
	var def authorization.PermissionDef = ordersModule{}.Permissions()[0]
	if def.Name != "orders.view" {
		t.Fatalf("Name = %q, want orders.view", def.Name)
	}

	app := fiber.New()
	mctx := &modcontract.ModuleContext{
		Config: &config.Config{},
		Router: app.Group("/api/v1"),
		Auth:   func(c fiber.Ctx) error { return c.Next() },
		Authz:  func(c fiber.Ctx) error { return c.Next() },
		Events: noopPublisher{},
		Logger: slog.Default(),
	}

	if err := (ordersModule{}).Register(mctx); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
}
