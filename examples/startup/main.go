// A consumer application: core owns identity; this service owns projects.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/examples/startup/projects"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	migrate := flag.Bool("migrate", false, "apply core and project migrations, then exit")
	report := flag.Bool("report", false, "print migration state without writing, then exit")
	flag.Parse()
	if *migrate && *report {
		return fmt.Errorf("choose either -migrate or -report")
	}
	// Load from the working directory. Environment variables take precedence.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load .env: %w", err)
	}
	module := projects.New()
	if *migrate || *report {
		cfg, err := app.LoadMigratorConfig()
		if err != nil {
			return err
		}
		m, err := app.NewMigrator(cfg, module.MigrationSource())
		if err != nil {
			return err
		}
		defer m.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if *report {
			state, err := m.Report(ctx)
			if err != nil {
				return err
			}
			result := struct {
				Report       app.MigrationReport `json:"report"`
				ServeAllowed bool                `json:"serve_allowed"`
				ServeRefusal string              `json:"serve_refusal,omitempty"`
			}{Report: state, ServeAllowed: true}
			if err := m.CheckServing(ctx); err != nil {
				result.ServeAllowed = false
				result.ServeRefusal = err.Error()
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		return m.Up(ctx)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	a, err := app.New(cfg, app.WithModules(module))
	if err != nil {
		return err
	}
	return a.Run()
}
