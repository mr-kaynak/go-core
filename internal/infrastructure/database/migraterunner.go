package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx for the database-only public migrator.
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
	"github.com/pressly/goose/v3"
	goosedb "github.com/pressly/goose/v3/database"
)

// gooseStore builds the history-table accessor goose itself uses, so the
// rows this package writes are indistinguishable from the ones goose writes.
func gooseStore(table string) (goosedb.Store, error) {
	store, err := goosedb.NewStore(goosedb.DialectPostgres, table)
	if err != nil {
		return nil, fmt.Errorf("failed to build the history store for %q: %w", table, err)
	}
	return store, nil
}

func gooseInsertRequest(version int64) goosedb.InsertRequest {
	return goosedb.InsertRequest{Version: version}
}

// MigrationConfig is everything a migration run needs and nothing else.
//
// It is separate from the application configuration on purpose: applying
// migrations should not require a JWT secret or an SMTP host, and a migration
// job that demands them is a job that cannot be run with least privilege.
type MigrationConfig struct {
	// DSN points at the database to migrate.
	DSN string
	// Schema is the schema migrations own. Defaults to "public".
	Schema string
	// LockWait bounds how long to wait for the migration lock.
	LockWait time.Duration
	// LockTimeout bounds how long a statement waits for a database lock, so
	// a migration blocked behind application traffic fails instead of
	// blocking that traffic indefinitely.
	LockTimeout time.Duration
	// StatementTimeout bounds how long any single statement may run.
	StatementTimeout time.Duration
	// Tolerances relax classification and admission for documented
	// procedures. Empty means the strict defaults.
	Tolerances migrationstate.Tolerances
	// Logger receives progress. Optional.
	Logger *slog.Logger
}

