package app

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// The refusal has to hold on the real path, not only in the runner's own
// tests: app.New reaches it through PrepareSchema, and what matters to an
// operator is that a database it must not touch is left exactly as it was.
//
// It runs with automatic migration both on and off. Off is the recommended
// production setting and the one where the refusal has no migration step to
// ride along with — which is why the check is separate from migrating in the
// first place.

func configFor(t *testing.T, db *pgtest.DB, autoMigrate bool) *config.Config {
	t.Helper()

	parsed, err := url.Parse(db.DSN)
	if err != nil {
		t.Fatalf("failed to parse the test DSN: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("failed to read the port from %q: %v", db.DSN, err)
	}

	password, _ := parsed.User.Password()
	cfg := &config.Config{}
	cfg.Database.Host = parsed.Hostname()
	cfg.Database.Port = port
	cfg.Database.Name = strings.TrimPrefix(parsed.Path, "/")
	cfg.Database.User = parsed.User.Username()
	cfg.Database.Password = password
	cfg.Database.SSLMode = "disable"
	cfg.Database.AutoMigrate = autoMigrate
	return cfg
}

func TestPrepareSchemaRefusesALegacyDatabaseAndWritesNothing(t *testing.T) {
	for _, autoMigrate := range []bool{true, false} {
		name := "auto-migrate off"
		if autoMigrate {
			name = "auto-migrate on"
		}

		t.Run(name, func(t *testing.T) {
			db := pgtest.New(t)
			// A database from before the histories were separated.
			pgtest.ApplyMigrationsFS(t, db.DB, CoreMigrationSource().FS,
				migrationstate.LegacyHistoryTable, 2)

			before := relationNames(t, db.DB)

			err := PrepareSchema(context.Background(), configFor(t, db, autoMigrate))
			if err == nil {
				t.Fatal("a legacy database must stop startup")
			}
			if !errors.Is(err, migrationstate.ErrRefused) {
				t.Fatalf("the refusal should be identifiable as one, got %T: %v", err, err)
			}
			// The message is the whole remedy an operator gets.
			if !strings.Contains(err.Error(), "migrate baseline") {
				t.Errorf("the refusal must name the command that fixes it, got: %v", err)
			}

			after := relationNames(t, db.DB)
			if added := difference(after, before); len(added) > 0 {
				t.Errorf(
					"a refused startup must leave the database untouched, but it created: %v",
					added,
				)
			}
			for _, unwanted := range []string{"core_schema_versions", "casbin_rule"} {
				if contains(after, unwanted) {
					t.Errorf("a refused startup created %s", unwanted)
				}
			}
		})
	}
}

// The ordering matters more than it looks: the next step after this one builds
// the Casbin service, whose adapter creates its own table and seeds policies.
// "Before bootstrap" — the obvious place — would already be too late.
func TestNewChecksTheSchemaBeforeAnythingWrites(t *testing.T) {
	var order []string

	deps := testInfra(t)
	deps.prepareSchema = func(context.Context, *Config, ...MigrationSource) error {
		order = append(order, "schema")
		return errors.New("refused for this test")
	}
	casbin := deps.newCasbin
	deps.newCasbin = func(cfg *Config, db *database.DB) (*authorization.CasbinService, error) {
		order = append(order, "casbin")
		return casbin(cfg, db)
	}

	_, err := newWithInfra(testConfig(), deps)
	if err == nil {
		t.Fatal("a failing schema check must stop startup")
	}

	if len(order) == 0 || order[0] != "schema" {
		t.Fatalf("the schema check must run first, order was %v", order)
	}
	if contains(order, "casbin") {
		t.Error("Casbin must not be built after the schema check refused: its adapter writes")
	}
}

func relationNames(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), `
		SELECT c.relname
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'`)
	if err != nil {
		t.Fatalf("failed to list relations: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan a relation name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate relations: %v", err)
	}
	return names
}

func difference(after, before []string) []string {
	seen := make(map[string]struct{}, len(before))
	for _, name := range before {
		seen[name] = struct{}{}
	}
	var added []string
	for _, name := range after {
		if _, ok := seen[name]; !ok {
			added = append(added, name)
		}
	}
	return added
}

func contains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
