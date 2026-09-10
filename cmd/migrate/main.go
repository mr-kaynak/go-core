// Package main is the migration CLI.
//
// It drives [database.MigrationRunner] rather than goose's process-global API,
// for two reasons that are not stylistic. The runner takes the migration
// advisory lock and runs the admission guard while holding it; a CLI calling
// global goose would be a second, uncoordinated writer against the same
// database — exactly what that lock exists to prevent. And goose's Status and
// Version initialize the version table as a side effect, so a `status` run
// against an unmigrated database would create a history table, and the absence
// of that table is precisely what classification reads to decide a database is
// still legacy. Diagnosing a database must never disarm the check that
// protects it, so the diagnostic commands here go through the runner's
// read-only Report.
//
// This package therefore does not import goose at all, which
// internal/test/boundary enforces: `create` writes its file directly rather
// than through goose.Create.
//
// The migrations come from the embedded filesystem, so the shipped binary can
// migrate and diagnose with no source tree present. Only `create` needs one.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for the migration pool
	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
)

// coreSourceDir is core's migrations in the source tree. It is reachable only
// from `create`, which writes a file: every other command reads the embedded
// filesystem, so a binary shipped without a checkout still works.
const coreSourceDir = "coremigrations/sql"

// migrationFileMode matches the permissions of the shipped migrations; these
// are source files, read by everyone and written by their author.
const migrationFileMode = 0o644

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(argv []string, out io.Writer) error {
	if len(argv) == 0 {
		printUsage(out)
		return errors.New("no command given; pick one of the commands above")
	}

	command, sourceFlag, args, err := parseArgs(argv)
	if err != nil {
		return err
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load the configuration: %w", err)
	}

	migrationCfg := migrationConfig(cfg)
	// sql.Open does not connect, so building the runner is free for the
	// commands that never reach the database.
	runner, err := database.NewMigrationRunner(migrationCfg, app.CoreMigrationSource())
	if err != nil {
		return err
	}
	// Closing the pool is what ends the sessions that may still hold the
	// advisory lock; returning a connection to the pool does not.
	defer runner.Close()

	source, err := resolveSource(runner, sourceFlag)
	if err != nil {
		return err
	}

	return dispatch(context.Background(), runner, command, source, args, migrationCfg.Tolerances, out)
}

// parseArgs takes the command first and flags after it, because every Makefile
// target and every operator already types it that way.
func parseArgs(argv []string) (command, source string, args []string, err error) {
	command = argv[0]

	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	sourceFlag := flags.String(
		"source", migrationsource.CoreName,
		"which migration history down, redo and reset act on",
	)
	if err := flags.Parse(argv[1:]); err != nil {
		return "", "", nil, err
	}
	return command, *sourceFlag, flags.Args(), nil
}

func dispatch(
	ctx context.Context,
	runner *database.MigrationRunner,
	command, source string,
	args []string,
	tol migrationstate.Tolerances,
	out io.Writer,
) error {
	switch command {
	case "up":
		return runner.Up(ctx)
	case "up-one":
		return upOne(ctx, runner, source, out)
	case "status":
		return printStatus(ctx, runner, tol, out)
	case "version":
		return printVersion(ctx, runner, out)
	case "down":
		return runner.DownOne(ctx, source)
	case "redo":
		return redo(ctx, runner, source, out)
	case "reset":
		return reset(ctx, runner, source, out)
	case "create":
		return createMigration(source, args, out)
	default:
		printUsage(out)
		return fmt.Errorf("unknown command %q; pick one of the commands above", command)
	}
}

func upOne(ctx context.Context, runner *database.MigrationRunner, source string, out io.Writer) error {
	// Nothing left to apply is the answer to the question, not a failure: a
	// deploy step that steps through migrations must not fail the deploy once
	// it reaches the end.
	if err := runner.UpOne(ctx, source); err != nil {
		if errors.Is(err, database.ErrNothingPending) {
			fmt.Fprintln(out, "Nothing to do: every migration is already applied.")
			return nil
		}
		return err
	}
	return nil
}

// redo rolls the newest migration of one source back and applies it again.
//
// Both halves are scoped to the same source: a re-apply that walked sources
// in registration order could apply a different source's pending migration
// than the one just rolled back.
func redo(ctx context.Context, runner *database.MigrationRunner, source string, out io.Writer) error {
	if err := runner.DownOne(ctx, source); err != nil {
		return err
	}
	return upOne(ctx, runner, source, out)
}

// reset rolls a source's history all the way back.
//
// The applied list is re-read between steps rather than counted once: each
// down is a separately locked operation, so a count taken up front is a claim
// about a database this process does not hold.
func reset(ctx context.Context, runner *database.MigrationRunner, source string, out io.Writer) error {
	// A bound so a down that reports success without shrinking the history
	// terminates instead of spinning forever.
	const maxSteps = 1000

	for step := 0; step < maxSteps; step++ {
		state, err := sourceState(ctx, runner, source)
		if err != nil {
			return err
		}
		if len(state.Applied) == 0 {
			fmt.Fprintf(out, "%s: history is empty.\n", source)
			return nil
		}
		if err := runner.DownOne(ctx, source); err != nil {
			return err
		}
	}
	return fmt.Errorf(
		"gave up rolling %q back after %d steps; its history is not shrinking, so roll it back by hand",
		source, maxSteps,
	)
}

