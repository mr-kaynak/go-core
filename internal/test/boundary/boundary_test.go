// Package boundary_test guards the public/internal import boundary (K2).
//
// Go's own rules stop an EXTERNAL module from importing go-core's internal
// packages, but examples/ lives inside this module and is therefore allowed to
// do so by the compiler. Since examples/ is the executable proof that a
// consumer app needs nothing but the public facade, that proof has to be
// enforced here instead of by the toolchain.
package boundary_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// facadeImport is the package every example must reach the application
// through; requiring it means an emptied or gutted example fails the gate
// instead of passing it vacuously.
const facadeImport = "github.com/mr-kaynak/go-core/app"

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

func isInternalImport(path string) bool {
	return strings.HasPrefix(path, "internal/") ||
		strings.Contains(path, "/internal/") ||
		strings.HasSuffix(path, "/internal")
}

// TestExamplesImportOnlyPublicPackages fails if anything under examples/ pulls
// in a go-core internal package, and equally if the examples stop importing
// the public facade at all.
func TestExamplesImportOnlyPublicPackages(t *testing.T) {
	root := repoRoot(t)
	examplesDir := filepath.Join(root, "examples")

	fset := token.NewFileSet()
	goFiles := 0
	facadeUsed := false

	walkErr := filepath.WalkDir(examplesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		goFiles++

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Errorf("%s: unparsable import %s", rel, spec.Path.Value)
				continue
			}
			if isInternalImport(importPath) {
				t.Errorf("%s imports internal package %q — examples must consume only the public facade "+
					"(app, identity, coremigrations)", rel, importPath)
			}
			if importPath == facadeImport {
				facadeUsed = true
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", examplesDir, walkErr)
	}

	if goFiles == 0 {
		t.Fatalf("no Go files found under %s: the import boundary gate has nothing to prove", examplesDir)
	}
	if !facadeUsed {
		t.Errorf("no file under %s imports %q: an example that does not use the facade cannot prove the boundary",
			examplesDir, facadeImport)
	}
}
