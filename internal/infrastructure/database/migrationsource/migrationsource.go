// Package migrationsource validates the set of migration sources an
// application registers, before anything touches a database.
//
// Validation is a batch operation on purpose. A half-accepted set would leave
// the application running with some of its schema owners registered and
// others silently dropped, which is worse than refusing to start: the missing
// ones only surface later, as a table that does not exist.
package migrationsource

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsql"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
	"github.com/pressly/goose/v3"
)

// CoreName is the reserved source name for go-core's own migrations. A
// consumer that could claim it would take over the history core reads to
// decide whether a database is up to date.
const CoreName = "core"

// historyTableSuffix turns a source name into its history table name.
const historyTableSuffix = "_schema_versions"

// maxNameLength keeps the derived table name inside PostgreSQL's 63-byte
// identifier limit with room to spare. TestDerivedTableNameFitsIdentifierLimit
// pins the relationship so the two cannot drift apart.
const maxNameLength = 31

// nameRE is the whole of the injection defense for the derived table name.
//
// Table names cannot be passed as query parameters, so the name is
// interpolated into DDL. Rather than escaping it, the alphabet is restricted
// to characters that cannot terminate an identifier or start a new statement,
// and the table name is derived here rather than accepted from a caller. A
// name that matches this and gains a fixed suffix is a valid, unambiguous,
// unquoted PostgreSQL identifier — which is also why there is no
// reserved-keyword check: the suffix means the result can never be a keyword.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)

// TableName is the history table a source records its applied versions in.
// Callers must not build this string themselves; validation is what makes it
// safe to interpolate.
func TableName(sourceName string) string {
	return sourceName + historyTableSuffix
}

