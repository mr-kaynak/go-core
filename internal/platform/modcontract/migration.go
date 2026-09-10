package modcontract

import "io/fs"

// MigrationSource is one owner of schema changes: a name and the SQL files
// belonging to it.
//
// Each source gets its own history table, so two independently developed
// modules can both ship a migration numbered 00001 without colliding — a
// collision a consumer could not fix, since it cannot renumber a dependency's
// files.
//
// Name is a permanent database identity, not a label. It is derived into the
// history table name and recorded in every applied row, so renaming a source
// after it has been deployed starts a fresh history and replays its SQL
// against objects that already exist. Choose it once.
//
// FS must expose the .sql files at its root, the shape coremigrations.FS()
// returns.
type MigrationSource struct {
	Name string
	FS   fs.FS
}

// MigrationProvider is the optional interface a [Module] implements to ship
// its own schema.
//
// It is separate from Module rather than a method on it for two reasons:
// adding a method to Module would break every existing implementation, and
// the migration identity has to be able to stay fixed while the module's own
// name changes. A module that does not implement this simply has no
// migrations.
type MigrationProvider interface {
	MigrationSource() MigrationSource
}