func (c *MigrationConfig) withDefaults() {
	if c.Schema == "" {
		c.Schema = "public"
	}
	if c.LockWait <= 0 {
		c.LockWait = 2 * time.Minute
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// preparedSource is a validated source plus what the runner needs to reason
// about it without reading the filesystem again.
type preparedSource struct {
	name     string
	table    string
	fsys     fs.FS
	versions []int64
}

// MigrationRunner applies migration sources to one database.
//
// Each source owns a history table of its own, so a consumer's migrations and
// core's advance independently and cannot collide on version numbers.
type MigrationRunner struct {
	pool     *sql.DB
	cfg      MigrationConfig
	sources  []preparedSource
	coreName string
}

// NewMigrationRunner validates the sources and opens a pool of its own.
//
// The pool is separate from the application's for two reasons. The
// application's may be configured down to a single connection, and a
// migration needs one for its own work while goose holds another — a
// deadlock that would only appear in someone else's configuration. And
// closing this pool at the end of the run closes its sessions outright, which
// is what actually releases a session-scoped advisory lock; returning a
// connection to a pool does not.
func NewMigrationRunner(
	cfg MigrationConfig,
	coreSource modcontract.MigrationSource,
	extra ...modcontract.MigrationSource,
) (*MigrationRunner, error) {
	cfg.withDefaults()

	if err := migrationsource.ValidateCore(coreSource); err != nil {
		return nil, err
	}
	if err := migrationsource.Validate(extra); err != nil {
		return nil, err
	}
	for _, source := range extra {
		if source.Name == coreSource.Name {
			return nil, fmt.Errorf(
				"migration source %q collides with the core source name", source.Name,
			)
		}
	}

	all := append([]modcontract.MigrationSource{coreSource}, extra...)
	prepared := make([]preparedSource, 0, len(all))
	for _, source := range all {
		versions, err := inventoryVersions(source.FS)
		if err != nil {
			return nil, fmt.Errorf("migration source %q: %w", source.Name, err)
		}
		prepared = append(prepared, preparedSource{
			name:     source.Name,
			table:    migrationsource.QualifiedTableName(cfg.Schema, source.Name),
			fsys:     source.FS,
			versions: versions,
		})
	}

	pool, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open the migration connection pool: %w", err)
	}
	// One connection for goose's work and one for anything reading alongside
	// it. More would only add ways to hold the lock on the wrong session.
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(2)

	return &MigrationRunner{
		pool:     pool,
		cfg:      cfg,
		sources:  prepared,
		coreName: coreSource.Name,
	}, nil
}

// Close releases the runner's pool. Closing it is what ends the sessions that
// may still hold an advisory lock, so callers must not skip it.
func (r *MigrationRunner) Close() error {
	return r.pool.Close()
}

// Up applies every pending migration, core first, then each consumer source
// in registration order.
//
// The order is a contract: a consumer's migrations may depend on core objects,
// so core is always further along than anything built on it. Dependencies
// between two consumer sources are not supported and cannot be detected here.
func (r *MigrationRunner) Up(ctx context.Context) error {
	for _, source := range r.sources {
		if err := r.upSource(ctx, source); err != nil {
			return fmt.Errorf("migration source %q: %w", source.name, err)
		}
	}
	return nil
}

// errStaleTarget means another runner advanced the history between planning
// and applying. Re-planning resolves it; it is not a failure.
var errStaleTarget = errors.New("another runner advanced this history")

func (r *MigrationRunner) upSource(ctx context.Context, source preparedSource) error {
	// The bound counts lost races only. Counting successful applies too would
	// make a source with more migrations than the bound fail partway through
	// with a concurrency error, having committed every one of them.
	staleRetries := 0

	for staleRetries < maxStaleRetries {
		pending, err := r.plan(ctx, source)
		if err != nil {
			return err
		}

		if len(pending) == 0 {
			// The plan above is an unlocked read, so "nothing pending" is not
			// yet a reason to stop: another runner may have advanced core
			// while this one was working, and this source's own emptiness
			// would hide that. The completion check re-reads under the lock.
			switch err := r.confirmComplete(ctx, source); {
			case errors.Is(err, errStaleTarget):
				continue
			case err != nil:
				return err
			default:
				return nil
			}
		}

		switch err := r.applyOne(ctx, source, pending[0]); {
		case errors.Is(err, errStaleTarget):
			staleRetries++
			continue
		case err != nil:
			return err
		}
	}

	return errTooManyRaces
}

// maxStaleRetries bounds how many times a runner will replan after losing a
// race. Losing that many in a row means something is wrong rather than busy.
const maxStaleRetries = 100

var errTooManyRaces = fmt.Errorf(
	"gave up after losing %d races; another runner keeps advancing this history",
	maxStaleRetries,
)

// plan reads which versions remain, without goose and without the lock.
//
// Reading it ourselves matters: goose's Up and UpByOne call HasPending first,
// which initializes the history table on an unlocked connection — creating
// the very table classification needs to find absent, and doing it outside
// the lock. This read creates nothing. It is racy by construction, which is
// why nothing is decided on it: the guard re-checks under the lock.
func (r *MigrationRunner) plan(ctx context.Context, source preparedSource) ([]int64, error) {
	inventories := r.inventories()
	report, err := migrationstate.Classify(ctx, r.pool, inventories, r.coreName, r.cfg.Schema, r.cfg.Tolerances)
	if err != nil {
		return nil, err
	}
	if err := report.Allows(migrationstate.OpMigrate, r.cfg.Tolerances); err != nil {
		return nil, err
	}

	for i := range report.Sources {
		if report.Sources[i].Source == source.name {
			return report.Sources[i].Pending, nil
		}
	}
	return nil, fmt.Errorf("source %q is missing from the classification report", source.name)
}

// confirmComplete re-checks under the lock before reporting success.
//
// Without it a runner can finish while being wrong: it passes preflight,
// applies core through 16, another runner applies core 17, and this one then
// finds its own source already current and returns success — never having
// noticed it is now running against a core schema it does not know. The
// unlocked plan cannot catch that; only a locked re-read can.
func (r *MigrationRunner) confirmComplete(ctx context.Context, source preparedSource) error {
	return withBoundedReadLock(ctx, r.pool, r.cfg.LockWait, r.cfg.StatementTimeout,
		func(ctx context.Context, tx *sql.Tx) error {
			report, err := migrationstate.Classify(ctx, tx, r.inventories(), r.coreName, r.cfg.Schema, r.cfg.Tolerances)
			if err != nil {
				return err
			}
			if err := report.Allows(migrationstate.OpMigrate, r.cfg.Tolerances); err != nil {
				return err
			}
			for i := range report.Sources {
				if report.Sources[i].Source == source.name && len(report.Sources[i].Pending) > 0 {
					return errStaleTarget
				}
			}
			return nil
		})
}

func (r *MigrationRunner) applyOne(ctx context.Context, source preparedSource, version int64) error {
	locker, err := newGuardedLocker(r.cfg.LockWait, func(ctx context.Context, conn *sql.Conn) error {
		return r.guard(ctx, conn, source, version)
	})
	if err != nil {
		return err
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres, r.pool, source.fsys,
		goose.WithTableName(source.table),
		goose.WithDisableGlobalRegistry(true),
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("failed to build the migration provider: %w", err)
	}

	r.cfg.Logger.Info("applying migration",
		"source", source.name, "version", version, "history", source.table)

	// ApplyVersion rather than Up or UpByOne: those consult HasPending on an
	// unlocked connection first, which both creates history metadata outside
	// the lock and skips the guard entirely when nothing is pending.
	if _, err := provider.ApplyVersion(ctx, version, true); err != nil {
		if errors.Is(err, errStaleTarget) {
			return errStaleTarget
		}
		return fmt.Errorf("failed to apply version %d: %w", version, err)
	}
	return nil
}

// guard runs on the locked connection, before goose creates or reads
// anything. Everything it checks has to be checked here rather than earlier,
// because earlier is unlocked and therefore already stale.
func (r *MigrationRunner) guard(
	ctx context.Context,
	conn *sql.Conn,
	source preparedSource,
	version int64,
) error {
	if err := r.applyConnectionLimits(ctx, conn); err != nil {
		return err
	}

	// Classification covers every source, not just this one, which is what
	// fences generations: a runner that applied core through 16 is stopped
	// here when it comes back for its own source and finds core at 17, a
	// version it does not ship.
	report, err := migrationstate.Classify(ctx, conn, r.inventories(), r.coreName, r.cfg.Schema, r.cfg.Tolerances)
	if err != nil {
		return err
	}
	if err := report.Allows(migrationstate.OpMigrate, r.cfg.Tolerances); err != nil {
		return err
	}

	for i := range report.Sources {
		if report.Sources[i].Source != source.name {
			continue
		}
		if pending := report.Sources[i].Pending; len(pending) == 0 || pending[0] != version {
			return fmt.Errorf("%w: version %d is no longer the next pending one", errStaleTarget, version)
		}
	}

	return r.ensureSentinel(ctx, conn, source)
}

// applyConnectionLimits bounds the migration connection itself.
//
// Session scope, not transaction scope: this runs on the connection before
// goose opens its transaction, and PostgreSQL ignores SET LOCAL there — the
// timeouts would silently never apply while the run held the migration lock.
func (r *MigrationRunner) applyConnectionLimits(ctx context.Context, conn *sql.Conn) error {
	if err := setTimeout(ctx, conn, "statement_timeout", r.cfg.StatementTimeout, scopeSession); err != nil {
		return err
	}
	return setTimeout(ctx, conn, "lock_timeout", r.cfg.LockTimeout, scopeSession)
}

// ensureSentinel creates the history table and its version-0 row when they
// are missing.
//
// goose skips its own initialization when the table already exists, and then
// rejects a history with no rows at all. A table left behind empty — by an
// interrupted run, or by a tool that created it and stopped — would therefore
// be permanently unusable. Writing the sentinel here, under the lock, on the
// connection goose is about to use, is what makes an empty history recoverable
// rather than fatal.
func (r *MigrationRunner) ensureSentinel(ctx context.Context, conn *sql.Conn, source preparedSource) error {
	store, err := gooseStore(source.table)
	if err != nil {
		return err
	}

	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, source.table,
	).Scan(&exists); err != nil {
		return fmt.Errorf("failed to look up history table %q: %w", source.table, err)
	}
	if !exists {
		if err := store.CreateVersionTable(ctx, conn); err != nil {
			return fmt.Errorf("failed to create history table %q: %w", source.table, err)
		}
	}

	var hasRows bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+source.table+`)`,
	).Scan(&hasRows); err != nil {
		return fmt.Errorf("failed to read history table %q: %w", source.table, err)
	}
	if hasRows {
		return nil
	}

	if err := store.Insert(ctx, conn, gooseInsertRequest(0)); err != nil {
		return fmt.Errorf("failed to initialize history table %q: %w", source.table, err)
	}
	return nil
}

