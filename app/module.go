package app

import (
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
)

// The module contract types live in a dependency-neutral internal package
// (modcontract) so the server can consume them without importing this
// package; consumers only ever write the app.* names below.

// Module is a consumer-owned business module registered via WithModules.
// See modcontract.Module for the full behavioral contract (Register must not
// start goroutines or open external connections).
type Module = modcontract.Module

// ModuleContext carries the wiring surface handed to Module.Register.
type ModuleContext = modcontract.ModuleContext

// Permission declares a module permission: a unique name mapped to one or
// more Casbin object path patterns and one action. keyMatch2 patterns like
// "/api/v1/orders/*" do NOT match the bare collection path, so a typical
// permission carries both: {"/api/v1/orders", "/api/v1/orders/*"}.
type Permission = modcontract.Permission

// EventPublisher is the outbox-backed event publishing surface available to
// modules. Wrap the context with ContextWithTx for transactional atomicity.
type EventPublisher = modcontract.EventPublisher

// Action re-exports the closed set of permission actions.
type Action = authorization.Action

// Permission action values.
const (
	ActionCreate = authorization.ActionCreate
	ActionRead   = authorization.ActionRead
	ActionUpdate = authorization.ActionUpdate
	ActionDelete = authorization.ActionDelete
	ActionList   = authorization.ActionList
	ActionManage = authorization.ActionManage
	ActionExport = authorization.ActionExport
	ActionImport = authorization.ActionImport
)

// Option configures New.
type Option func(*options)

type options struct {
	modules  []Module
	registry *authorization.PermissionRegistry
}

// WithModules registers consumer modules: their permissions enter the shared
// permission registry (validated atomically, before bootstrap) and their
// Register hooks run after core routes.
func WithModules(mods ...Module) Option {
	return func(o *options) {
		o.modules = append(o.modules, mods...)
	}
}

func resolveOptions(opts []Option) *options {
	o := &options{}
	for _, apply := range opts {
		apply(o)
	}
	if o.registry == nil {
		o.registry = authorization.NewPermissionRegistry()
	}
	return o
}
