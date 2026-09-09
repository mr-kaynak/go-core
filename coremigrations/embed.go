// Package coremigrations is the portable source of the core SQL migrations for
// consumers that depend on go-core as a Go module. A module dependency (or a
// go.mod replace directive) carries compiled packages and embedded files but no
// loose SQL files, so consumers cannot boot from a filesystem path they do not
// have; embedding makes the migrations travel with the module.
//
// The single migration history (00001-00016) is preserved as-is. Separating the
// core history from consumer application history is Phase C work; this package
// only solves the portability of the source.
package coremigrations

import (
	"embed"
	"io/fs"
)

//go:embed sql/*.sql
var migrationsFS embed.FS

// FS returns the embedded core migrations rooted at the directory holding the
// .sql files, so goose finds them with a "." directory argument:
//
//	database.RunMigrationsFS(db, coremigrations.FS())
func FS() fs.FS {
	sub, err := fs.Sub(migrationsFS, "sql")
	if err != nil {
		// Unreachable: "sql" is a valid path and the embed directive above
		// guarantees the directory exists in the binary.
		panic("coremigrations: failed to root embedded FS at sql: " + err.Error())
	}
	return sub
}
