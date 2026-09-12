// Package orders is the example consumer module: a small orders API that
// plugs into go-core through the public facade alone.
//
// It imports nothing but the standard library, fiber, and go-core's public
// app and identity packages — no go-core internal package. That restriction
// is what makes the example a proof of the consumer contract rather than a
// demo, and internal/test/boundary enforces it on every run.
//
// The store is in-memory on purpose: this is the smallest module example.
// See examples/startup for persistent tables, module migrations and transactions.
// Orders here are shared by all callers with orders permissions. This is not
// an ownership/tenancy template; use examples/startup for caller-scoped data.
package orders

import (
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/identity"
)

// Order is a single stored order.
type Order struct {
	ID        string    `json:"id"`
	Item      string    `json:"item"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// module implements app.Module.
type module struct {
	mu     sync.RWMutex
	nextID int
	orders []Order

	events app.EventPublisher
	log    *slog.Logger
}

// New returns the orders module, ready to pass to app.WithModules.
func New() app.Module { return &module{} }

// Name identifies the module and must be unique across the application.
func (m *module) Name() string { return "orders" }

// Permissions declares what the module's routes require. Each permission
// carries BOTH path patterns: the keyMatch2 pattern "/api/v1/orders/*" does
// not match the bare collection path "/api/v1/orders".
func (m *module) Permissions() []app.Permission {
	return []app.Permission{
		{
			Name:    "orders.view",
			Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
			Action:  app.ActionRead,
		},
		{
			Name:    "orders.create",
			Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
			Action:  app.ActionCreate,
		},
	}
}

// Register wires the module's routes. Auth authenticates the caller and Authz
// enforces the permissions declared above — with both on the group, handlers
// only ever run for an authorized principal. Register starts no goroutines and
// opens no connections, as the Module contract requires.
func (m *module) Register(mctx *app.ModuleContext) error {
	m.events = mctx.Events
	m.log = mctx.Logger

	orders := mctx.Router.Group("/orders", mctx.Auth, mctx.Authz)
	orders.Get("/", m.list)
	orders.Get("/:id", m.get)
	orders.Post("/", m.create)
	return nil
}

func (m *module) list(c fiber.Ctx) error {
	m.mu.RLock()
	out := make([]Order, len(m.orders))
	copy(out, m.orders)
	m.mu.RUnlock()

	return c.JSON(fiber.Map{"orders": out, "total": len(out)})
}

func (m *module) get(c fiber.Ctx) error {
	id := c.Params("id")

	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.orders {
		if m.orders[i].ID == id {
			return c.JSON(m.orders[i])
		}
	}
	return fiber.NewError(fiber.StatusNotFound, "order not found")
}

func (m *module) create(c fiber.Ctx) error {
	var req struct {
		Item string `json:"item"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.Item == "" {
		return fiber.NewError(fiber.StatusBadRequest, "item is required")
	}

	// The caller comes from the public identity facade, which reads the
	// request locals the Auth middleware populated.
	principal, ok := identity.FromContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	m.mu.Lock()
	m.nextID++
	order := Order{
		ID:        strconv.Itoa(m.nextID),
		Item:      req.Item,
		CreatedBy: principal.UserID.String(),
		CreatedAt: time.Now().UTC(),
	}
	m.orders = append(m.orders, order)
	m.mu.Unlock()

	// No transaction is carried here because this module has no database write
	// to be atomic with. A DB-backed module publishes from inside its own
	// transaction instead: app.ContextWithTx(ctx, tx) makes the outbox insert
	// commit together with the business row, so a crash cannot drop the event.
	if err := m.events.Dispatch(c.Context(), "order.created", order.ID, map[string]any{
		"order_id":   order.ID,
		"item":       order.Item,
		"created_by": order.CreatedBy,
	}); err != nil {
		// The order is already stored, so failing the request here would
		// misreport what happened; log and let the client see the created row.
		m.log.Error("Failed to dispatch order.created", "order_id", order.ID, "error", err)
	}

	return c.Status(fiber.StatusCreated).JSON(order)
}
