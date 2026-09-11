package app

import (
	"context"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
)

// Migrator applies migrations without starting an application.
//
// It exists because migrating and serving are different jobs with different
// privileges and different failure modes. The recommended production setup
// runs migrations as their own step — so that step needs a way in that does
// not require a JWT secret, an SMTP host, or a Redis connection merely to run
// SQL.
//
// A consumer builds one over the same sources it registers with [New], so the
// job and the application agree about what schema exists.
type Migrator struct {
	runner *database.MigrationRunner
}

// MigratorConfig is what a migration run needs, and nothing else.
type MigratorConfig = database.MigrationConfig

// BaselinePlan states which versions of a legacy history belong to which
// source. See [Migrator.Baseline].
type BaselinePlan = database.BaselinePlan

// BaselineOutcome is what a baseline did, or would do.
type BaselineOutcome = database.BaselineOutcome

// MigrationReport is the read-only classification of a database's migration
// metadata.
type MigrationReport = migrationstate.Report

// NewMigrator builds a migrator over core's schema plus the given consumer
// sources.
//
// Core is always included and always runs first; a consumer passes only its
// own. Close it when done — that is what releases the advisory lock a run
// may still hold.
func NewMigrator(cfg MigratorConfig, sources ...MigrationSource) (*Migrator, error) {
	if cfg.Logger == nil {
		cfg.Logger = logger.Get().Logger
	}

	runner, err := database.NewMigrationRunner(cfg, CoreMigrationSource(), sources...)
	if err != nil {
		return nil, err
	}
	return &Migrator{runner: runner}, nil
}

// Up applies every pending migration, core first, then each consumer source
// in registration order.
func (m *Migrator) Up(ctx context.Context) error {
	return m.runner.Up(ctx)
}

// UpOne applies the single next pending migration of one source, for stepping
// through a migration by hand. It reports [ErrNothingPending] when the source
// is already current.
func (m *Migrator) UpOne(ctx context.Context, source string) error {
	return m.runner.UpOne(ctx, source)
}

// ErrNothingPending reports that there was no migration left to apply. It is
// an outcome rather than a failure.
var ErrNothingPending = database.ErrNothingPending

// Report classifies the database without changing it.
//
// It creates nothing — not even the history tables it reports on — so it is
// safe to run against a database that has never been migrated, which is
// exactly when an operator most wants to look.
func (m *Migrator) Report(ctx context.Context) (MigrationReport, error) {
	return m.runner.Report(ctx)
}

// Baseline converts a legacy single migration history into separated ones.
//
// It writes only metadata, and refuses unless the schema matches what the
// claimed version is recorded to produce. Pass dryRun to run every check and
// report the outcome without writing; that is what an operator should do
// first, because the result is a claim about their database that everything
// afterwards depends on.
func (m *Migrator) Baseline(ctx context.Context, plan BaselinePlan, dryRun bool) (*BaselineOutcome, error) {
	return m.runner.Baseline(ctx, plan, dryRun)
}

// Sources are the registered source names, core first.
func (m *Migrator) Sources() []string {
	return m.runner.Sources()
}

// Close releases the migrator's connections.
func (m *Migrator) Close() error {
	return m.runner.Close()
}

// MigratorConfigFromApp builds a migrator configuration from an application
// configuration, so a consumer with one already loaded need not restate it.
func MigratorConfigFromApp(cfg *Config) MigratorConfig {
	return migrationConfig(cfg)
}

// LoadMigratorConfig reads what a migration run needs from the environment —
// and only that.
//
// Use it in a migration job. [LoadConfig] validates the whole application
// configuration, so a job that exists to run SQL would refuse to start
// without a JWT secret, an SMTP host and an encryption key. Supplying them to
// satisfy the validator is worse than the inconvenience it avoids: it puts
// the application's full credential set in the one place that should hold
// nothing but a database role with DDL rights.
//
// It reads the same variables, defaults and config file [LoadConfig] reads,
// so the two agree about every setting they share — including the lock and
// statement bounds, which must not differ between the job and the application
// waiting on it.
func LoadMigratorConfig() (MigratorConfig, error) {
	settings, err := config.LoadMigration()
	if err != nil {
		return MigratorConfig{}, err
	}
	return migrationConfigFrom(settings.GetDSN(), settings.Database), nil
}
