// Package modcontract is the dependency-neutral home of the consumer module
// contract. Both the public app package and internal/infrastructure/server
// need these types; defining them here (instead of in app) is what keeps
// app -> server -> app from forming an import cycle. The public package
// aliases every type, so consumers only ever write app.Module and never see
// this path.
//
// This package deliberately depends on nothing but stdlib, fiber, gorm and
// internal/core/config (plus internal/infrastructure/authorization for the
// Permission alias). It must never import internal/infrastructure/server or
// anything that does.
package modcontract

import (
	"context"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"gorm.io/gorm"
)

// Module is a consumer-supplied unit of routes, handlers and services.
type Module interface {
	// Name identifies the module and must be unique across the application;
	// a collision is reported as an error from app.New.
	Name() string

	// Permissions declares the module's permissions. They are added to the
	// instance-scoped permission registry before bootstrap runs, so bootstrap
	// persists them and (per existing core behavior) grants them to
	// system_admin. Granting them to any other role is the consumer's call.
	Permissions() []Permission

	// Register wires the module's routes, handlers and services.
	//
	// Register MUST NOT start goroutines and MUST NOT open external
	// connections. Because of this rule module cleanup is a no-op: the only
	// resources app.New has to close on its error path are the ones core
	// itself started. Background work arrives in a later phase through
	// dedicated Starter/Stopper interfaces.
	//
	// Returning an error aborts startup; app.New then shuts down what it had
	// already started, in reverse order.
	Register(mctx *ModuleContext) error
}

// Permission declares a permission name and the Casbin (object, action) pairs
// it authorizes. It is an alias of the internal registry type, so the public
// facade and the registry can never drift apart.
//
// Objects are request PATH PATTERNS matched with keyMatch2, not bare resource
// names, and the field is plural because "/api/v1/orders/*" does NOT match the
// collection path "/api/v1/orders". A single permission therefore carries both
// patterns, e.g.:
//
//	Permission{
//	    Name:    "orders.view",
//	    Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
//	    Action:  "read",
//	}
//
// A permission name must be unique registry-wide, and so must every
// (object, action) pair: removing one permission must never silently revoke
// another. Grant and revoke always apply to ALL pairs of a permission. The
// consistency contract for multi-object mutations is NOT atomic — see the
// permission registry and permission service documentation.
//
// Action must be one of the core action set ("create", "read", "update",
// "delete", "list", "manage", "export", "import"); untyped string constants
// convert implicitly, so consumers need not name the internal action type.
type Permission = authorization.PermissionDef

// ModuleContext carries everything core hands a module at registration time.
// All fields are set by core and are non-nil.
type ModuleContext struct {
	// Config is the application configuration this instance was built with.
	Config *config.Config

	// DB is the shared GORM handle. Modules own their tables and migrations.
	DB *gorm.DB

	// Router is the /api/v1 group. Route paths registered here are the same
	// paths Permission.Objects must match.
	Router fiber.Router

	// Auth validates JWT or API-key credentials and populates the request
	// locals the identity package reads.
	Auth fiber.Handler

	// Authz enforces Casbin RBAC on the request's full path and the action
	// derived from its HTTP method (GET->read, POST->create, PUT/PATCH->update,
	// DELETE->delete).
	Authz fiber.Handler

	// Events publishes domain events through the transactional outbox.
	Events EventPublisher

	// Logger is the application logger.
	Logger *slog.Logger
}

// EventPublisher publishes domain events through the transactional outbox.
type EventPublisher interface {
	// Dispatch publishes an event of the given type for the given aggregate.
	//
	// Transactionality is the caller's choice: wrap ctx with the transaction
	// wrapper (app.ContextWithTx) to make the outbox insert commit atomically
	// with the business write. Without a transaction the insert runs
	// standalone and the event can be lost if the process crashes between the
	// business commit and the insert.
	//
	// Two further contracts hold:
	//
	//  1. aggregateID is preserved in the persisted outbox record; it is not
	//     dropped on the way to the queue.
	//  2. Local handlers and channel subscribers run BEFORE the transaction
	//     commits. Transaction atomicity covers ONLY the outbox write, so such
	//     handlers must not perform irreversible side effects and must not
	//     assume the business data is already visible.
	Dispatch(ctx context.Context, eventType string, aggregateID string, data map[string]any) error
}
