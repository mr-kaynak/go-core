package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/pressly/goose/v3"
)

const migrationsDir = "platform/migrations"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	db, err := openDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if cmdErr := runCommand(db, os.Args[1], os.Args[2:]); cmdErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", cmdErr)
		os.Exit(1) //nolint:gocritic // exitAfterDefer: deferred cleanup is best-effort
	}
}

func openDatabase(cfg *config.Config) (*sql.DB, error) {
	dsn := cfg.GetDSN()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err = db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err = goose.SetDialect("postgres"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set dialect: %w", err)
	}

	return db, nil
}

func runCommand(db *sql.DB, command string, args []string) error {
	switch command {
	case "up":
		return goose.Up(db, migrationsDir)
	case "up-one":
		return goose.UpByOne(db, migrationsDir)
	case "down":
		return goose.Down(db, migrationsDir)
	case "redo":
		return goose.Redo(db, migrationsDir)
	case "reset":
		return goose.Reset(db, migrationsDir)
	case "status":
		return goose.Status(db, migrationsDir)
	case "version":
		return goose.Version(db, migrationsDir)
	case "create":
		if len(args) == 0 {
			return fmt.Errorf(
				"migration name required\nUsage: go run cmd/migrate/main.go create <name>",
			)
		}
		return goose.Create(db, migrationsDir, args[0], "sql")
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printUsage() {
	fmt.Println("Usage: go run cmd/migrate/main.go <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  up        Apply all pending migrations")
	fmt.Println("  up-one    Apply the next pending migration")
	fmt.Println("  down      Roll back the last migration")
	fmt.Println("  redo      Roll back and re-apply the last migration")
	fmt.Println("  reset     Roll back all migrations")
	fmt.Println("  status    Show migration status")
	fmt.Println("  version   Show current migration version")
	fmt.Println("  create    Create a new migration file (requires name)")
}