func (r *MigrationRunner) inventories() []migrationstate.Inventory {
	out := make([]migrationstate.Inventory, 0, len(r.sources))
	for _, source := range r.sources {
		out = append(out, migrationstate.Inventory{
			Source:   source.name,
			Table:    source.table,
			Versions: source.versions,
		})
	}
	return out
}

// UpOne applies the single next pending migration of one source. It exists
// for stepping through a migration by hand, and takes the same lock and runs
// the same guard as a full run.
//
// It is scoped to a source rather than "whatever is next overall" because the
// caller that wants one step — rolling a source back and re-applying it —
// means that source's step. Walking sources in registration order instead
// would quietly re-apply core's pending migration after a consumer rollback.
func (r *MigrationRunner) UpOne(ctx context.Context, sourceName string) error {
	source, err := r.source(sourceName)
	if err != nil {
		return err
	}

	// Same shape as a full run, for the same reason: an unlocked plan is not
	// proof of anything, so "nothing pending" goes through the locked
	// completion check, and losing a race is replanned rather than reported.
	for round := 0; round < maxStaleRetries; round++ {
		pending, err := r.plan(ctx, source)
		if err != nil {
			return fmt.Errorf("migration source %q: %w", source.name, err)
		}

		if len(pending) == 0 {
			switch err := r.confirmComplete(ctx, source); {
			case errors.Is(err, errStaleTarget):
				continue
			case err != nil:
				return fmt.Errorf("migration source %q: %w", source.name, err)
			default:
				return ErrNothingPending
			}
		}

		switch err := r.applyOne(ctx, source, pending[0]); {
		case errors.Is(err, errStaleTarget):
			continue
		case err != nil:
			return fmt.Errorf("migration source %q: %w", source.name, err)
		default:
			return nil
		}
	}
	return errTooManyRaces
}

