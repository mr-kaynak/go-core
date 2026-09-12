package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/examples/startup/projects"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

func TestStartupExamplePostgresMigrations(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	migrator, err := app.NewMigrator(app.MigratorConfig{DSN: db.DSN}, projects.New().MigrationSource())
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	if err := migrator.CheckServing(ctx); err == nil {
		t.Fatal("fresh database should refuse serving")
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if err := migrator.CheckServing(ctx); err != nil {
		t.Fatalf("migrated database must allow serving: %v", err)
	}
	var version int64
	if err := db.QueryRowContext(ctx,
		"SELECT max(version_id) FROM startup_projects_schema_versions WHERE is_applied").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("example version %d, want 1", version)
	}
	var coreTable string
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('core_schema_versions')::text").Scan(&coreTable); err != nil {
		t.Fatal(err)
	}
	// A missing owner must be rejected by the real PostgreSQL foreign key.
	_, err = db.ExecContext(ctx,
		"INSERT INTO startup_projects(id,name,owner_id,created_at) VALUES ($1,'orphan',$2,now())", uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "23503") {
		t.Fatalf("expected FK rejection, got %v", err)
	}
	// The SQL check, not GORM AutoMigrate, enforces nonblank names.
	_, err = db.ExecContext(ctx,
		"INSERT INTO startup_projects(id,name,owner_id,created_at) VALUES ($1,' ',$2,now())", uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "23514") {
		t.Fatalf("expected check rejection, got %v", err)
	}
}

func TestPublicMigratorRefusesLegacyBeforeWriting(t *testing.T) {
	for _, operation := range []string{"up", "up-one"} {
		t.Run(operation, func(t *testing.T) {
			db := pgtest.New(t)
			ctx := context.Background()
			_, err := db.ExecContext(ctx, `CREATE TABLE goose_db_version (
 id serial PRIMARY KEY, version_id bigint NOT NULL,
 is_applied boolean NOT NULL, tstamp timestamp DEFAULT now());
INSERT INTO goose_db_version(version_id,is_applied) VALUES (0,true),(1,true);`)
			if err != nil {
				t.Fatal(err)
			}
			m, err := app.NewMigrator(app.MigratorConfig{DSN: db.DSN}, projects.New().MigrationSource())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if operation == "up" {
				err = m.Up(ctx)
			} else {
				err = m.UpOne(ctx, "core")
			}
			if err == nil {
				t.Fatal("legacy history accepted")
			}
			var count int
			if err := db.QueryRowContext(ctx,
				"SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename <> 'goose_db_version'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("legacy refusal created %d tables", count)
			}
		})
	}
}
