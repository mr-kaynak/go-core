package coremigrations

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

// FileDigest identifies one migration file by name and content.
//
// SHA256 is the hex-encoded digest of the file's exact bytes. Version numbers
// alone cannot detect an edited migration: a database records that version N
// was applied, never what SQL version N contained at the time. The digest is
// what makes "already-released SQL is immutable" a checkable claim rather than
// a convention.
type FileDigest struct {
	Name   string
	SHA256 string
}

// Inventory reads every migration in fsys and returns it keyed by goose
// version. The filesystem is expected to hold the .sql files at its root, the
// same shape [FS] returns.
//
// It reports an error when a file carries no goose numeric prefix or when two
// files claim the same version, because either makes the inventory ambiguous.
func Inventory(fsys fs.FS) (map[int64]FileDigest, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("coremigrations: failed to list migrations: %w", err)
	}

	inventory := make(map[int64]FileDigest, len(names))
	for _, name := range names {
		version, verErr := goose.NumericComponent(name)
		if verErr != nil {
			return nil, fmt.Errorf("coremigrations: file %q has no goose numeric prefix: %w", name, verErr)
		}
		if previous, dup := inventory[version]; dup {
			return nil, fmt.Errorf(
				"coremigrations: version %d claimed by both %q and %q",
				version, previous.Name, name,
			)
		}

		digest, digestErr := digestFile(fsys, name)
		if digestErr != nil {
			return nil, digestErr
		}
		inventory[version] = FileDigest{Name: name, SHA256: digest}
	}

	return inventory, nil
}

func digestFile(fsys fs.FS, name string) (string, error) {
	file, err := fsys.Open(name)
	if err != nil {
		return "", fmt.Errorf("coremigrations: failed to open %q: %w", name, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("coremigrations: failed to read %q: %w", name, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// WriteInventory renders inventory in the lock-file format VerifyInventory
// reads: a comment header, then one "version<TAB>sha256<TAB>filename" line per
// migration, ordered by version. The format is line-oriented and sorted so a
// changed migration shows up as a one-line diff in review.
func WriteInventory(w io.Writer, inventory map[int64]FileDigest) error {
	versions := make([]int64, 0, len(inventory))
	for version := range inventory {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	if _, err := fmt.Fprint(w, lockHeader); err != nil {
		return err
	}
	for _, version := range versions {
		entry := inventory[version]
		if _, err := fmt.Fprintf(w, "%d\t%s\t%s\n", version, entry.SHA256, entry.Name); err != nil {
			return err
		}
	}
	return nil
}

const lockHeader = `# Migration inventory lock.
#
# Released migration SQL is immutable: a database that already recorded version
# N will never re-run it, so editing N changes what fresh installs get while
# leaving existing ones behind. Fix forward with a new migration instead.
#
# Regenerate deliberately (and explain why in the commit) when adding a
# migration:  go run ./cmd/inventorylock
#
# version	sha256	filename
`

// VerifyInventory compares fsys against a previously written lock file and
// reports every difference in one error: migrations that were added without
// updating the lock, migrations that disappeared, and — the case version
// numbers cannot catch — migrations whose content changed under an unchanged
// version.
//
// Consumers with their own migration sources are expected to run the same
// check in their CI over their own filesystem and lock file; see
// docs/migrations-lock.md.
func VerifyInventory(fsys fs.FS, lock io.Reader) error {
	current, err := Inventory(fsys)
	if err != nil {
		return err
	}
	recorded, err := ReadInventory(lock)
	if err != nil {
		return err
	}

	versions := make([]int64, 0, len(current)+len(recorded))
	seen := make(map[int64]struct{}, len(current)+len(recorded))
	for _, set := range []map[int64]FileDigest{current, recorded} {
		for version := range set {
			if _, dup := seen[version]; dup {
				continue
			}
			seen[version] = struct{}{}
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	var problems []string
	for _, version := range versions {
		have, inCurrent := current[version]
		want, inLock := recorded[version]

		switch {
		case inCurrent && !inLock:
			problems = append(problems, fmt.Sprintf(
				"version %d (%s) is not in the lock file: add it deliberately",
				version, have.Name,
			))
		case !inCurrent && inLock:
			problems = append(problems, fmt.Sprintf(
				"version %d (%s) is in the lock file but missing from the source: released migrations must not be deleted",
				version, want.Name,
			))
		case have.Name != want.Name:
			problems = append(problems, fmt.Sprintf(
				"version %d was renamed from %q to %q: released migrations must not be renumbered or renamed",
				version, want.Name, have.Name,
			))
		case have.SHA256 != want.SHA256:
			problems = append(problems, fmt.Sprintf(
				"version %d (%s) content changed (lock %s, source %s): released migrations are immutable, fix forward with a new migration",
				version, have.Name, shortDigest(want.SHA256), shortDigest(have.SHA256),
			))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf(
			"coremigrations: migration inventory does not match the lock file:\n  - %s",
			strings.Join(problems, "\n  - "),
		)
	}
	return nil
}

// shortDigestLen keeps digests in error messages long enough to identify a
// file but short enough to read.
const shortDigestLen = 12

func shortDigest(digest string) string {
	if len(digest) <= shortDigestLen {
		return digest
	}
	return digest[:shortDigestLen]
}

// ReadInventory parses the lock-file format written by WriteInventory.
func ReadInventory(r io.Reader) (map[int64]FileDigest, error) {
	inventory := make(map[int64]FileDigest)
	scanner := bufio.NewScanner(r)

	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		fields := strings.Split(text, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf(
				"coremigrations: lock file line %d is malformed, want \"version<TAB>sha256<TAB>filename\": %q",
				line, text,
			)
		}

		version, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("coremigrations: lock file line %d has a non-numeric version %q", line, fields[0])
		}
		if _, dup := inventory[version]; dup {
			return nil, fmt.Errorf("coremigrations: lock file lists version %d twice", version)
		}
		if len(fields[1]) != sha256.Size*2 {
			return nil, fmt.Errorf(
				"coremigrations: lock file line %d has a malformed sha256 %q",
				line, fields[1],
			)
		}

		inventory[version] = FileDigest{Name: fields[2], SHA256: fields[1]}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("coremigrations: failed to read lock file: %w", err)
	}

	return inventory, nil
}