// printStatus reports every history without writing to any of them.
func printStatus(
	ctx context.Context,
	runner *database.MigrationRunner,
	tol migrationstate.Tolerances,
	out io.Writer,
) error {
	report, err := runner.Report(ctx)
	if err != nil {
		return err
	}

	for i := range report.Sources {
		state := &report.Sources[i]
		fmt.Fprintf(out, "%s\n", state.Source)
		field(out, "history table", migrationsource.TableName(state.Source))
		field(out, "applied", appliedSummary(state.Applied))
		field(out, "pending", formatVersions(state.Pending))
		field(out, "state", string(state.State))
		if len(state.Unknown) > 0 {
			field(out, "not shipped here", formatVersions(state.Unknown))
		}
		if len(state.MissingBelowMax) > 0 {
			field(out, "missing from history", formatVersions(state.MissingBelowMax))
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "overall state: %s\n", report.State())

	// The refusal text names the command that fixes the database, which is the
	// part an operator reading status while something is wrong actually needs.
	if serveErr := report.Allows(migrationstate.OpServe, tol); serveErr != nil {
		fmt.Fprintf(out, "\n%v\n", serveErr)
	}
	return nil
}

func printVersion(ctx context.Context, runner *database.MigrationRunner, out io.Writer) error {
	report, err := runner.Report(ctx)
	if err != nil {
		return err
	}
	for i := range report.Sources {
		fmt.Fprintf(out, "%s %d\n", report.Sources[i].Source, highest(report.Sources[i].Applied))
	}
	return nil
}

// migrationTemplate is what a new migration file starts as.
//
// The shape is goose's: an Up section, a Down section, and nothing else the
// server would run. The sixteen shipped migrations and the inventory lock are
// all that shape, and goose parses the sections by these exact annotations.
//
// The transaction rules are stated in the file rather than only in the
// validator, because the file is where an author is when they need them — and
// a migration that breaks them is refused at source registration, long after
// it was written.
const migrationTemplate = `-- +goose Up

-- Keep this migration transactional: one commit per migration is the whole
-- reason an interrupted run leaves nothing behind. Validation refuses the
-- NO TRANSACTION and ENVSUB ON goose annotations, and refuses BEGIN, COMMIT,
-- ROLLBACK and SAVEPOINT here. Anything that genuinely cannot run inside a
-- transaction (CREATE INDEX CONCURRENTLY, ALTER TYPE ... ADD VALUE) is an
-- operational step outside the migration history, not a migration.
SELECT 'up SQL query';

-- +goose Down

-- Write the rollback even though rolling back is development-only: a down
-- that drops a column destroys the data in it, so the value of writing one is
-- that it makes the Up section reviewable.
SELECT 'down SQL query';
`

func createMigration(source string, args []string, out io.Writer) error {
	if source != migrationsource.CoreName {
		return fmt.Errorf(
			"create writes into core's migration directory only; source %q owns its own source tree, "+
				"so add the file there", source,
		)
	}
	if len(args) == 0 {
		return errors.New("create needs a name: migrate create <name>")
	}

	name := snakeCase(args[0])
	if name == "" {
		return fmt.Errorf("name %q has no letters or digits to build a filename from", args[0])
	}

	info, err := os.Stat(coreSourceDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf(
			"migration directory %q is not here; create needs a go-core checkout, because the "+
				"shipped binary carries the migrations embedded and has no source tree. "+
				"Run it from the repository root",
			coreSourceDir,
		)
	}

	version, err := nextVersion(coreSourceDir)
	if err != nil {
		return err
	}

	path := filepath.Join(coreSourceDir, fmt.Sprintf("%05d_%s.sql", version, name))
	if err := writeNewFile(path, migrationTemplate); err != nil {
		return err
	}

	fmt.Fprintf(out, "Created %s\n", path)
	fmt.Fprintln(out, "Fill in both sections, then regenerate the lock: go run ./cmd/inventorylock")
	return nil
}

// writeNewFile refuses to overwrite. O_EXCL rather than a stat first: two
// authors creating a migration at the same moment must collide loudly instead
// of one of them silently losing their file.
func writeNewFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, migrationFileMode)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	if _, err := file.WriteString(content); err != nil {
		// The write error is the one worth reporting; the close is cleanup.
		file.Close()
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", path, err)
	}
	return nil
}

// nextVersion is one past the highest version already in the directory.
//
// Sequential rather than goose's default timestamp, because core's history has
// to run 1..N with no gaps — migrationsource.ValidateCore enforces it, and
// baseline expresses a conversion as a prefix of that range. A timestamp
// version would leave a gap of several trillion and make the source invalid
// the moment it was written.
func nextVersion(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("failed to read %s: %w", dir, err)
	}

	var highest int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version, verErr := parseVersion(entry.Name())
		if verErr != nil {
			// Skipping it would number the new file over an existing version.
			return 0, verErr
		}
		if version > highest {
			highest = version
		}
	}
	return highest + 1, nil
}

