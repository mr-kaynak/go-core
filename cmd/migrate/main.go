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
	"sort"
	"strconv"
	"strings"
	"unicode"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for the migration pool
	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp"
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

	opts, err := parseArgs(argv)
	if err != nil {
		return err
	}

	_ = godotenv.Load()

	// Only the database settings are read. This CLI runs SQL; requiring a JWT
	// secret and an SMTP host to do that would mean every migration container
	// carries the application's whole credential set.
	migrationCfg, err := app.LoadMigratorConfig()
	if err != nil {
		return fmt.Errorf("failed to load the configuration: %w", err)
	}

	// sql.Open does not connect, so building the runner is free for the
	// commands that never reach the database.
	//
	// The consumer sources come from --source-dir. Registering them is what
	// lets a mapped version be checked against a real file, a consumer history
	// be written, and a status report the histories that exist rather than
	// core's alone.
	runner, err := database.NewMigrationRunner(
		migrationCfg, app.CoreMigrationSource(), opts.sources()...,
	)
	if err != nil {
		return err
	}
	// Closing the pool is what ends the sessions that may still hold the
	// advisory lock; returning a connection to the pool does not.
	defer runner.Close()

	ctx := context.Background()

	// baseline is routed here rather than through dispatch because what it
	// acts on is a plan built from flags, not a runner plus a source name.
	if opts.command == commandBaseline {
		return baseline(ctx, runner, opts.baseline, out)
	}

	source, err := resolveSource(runner, opts.source)
	if err != nil {
		return err
	}

	return dispatch(ctx, runner, opts.command, source, opts.args, migrationCfg.Tolerances, out)
}

// commandBaseline converts a legacy single history into separated ones. It is
// named here because run routes it before dispatch.
const commandBaseline = "baseline"

// options is one parse of the command line.
type options struct {
	command  string
	source   string
	args     []string
	dirs     []consumerSource
	baseline baselineOptions
}

// sources are the consumer sources to register alongside core.
//
// os.DirFS rather than the embedded filesystem: these are somebody else's
// migrations, and this binary ships none of them.
func (o options) sources() []app.MigrationSource {
	sources := make([]app.MigrationSource, 0, len(o.dirs))
	for _, dir := range o.dirs {
		sources = append(sources, app.MigrationSource{Name: dir.name, FS: os.DirFS(dir.path)})
	}
	return sources
}

// parseArgs takes the command first and flags after it, because every Makefile
// target and every operator already types it that way.
func parseArgs(argv []string) (options, error) {
	opts := options{command: argv[0]}

	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	sourceFlag := flags.String(
		"source", migrationsource.CoreName,
		"which migration history down, redo and reset act on",
	)
	// --source-dir is not baseline's alone. Every command that reads a history
	// needs to know the source exists: after a conversion, a status that
	// reported only core would be silent about the history the operator has
	// just written, which is the moment they most need to see it.
	var sourceDirs repeatedFlag
	flags.Var(&sourceDirs, "source-dir",
		"a consumer source's migration files, as name=path (repeatable)")

	var raw baselineFlags
	raw.register(flags)

	if err := flags.Parse(argv[1:]); err != nil {
		return options{}, err
	}
	if err := refuseMisplacedBaselineFlags(flags, opts.command); err != nil {
		return options{}, err
	}

	opts.source = *sourceFlag
	opts.args = flags.Args()

	dirs, err := parseSourceDirs(sourceDirs)
	if err != nil {
		return options{}, err
	}
	opts.dirs = dirs

	if opts.command == commandBaseline {
		baselineOpts, err := raw.parse(dirs)
		if err != nil {
			return options{}, err
		}
		opts.baseline = baselineOpts
	}
	return opts, nil
}

// baselineFlagNames are the flags only baseline reads.
var baselineFlagNames = map[string]bool{
	"map": true, "unverified-source": true,
	"apply": true, "force": true, "acknowledge-data-migrations": true,
}

