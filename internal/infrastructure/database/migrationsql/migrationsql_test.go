package migrationsql_test

import (
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsql"
)

func TestScanAcceptsAnOrdinaryMigration(t *testing.T) {
	sql := `
-- +goose Up
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT now()
);
CREATE UNIQUE INDEX idx_orders_ref ON orders(reference) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE orders;
`
	assertClean(t, sql)
}

func TestScanRejectsTheNoTransactionAnnotation(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",
		"-- +goose NO TRANSACTION",
		"CREATE INDEX CONCURRENTLY idx_orders_status ON orders(status);",
	}, "\n")

	assertViolations(t, sql, []int{2})
	assertReasonMentions(t, migrationsql.Scan(sql)[0], "NO TRANSACTION")
}

func TestScanRejectsTheEnvsubOnAnnotation(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",
		"-- +goose ENVSUB ON",
		"CREATE TABLE orders(id UUID PRIMARY KEY);",
	}, "\n")

	assertViolations(t, sql, []int{2})
	assertReasonMentions(t, migrationsql.Scan(sql)[0], "ENVSUB ON")
}

// goose compares annotations case-insensitively after collapsing the comment
// marker, so the scan has to recognize the same spellings goose acts on.
func TestScanRecognizesAnnotationSpellingVariants(t *testing.T) {
	variants := map[string]string{
		"lower case":       "-- +goose no transaction",
		"mixed case":       "-- +goose No Transaction",
		"no space after -": "--+goose NO TRANSACTION",
		"extra spacing":    "--   +goose   NO   TRANSACTION",
		"indented":         "    -- +goose NO TRANSACTION",
	}

	for name, annotation := range variants {
		t.Run(name, func(t *testing.T) {
			sql := "-- +goose Up\n" + annotation + "\nCREATE TABLE a(id INT);\n"
			assertViolations(t, sql, []int{2})
		})
	}
}

func TestScanAcceptsAnnotationsThatKeepTheTransaction(t *testing.T) {
	sql := `
-- +goose Up
-- +goose ENVSUB OFF
-- +goose StatementBegin
CREATE TABLE a(id INT);
-- +goose StatementEnd
`
	assertClean(t, sql)
}

func TestScanRejectsTransactionControlStatements(t *testing.T) {
	cases := []struct {
		name      string
		statement string
		keyword   string
	}{
		{name: "begin", statement: "BEGIN;", keyword: "BEGIN"},
		{name: "start transaction", statement: "START TRANSACTION;", keyword: "START"},
		{name: "commit", statement: "COMMIT;", keyword: "COMMIT"},
		{name: "end", statement: "END;", keyword: "END"},
		{name: "rollback", statement: "ROLLBACK;", keyword: "ROLLBACK"},
		{name: "abort", statement: "ABORT;", keyword: "ABORT"},
		{name: "savepoint", statement: "SAVEPOINT before_backfill;", keyword: "SAVEPOINT"},
		{name: "release", statement: "RELEASE SAVEPOINT before_backfill;", keyword: "RELEASE"},
		{name: "two-phase commit", statement: "PREPARE TRANSACTION 'orders';", keyword: "PREPARE TRANSACTION"},
		{name: "lower case", statement: "commit;", keyword: "COMMIT"},
		{name: "mixed case", statement: "Rollback;", keyword: "ROLLBACK"},
		{name: "commit with work", statement: "COMMIT WORK;", keyword: "COMMIT"},
		{name: "commit and chain", statement: "COMMIT AND CHAIN;", keyword: "COMMIT"},
		{name: "begin isolation level", statement: "BEGIN ISOLATION LEVEL SERIALIZABLE;", keyword: "BEGIN"},
		{name: "unterminated", statement: "COMMIT", keyword: "COMMIT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql := "-- +goose Up\n" + tc.statement + "\n"

			assertViolations(t, sql, []int{2})
			assertReasonMentions(t, migrationsql.Scan(sql)[0], tc.keyword)
		})
	}
}

// StatementBegin and StatementEnd only stop goose from splitting on
// semicolons. The statements between them still run, so wrapping a COMMIT in
// them changes nothing about what it does to the transaction.
func TestScanRejectsCommitWrappedInStatementAnnotations(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",
		"-- +goose StatementBegin",
		"COMMIT;",
		"-- +goose StatementEnd",
	}, "\n")

	assertViolations(t, sql, []int{3})
}

// ABORT is a synonym for ROLLBACK, so a file that avoids the obvious keyword
// can still end the transaction.
func TestScanRejectsAbortAsARollbackSynonym(t *testing.T) {
	sql := "-- +goose Up\nUPDATE orders SET status = 'void';\nABORT;\n"

	assertViolations(t, sql, []int{3})
	assertReasonMentions(t, migrationsql.Scan(sql)[0], "ABORT")
}

// PREPARE is transaction control only in its two-phase-commit form; the
// prepared-statement form is ordinary SQL.
func TestScanAcceptsPreparedStatements(t *testing.T) {
	sql := `
-- +goose Up
PREPARE active_orders AS SELECT id FROM orders WHERE status = 'active';
EXECUTE active_orders;
DEALLOCATE active_orders;
`
	assertClean(t, sql)
}

