// @title Go-Core API
// @version 1.0.0
// @description Enterprise-grade Go boilerplate with REST API, gRPC services, and notification system

// @contact.name Go-Core Team
// @contact.url https://github.com/mr-kaynak/go-core

// @license.name MIT

// @host localhost:3000
// @BasePath /api/v1

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description JWT Bearer token. Format: "Bearer {token}"

// Package main is the reference consumer of the public app facade: it does
// exactly what an external application does — load config, construct the app,
// run it. Anything this binary needs beyond the facade is a facade gap.
package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/core/logger"
)

func main() {
	if err := run(); err != nil {
		logger.Get().Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func printBanner() {
	banner := `
  ██████╗  ██████╗        ██████╗ ██████╗ ██████╗ ███████╗
 ██╔════╝ ██╔═══██╗      ██╔════╝██╔═══██╗██╔══██╗██╔════╝
 ██║  ███╗██║   ██║█████╗██║     ██║   ██║██████╔╝█████╗
 ██║   ██║██║   ██║╚════╝██║     ██║   ██║██╔══██╗██╔══╝
 ╚██████╔╝╚██████╔╝      ╚██████╗╚██████╔╝██║  ██║███████╗
  ╚═════╝  ╚═════╝        ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝
`
	fmt.Print(banner)
}

func run() error {
	printBanner()

	// Load .env file (development convenience; the facade itself reads only
	// environment variables).
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Warning: .env file not found or couldn't be loaded: %v\n", err)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	a, err := app.New(cfg)
	if err != nil {
		return err
	}

	runErr := a.Run()

	// Process-level resources are closed here, not in App.Shutdown: the
	// logger and a globally-published tracer provider belong to the process.
	if closeErr := logger.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}

	return runErr
}