// refuseMisplacedBaselineFlags rejects baseline's flags on another command
// instead of ignoring them.
//
// --apply reads as consent to write, and an operator who gave it has decided
// something. Accepting it silently on a command that does not read it would
// let them believe the decision landed somewhere.
func refuseMisplacedBaselineFlags(flags *flag.FlagSet, command string) error {
	if command == commandBaseline {
		return nil
	}

	var misplaced error
	flags.Visit(func(f *flag.Flag) {
		if misplaced == nil && baselineFlagNames[f.Name] {
			misplaced = fmt.Errorf(
				"--%s belongs to the baseline command; %q does not read it", f.Name, command,
			)
		}
	})
	return misplaced
}

// baselineFlags is baseline's flag text as it was given, before any of it has
// been checked against the rest.
type baselineFlags struct {
	mapping     repeatedFlag
	unverified  repeatedFlag
	apply       bool
	force       bool
	acknowledge bool
}

func (f *baselineFlags) register(flags *flag.FlagSet) {
	flags.Var(&f.mapping, "map",
		"baseline: which legacy versions belong to a source, as source:versions (repeatable)")
	flags.Var(&f.unverified, "unverified-source",
		"baseline: accept a source's mapped versions without checking them against its files (repeatable)")
	flags.BoolVar(&f.apply, "apply", false,
		"baseline: perform the conversion; without it baseline is a dry run")
	flags.BoolVar(&f.force, "force", false,
		"baseline: proceed despite a schema fingerprint mismatch")
	flags.BoolVar(&f.acknowledge, "acknowledge-data-migrations", false,
		"baseline: proceed despite versions whose effect no fingerprint can see")
}

// repeatedFlag collects every occurrence of a flag, in the order given.
type repeatedFlag []string

func (r *repeatedFlag) String() string     { return strings.Join(*r, " ") }
func (r *repeatedFlag) Set(v string) error { *r = append(*r, v); return nil }

// consumerSource is one --source-dir: a source name and the directory holding
// its migrations.
type consumerSource struct {
	name string
	path string
}

// baselineOptions is a checked baseline command line: which versions would be
// recorded where, which sources can be checked against files, and what the
// operator has already accepted.
type baselineOptions struct {
	mapping     map[string][]int64
	dirs        []consumerSource
	unverified  map[string]bool
	apply       bool
	force       bool
	acknowledge bool
}

func (o baselineOptions) plan() database.BaselinePlan {
	return database.BaselinePlan{
		Mapping:                   o.mapping,
		Unverified:                o.unverified,
		Force:                     o.force,
		AcknowledgeDataMigrations: o.acknowledge,
	}
}

func (f *baselineFlags) parse(dirs []consumerSource) (baselineOptions, error) {
	mapping, err := parseMapping(f.mapping)
	if err != nil {
		return baselineOptions{}, err
	}
	if len(mapping) == 0 {
		return baselineOptions{}, errors.New(
			"baseline needs at least one --map: the legacy history is a single stream of numbers " +
				"with no record of who owned each one, so it cannot be guessed. Run `migrate status` " +
				"to see which versions it holds, then state them, for example --map core:1-16",
		)
	}

	unverified, err := parseUnverified(f.unverified, mapping)
	if err != nil {
		return baselineOptions{}, err
	}
	if err := checkSourcesAreSupplied(mapping, dirs); err != nil {
		return baselineOptions{}, err
	}

	return baselineOptions{
		mapping:     mapping,
		dirs:        dirs,
		unverified:  unverified,
		apply:       f.apply,
		force:       f.force,
		acknowledge: f.acknowledge,
	}, nil
}

// parseMapping reads the operator's statement of which legacy version belonged
// to which source.
//
// Nothing here is lenient. A malformed --map that was skipped would leave the
// versions it meant to assign unassigned, and the conversion would then be
// refused — or, worse, succeed while recording less than the operator wrote.
func parseMapping(values []string) (map[string][]int64, error) {
	mapping := make(map[string][]int64, len(values))

	for _, value := range values {
		name, spec, found := strings.Cut(value, ":")
		name = strings.TrimSpace(name)

		switch {
		case !found:
			return nil, fmt.Errorf(
				"--map %q has no colon; write it as source:versions, for example --map core:1-16", value)
		case name == "":
			return nil, fmt.Errorf("--map %q names no source before the colon", value)
		case strings.TrimSpace(spec) == "":
			return nil, fmt.Errorf(
				"--map %q assigns no versions to %q; a source mapped to nothing would be left "+
					"unrecorded rather than converted", value, name)
		}
		if _, duplicate := mapping[name]; duplicate {
			return nil, fmt.Errorf(
				"--map names %q twice; give a source all of its versions in one --map, "+
					"for example --map %s:1-3,7", name, name)
		}

		versions, err := parseVersionSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("--map %q %w", value, err)
		}
		mapping[name] = versions
	}
	return mapping, nil
}