// Validate reports every problem across the whole set in a single error.
//
// Reporting them one at a time would mean a consumer fixes one, restarts,
// and meets the next — so the message lists all of them at once.
func Validate(sources []modcontract.MigrationSource) error {
	var problems []string

	seen := make(map[string]int, len(sources))
	for i, source := range sources {
		label := fmt.Sprintf("source %d", i)
		if source.Name != "" {
			label = fmt.Sprintf("source %q", source.Name)
		}

		if nameProblems := validateName(source.Name); len(nameProblems) > 0 {
			problems = append(problems, prefixAll(label, nameProblems)...)
		} else if previous, duplicate := seen[source.Name]; duplicate {
			problems = append(problems, fmt.Sprintf(
				"%s: already registered by source %d; two sources cannot share a history table",
				label, previous,
			))
		} else {
			seen[source.Name] = i
		}

		problems = append(problems, prefixAll(label, validateFS(source))...)
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf(
		"migration sources are not valid:\n  - %s",
		strings.Join(problems, "\n  - "),
	)
}

func validateName(name string) []string {
	switch {
	case name == "":
		return []string{"name is empty; every source needs a stable name of its own"}
	case name == CoreName:
		return []string{fmt.Sprintf("name %q is reserved for go-core's own migrations", CoreName)}
	case strings.HasPrefix(name, "pg_"):
		return []string{"names starting with \"pg_\" are reserved by PostgreSQL for system objects"}
	case len(name) > maxNameLength:
		return []string{fmt.Sprintf(
			"name is %d characters; the limit is %d so the derived history table fits PostgreSQL's identifier limit",
			len(name), maxNameLength,
		)}
	case !nameRE.MatchString(name):
		return []string{
			"name must start with a lowercase letter and contain only lowercase letters, " +
				"digits and underscores; it is interpolated into the history table name",
		}
	}
	return nil
}

func validateFS(source modcontract.MigrationSource) []string {
	if source.FS == nil {
		return []string{"has no filesystem; a source with no migrations should not be registered"}
	}

	entries, err := fs.ReadDir(source.FS, ".")
	if err != nil {
		return []string{fmt.Sprintf("filesystem cannot be read: %v", err)}
	}

	var problems []string
	versions := make(map[int64]string)
	sqlFiles := 0

	for _, entry := range entries {
		if entry.IsDir() {
			problems = append(problems, fmt.Sprintf(
				"contains directory %q; migrations must sit at the root of the filesystem, "+
					"which is what goose reads",
				entry.Name(),
			))
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		sqlFiles++

		version, verErr := goose.NumericComponent(name)
		if verErr != nil {
			problems = append(problems, fmt.Sprintf(
				"file %q has no leading version number, so its place in the history is undefined", name,
			))
			continue
		}
		if previous, duplicate := versions[version]; duplicate {
			problems = append(problems, fmt.Sprintf(
				"version %d is claimed by both %q and %q", version, previous, name,
			))
			continue
		}
		versions[version] = name

		problems = append(problems, scanFile(source.FS, name)...)
	}

	if sqlFiles == 0 {
		problems = append(problems, "has no .sql files at the root of its filesystem")
	}

	return problems
}

// scanFile rejects migrations that would break the guarantee an interrupted
// run rolls back cleanly. See package migrationsql for what and why.
func scanFile(fsys fs.FS, name string) []string {
	file, err := fsys.Open(name)
	if err != nil {
		return []string{fmt.Sprintf("file %q cannot be opened: %v", name, err)}
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return []string{fmt.Sprintf("file %q cannot be read: %v", name, err)}
	}

	violations := migrationsql.Scan(string(content))
	problems := make([]string, 0, len(violations))
	for _, v := range violations {
		problems = append(problems, fmt.Sprintf("file %q line %d: %s", name, v.Line, v.Reason))
	}
	return problems
}

// ValidateCore checks core's own source.
//
// It is separate from [Validate] because the two answer different questions.
// Validate is asked "may a consumer register this?", and the answer for the
// name "core" is no — that name is reserved. ValidateCore is asked "is core's
// own source well formed?", where "core" is the only acceptable name.
//
// Core additionally has to be contiguous. Its history is the baseline target,
// and a conversion is expressed as a prefix 1..N; a gap would make that
// impossible to state.
func ValidateCore(source modcontract.MigrationSource) error {
	if source.Name != CoreName {
		return fmt.Errorf(
			"the core migration source must be named %q, not %q", CoreName, source.Name,
		)
	}
	return validateContiguous(source, validateFS(source))
}

// ValidateContiguous requires a consumer source's versions to run 1..N with
// no gaps, on top of the ordinary rules.
func ValidateContiguous(source modcontract.MigrationSource) error {
	if err := Validate([]modcontract.MigrationSource{source}); err != nil {
		return err
	}
	return validateContiguous(source, nil)
}

func validateContiguous(source modcontract.MigrationSource, problems []string) error {
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf(
			"migration source %q is not valid:\n  - %s",
			source.Name, strings.Join(problems, "\n  - "),
		)
	}

	names, err := fs.Glob(source.FS, "*.sql")
	if err != nil {
		return fmt.Errorf("source %q: filesystem cannot be listed: %w", source.Name, err)
	}

	present := make(map[int64]bool, len(names))
	var highest int64
	for _, name := range names {
		version, verErr := goose.NumericComponent(name)
		if verErr != nil {
			return fmt.Errorf("source %q: file %q has no version number: %w", source.Name, name, verErr)
		}
		present[version] = true
		if version > highest {
			highest = version
		}
	}

	var missing []int64
	for version := int64(1); version <= highest; version++ {
		if !present[version] {
			missing = append(missing, version)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"source %q: versions %v are missing below the highest version %d; "+
				"this history must be contiguous from 1",
			source.Name, missing, highest,
		)
	}
	return nil
}

func prefixAll(label string, problems []string) []string {
	out := make([]string, 0, len(problems))
	for _, problem := range problems {
		out = append(out, label+": "+problem)
	}
	return out
}

// ErrNoSources is returned when a runner is built with nothing to run, which
// is always a wiring mistake rather than an empty-but-valid configuration.
var ErrNoSources = errors.New("no migration sources were registered")