// parseVersion reads the version prefix by goose's rule — everything before
// the first underscore, and it must be above zero — so a name this command
// builds is one goose will later agree with.
func parseVersion(name string) (int64, error) {
	prefix, _, found := strings.Cut(name, "_")
	if !found {
		return 0, fmt.Errorf(
			"migration %q has no underscore separating its version from its name; "+
				"rename it to NNNNN_name.sql", name,
		)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version < 1 {
		return 0, fmt.Errorf(
			"migration %q does not start with a version number above zero; "+
				"rename it to NNNNN_name.sql", name,
		)
	}
	return version, nil
}

// snakeCase reduces a name to the lowercase, underscore-separated form the
// shipped migrations use. The filename carries a version prefix goose parses
// at the first underscore, so anything else in it is noise at best.
func snakeCase(name string) string {
	var b strings.Builder
	separate := false
	previousWasLower := false

	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			separate = b.Len() > 0
			previousWasLower = false
			continue
		}
		if unicode.IsUpper(r) && previousWasLower {
			separate = true
		}
		if separate {
			b.WriteByte('_')
			separate = false
		}
		b.WriteRune(unicode.ToLower(r))
		previousWasLower = !unicode.IsUpper(r)
	}
	return b.String()
}

func resolveSource(runner *database.MigrationRunner, name string) (string, error) {
	registered := runner.Sources()
	for _, candidate := range registered {
		if candidate == name {
			return name, nil
		}
	}
	return "", fmt.Errorf(
		"unknown migration source %q; this binary registers: %s",
		name, strings.Join(registered, ", "),
	)
}

func sourceState(
	ctx context.Context,
	runner *database.MigrationRunner,
	source string,
) (migrationstate.SourceState, error) {
	report, err := runner.Report(ctx)
	if err != nil {
		return migrationstate.SourceState{}, err
	}
	for i := range report.Sources {
		if report.Sources[i].Source == source {
			return report.Sources[i], nil
		}
	}
	return migrationstate.SourceState{}, fmt.Errorf(
		"migration source %q is missing from the classification report", source,
	)
}

// migrationConfig mirrors app's own, which is unexported. A CLI that took the
// lock with different bounds than the application would be a second policy for
// the same database.
func migrationConfig(cfg *config.Config) database.MigrationConfig {
	return database.MigrationConfig{
		DSN:              cfg.GetDSN(),
		LockWait:         cfg.Database.MigrationLockWait,
		LockTimeout:      cfg.Database.MigrationLockTimeout,
		StatementTimeout: cfg.Database.MigrationStatementTimeout,
		Tolerances: migrationstate.Tolerances{
			UnknownAppliedVersions: cfg.Database.AllowUnknownAppliedVersions,
			PendingMigrations:      cfg.Database.AllowPendingMigrations,
		},
		Logger: logger.Get().Logger,
	}
}

func field(out io.Writer, label, value string) {
	fmt.Fprintf(out, "  %-22s %s\n", label, value)
}

func appliedSummary(applied []int64) string {
	if len(applied) == 0 {
		return "none"
	}
	return fmt.Sprintf("%d, highest %d", len(applied), highest(applied))
}

// highest is the version a source is at. Zero means nothing has been applied,
// which is also what goose's own version command prints for an empty history.
func highest(applied []int64) int64 {
	var top int64
	for _, version := range applied {
		if version > top {
			top = version
		}
	}
	return top
}

// formatVersions collapses a contiguous run into a range, so a fresh
// database's pending list reads as "1-16" rather than as a wall of numbers.
// The report's version lists are already ascending.
func formatVersions(versions []int64) string {
	if len(versions) == 0 {
		return "none"
	}
	last := len(versions) - 1
	if last > 0 && versions[last]-versions[0] == int64(last) {
		return fmt.Sprintf("%d-%d", versions[0], versions[last])
	}

	parts := make([]string, 0, len(versions))
	for _, version := range versions {
		parts = append(parts, strconv.FormatInt(version, 10))
	}
	return strings.Join(parts, ", ")
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: migrate <command> [--source <name>] [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  up        Apply every pending migration")
	fmt.Fprintln(out, "  up-one    Apply the next pending migration")
	fmt.Fprintln(out, "  status    Report every history and what it permits; changes nothing")
	fmt.Fprintln(out, "  version   Print the highest applied version of every history")
	fmt.Fprintln(out, "  create    Write a new migration file (needs a name and a source checkout)")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Development only. A down migration that drops a column destroys the data in it,")
	fmt.Fprintln(out, "and no lock makes that recoverable; production recovers by migrating forward.")
	fmt.Fprintln(out, "  down      Roll back the newest migration of one history")
	fmt.Fprintln(out, "  redo      Roll back the newest migration and apply it again")
	fmt.Fprintln(out, "  reset     Roll back every migration of one history")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Flags:")
	fmt.Fprintln(out, "  --source <name>   Which history down, redo and reset act on (default \"core\")")
}
