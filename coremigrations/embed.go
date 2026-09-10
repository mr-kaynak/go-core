// Package coremigrations is the portable source of the core SQL migrations for
// consumers that depend on go-core as a Go module. A module dependency (or a
// go.mod replace directive) carries compiled packages and embedded files but no
// loose SQL files, so consumers cannot boot from a filesystem path they do not
// have; embedding makes the migrations travel with the module.
//
// Core's migrations record themselves in a history table of their own,
// separate from any a consumer registers, so an application's schema and
// core's advance independently and cannot collide on version numbers. The
// runner that arranges that lives in internal/infrastructure/database; this
// package is only the portable source.
//
// The files are append-only once released: a database that already recorded
// version N will never run it again, so editing N diverges fresh installs
// from existing ones. [VerifyInventory] and coremigrations/migrations.lock
// enforce that, and consumers can run the same check over their own
// migrations — see docs/migration-inventory-lock.md.
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