// maxMappedVersions bounds one --map, so a mistyped range becomes an error
// rather than an allocation the size of the typo.
const maxMappedVersions = 10_000

// parseVersionSpec reads one --map's version list: numbers, ranges, or both.
func parseVersionSpec(spec string) ([]int64, error) {
	var versions []int64
	seen := map[int64]bool{}

	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New(
				"has an empty entry in its version list; write versions as 1-16 or 1,2,3")
		}

		low, high, err := parseVersionRange(part)
		if err != nil {
			return nil, err
		}
		// Counted before the range is expanded: a missing digit turns 1-16
		// into a range nothing should try to hold in memory first and reject
		// afterwards.
		if high-low >= int64(maxMappedVersions-len(versions)) {
			return nil, fmt.Errorf(
				"lists more than %d versions; a migration history is not that long", maxMappedVersions)
		}

		for version := low; version <= high; version++ {
			if seen[version] {
				return nil, fmt.Errorf("lists version %d twice", version)
			}
			seen[version] = true
			versions = append(versions, version)
		}
	}
	return versions, nil
}

// parseVersionRange reads one entry of a version list, which is either a
// single version or a low-high range.
func parseVersionRange(part string) (low, high int64, err error) {
	if strings.Count(part, "-") > 1 {
		return 0, 0, fmt.Errorf(
			"lists range %q with more than one dash; write it low-high, for example 1-16", part)
	}

	lowText, highText, isRange := strings.Cut(part, "-")
	if !isRange {
		version, verErr := parseVersionNumber(part)
		return version, version, verErr
	}

	lowText, highText = strings.TrimSpace(lowText), strings.TrimSpace(highText)
	if lowText == "" || highText == "" {
		return 0, 0, fmt.Errorf(
			"lists range %q, which is missing an end; write it low-high, for example 1-16", part)
	}
	if low, err = parseVersionNumber(lowText); err != nil {
		return 0, 0, err
	}
	if high, err = parseVersionNumber(highText); err != nil {
		return 0, 0, err
	}
	if high < low {
		return 0, 0, fmt.Errorf(
			"lists range %q, which counts down; write it low-high, for example 1-16", part)
	}
	return low, high, nil
}

func parseVersionNumber(text string) (int64, error) {
	version, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf(
			"lists %q, which is not a version number; write a number like 7 or a range like 1-16", text)
	}
	if version < 1 {
		return 0, fmt.Errorf(
			"lists version %d, which is not above zero; 0 is goose's sentinel row rather than a migration",
			version)
	}
	return version, nil
}

// parseSourceDirs reads the directories that supply the consumer sources.
func parseSourceDirs(values []string) ([]consumerSource, error) {
	dirs := make([]consumerSource, 0, len(values))
	seen := make(map[string]string, len(values))

	for _, value := range values {
		name, path, found := strings.Cut(value, "=")
		name, path = strings.TrimSpace(name), strings.TrimSpace(path)

		switch {
		case !found:
			return nil, fmt.Errorf(
				"--source-dir %q has no \"=\"; write it as name=path, "+
					"for example --source-dir orders=./migrations", value)
		case name == "":
			return nil, fmt.Errorf("--source-dir %q names no source before the \"=\"", value)
		case path == "":
			return nil, fmt.Errorf("--source-dir %q gives no path after the \"=\"", value)
		case name == migrationsource.CoreName:
			return nil, fmt.Errorf(
				"--source-dir cannot supply %q; core's migrations are embedded in this binary, and "+
					"they are the ones its fingerprints describe", migrationsource.CoreName)
		}
		if previous, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf(
				"--source-dir names %q twice, as %s and %s; one source has one directory",
				name, previous, path)
		}

		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("--source-dir %s: %w", value, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("--source-dir %s: %s is not a directory", value, path)
		}

		seen[name] = path
		dirs = append(dirs, consumerSource{name: name, path: path})
	}
	return dirs, nil
}