func TestScanAcceptsStatementsThatMerelyMentionKeywords(t *testing.T) {
	sql := `
-- +goose Up
CREATE TABLE audit_log (
    action VARCHAR(20) NOT NULL DEFAULT 'commit',
    "commit" TEXT,
    phase VARCHAR(20) CHECK (phase IN ('begin', 'end'))
);
INSERT INTO audit_log(action) VALUES ('rollback'), ('abort');
SELECT CASE WHEN status = 'a' THEN 1 ELSE 2 END FROM orders;
UPDATE audit_log SET action = E'it\'s a commit; really' WHERE action IS NULL;
`
	assertClean(t, sql)
}

func TestScanAcceptsATransactionKeywordInsideAFunctionBody(t *testing.T) {
	sql := `
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION settle_order(order_id UUID) RETURNS void AS $$
BEGIN
    UPDATE orders SET status = 'settled' WHERE id = order_id;
    COMMIT;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
`
	assertClean(t, sql)
}

// A tagged dollar quote ends only at its own tag, so an inner $$ is body text
// rather than a delimiter.
func TestScanAcceptsATaggedBodyContainingUntaggedDollarQuotes(t *testing.T) {
	sql := `
-- +goose Up
CREATE FUNCTION describe() RETURNS text AS $body$
BEGIN
    RETURN $$ COMMIT; $$;
END;
$body$ LANGUAGE plpgsql;
SELECT 1;
`
	assertClean(t, sql)
}

// The negative control for opacity: skipping a body must not swallow the rest
// of the file.
func TestScanResumesAfterADollarQuotedBody(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",
		"CREATE FUNCTION touch() RETURNS trigger AS $fn$",
		"BEGIN",
		"    NEW.updated_at = now(); RETURN NEW;",
		"END;",
		"$fn$ LANGUAGE plpgsql;",
		"CREATE TRIGGER t BEFORE UPDATE ON orders EXECUTE touch();",
		"COMMIT;",
	}, "\n")

	assertViolations(t, sql, []int{8})
}

// A dollar sign is not always a quote: positional parameters must not put the
// scan into a string that never ends.
func TestScanKeepsScanningAfterPositionalParameters(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",
		"PREPARE plan AS SELECT $1, $2 FROM orders;",
		"COMMIT;",
	}, "\n")

	assertViolations(t, sql, []int{3})
}

func TestScanHandlesNestedBlockComments(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",
		"/* disabled for now:",
		"   /* nested note: COMMIT; */",
		"   DROP TABLE orders;",
		"*/",
		"CREATE TABLE orders(id UUID PRIMARY KEY);",
	}, "\n")

	assertClean(t, sql)
}

func TestScanReportsTheLineTheStatementStartsOn(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",                        // 1
		"/* a block comment",                  // 2
		"   spanning several lines */",        // 3
		"CREATE TABLE orders (",               // 4
		"    id UUID PRIMARY KEY,",            // 5
		"    note TEXT DEFAULT 'many",         // 6
		"lines of literal'",                   // 7
		");",                                  // 8
		"",                                    // 9
		"-- a trailing thought",               // 10
		"COMMIT;",                             // 11
		"INSERT INTO orders(id) VALUES ($1);", // 12
		"   ROLLBACK",                         // 13
		"   ;",                                // 14
	}, "\n")

	assertViolations(t, sql, []int{11, 13})
}

func TestScanReportsEveryViolationInOneFile(t *testing.T) {
	sql := strings.Join([]string{
		"-- +goose Up",                  // 1
		"-- +goose NO TRANSACTION",      // 2
		"BEGIN;",                        // 3
		"CREATE TABLE orders(id UUID);", // 4
		"SAVEPOINT s1;",                 // 5
		"-- +goose ENVSUB ON",           // 6
		"RELEASE SAVEPOINT s1;",         // 7
		"COMMIT;",                       // 8
	}, "\n")

	assertViolations(t, sql, []int{2, 3, 5, 6, 7, 8})
}

func TestScanAcceptsEmptyInput(t *testing.T) {
	assertClean(t, "")
	assertClean(t, ";;\n;\n")
}

func assertClean(t *testing.T, sql string) {
	t.Helper()

	if violations := migrationsql.Scan(sql); len(violations) != 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
}

func assertViolations(t *testing.T, sql string, wantLines []int) {
	t.Helper()

	violations := migrationsql.Scan(sql)
	if len(violations) != len(wantLines) {
		t.Fatalf("expected %d violation(s) on lines %v, got %v", len(wantLines), wantLines, violations)
	}
	for i, want := range wantLines {
		if violations[i].Line != want {
			t.Errorf("violation %d: expected line %d, got %d (%s)", i, want, violations[i].Line, violations[i].Reason)
		}
		if violations[i].Reason == "" {
			t.Errorf("violation %d on line %d has no reason", i, violations[i].Line)
		}
	}
}

func assertReasonMentions(t *testing.T, violation migrationsql.Violation, want string) {
	t.Helper()

	if !strings.Contains(violation.Reason, want) {
		t.Errorf("reason should name %q, got: %s", want, violation.Reason)
	}
}
