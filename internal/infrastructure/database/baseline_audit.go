package database

import (
	"context"
	"database/sql"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
)

// The audit table is not bookkeeping. Classification reads it to tell a
// converted database from one being written by two different migrators: both
// have a populated legacy history and a populated separated one, and the only
// thing distinguishing them is a record that somebody performed the
// conversion deliberately.
//
// It also carries what the conversion could not establish — a forced
// mismatch, an unverified source, versions no fingerprint can confirm — so
// the next person to look at this database can see what was accepted rather
// than having to assume it was clean.

func auditTable(schema string) string {
	return migrationstate.Qualify(schema, migrationstate.BaselineAuditTable)
}

type auditRow struct {
	Source       string
	Table        string
	Versions     []int64
	LegacyTable  string
	CoreTarget   int64
	Forced       bool
	Unverified   bool
	Differences  int
	Unverifiable int
	RecordedAt   time.Time
}

func ensureAuditTable(ctx context.Context, tx *sql.Tx, schema string) error {
	// Created here rather than by a migration: it has to exist before the
	// history it explains, and a migration recording its own prerequisite
	// would be circular.
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+auditTable(schema)+` (
			id             BIGSERIAL PRIMARY KEY,
			source         TEXT        NOT NULL,
			history_table  TEXT        NOT NULL,
			versions       TEXT        NOT NULL,
			legacy_table   TEXT        NOT NULL,
			core_target    BIGINT      NOT NULL,
			forced         BOOLEAN     NOT NULL,
			unverified     BOOLEAN     NOT NULL,
			differences    INTEGER     NOT NULL,
			unverifiable   INTEGER     NOT NULL,
			tool_version   TEXT        NOT NULL,
			recorded_at    TIMESTAMPTZ NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", auditTable(schema), err)
	}
	return nil
}

func recordAudit(ctx context.Context, tx *sql.Tx, schema string, row auditRow) error {
	// The table name is derived from a validated schema name and a package
	// constant, never from caller input; the values are all parameters.
	//nolint:gosec // G202: the only interpolated part is the qualified table name
	_, err := tx.ExecContext(ctx, `
		INSERT INTO `+auditTable(schema)+`
			(source, history_table, versions, legacy_table, core_target,
			 forced, unverified, differences, unverifiable, tool_version, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		row.Source, row.Table, formatVersionList(row.Versions), row.LegacyTable, row.CoreTarget,
		row.Forced, row.Unverified, row.Differences, row.Unverifiable, toolVersion(), row.RecordedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record the baseline of %q: %w", row.Source, err)
	}
	return nil
}

// formatVersionList renders a run of versions as a range so the audit row
// stays readable for a sixteen-migration history.
func formatVersionList(versions []int64) string {
	if len(versions) == 0 {
		return ""
	}
	if len(versions) > 1 && versions[len(versions)-1]-versions[0] == int64(len(versions)-1) {
		return strconv.FormatInt(versions[0], 10) + "-" + strconv.FormatInt(versions[len(versions)-1], 10)
	}

	parts := make([]string, 0, len(versions))
	for _, version := range versions {
		parts = append(parts, strconv.FormatInt(version, 10))
	}
	return strings.Join(parts, ",")
}

// toolVersion records which build performed the conversion, read from the
// build info rather than a constant so it cannot go stale. A binary built
// without VCS stamping reports "unknown" rather than something misleading.
func toolVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value
		}
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "unknown"
}
