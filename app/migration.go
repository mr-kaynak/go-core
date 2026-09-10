package app

import (
	"context"
	"fmt"

	"github.com/mr-kaynak/go-core/coremigrations"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
)

// MigrationSource is one owner of schema changes: a name and its SQL files.
// A consumer registers its own through [WithMigrations] or by having a module
// implement [MigrationProvider].
type MigrationSource = modcontract.MigrationSource

// MigrationProvider is the optional interface a [Module] implements to ship
// its own migrations. Modules that do not implement it simply have none.
type MigrationProvider = modcontract.MigrationProvider

// WithMigrations registers application-level migration sources — schema the
// application owns directly rather than through a module.
//
// Each source gets a history table of its own, independent of core's, so
// upgrading core never has to reconcile version numbers with an application's
// own migrations.
func WithMigrations(sources ...MigrationSource) Option {
	return func(o *options) {
		o.migrations = append(o.migrations, sources...)
	}
}

// CoreMigrationSource is core's own schema, under the reserved name.
func CoreMigrationSource() MigrationSource {
	return MigrationSource{Name: migrationsource.CoreName, FS: coremigrations.FS()}
}

// allMigrationSources is every consumer-owned source: the ones registered
// directly, then the ones contributed by modules, in registration order.
//
// Core is not among them; the runner takes it separately so it always runs
// first and cannot be displaced by registration order.
func allMigrationSources(o *options) []MigrationSource {
	sources := append([]MigrationSource(nil), o.migrations...)
	for _, module := range o.modules {
		provider, ok := module.(MigrationProvider)
		if !ok {
			continue
		}
		sources = append(sources, provider.MigrationSource())
	}
	return sources
}

// PrepareSchema brings the database to a state the application may serve
// from, or refuses to start.
//
// The sequence is preflight, migrate, re-check — not just "migrate". The
// preflight is what refuses a legacy or orphaned database before any DDL is
// attempted against it, and the re-check is what confirms the migration
// actually reached a serveable state rather than assuming it did.
//
// Both checks run whether or not automatic migration is enabled. The
// recommended production setting turns it off, and a refusal reachable only
// through the migration path would then never run at all.
//
// It is exported because an application with its own entry point — a gRPC
// server, a dedicated migration job — needs the same sequence, and a second
// implementation of it would be a second place for the ordering to be wrong.
func PrepareSchema(ctx context.Context, cfg *Config, sources ...MigrationSource) error {
	runner, err := database.NewMigrationRunner(
		migrationConfig(cfg),
		CoreMigrationSource(),
		sources...,
	)
	if err != nil {
		return err
	}
	defer runner.Close()

	log := logger.Get()

	if cfg.Database.AutoMigrate {
		if err := runner.CheckAdmission(ctx, migrationstate.OpMigrate); err != nil {
			return err
		}
		if err := runner.Up(ctx); err != nil {
			return err
		}
	} else {
		log.Info("Automatic migration is disabled; verifying the schema is already current",
			"setting", "DB_AUTO_MIGRATE=false")
	}

	if err := runner.CheckAdmission(ctx, migrationstate.OpServe); err != nil {
		return fmt.Errorf("this build cannot serve traffic against this database: %w", err)
	}
	return nil
}

func migrationConfig(cfg *Config) database.MigrationConfig {
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
