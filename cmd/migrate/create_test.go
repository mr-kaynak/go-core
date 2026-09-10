package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
)

// create no longer goes through goose, so the file it writes has to satisfy
// the rules goose and the source validator apply on their own: a parseable
// sequential version, and SQL that keeps the migration transactional. Nothing
// enforces that at compile time, which is what makes it worth a test.
func TestCreateWritesAMigrationTheValidatorAccepts(t *testing.T) {
	sqlDir := newSourceTree(t, "00001_initial_schema.sql", "00002_add_user_avatar.sql")

	var out strings.Builder
	if err := createMigration(migrationsource.CoreName, []string{"Add Order Table"}, &out); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Sequential, not a timestamp: core's history has to stay contiguous
	// from 1, and a timestamp version would open a gap of several trillion.
	const want = "00003_add_order_table.sql"
	if _, err := os.Stat(filepath.Join(sqlDir, want)); err != nil {
		t.Fatalf("expected %s to exist: %v", want, err)
	}
	if !strings.Contains(out.String(), want) {
		t.Fatalf("create did not report the file it wrote:\n%s", out.String())
	}

	// A consumer name, because "core" is reserved for the real source; the
	// rules being checked here are the same either way.
	source := modcontract.MigrationSource{Name: "orders", FS: os.DirFS(sqlDir)}
	if err := migrationsource.ValidateContiguous(source); err != nil {
		t.Fatalf("the generated migration is not a valid source: %v", err)
	}
}

func TestWriteNewFileRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "00001_first.sql")

	if err := writeNewFile(path, "original"); err != nil {
		t.Fatalf("the first write failed: %v", err)
	}
	if err := writeNewFile(path, "replacement"); err == nil {
		t.Fatal("an existing migration was overwritten")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read the file back: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("the file was modified: %q", content)
	}
}

// A version that cannot be parsed has to stop create rather than be skipped:
// skipping it would number the new file over a migration that already exists.
func TestCreateFailsOnAnUnparseableExistingVersion(t *testing.T) {
	newSourceTree(t, "00001_initial_schema.sql", "notes.sql")

	err := createMigration(migrationsource.CoreName, []string{"anything"}, io.Discard)
	if err == nil {
		t.Fatal("create should refuse a directory it cannot number")
	}
	if !strings.Contains(err.Error(), "notes.sql") {
		t.Fatalf("the error should name the offending file, got: %v", err)
	}
}

// The shipped binary embeds the migrations and has no source tree, so create
// is the one command that cannot work there. It has to say so.
func TestCreateFailsClearlyWithoutASourceTree(t *testing.T) {
	t.Chdir(t.TempDir())

	err := createMigration(migrationsource.CoreName, []string{"anything"}, io.Discard)
	if err == nil {
		t.Fatal("create should fail with no migration directory present")
	}
	for _, want := range []string{coreSourceDir, "embedded"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error should mention %q, got: %v", want, err)
		}
	}
}

func TestCreateRejectsANonCoreSource(t *testing.T) {
	newSourceTree(t, "00001_initial_schema.sql")

	if err := createMigration("orders", []string{"anything"}, io.Discard); err == nil {
		t.Fatal("create should refuse a source whose files it does not own")
	}
}

// newSourceTree puts the process in a checkout-shaped temporary directory
// holding the given migrations, and returns the migration directory.
func newSourceTree(t *testing.T, names ...string) string {
	t.Helper()

	root := t.TempDir()
	sqlDir := filepath.Join(root, coreSourceDir)
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("failed to build the source tree: %v", err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(sqlDir, name), []byte(migrationTemplate), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	t.Chdir(root)
	return sqlDir
}
