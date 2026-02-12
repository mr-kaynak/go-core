package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/internal/core/config"
	_ "github.com/jackc/pgx/v5/stdlib"
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

	dsn := cfg.GetDSN()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to ping database: %v\n", err)
		os.Exit(1)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set dialect: %v\n", err)
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "up":
		err = goose.Up(db, migrationsDir)
	case "up-one":
		err = goose.UpByOne(db, migrationsDir)
	case "down":
		err = goose.Down(db, migrationsDir)
	case "redo":
		err = goose.Redo(db, migrationsDir)
	case "reset":
		err = goose.Reset(db, migrationsDir)
	case "status":
		err = goose.Status(db, migrationsDir)
	case "version":
		err = goose.Version(db, migrationsDir)
	case "create":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "Error: migration name required\n")
			fmt.Fprintf(os.Stderr, "Usage: go run cmd/migrate/main.go create <name>\n")
			os.Exit(1)
		}
		err = goose.Create(db, migrationsDir, args[0], "sql")
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Migration %s failed: %v\n", command, err)
		os.Exit(1)
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
