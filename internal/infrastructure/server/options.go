package server

import (
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
)

// Option configures optional server behavior (consumer modules, shared
// permission registry). Core wiring stays positional; options carry only the
// extension surface so existing callers compile unchanged.
type Option func(*serverOptions)

type serverOptions struct {
	modules  []modcontract.Module
	registry *authorization.PermissionRegistry
}

// WithConsumerModules registers consumer-owned modules. Their permissions are
// added to the permission registry before any route is served, and their
// Register hooks run AFTER core routes, receiving the /api/v1 router group,
// the auth/authz middleware chain and the event publisher. A failing module
// aborts server construction — a half-registered application never starts.
func WithConsumerModules(mods ...modcontract.Module) Option {
	return func(o *serverOptions) {
		o.modules = append(o.modules, mods...)
	}
}

// WithPermissionRegistry supplies the instance-scoped permission registry
// shared across bootstrap, the authorization layer and the permission
// service. Defaults to a fresh core-only registry when omitted.
func WithPermissionRegistry(r *authorization.PermissionRegistry) Option {
	return func(o *serverOptions) {
		if r != nil {
			o.registry = r
		}
	}
}

func resolveOptions(opts []Option) *serverOptions {
	o := &serverOptions{}
	for _, apply := range opts {
		apply(o)
	}
	if o.registry == nil {
		o.registry = authorization.NewPermissionRegistry()
	}
	return o
}
