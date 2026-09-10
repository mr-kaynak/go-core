package migrationsource_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mr-kaynak/go-core/coremigrations"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
)

const upMigration = "-- +goose Up\nCREATE TABLE orders (id uuid PRIMARY KEY);\n"

func sourceFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

func valid(name string) modcontract.MigrationSource {
	return modcontract.MigrationSource{
		Name: name,
		FS:   sourceFS(map[string]string{"00001_init.sql": upMigration}),
	}
}

func TestValidateAcceptsAWellFormedSource(t *testing.T) {
	if err := migrationsource.Validate([]modcontract.MigrationSource{valid("orders")}); err != nil {
		t.Fatalf("expected the source to be accepted: %v", err)
	}
}

func TestValidateAcceptsAnEmptySet(t *testing.T) {
	// An application with no consumer sources is ordinary, not a mistake.
	if err := migrationsource.Validate(nil); err != nil {
		t.Fatalf("an empty set should be valid: %v", err)
	}
}

func TestValidateRejectsBadNames(t *testing.T) {
	cases := map[string]struct {
		name string
		want string
	}{
		"empty":              {"", "name is empty"},
		"reserved core":      {"core", "reserved"},
		"postgres prefix":    {"pg_orders", "pg_"},
		"uppercase":          {"Orders", "lowercase"},
		"leading digit":      {"1orders", "lowercase letter"},
		"leading underscore": {"_orders", "lowercase letter"},
		"hyphen":             {"order-service", "lowercase"},
		"too long":           {strings.Repeat("a", 32), "the limit is"},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			source := valid("placeholder")
			source.Name = tc.name

			err := migrationsource.Validate([]modcontract.MigrationSource{source})
			if err == nil {
				t.Fatalf("name %q should have been rejected", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should explain why; want it to mention %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestValidateAcceptsNamesAtTheLengthLimit(t *testing.T) {
	source := valid(strings.Repeat("a", 31))

	if err := migrationsource.Validate([]modcontract.MigrationSource{source}); err != nil {
		t.Fatalf("a name at the limit should be accepted: %v", err)
	}
}

// The name is interpolated into DDL, so the alphabet is the whole defense.
func TestValidateRejectsNamesThatCouldEscapeAnIdentifier(t *testing.T) {
	for _, name := range []string{
		`orders"`,
		`orders; DROP TABLE users; --`,
		"orders schema",
		"orders'",
		"orders`",
	} {
		t.Run(name, func(t *testing.T) {
			source := valid("placeholder")
			source.Name = name

			if err := migrationsource.Validate([]modcontract.MigrationSource{source}); err == nil {
				t.Fatalf("name %q must be rejected", name)
			}
		})
	}
}

func TestValidateRejectsDuplicateNames(t *testing.T) {
	err := migrationsource.Validate([]modcontract.MigrationSource{valid("orders"), valid("orders")})

	if err == nil {
		t.Fatal("two sources with the same name should be rejected")
	}
	if !strings.Contains(err.Error(), "history table") {
		t.Fatalf("error should say why sharing a name is a problem, got: %v", err)
	}
}

func TestValidateRejectsUnusableFilesystems(t *testing.T) {
	cases := map[string]struct {
		source modcontract.MigrationSource
		want   string
	}{
		"nil filesystem": {
			modcontract.MigrationSource{Name: "orders"},
			"no filesystem",
		},
		"no sql files": {
			modcontract.MigrationSource{Name: "orders", FS: sourceFS(map[string]string{"readme.md": "x"})},
			"no .sql files",
		},
		"nested directory": {
			modcontract.MigrationSource{Name: "orders", FS: sourceFS(map[string]string{
				"00001_init.sql":  upMigration,
				"archive/old.sql": upMigration,
			})},
			"root of the filesystem",
		},
		"file without a version": {
			modcontract.MigrationSource{Name: "orders", FS: sourceFS(map[string]string{
				"schema.sql": upMigration,
			})},
			"no leading version number",
		},
		"duplicate versions": {
			modcontract.MigrationSource{Name: "orders", FS: sourceFS(map[string]string{
				"00001_init.sql":  upMigration,
				"00001_again.sql": upMigration,
			})},
			"claimed by both",
		},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			err := migrationsource.Validate([]modcontract.MigrationSource{tc.source})
			if err == nil {
				t.Fatal("expected the source to be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want the error to mention %q, got: %v", tc.want, err)
			}
		})
	}
}

// The bans exist so an interrupted migration rolls back cleanly. They are not
// a core-only rule: a consumer source that manages its own transaction breaks
// the same guarantee, so the check runs over every registered source.
func TestValidateRejectsTransactionControlInAConsumerSource(t *testing.T) {
	source := modcontract.MigrationSource{
		Name: "orders",
		FS: sourceFS(map[string]string{
			"00001_init.sql": "-- +goose Up\nCREATE TABLE orders (id uuid);\nCOMMIT;\nSELECT pg_sleep(1);\n",
		}),
	}

	err := migrationsource.Validate([]modcontract.MigrationSource{source})
	if err == nil {
		t.Fatal("a migration that commits its own transaction must be rejected")
	}
	if !strings.Contains(err.Error(), "00001_init.sql") {
		t.Fatalf("error should name the file, got: %v", err)
	}
}

func TestValidateRejectsNoTransactionAnnotationInAConsumerSource(t *testing.T) {
	source := modcontract.MigrationSource{
		Name: "orders",
		FS: sourceFS(map[string]string{
			"00001_init.sql": "-- +goose NO TRANSACTION\n-- +goose Up\nCREATE INDEX CONCURRENTLY x ON orders (id);\n",
		}),
	}

	if err := migrationsource.Validate([]modcontract.MigrationSource{source}); err == nil {
		t.Fatal("a NO TRANSACTION migration must be rejected")
	}
}

// One restart per problem is a bad way to learn a configuration is wrong.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	sources := []modcontract.MigrationSource{
		{Name: "Orders", FS: sourceFS(map[string]string{"00001_init.sql": upMigration})},
		{Name: "core", FS: sourceFS(map[string]string{"00001_init.sql": upMigration})},
		{Name: "billing", FS: sourceFS(map[string]string{"readme.md": "x"})},
	}

	err := migrationsource.Validate(sources)
	if err == nil {
		t.Fatal("expected the set to be rejected")
	}
	for _, want := range []string{"lowercase", "reserved", "no .sql files"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("combined error is missing %q:\n%v", want, err)
		}
	}
}

