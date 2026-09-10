package coremigrations_test

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mr-kaynak/go-core/coremigrations"
)

const lockPath = "migrations.lock"

// TestCommittedLockMatchesEmbeddedMigrations is the immutability gate itself:
// it fails when a core migration is added, removed, renamed or edited without
// the lock file being regenerated in the same change. That makes every such
// edit visible in review instead of silently diverging fresh installs from
// existing databases.
func TestCommittedLockMatchesEmbeddedMigrations(t *testing.T) {
	lock, err := os.Open(lockPath)
	if err != nil {
		t.Fatalf("failed to open %s: %v", lockPath, err)
	}
	defer lock.Close() //nolint:errcheck // read-only handle

	if err := coremigrations.VerifyInventory(coremigrations.FS(), lock); err != nil {
		t.Fatalf(
			"embedded migrations do not match %s.\n%v\n\n"+
				"If you added a migration, regenerate the lock: go run ./cmd/inventorylock\n"+
				"If you edited an existing one, don't: fix forward with a new migration.",
			lockPath, err,
		)
	}
}

func TestInventoryDigestsEveryMigration(t *testing.T) {
	inventory, err := coremigrations.Inventory(coremigrations.FS())
	if err != nil {
		t.Fatalf("Inventory failed: %v", err)
	}

	if len(inventory) == 0 {
		t.Fatal("expected at least one embedded migration")
	}
	for version, entry := range inventory {
		if entry.Name == "" {
			t.Errorf("version %d has an empty filename", version)
		}
		if len(entry.SHA256) != 64 {
			t.Errorf("version %d has a malformed digest %q", version, entry.SHA256)
		}
	}
}

func TestInventoryRejectsDuplicateVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_first.sql":     &fstest.MapFile{Data: []byte("-- +goose Up\n")},
		"00001_conflict.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\n")},
		"00002_unrelated.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}

	_, err := coremigrations.Inventory(fsys)
	if err == nil {
		t.Fatal("expected duplicate versions to be rejected")
	}
	if !strings.Contains(err.Error(), "claimed by both") {
		t.Fatalf("error should name the collision, got: %v", err)
	}
}

func TestInventoryRejectsMissingVersionPrefix(t *testing.T) {
	fsys := fstest.MapFS{
		"schema.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}

	_, err := coremigrations.Inventory(fsys)
	if err == nil {
		t.Fatal("expected a file without a numeric prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "numeric prefix") {
		t.Fatalf("error should explain the prefix requirement, got: %v", err)
	}
}

// The four differences below are the ones the lock file exists to catch. The
// edited-content case is the important one: the version number is unchanged,
// so nothing else in the system would notice.
func TestVerifyInventoryDetectsEditedMigration(t *testing.T) {
	original := fstest.MapFS{
		"00001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE a();\n")},
	}
	edited := fstest.MapFS{
		"00001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE b();\n")},
	}

	err := coremigrations.VerifyInventory(edited, lockFor(t, original))
	if err == nil {
		t.Fatal("expected an edited migration to be rejected")
	}
	if !strings.Contains(err.Error(), "content changed") {
		t.Fatalf("error should name the content change, got: %v", err)
	}
	if !strings.Contains(err.Error(), "fix forward") {
		t.Fatalf("error should tell the operator what to do instead, got: %v", err)
	}
}

func TestVerifyInventoryDetectsDeletedMigration(t *testing.T) {
	original := fstest.MapFS{
		"00001_first.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\n")},
		"00002_second.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}
	reduced := fstest.MapFS{
		"00001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}

	err := coremigrations.VerifyInventory(reduced, lockFor(t, original))
	if err == nil {
		t.Fatal("expected a deleted migration to be rejected")
	}
	if !strings.Contains(err.Error(), "must not be deleted") {
		t.Fatalf("error should name the deletion, got: %v", err)
	}
}

func TestVerifyInventoryDetectsRenamedMigration(t *testing.T) {
	original := fstest.MapFS{
		"00001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}
	renamed := fstest.MapFS{
		"00001_renamed.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}

	err := coremigrations.VerifyInventory(renamed, lockFor(t, original))
	if err == nil {
		t.Fatal("expected a renamed migration to be rejected")
	}
	if !strings.Contains(err.Error(), "renamed") {
		t.Fatalf("error should name the rename, got: %v", err)
	}
}

func TestVerifyInventoryDetectsUnlockedAddition(t *testing.T) {
	original := fstest.MapFS{
		"00001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}
	extended := fstest.MapFS{
		"00001_first.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\n")},
		"00002_second.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}

	err := coremigrations.VerifyInventory(extended, lockFor(t, original))
	if err == nil {
		t.Fatal("expected an unlocked addition to be reported")
	}
	if !strings.Contains(err.Error(), "not in the lock file") {
		t.Fatalf("error should name the addition, got: %v", err)
	}
}

// A single run reports every difference, so one CI failure shows the whole
// picture instead of one problem per push.
func TestVerifyInventoryReportsAllDifferencesAtOnce(t *testing.T) {
	original := fstest.MapFS{
		"00001_first.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE a();\n")},
		"00002_second.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}
	mangled := fstest.MapFS{
		"00001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE changed();\n")},
		"00003_third.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n")},
	}

	err := coremigrations.VerifyInventory(mangled, lockFor(t, original))
	if err == nil {
		t.Fatal("expected the mangled inventory to be rejected")
	}
	for _, want := range []string{"content changed", "must not be deleted", "not in the lock file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("combined error is missing %q:\n%v", want, err)
		}
	}
}

func TestVerifyInventoryAcceptsUnchangedInventory(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_first.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE a();\n")},
		"00002_second.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE b();\n")},
	}

	if err := coremigrations.VerifyInventory(fsys, lockFor(t, fsys)); err != nil {
		t.Fatalf("unchanged inventory should verify: %v", err)
	}
}

func TestReadInventoryRejectsMalformedLines(t *testing.T) {
	cases := map[string]string{
		"missing fields":  "1\tabc\n",
		"bad version":     "one\t" + strings.Repeat("a", 64) + "\t00001_first.sql\n",
		"short digest":    "1\tdeadbeef\t00001_first.sql\n",
		"duplicate entry": "1\t" + strings.Repeat("a", 64) + "\tx.sql\n1\t" + strings.Repeat("b", 64) + "\ty.sql\n",
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := coremigrations.ReadInventory(strings.NewReader(content)); err == nil {
				t.Fatal("expected a malformed lock file to be rejected")
			}
		})
	}
}

func TestReadInventorySkipsCommentsAndBlankLines(t *testing.T) {
	content := "# header\n\n1\t" + strings.Repeat("a", 64) + "\t00001_first.sql\n"

	inventory, err := coremigrations.ReadInventory(strings.NewReader(content))
	if err != nil {
		t.Fatalf("ReadInventory failed: %v", err)
	}
	if len(inventory) != 1 || inventory[1].Name != "00001_first.sql" {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}
}

func lockFor(t *testing.T, fsys fstest.MapFS) *strings.Reader {
	t.Helper()

	inventory, err := coremigrations.Inventory(fsys)
	if err != nil {
		t.Fatalf("failed to build inventory: %v", err)
	}
	var buf strings.Builder
	if err := coremigrations.WriteInventory(&buf, inventory); err != nil {
		t.Fatalf("failed to write inventory: %v", err)
	}
	return strings.NewReader(buf.String())
}
