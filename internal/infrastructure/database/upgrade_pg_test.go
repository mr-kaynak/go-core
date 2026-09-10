package database_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/coremigrations"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// Upgrading an existing database is the path every real deployment takes, and
// the one the rest of the suite never exercises: those tests build their
// schema from scratch, which is the single situation an upgrade is never in.
//
// Two variants cover it. The first runs on every pull request using an earlier
// version of this repository's own migrations as the stand-in predecessor, so
// the mechanism is under test today. The second runs against the previous
// release's migrations once a release exists; scripts/previous-release-upgrade.sh
// drives it.

func TestUpgradeFromEarlierVersionPreservesData(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	if latest < 3 {
		t.Skipf("need at least three core migrations to upgrade between, have %d", latest)
	}
	const stepsBack = 2
	from := latest - stepsBack

	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, from)

	seeded := seedUser(t, db.DB)
	before := coreVersion(t, db.DB)
	if before != from {
		t.Fatalf("expected the database to start at version %d, got %d", from, before)
	}

	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	if after := coreVersion(t, db.DB); after != latest {
		t.Fatalf("expected the upgrade to reach version %d, got %d", latest, after)
	}
	assertUserPresent(t, db.DB, seeded)
}

// TestUpgradeFromPreviousRelease upgrades a database built by the previous
// release's migrations, read from the git worktree the driving script checked
// out. It is inert until the first release tag exists.
func TestUpgradeFromPreviousRelease(t *testing.T) {
	worktree := os.Getenv("GOCORE_UPGRADE_WORKTREE")
	from := os.Getenv("GOCORE_UPGRADE_FROM")
	if worktree == "" || from == "" {
		t.Skip("GOCORE_UPGRADE_WORKTREE/GOCORE_UPGRADE_FROM are unset; " +
			"scripts/previous-release-upgrade.sh sets them once a release exists")
	}

	previous := os.DirFS(filepath.Join(worktree, "coremigrations", "sql"))
	previousInventory, err := coremigrations.Inventory(previous)
	if err != nil {
		t.Fatalf("failed to read %s migrations: %v", from, err)
	}
	currentInventory, err := coremigrations.Inventory(coremigrations.FS())
	if err != nil {
		t.Fatalf("failed to read the current migrations: %v", err)
	}

	// Released SQL is immutable, so every version the predecessor shipped must
	// still be present here byte for byte. Catching that here as well as in the
	// lock-file gate is deliberate: this compares against what was actually
	// published rather than against a file in the same commit.
	for version, was := range previousInventory {
		now, present := currentInventory[version]
		switch {
		case !present:
			t.Errorf("migration %d (%s) shipped in %s but is missing from HEAD", version, was.Name, from)
		case now.SHA256 != was.SHA256:
			t.Errorf("migration %d (%s) changed since %s: released SQL is immutable", version, was.Name, from)
		}
	}
	if t.Failed() {
		t.Fatalf("HEAD is not a valid successor of %s", from)
	}

	db := pgtest.New(t)
	pgtest.ApplyMigrationsFS(t, db.DB, previous, pgtest.CoreHistoryTable, 0)
	seeded := seedUser(t, db.DB)

	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	if after, want := coreVersion(t, db.DB), pgtest.LatestCoreVersion(t); after != want {
		t.Fatalf("expected the upgrade to reach version %d, got %d", want, after)
	}
	assertUserPresent(t, db.DB, seeded)
}

func seedUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO users (id, email, username, password)
		VALUES ($1, $2, $3, $4)`,
		id, id.String()+"@example.test", "u"+id.String()[:8], "not-a-real-hash",
	)
	if err != nil {
		t.Fatalf("failed to seed a user: %v", err)
	}
	return id
}

func assertUserPresent(t *testing.T, db *sql.DB, id uuid.UUID) {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM users WHERE id = $1`, id,
	).Scan(&count); err != nil {
		t.Fatalf("failed to read the seeded user back: %v", err)
	}
	if count != 1 {
		t.Fatalf("the seeded user did not survive the upgrade (found %d rows)", count)
	}
}

func coreVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	var version int64
	err := db.QueryRowContext(context.Background(),
		`SELECT coalesce(max(version_id), 0) FROM `+pgtest.CoreHistoryTable+` WHERE is_applied`,
	).Scan(&version)
	if err != nil {
		t.Fatalf("failed to read the core history version: %v", err)
	}
	return version
}
