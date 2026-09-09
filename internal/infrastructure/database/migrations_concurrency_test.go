package database

import (
	"fmt"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// sqliteMigrationFS returns a tiny goose-compatible migration set that SQLite
// can execute (the real core migrations are PostgreSQL-only, so this test uses
// its own fixture to exercise the RUNNER, not the SQL).
func sqliteMigrationFS(table string) fstest.MapFS {
	return fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{Data: []byte(fmt.Sprintf(
			"-- +goose Up\nCREATE TABLE %s (id INTEGER PRIMARY KEY);\n-- +goose Down\nDROP TABLE %s;\n",
			table, table,
		))},
	}
}

func openMigrationTestDB(t *testing.T, name string) *DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &DB{DB: gdb}
}

// TestRunMigrationsFS_ConcurrentStartupsAreIsolated: goose keeps the base
// filesystem in a package-global. Two concurrent application startups (e.g.
// parallel app.New with auto-migration) must not clobber each other's
// filesystem mid-run — a finished invocation's reset must never switch a
// still-running one back to the OS filesystem.
func TestRunMigrationsFS_ConcurrentStartupsAreIsolated(t *testing.T) {
	const rounds = 20
	for i := 0; i < rounds; i++ {
		dbA := openMigrationTestDB(t, fmt.Sprintf("migA_%d", i))
		dbB := openMigrationTestDB(t, fmt.Sprintf("migB_%d", i))

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = runMigrationsFSWithDialect(dbA, sqliteMigrationFS("table_a"), "sqlite3") }()
		go func() { defer wg.Done(); errs[1] = runMigrationsFSWithDialect(dbB, sqliteMigrationFS("table_b"), "sqlite3") }()
		wg.Wait()

		for n, err := range errs {
			if err != nil {
				t.Fatalf("round %d, runner %d: concurrent migration failed: %v", i, n, err)
			}
		}
	}
}
