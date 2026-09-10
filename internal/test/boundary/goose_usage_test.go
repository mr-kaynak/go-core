package boundary_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Migration safety rests on every migration going through the runner, which
// takes a lock and refuses databases the migration cannot safely act on.
// Two ways around that are easy to reach for by accident, and neither
// produces a compile error — so they are gated here.
//
// The first is goose's process-global API: goose.Up, goose.SetBaseFS and
// friends operate on package-level state, so two of them running at once
// clobber each other, and none of them takes the lock.
//
// The second is subtler and cost a design round to find. Provider.Up and
// Provider.UpByOne call HasPending first, and HasPending initializes the
// history table on an *unlocked* connection. So they create migration
// metadata outside the lock — and when nothing is pending they return
// without ever entering the locked path, skipping the guard entirely. Only
// ApplyVersion and Down take the lock before touching anything.

const gooseModulePath = "github.com/pressly/goose/v3"

// gooseImporters are the only packages allowed to import goose at all.
//
// Confining the import is what makes the rest of this gate precise. Without
// it the method check has to guess, from a name alone, whether Up() is
// goose's or the runner's — and it would have to guess in every package in
// the repository. With it, the guess is only ever made in these four, where
// a same-named method would be obvious in review.
var gooseImporters = map[string]bool{
	"coremigrations":                                   true,
	"internal/infrastructure/database":                 true,
	"internal/infrastructure/database/migrationsource": true,
	"internal/test/pgtest":                             true,
}

// allowedGooseSymbols are the package-level goose identifiers that carry no
// global state and no implicit database work.
var allowedGooseSymbols = map[string]bool{
	"NewProvider":               true,
	"NumericComponent":          true,
	"DialectPostgres":           true,
	"DialectSQLite3":            true,
	"Dialect":                   true,
	"WithTableName":             true,
	"WithDisableGlobalRegistry": true,
	"WithSessionLocker":         true,
	"WithLogger":                true,
	"WithStore":                 true,
	"Provider":                  true,
	"MigrationResult":           true,
	"Source":                    true,
}

// forbiddenProviderMethods bypass the lock or the guard, or write while
// claiming to report.
var forbiddenProviderMethods = map[string]string{
	"Up":         "consults HasPending on an unlocked connection first, and returns without entering the locked path when nothing is pending — use the runner, which applies one version at a time through ApplyVersion",
	"UpByOne":    "same unlocked HasPending as Up — use the runner's UpOne",
	"UpTo":       "same unlocked HasPending as Up",
	"HasPending": "initializes the history table on an unlocked connection as a side effect of asking; read the history directly instead",
	"Status":     "initializes the history table as a side effect, so a diagnostic run creates the metadata it is reporting on — use the runner's Report",
	"Version":    "initializes the history table as a side effect — use the runner's Report",
}

func TestGooseIsOnlyUsedThroughTheLockedPath(t *testing.T) {
	root := repoRoot(t)

	var problems []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		problems = append(problems, inspectFile(t, root, path)...)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk the repository: %v", err)
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf(
			"goose is being used in a way that bypasses the migration lock or the guard:\n  - %s\n\n"+
				"Migrations go through database.MigrationRunner. If a new goose symbol is genuinely "+
				"safe, add it to allowedGooseSymbols with a note on why it holds no global state and "+
				"performs no implicit database work.",
			strings.Join(problems, "\n  - "),
		)
	}
}

func inspectFile(t *testing.T, root, path string) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}

	alias, importsGoose := gooseImportName(file)
	if !importsGoose {
		return nil
	}

	relative, relErr := filepath.Rel(root, path)
	if relErr != nil {
		relative = path
	}

	if pkg := filepath.ToSlash(filepath.Dir(relative)); !gooseImporters[pkg] {
		return []string{fmt.Sprintf(
			"%s imports goose, but only %s may. Migrations go through "+
				"database.MigrationRunner; anything else needs the runner's API, not goose's",
			relative, formatImporters(),
		)}
	}

	var problems []string
	ast.Inspect(file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if ident, isIdent := selector.X.(*ast.Ident); isIdent && ident.Name == alias {
				if !allowedGooseSymbols[selector.Sel.Name] {
					problems = append(problems, fmt.Sprintf(
						"%s:%d uses goose.%s, which is not on the allowlist",
						relative, fset.Position(selector.Pos()).Line, selector.Sel.Name,
					))
				}
			}
			return true
		}

		// Only calls, never field reads: goose.Source has a Version field, and
		// reading it is not calling Provider.Version.
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// Package-level goose calls are judged by the allowlist above.
		if ident, isIdent := selector.X.(*ast.Ident); isIdent && ident.Name == alias {
			return true
		}

		// Without type information this cannot prove the receiver is a
		// *goose.Provider, so it is deliberately conservative: these method
		// names are forbidden in any file that imports goose at all. A false
		// positive is a same-named method in a file that also happens to
		// import goose, which is rare and easy to resolve by moving the call.
		if reason, forbidden := forbiddenProviderMethods[selector.Sel.Name]; forbidden {
			problems = append(problems, fmt.Sprintf(
				"%s:%d calls %s(), which %s",
				relative, fset.Position(selector.Pos()).Line, selector.Sel.Name, reason,
			))
		}
		return true
	})

	return problems
}

// gooseImportName returns the name goose is bound to in this file, honoring
// an alias, and whether it is imported at all.
func gooseImportName(file *ast.File) (string, bool) {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != gooseModulePath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name, true
		}
		return "goose", true
	}
	return "", false
}

func formatImporters() string {
	names := make([]string, 0, len(gooseImporters))
	for name := range gooseImporters {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "bin":
		return true
	}
	return false
}