// parseUnverified reads which sources the operator accepts cannot be checked
// against their files.
func parseUnverified(values []string, mapping map[string][]int64) (map[string]bool, error) {
	unverified := make(map[string]bool, len(values))

	for _, value := range values {
		name := strings.TrimSpace(value)
		switch name {
		case "":
			return nil, errors.New("--unverified-source names no source")
		case migrationsource.CoreName:
			return nil, fmt.Errorf(
				"--unverified-source cannot be used for %q; core's migrations are embedded in this "+
					"binary, so its mapped versions are always checkable", migrationsource.CoreName)
		}
		if _, mapped := mapping[name]; !mapped {
			return nil, fmt.Errorf(
				"--unverified-source %q has no --map, so there is nothing to leave unchecked", name)
		}
		unverified[name] = true
	}
	return unverified, nil
}

// checkSourcesAreSupplied requires every mapped source other than core to come
// with its files.
//
// A history table is written only for a source the runner registers, and
// registering one means reading its migrations from somewhere. So a mapping
// for a source with no --source-dir cannot be carried out at all, and
// --unverified-source does not change that: it relaxes what is checked against
// the files, not whether they are needed.
func checkSourcesAreSupplied(mapping map[string][]int64, dirs []consumerSource) error {
	supplied := map[string]bool{migrationsource.CoreName: true}
	for _, dir := range dirs {
		supplied[dir.name] = true
	}

	for _, name := range sortedNames(mapping) {
		if !supplied[name] {
			return fmt.Errorf(
				"--map assigns versions to %q, but nothing supplies its migrations; "+
					"add --source-dir %s=<path to its .sql files>", name, name)
		}
	}
	return nil
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

// baseline converts a legacy single history into separated ones.
//
// The dry run is the default, and that is the whole design of this command.
// What a baseline writes is a claim that a schema is already at a given
// version; nothing reads that claim again until the next deploy, and a wrong
// one breaks it silently. So the product here is the report an operator reads
// before allowing the write — it has to be complete enough to decide on at
// three in the morning with the application stopped.
func baseline(
	ctx context.Context,
	runner *database.MigrationRunner,
	opts baselineOptions,
	out io.Writer,
) error {
	if opts.apply {
		outcome, err := runner.Baseline(ctx, opts.plan(), false)
		if err != nil {
			return err
		}
		printBaselineApplied(outcome, opts, out)
		return nil
	}

	// Read before the dry run, so what is reported as found describes the
	// database the checks then ran against rather than one read after them.
	report, err := runner.Report(ctx)
	if err != nil {
		return err
	}

	outcome, err := runner.Baseline(ctx, opts.plan(), true)
	if err != nil {
		// The refusal already names what is wrong and what to do about it.
		// A plan printed alongside it would describe a conversion that is not
		// going to happen, which is the last thing to put in front of someone
		// who has just been told to stop.
		return err
	}

	printBaselinePlan(report, outcome, opts, out)
	return nil
}

func printBaselinePlan(
	report migrationstate.Report,
	outcome *database.BaselineOutcome,
	opts baselineOptions,
	out io.Writer,
) {
	fmt.Fprintln(out, "Baseline dry run. Nothing below has been written.")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Found")
	field(out, "legacy history", outcome.LegacyTable)
	field(out, "versions applied", formatVersions(report.LegacyApplied))
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Would write")
	printHistories(outcome, opts, out)
	fmt.Fprintln(out)

	printFingerprint(outcome, out)
	fmt.Fprintln(out)

	printUnverified(outcome, opts, out)
	fmt.Fprintln(out)

	printAcceptances(outcome, out)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Nothing was written. To perform this conversion, stop every application and")
	fmt.Fprintln(out, "migration job against this database, then run:")
	fmt.Fprintf(out, "  %s\n", applyCommand(opts))
}

func printBaselineApplied(outcome *database.BaselineOutcome, opts baselineOptions, out io.Writer) {
	fmt.Fprintln(out, "Baseline applied.")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Wrote")
	printHistories(outcome, opts, out)
	fmt.Fprintln(out)

	if len(outcome.FingerprintDiffs) > 0 {
		fmt.Fprintf(out, "%d schema difference(s) were accepted with --force.\n",
			len(outcome.FingerprintDiffs))
	}
	fmt.Fprintf(out, "The conversion is recorded in %s.\n", migrationstate.BaselineAuditTable)
	fmt.Fprintf(out,
		"The legacy history %s was left in place as evidence and is not read again.\n",
		outcome.LegacyTable)
}

func printHistories(outcome *database.BaselineOutcome, opts baselineOptions, out io.Writer) {
	for _, source := range sortedNames(outcome.Histories) {
		versions := outcome.Histories[source]
		fmt.Fprintf(out, "  %s\n", source)
		subfield(out, "history table", migrationsource.TableName(source))
		subfield(out, "versions", fmt.Sprintf("%s (%d migrations)", formatVersions(versions), len(versions)))
		if opts.unverified[source] {
			subfield(out, "checked against files", "no (--unverified-source)")
		}
	}
}

// differenceLimit is how many schema differences are listed before the rest
// are counted. It matches the limit the refusal itself uses, so the dry run
// and the refusal describe the same mismatch the same way.
const differenceLimit = 20

// printFingerprint reports the evidence: the history is about to claim the
// schema is at a version, so the schema was compared against what that version
// is recorded to produce.
func printFingerprint(outcome *database.BaselineOutcome, out io.Writer) {
	// The outcome does not carry the version the comparison was made against.
	// It is core's highest mapped version, which is where the check derives it
	// from too.
	target := highest(outcome.Histories[migrationsource.CoreName])

	fmt.Fprintln(out, "Schema fingerprint")
	if len(outcome.FingerprintDiffs) == 0 {
		fmt.Fprintf(out, "  matches what core version %d produces\n", target)
		return
	}
	fmt.Fprintf(out, "  differs from what core version %d produces:\n", target)
	fmt.Fprintf(out, "  %s", schemafp.FormatDifferences(outcome.FingerprintDiffs, differenceLimit))
}

// printUnverified names everything this run could not establish for itself,
// because those are the claims the operator is making on their own authority.
func printUnverified(outcome *database.BaselineOutcome, opts baselineOptions, out io.Writer) {
	fmt.Fprintln(out, "Not verified")

	if len(outcome.UnverifiableVersions) == 0 && len(opts.unverified) == 0 {
		fmt.Fprintln(out, "  nothing; every mapped version was checked")
		return
	}

	if len(outcome.UnverifiableVersions) > 0 {
		fmt.Fprintf(out,
			"  core version(s) %s change no catalog state, so no fingerprint can confirm they ran\n",
			formatVersions(outcome.UnverifiableVersions))
	}
	for _, source := range sortedNames(opts.unverified) {
		fmt.Fprintf(out,
			"  %s version(s) %s were not checked against its migration files\n",
			source, formatVersions(outcome.Histories[source]))
	}
}

// printAcceptances says which of the two acceptances this conversion needs.
// Needing one is the difference between a routine conversion and a decision
// somebody has to own.
func printAcceptances(outcome *database.BaselineOutcome, out io.Writer) {
	fmt.Fprintln(out, "Requires")

	if len(outcome.FingerprintDiffs) == 0 && len(outcome.UnverifiableVersions) == 0 {
		fmt.Fprintln(out, "  nothing beyond --apply")
		return
	}
	if len(outcome.FingerprintDiffs) > 0 {
		fmt.Fprintf(out, "  --force                        %d schema difference(s), listed above\n",
			len(outcome.FingerprintDiffs))
	}
	if len(outcome.UnverifiableVersions) > 0 {
		fmt.Fprintf(out, "  --acknowledge-data-migrations  version(s) %s\n",
			formatVersions(outcome.UnverifiableVersions))
	}
}

// applyCommand is the exact invocation that performs what was just described.
//
// It is rebuilt from the parsed flags rather than echoed back from the
// arguments, so what an operator pastes is the plan they just read rather than
// whatever they typed to produce it.
func applyCommand(opts baselineOptions) string {
	parts := []string{"migrate", commandBaseline}

	for _, source := range sortedNames(opts.mapping) {
		parts = append(parts, "--map", source+":"+formatVersionSpec(opts.mapping[source]))
	}
	for _, dir := range opts.dirs {
		parts = append(parts, "--source-dir", dir.name+"="+dir.path)
	}
	for _, source := range sortedNames(opts.unverified) {
		parts = append(parts, "--unverified-source", source)
	}
	if opts.force {
		parts = append(parts, "--force")
	}
	if opts.acknowledge {
		parts = append(parts, "--acknowledge-data-migrations")
	}
	return strings.Join(append(parts, "--apply"), " ")
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

func field(out io.Writer, label, value string) {
	fmt.Fprintf(out, "  %-22s %s\n", label, value)
}

// subfield is a field one level in, for the lines belonging to a named source.
// The widths are chosen so both kinds of line share a value column.
func subfield(out io.Writer, label, value string) {
	fmt.Fprintf(out, "    %-20s %s\n", label, value)
}

// sortedNames lists source names core first, then alphabetically — the order
// the runner itself applies them in, so a report reads in the order things
// happen.
func sortedNames[V any](byName map[string]V) []string {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if isCore := names[i] == migrationsource.CoreName; isCore != (names[j] == migrationsource.CoreName) {
			return isCore
		}
		return names[i] < names[j]
	})
	return names
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

// formatVersionSpec renders versions in the form --map accepts, collapsing
// every contiguous run into a range.
//
// It is the inverse of parseVersionSpec, which is what makes the command a dry
// run prints pasteable: TestVersionSpecRoundTrip pins the two together so a
// change to one cannot quietly print a plan the other reads differently.
func formatVersionSpec(versions []int64) string {
	sorted := append([]int64(nil), versions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var parts []string
	for start := 0; start < len(sorted); {
		end := start
		for end+1 < len(sorted) && sorted[end+1] == sorted[end]+1 {
			end++
		}
		if end == start {
			parts = append(parts, strconv.FormatInt(sorted[start], 10))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", sorted[start], sorted[end]))
		}
		start = end + 1
	}
	return strings.Join(parts, ",")
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
	fmt.Fprintln(out, "Run once per database, by hand, with the application stopped.")
	fmt.Fprintln(out, "  baseline  Convert the single pre-separation history into separated ones")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Baseline flags. Without --apply it is a dry run, which is where to start.")
	fmt.Fprintln(out, "  --map source:versions          Which legacy versions belong to a source,")
	fmt.Fprintln(out, "                                 as 1-16 or 17,19-20 (repeatable)")
	fmt.Fprintln(out, "  --apply                        Perform the conversion")
	fmt.Fprintln(out, "  --force                        Proceed despite a schema fingerprint mismatch")
	fmt.Fprintln(out, "  --acknowledge-data-migrations  Proceed despite versions no fingerprint can see")
	fmt.Fprintln(out, "  --unverified-source name       Accept a source's mapped versions without")
	fmt.Fprintln(out, "                                 checking them against its files (repeatable)")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Development only. A down migration that drops a column destroys the data in it,")
	fmt.Fprintln(out, "and no lock makes that recoverable; production recovers by migrating forward.")
	fmt.Fprintln(out, "  down      Roll back the newest migration of one history")
	fmt.Fprintln(out, "  redo      Roll back the newest migration and apply it again")
	fmt.Fprintln(out, "  reset     Roll back every migration of one history")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Flags:")
	fmt.Fprintln(out, "  --source <name>        Which history down, redo and reset act on (default \"core\")")
	fmt.Fprintln(out, "  --source-dir name=path A consumer source's migration files (repeatable). Core")
	fmt.Fprintln(out, "                         ships its own; a consumer history is invisible to every")
	fmt.Fprintln(out, "                         command without this.")
}
