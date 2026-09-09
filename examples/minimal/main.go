// Package main is the minimal go-core consumer application. It serves
// everything core provides — login, profile, roles, notifications — plus its
// own orders module, while importing ONLY go-core's public packages.
//
// That is the point of the example: no go-core internal package appears
// anywhere under examples/ (enforced by internal/test/boundary), so whatever
// this application needs, the facade already exposes. A gap here is a gap in
// the facade.
package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/examples/minimal/orders"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Development convenience only; the facade itself reads the environment.
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no .env file loaded: %v\n", err)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	a, err := app.New(cfg, app.WithModules(orders.New()))
	if err != nil {
		return err
	}

	return a.Run()
}
