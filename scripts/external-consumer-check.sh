#!/usr/bin/env bash
#
# external-consumer-check.sh — CI gate for K3 (public facade sufficiency).
#
# Builds a throwaway Go module OUTSIDE this repository that depends on go-core
# via `replace` and consumes only the public facade (app + identity). Because
# it is a separate module, Go itself forbids it from reaching into
# `internal/`: if the facade is missing anything a consumer needs, this build
# fails. Compile-only — the application is never run, so no PostgreSQL, Redis
# or RabbitMQ is required.
#
# Usage: bash scripts/external-consumer-check.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_VERSION="$(awk '/^go [0-9]/ {print $2; exit}' "$REPO_ROOT/go.mod")"

if [ -z "$GO_VERSION" ]; then
    echo "error: could not read the go directive from $REPO_ROOT/go.mod" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/go-core-external-consumer.XXXXXX")"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT

echo "==> External consumer compile check"
echo "    go-core:  $REPO_ROOT"
echo "    go:       $GO_VERSION"
echo "    work dir: $WORK_DIR"

cat >"$WORK_DIR/go.mod" <<EOF
module example.com/consumer

go $GO_VERSION

require github.com/mr-kaynak/go-core v0.0.0

replace github.com/mr-kaynak/go-core => $REPO_ROOT
EOF

cat >"$WORK_DIR/main.go" <<'EOF'
// Package main is a throwaway external consumer of go-core. It mirrors
// examples/minimal: a module implementing app.Module, plus application
// construction through app.New. It exists to be compiled, not run.
package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/identity"
)

type ordersModule struct{}

func (ordersModule) Name() string { return "orders" }

func (ordersModule) Permissions() []app.Permission {
	return []app.Permission{{
		Name:    "orders.view",
		Objects: []string{"/api/v1/orders", "/api/v1/orders/*"},
		Action:  app.ActionRead,
	}}
}

func (ordersModule) Register(mctx *app.ModuleContext) error {
	mctx.Router.Group("/orders", mctx.Auth, mctx.Authz).Get("/", func(c fiber.Ctx) error {
		principal, ok := identity.FromContext(c)
		if !ok {
			return fiber.ErrUnauthorized
		}
		return c.JSON(fiber.Map{"user_id": principal.UserID.String()})
	})
	return nil
}

// serve is compiled but never invoked: running it would need a live
// PostgreSQL. Compilation alone is the proof that the public facade covers
// config loading, module registration and the application lifecycle.
func serve() error {
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	a, err := app.New(cfg, app.WithModules(ordersModule{}))
	if err != nil {
		return err
	}
	return a.Run()
}

func main() {
	fmt.Println("compile-only external consumer; serve is intentionally not called")
	_ = serve
}
EOF

cd "$WORK_DIR"

# go mod tidy talks to the module proxy and sum database; transient network
# hiccups (e.g. sum.golang.org stream errors) must not fail the gate, so retry
# a few times before declaring a real failure.
tidy_ok=false
for attempt in 1 2 3; do
    if go mod tidy; then
        tidy_ok=true
        break
    fi
    echo "WARN: go mod tidy attempt ${attempt} failed; retrying..." >&2
    sleep $((attempt * 5))
done
if [ "$tidy_ok" != "true" ]; then
    echo "FAIL: go mod tidy failed in the external consumer module after 3 attempts" >&2
    exit 1
fi

if ! go build ./...; then
    echo "FAIL: the external consumer module does not compile against the public facade" >&2
    echo "      A missing type or function here means the facade (app/identity/coremigrations) has a gap." >&2
    exit 1
fi

echo "PASS: external consumer module compiles against the public facade only"
