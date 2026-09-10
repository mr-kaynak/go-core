package boundary_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Migration safety rests on every migration going through the runner, which
// takes a lock and refuses databases the migration cannot safely act on.
// Two ways around that are easy to reach for by accident, and neither
// produces a compile error.
//
// The first is goose's process-global API: goose.Up, goose.SetBaseFS and
// friends operate on package-level state, so two of them running at once
// clobber each other, and none of them takes the lock.
//
// The second is subtler and cost a design round to find. Provider.Up and
// Provider.UpByOne call HasPending first, and HasPending initializes the
// history table on an *unlocked* connection. So they create migration
// metadata outside the lock — and when nothing is pending they return
// without ever entering the locked path, skipping the guard entirely.
// Provider.Status and GetDBVersion likewise create the table they report on.
// Only ApplyVersion and Down take the lock before touching anything.
//
// This gate resolves types rather than matching names. A name-based check
// cannot tell runner.Up from provider.Up, and every way of narrowing one is
// wrong in some direction: per-file misses a helper handed a Provider it did
// not import, per-package rejects the runner's own Up. Loading type
// information costs a slow test and removes the guess.

const gooseModulePath = "github.com/pressly/goose/v3"

// gooseImporters are the only packages allowed to import goose at all.
// Confining the import keeps the dependency legible; the type-aware checks
// below are what make it safe.
var gooseImporters = map[string]bool{
	"coremigrations":                                   true,
	"internal/infrastructure/database":                 true,
	"internal/infrastructure/database/migrationsource": true,
	"internal/test/pgtest":                             true,
}

// allowedGooseSymbols are the package-level goose identifiers that carry no
// global state and perform no implicit database work.
var allowedGooseSymbols = map[string]bool{
	"NewProvider": true, "NumericComponent": true,
	"Dialect": true, "DialectPostgres": true, "DialectSQLite3": true,
	"WithTableName": true, "WithDisableGlobalRegistry": true,
	"WithSessionLocker": true, "WithLogger": true, "WithStore": true,
	"Provider": true, "MigrationResult": true, "Source": true,
}

// forbiddenProviderMethods reach Provider.initialize, which creates the
// history table. The ones routed through initialize(ctx, false) do it
// without the lock and without the guard.
var forbiddenProviderMethods = map[string]string{
	"Up":           "consults HasPending on an unlocked connection first, and returns without entering the locked path when nothing is pending; use MigrationRunner.Up",
	"UpByOne":      "has the same unlocked HasPending as Up; use MigrationRunner.UpOne",
	"UpTo":         "has the same unlocked HasPending as Up",
	"HasPending":   "initializes the history table on an unlocked connection as a side effect of asking; read the history directly",
	"GetVersions":  "initializes the history table on an unlocked connection; use MigrationRunner.Report",
	"GetDBVersion": "initializes the history table as a side effect; use MigrationRunner.Report",
	"Status":       "initializes the history table as a side effect, so a diagnostic run creates the metadata it reports on; use MigrationRunner.Report",
	"DownTo":       "rolls back without the admission check; use MigrationRunner.DownOne, which performs it",
}

// Note when running this locally: its inputs are the other packages' source
// files, which Go's test cache does not track. A run after changing only
// another package can therefore replay a stale pass. Use -count=1 when
// checking that a change is caught. CI always starts from a cold cache.
func TestGooseIsOnlyUsedThroughTheLockedPath(t *testing.T) {
	root := repoRoot(t)

	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:   root,
		Tests: true,
	}, "./...")
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no packages were loaded; this gate would pass without inspecting anything")
	}

	var problems []string
	for _, pkg := range loaded {
		problems = append(problems, inspectPackage(root, pkg)...)
	}

	if len(problems) > 0 {
		t.Fatalf(
			"goose is being used in a way that bypasses the migration lock or the guard:\n  - %s\n\n"+
				"Migrations go through database.MigrationRunner. If a new goose symbol is genuinely "+
				"safe, add it to allowedGooseSymbols with a note on why it holds no global state and "+
				"performs no implicit database work.",
			strings.Join(dedupe(problems), "\n  - "),
		)
	}
}

func inspectPackage(root string, pkg *packages.Package) []string {
	var problems []string

	for _, file := range pkg.Syntax {
		where := func(node ast.Node) string {
			pos := pkg.Fset.Position(node.Pos())
			relative, err := filepath.Rel(root, pos.Filename)
			if err != nil {
				relative = pos.Filename
			}
			return fmt.Sprintf("%s:%d", relative, pos.Line)
		}

		problems = append(problems, inspectImports(file, where)...)

		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			// A method on a *goose.Provider — whatever the receiver is called,
			// and whichever file it was handed to.
			if selection := pkg.TypesInfo.Selections[selector]; selection != nil {
				if isGooseProvider(selection.Recv()) {
					if reason, forbidden := forbiddenProviderMethods[selector.Sel.Name]; forbidden {
						problems = append(problems, fmt.Sprintf(
							"%s calls Provider.%s, which %s",
							where(selector), selector.Sel.Name, reason,
						))
					}
				}
				return true
			}

			// A package-level goose identifier.
			object := pkg.TypesInfo.Uses[selector.Sel]
			if object == nil || object.Pkg() == nil || object.Pkg().Path() != gooseModulePath {
				return true
			}
			if !allowedGooseSymbols[selector.Sel.Name] {
				problems = append(problems, fmt.Sprintf(
					"%s uses goose.%s, which is not on the allowlist",
					where(selector), selector.Sel.Name,
				))
			}
			return true
		})
	}

	return problems
}

func inspectImports(file *ast.File, where func(ast.Node) string) []string {
	var problems []string

	for _, spec := range file.Imports {
		if strings.Trim(spec.Path.Value, `"`) != gooseModulePath {
			continue
		}
		location := where(spec)

		// A dot import erases the qualifier, so goose.Up would appear as a
		// bare Up with no selector for the type check to resolve.
		if spec.Name != nil && spec.Name.Name == "." {
			problems = append(problems, fmt.Sprintf(
				"%s dot-imports goose, which hides its package-level API from this check", location,
			))
			continue
		}

		pkgDir := filepath.ToSlash(filepath.Dir(strings.SplitN(location, ":", 2)[0]))
		if !gooseImporters[pkgDir] {
			problems = append(problems, fmt.Sprintf(
				"%s imports goose, but only %s may. Migrations go through database.MigrationRunner",
				location, formatImporters(),
			))
		}
	}

	return problems
}

func isGooseProvider(recv types.Type) bool {
	if pointer, ok := recv.(*types.Pointer); ok {
		recv = pointer.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == gooseModulePath && named.Obj().Name() == "Provider"
}

// dedupe collapses duplicates arising from a package and its test variant
// sharing files.
func dedupe(problems []string) []string {
	seen := make(map[string]struct{}, len(problems))
	out := make([]string, 0, len(problems))
	for _, problem := range problems {
		if _, ok := seen[problem]; ok {
			continue
		}
		seen[problem] = struct{}{}
		out = append(out, problem)
	}
	sort.Strings(out)
	return out
}

func formatImporters() string {
	names := make([]string, 0, len(gooseImporters))
	for name := range gooseImporters {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
