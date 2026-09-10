package migrationstate_test

import (
	"errors"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
)

// A caller choosing an exit code has to tell "this database may not be used
// this way" from "the database could not be read". Documenting the sentinel
// without returning it made that impossible.
func TestRefusalsAreIdentifiableAsRefusals(t *testing.T) {
	report := migrationstate.Report{
		Sources: []migrationstate.SourceState{{Source: "core", State: migrationstate.StateLegacy}},
	}

	err := report.Allows(migrationstate.OpServe, migrationstate.Tolerances{})
	if err == nil {
		t.Fatal("a legacy database must not be admitted for serving")
	}
	if !errors.Is(err, migrationstate.ErrRefused) {
		t.Fatalf("a refusal must match ErrRefused, got %T: %v", err, err)
	}

	var refusal *migrationstate.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("a refusal must carry its operation and state, got %T", err)
	}
	if refusal.Op != migrationstate.OpServe || refusal.DatabaseState != migrationstate.StateLegacy {
		t.Fatalf("refusal describes the wrong situation: %+v", refusal)
	}
	// The operator reads the message before anything else, so the refused
	// operation has to come first and the sentinel must not push it aside.
	if got := err.Error(); got[:len("cannot serve")] != "cannot serve" {
		t.Fatalf("message should open with the refused operation, got: %s", got)
	}
}
