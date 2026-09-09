package coremigrations

import (
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
)

// expectedMigrationCount is the size of the single core migration history
// (00001-00016). Bump it together with every new core migration file.
const expectedMigrationCount = 16

// TestFSContainsCoreMigrationsAtRoot verifies the embedded filesystem exposes
// the .sql files at its top level, which is what goose expects when it is
// pointed at ".". Migrations are not executed here: they are PostgreSQL-only.
func TestFSContainsCoreMigrationsAtRoot(t *testing.T) {
	names, err := fs.Glob(FS(), "*.sql")
	if err != nil {
		t.Fatalf("failed to glob embedded migrations: %v", err)
	}

	if len(names) != expectedMigrationCount {
		t.Fatalf("expected %d embedded migrations, got %d: %v",
			expectedMigrationCount, len(names), names)
	}
}

// TestFSMigrationVersionsAreContiguous verifies every embedded file carries a
// goose-parseable numeric prefix and that the versions run 1..16 without gaps
// or duplicates, so a consumer booting from the embedded FS gets the same
// history as one booting from the source tree.
func TestFSMigrationVersionsAreContiguous(t *testing.T) {
	names, err := fs.Glob(FS(), "*.sql")
	if err != nil {
		t.Fatalf("failed to glob embedded migrations: %v", err)
	}

	seen := make(map[int64]string, len(names))
	for _, name := range names {
		version, verErr := goose.NumericComponent(name)
		if verErr != nil {
			t.Fatalf("file %q has no goose numeric prefix: %v", name, verErr)
		}
		if previous, dup := seen[version]; dup {
			t.Fatalf("version %d claimed by both %q and %q", version, previous, name)
		}
		seen[version] = name
	}

	for version := int64(1); version <= expectedMigrationCount; version++ {
		if _, ok := seen[version]; !ok {
			t.Fatalf("missing migration version %d in embedded FS", version)
		}
	}
}

// TestFSExcludesNestedDirectories verifies FS() is rooted at the sql directory
// itself, so goose never has to walk into a subdirectory to find migrations.
func TestFSExcludesNestedDirectories(t *testing.T) {
	entries, err := fs.ReadDir(FS(), ".")
	if err != nil {
		t.Fatalf("failed to read embedded root: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected directory %q at embedded root", entry.Name())
		}
	}
}
