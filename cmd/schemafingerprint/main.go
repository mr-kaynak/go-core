// Command schemafingerprint records what each core migration does to the
// catalog.
//
// Baseline converts an existing database onto a separated migration history,
// and to do that safely it has to establish that the schema really is at the
// version the old history claims. A history cannot answer that — it records
// that version N was applied, never what N did. These files are the answer:
// the catalog state each version is supposed to produce.
//
// Run it against a throwaway PostgreSQL server after adding a migration:
//
//	docker run --rm -d -p 5432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17-alpine
//	GOCORE_TEST_POSTGRES_DSN="postgres://postgres@localhost:5432/postgres?sslmode=disable" \
//	  go run ./cmd/schemafingerprint
//
// It creates a database of its own, migrates it one version at a time, and
// writes one file per migration. Commit them with the migration.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp"
)

const (
	dsnEnv              = "GOCORE_TEST_POSTGRES_DSN"
	schema              = "public"
	migrationSourceName = "core"
	buildDatabase       = "gocore_fingerprint_build"

	// dirMode and fileMode keep the generated evidence readable by its owner
	// and nobody else; it describes the shape of a production schema.
	dirMode  = 0o750
	fileMode = 0o600
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "schemafingerprint: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run from the repository root (go.mod not found): %w", err)
	}
	serverDSN := os.Getenv(dsnEnv)
	if serverDSN == "" {
		return fmt.Errorf(
			"%s is not set. Point it at a PostgreSQL server this command may create a database on;\n"+
				"see the command's documentation for a throwaway container", dsnEnv,
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	build, cleanup, err := freshDatabase(ctx, serverDSN)
	if err != nil {
		return err
	}
	defer cleanup()

	deltas, err := record(ctx, build, replaceDatabase(serverDSN, buildDatabase))
	if err != nil {
		return err
	}

	return write(deltas)
}

// freshDatabase builds the database the migrations are replayed into. It is
// dropped and recreated so a previous run cannot contribute state.
func freshDatabase(ctx context.Context, serverDSN string) (*sql.DB, func(), error) {
	admin, err := sql.Open("pgx", serverDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to reach the server: %w", err)
	}
	defer admin.Close()

	for _, statement := range []string{
		`DROP DATABASE IF EXISTS ` + buildDatabase + ` WITH (FORCE)`,
		`CREATE DATABASE ` + buildDatabase,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			return nil, nil, fmt.Errorf("failed to prepare the build database: %w", err)
		}
	}

	build, err := sql.Open("pgx", replaceDatabase(serverDSN, buildDatabase))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open the build database: %w", err)
	}

	cleanup := func() {
		build.Close()
		dropper, err := sql.Open("pgx", serverDSN)
		if err != nil {
			return
		}
		defer dropper.Close()
		dropper.ExecContext( //nolint:errcheck // best-effort
			context.Background(), `DROP DATABASE IF EXISTS `+buildDatabase+` WITH (FORCE)`)
	}
	return build, cleanup, nil
}

// record migrates one version at a time, taking a fingerprint after each, and
// keeps the difference.
//
// It steps through the runner rather than driving goose directly. That is not
// only about the import rule: the evidence baseline will trust should be
// produced by the same path that applies migrations in production, so a
// difference between the two would show up here rather than in a database
// somebody depends on.
func record(ctx context.Context, db *sql.DB, dsn string) ([]*schemafp.Delta, error) {
	runner, err := database.NewMigrationRunner(
		database.MigrationConfig{DSN: dsn, Schema: schema}, app.CoreMigrationSource())
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	var previous *schemafp.Snapshot
	var deltas []*schemafp.Delta

	for {
		report, reportErr := runner.Report(ctx)
		if reportErr != nil {
			return nil, reportErr
		}
		next, pending := nextPending(report)
		if !pending {
			break
		}

		if err := runner.UpOne(ctx, migrationSourceName); err != nil {
			return nil, fmt.Errorf("failed to apply version %d: %w", next, err)
		}

		current, err := schemafp.Extract(ctx, db, schema)
		if err != nil {
			return nil, fmt.Errorf("failed to fingerprint version %d: %w", next, err)
		}

		delta, err := schemafp.Diff(next, previous, current)
		if err != nil {
			return nil, fmt.Errorf("failed to diff version %d: %w", next, err)
		}
		if delta.Empty() {
			// Not an error: a data-only migration is legitimate. But it means
			// the fingerprint can attest to nothing about it, which is a limit
			// worth stating out loud rather than discovering later.
			fmt.Printf("  v%05d  no catalog change — this version cannot be verified by fingerprint\n", next)
		}

		deltas = append(deltas, delta)
		previous = current
	}

	return deltas, nil
}

func nextPending(report migrationstate.Report) (int64, bool) {
	for i := range report.Sources {
		if report.Sources[i].Source != migrationSourceName {
			continue
		}
		if len(report.Sources[i].Pending) == 0 {
			return 0, false
		}
		return report.Sources[i].Pending[0], true
	}
	return 0, false
}

func write(deltas []*schemafp.Delta) error {
	if err := os.MkdirAll(schemafp.RecordDir, dirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", schemafp.RecordDir, err)
	}

	// Remove files for versions that no longer exist, so a deleted migration
	// cannot leave evidence behind claiming it is still there.
	existing, err := filepath.Glob(filepath.Join(schemafp.RecordDir, "*.json"))
	if err != nil {
		return fmt.Errorf("failed to list existing fingerprints: %w", err)
	}
	wanted := make(map[string]struct{}, len(deltas))
	for _, delta := range deltas {
		wanted[filepath.Join(schemafp.RecordDir, schemafp.RecordName(delta.Version))] = struct{}{}
	}
	for _, path := range existing {
		if _, keep := wanted[path]; !keep {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to remove stale fingerprint %s: %w", path, err)
			}
			fmt.Printf("  removed %s (no such migration)\n", filepath.Base(path))
		}
	}

	for _, delta := range deltas {
		rendered, err := schemafp.Render(delta)
		if err != nil {
			return err
		}
		path := filepath.Join(schemafp.RecordDir, schemafp.RecordName(delta.Version))
		if err := os.WriteFile(path, rendered, fileMode); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	fmt.Printf("wrote %d fingerprint(s) to %s\n", len(deltas), schemafp.RecordDir)
	return nil
}

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
