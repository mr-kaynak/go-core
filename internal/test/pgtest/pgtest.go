// Package pgtest hands tests a real, isolated PostgreSQL database.
//
// Migration behavior cannot be established on SQLite: advisory locks, the
// catalog fingerprint, DDL transactionality and goose's PostgreSQL dialect all
// differ. These tests therefore need a real server, which not every developer
// has running — so they skip by default and are made mandatory in CI.
//
// # Silence is not success
//
// A skipped test looks exactly like a passing one in a summary. CI therefore
// sets GOCORE_REQUIRE_POSTGRES=1, which turns "no DSN" from a skip into a
// failure. Never set that variable locally; never unset it in CI.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for the test databases
)

const (
	// DSNEnv points at a PostgreSQL server the tests may create databases on.
	DSNEnv = "GOCORE_TEST_POSTGRES_DSN"
	// RequireEnv makes a missing DSN a failure instead of a skip.
	RequireEnv = "GOCORE_REQUIRE_POSTGRES"
)

var dbCounter atomic.Uint64

// runTag distinguishes this process's databases from another's.
//
// The counter alone restarts at zero in every test binary, so two packages
// running against the same server — which is exactly what `go test ./...`
// does — can generate the same name and clobber each other. That failure is
// intermittent and looks like a bug in whichever test loses, which is the
// worst kind to leave lying around.
var runTag = fmt.Sprintf("%d_%d", os.Getpid(), time.Now().UnixNano()%1_000_000)

// DB is an isolated database created for one test and dropped afterwards.
type DB struct {
	*sql.DB
	Name string
	DSN  string
}

// New creates a database of its own for t and returns a connection to it.
//
// Every test gets a distinct database rather than a shared one with distinct
// table names: the code under test creates history tables, takes advisory
// locks and inspects the whole catalog, none of which are isolated by naming
// alone.
func New(t *testing.T) *DB {
	t.Helper()

	adminDSN := serverDSN(t)
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("pgtest: failed to open the admin connection: %v", err)
	}
	defer admin.Close() //nolint:errcheck // admin handle is short-lived

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("pgtest: %s is set but the server is unreachable: %v", DSNEnv, err)
	}

	name := databaseName(t)
	// Identifiers cannot be parameterized; databaseName guarantees the value
	// is built from a sanitised test name plus a counter.
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatalf("pgtest: failed to create database %q: %v", name, err)
	}

	dsn := replaceDatabase(adminDSN, name)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		dropDatabase(adminDSN, name)
		t.Fatalf("pgtest: failed to open %q: %v", name, err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck // already failing
		dropDatabase(adminDSN, name)
		t.Fatalf("pgtest: failed to reach %q: %v", name, err)
	}

	t.Cleanup(func() {
		db.Close() //nolint:errcheck // best-effort before the drop
		dropDatabase(adminDSN, name)
	})

	return &DB{DB: db, Name: name, DSN: dsn}
}

// Open returns an additional independent connection pool to the same database.
// Concurrency tests need one per simulated process.
func (d *DB) Open(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", d.DSN)
	if err != nil {
		t.Fatalf("pgtest: failed to open a second pool for %q: %v", d.Name, err)
	}
	t.Cleanup(func() { db.Close() }) //nolint:errcheck // test cleanup
	return db
}

// serverDSN resolves the server to use, or ends the test according to the
// require/skip policy described in the package comment.
func serverDSN(t *testing.T) string {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(DSNEnv))
	if dsn != "" {
		return dsn
	}
	if os.Getenv(RequireEnv) != "" {
		t.Fatalf(
			"pgtest: %s is set but %s is empty — PostgreSQL tests must run here, "+
				"a skip would report success without testing anything",
			RequireEnv, DSNEnv,
		)
	}
	t.Skipf(
		"pgtest: %s is not set; skipping the PostgreSQL test. "+
			"Run one with: docker run --rm -d -p 5432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17-alpine",
		DSNEnv,
	)
	return ""
}

func databaseName(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	b.WriteString("gocore_t_")
	for _, r := range strings.ToLower(t.Name()) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	name := b.String()
	// PostgreSQL truncates identifiers at 63 bytes; truncate the readable
	// part first so the uniqueness suffix always survives.
	const maxBase = 24
	if len(name) > maxBase {
		name = name[:maxBase]
	}
	return fmt.Sprintf("%s_%s_%d", name, runTag, dbCounter.Add(1))
}

func dropDatabase(adminDSN, name string) {
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return
	}
	defer admin.Close() //nolint:errcheck // best-effort cleanup

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// FORCE detaches sessions a failed test may have left behind; without it
	// one leaked connection blocks the drop and leaks the database instead.
	admin.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`) //nolint:errcheck // best-effort
}

// replaceDatabase swaps the database component of a PostgreSQL URL.
func replaceDatabase(dsn, name string) string {
	scheme, rest, found := strings.Cut(dsn, "://")
	if !found {
		return dsn
	}
	authority, tail, hasTail := strings.Cut(rest, "/")
	query := ""
	if hasTail {
		if _, q, hasQuery := strings.Cut(tail, "?"); hasQuery {
			query = "?" + q
		}
	}
	return scheme + "://" + authority + "/" + name + query
}