func TestTableNameIsDerivedFromTheSourceName(t *testing.T) {
	if got, want := migrationsource.TableName("orders"), "orders_schema_versions"; got != want {
		t.Fatalf("TableName(orders) = %q, want %q", got, want)
	}
	if got, want := migrationsource.TableName(migrationsource.CoreName), "core_schema_versions"; got != want {
		t.Fatalf("core history table = %q, want %q", got, want)
	}
}

// The name length limit exists only to keep the derived table name legal.
// Pin the relationship so raising one without the other cannot slip through.
func TestDerivedTableNameFitsIdentifierLimit(t *testing.T) {
	const postgresIdentifierLimit = 63

	longest := migrationsource.TableName(strings.Repeat("a", 31))
	if len(longest) > postgresIdentifierLimit {
		t.Fatalf(
			"the longest derived table name is %d bytes, over PostgreSQL's %d-byte limit: %q",
			len(longest), postgresIdentifierLimit, longest,
		)
	}
}

func TestValidateContiguousRejectsAGap(t *testing.T) {
	source := modcontract.MigrationSource{
		Name: "orders",
		FS: sourceFS(map[string]string{
			"00001_a.sql": upMigration,
			"00003_c.sql": upMigration,
		}),
	}

	err := migrationsource.ValidateContiguous(source)
	if err == nil {
		t.Fatal("a gap should be rejected for a contiguous source")
	}
	if !strings.Contains(err.Error(), "[2]") {
		t.Fatalf("error should name the missing version, got: %v", err)
	}
}

func TestValidateCoreAcceptsTheRealCoreMigrations(t *testing.T) {
	if err := migrationsource.ValidateCore(coreSource()); err != nil {
		t.Fatalf("the shipped core migrations must satisfy their own rules: %v", err)
	}
}

// The reserved name is reserved *from consumers*. Core itself must be able to
// use it, and validating core through the consumer path would reject the only
// source allowed to have that name.
func TestValidateCoreRequiresTheReservedName(t *testing.T) {
	source := coreSource()
	source.Name = "notcore"

	err := migrationsource.ValidateCore(source)
	if err == nil {
		t.Fatal("the core source must be named core")
	}
	if !strings.Contains(err.Error(), migrationsource.CoreName) {
		t.Fatalf("error should name the required name, got: %v", err)
	}
}

func TestValidateStillReservesTheCoreNameFromConsumers(t *testing.T) {
	if err := migrationsource.Validate([]modcontract.MigrationSource{coreSource()}); err == nil {
		t.Fatal("a consumer must not be able to register the core name")
	}
}

func coreSource() modcontract.MigrationSource {
	return modcontract.MigrationSource{Name: migrationsource.CoreName, FS: coremigrations.FS()}
}

// The rules are only credible if core's own migrations satisfy them. If a
// shipped migration ever fails this, the honest fix is the migration, not the
// rule.
func TestShippedCoreMigrationsSatisfyTheTransactionRules(t *testing.T) {
	source := modcontract.MigrationSource{Name: "corelike", FS: coremigrations.FS()}

	if err := migrationsource.Validate([]modcontract.MigrationSource{source}); err != nil {
		t.Fatalf("the shipped core migrations must pass the checks core imposes on consumers:\n%v", err)
	}
}
