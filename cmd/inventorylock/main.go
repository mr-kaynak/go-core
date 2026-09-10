// Command inventorylock regenerates coremigrations/migrations.lock from the
// embedded core migrations.
//
// Run it only when deliberately adding a migration:
//
//	go run ./cmd/inventorylock
//
// Regenerating after editing an existing migration defeats the check the lock
// file exists for — see the header it writes.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mr-kaynak/go-core/coremigrations"
)

const lockPath = "coremigrations/migrations.lock"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "inventorylock: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run from the repository root (go.mod not found): %w", err)
	}

	inventory, err := coremigrations.Inventory(coremigrations.FS())
	if err != nil {
		return err
	}

	// Write through a temporary file in the same directory so an interrupted
	// run cannot leave a half-written lock behind.
	dir := filepath.Dir(lockPath)
	tmp, err := os.CreateTemp(dir, ".migrations.lock.*")
	if err != nil {
		return fmt.Errorf("failed to create temporary lock file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if err := coremigrations.WriteInventory(tmp, inventory); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary lock file: %w", err)
	}
	if err := os.Rename(tmp.Name(), lockPath); err != nil {
		return fmt.Errorf("failed to install lock file: %w", err)
	}

	fmt.Printf("wrote %s (%d migrations)\n", lockPath, len(inventory))
	return nil
}