// ErrNothingPending reports that there was no migration left to apply. It is
// an outcome rather than a failure; the CLI reports it and exits zero.
var ErrNothingPending = errors.New("no pending migrations")

// DownOne rolls back the newest applied migration of one source.
//
// Rolling back is a development convenience and is not part of any supported
// production procedure: a down migration that drops a column destroys the
// data in it, and no lock makes that recoverable. Production recovers by
// migrating forward, or from a backup.
func (r *MigrationRunner) DownOne(ctx context.Context, sourceName string) error {
	source, err := r.source(sourceName)
	if err != nil {
		return err
	}

	// Rolling back writes, and destructively: it runs the down SQL. It
	// therefore passes the same admission check as a forward migration.
	// Without it, a rollback against a legacy history would create separated
	// metadata and report success, and one against an ambiguous history would
	// execute destructive SQL that admission exists to refuse.
	locker, lockErr := newGuardedLocker(r.cfg.LockWait, func(ctx context.Context, conn *sql.Conn) error {
		if err := r.applyConnectionLimits(ctx, conn); err != nil {
			return err
		}
		report, err := migrationstate.Classify(ctx, conn, r.inventories(), r.coreName, r.cfg.Schema, r.cfg.Tolerances)
		if err != nil {
			return err
		}
		return report.Allows(migrationstate.OpMigrate, r.cfg.Tolerances)
	})
	if lockErr != nil {
		return lockErr
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres, r.pool, source.fsys,
		goose.WithTableName(source.table),
		goose.WithDisableGlobalRegistry(true),
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("failed to build the migration provider: %w", err)
	}

	if _, err := provider.Down(ctx); err != nil {
		return fmt.Errorf("failed to roll back the newest migration of %q: %w", sourceName, err)
	}
	return nil
}

// Sources are the registered source names, core first.
func (r *MigrationRunner) Sources() []string {
	names := make([]string, 0, len(r.sources))
	for _, source := range r.sources {
		names = append(names, source.name)
	}
	return names
}

func (r *MigrationRunner) source(name string) (preparedSource, error) {
	for _, source := range r.sources {
		if source.name == name {
			return source, nil
		}
	}
	return preparedSource{}, fmt.Errorf(
		"unknown migration source %q; registered sources are %v", name, r.Sources(),
	)
}

// CheckAdmission verifies the database permits op, reading under the
// migration lock so the answer is not already stale when it is returned.
//
// It runs regardless of whether migrations are enabled. That is the point:
// the recommended production setting turns automatic migration off, and a
// refusal that lives only inside the migration path would then never run —
// the application would start against a legacy or orphaned schema and reach
// its first write with nothing having objected. The check is read-only and
// performs no DDL.
func (r *MigrationRunner) CheckAdmission(ctx context.Context, op migrationstate.Operation) error {
	return withBoundedReadLock(ctx, r.pool, r.cfg.LockWait, r.cfg.StatementTimeout,
		func(ctx context.Context, tx *sql.Tx) error {
			report, err := migrationstate.Classify(ctx, tx, r.inventories(), r.coreName, r.cfg.Schema, r.cfg.Tolerances)
			if err != nil {
				return err
			}
			return report.Allows(op, r.cfg.Tolerances)
		})
}

// Explain reports why op is refused, or nil when it is permitted.
//
// It exists so a caller does not have to carry the tolerances alongside the
// runner in order to interpret a Report — two copies of that state are two
// chances for the runner and its caller to disagree about the same database.
func (r *MigrationRunner) Explain(ctx context.Context, op migrationstate.Operation) error {
	report, err := r.Report(ctx)
	if err != nil {
		return err
	}
	return report.Allows(op, r.cfg.Tolerances)
}

// Report classifies the database without changing it. It is the read behind
// the status commands, which must never create the metadata they report on.
func (r *MigrationRunner) Report(ctx context.Context) (migrationstate.Report, error) {
	return migrationstate.Classify(ctx, r.pool, r.inventories(), r.coreName, r.cfg.Schema, r.cfg.Tolerances)
}

func inventoryVersions(fsys fs.FS) ([]int64, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("failed to list migrations: %w", err)
	}

	versions := make([]int64, 0, len(names))
	for _, name := range names {
		version, verErr := goose.NumericComponent(name)
		if verErr != nil {
			return nil, fmt.Errorf("file %q has no version number: %w", name, verErr)
		}
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions, nil
}
