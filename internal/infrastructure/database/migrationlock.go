package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pressly/goose/v3/lock"
)

// migrationLockID is the advisory lock every migration operation contends
// for. It is a fixed constant rather than a derived value so two builds of
// this module always pick the same one, and it is deliberately not goose's
// DefaultLockID: a tool bypassing this package should not appear to be
// holding our lock.
const migrationLockID int64 = 5_712_004_311

// guardedLocker runs a check on the connection goose is about to migrate on,
// while the lock is held and before goose touches anything.
//
// This is the only place such a check can go. goose acquires its session lock
// inside Provider initialization and then immediately ensures the history
// table exists, so a check performed before calling goose races with other
// runners, and one performed after is too late — the table it was supposed to
// notice the absence of already exists. SessionLock is the hook in between:
// goose calls it on the connection it will use, before the version table is
// touched, and treats an error from it as a reason to abandon the run.
type guardedLocker struct {
	inner lock.SessionLocker
	guard func(context.Context, *sql.Conn) error
}

func (g *guardedLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	if err := g.inner.SessionLock(ctx, conn); err != nil {
		return err
	}
	if err := g.guard(ctx, conn); err != nil {
		// Release before reporting: goose only unlocks through the cleanup it
		// installs after SessionLock returns successfully, so a lock taken
		// here and abandoned would be held until the connection is discarded.
		// The unlock runs on a detached context so a canceled run still
		// releases.
		return errors.Join(err, g.inner.SessionUnlock(context.WithoutCancel(ctx), conn))
	}
	return nil
}

func (g *guardedLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	return g.inner.SessionUnlock(ctx, conn)
}

// newGuardedLocker builds the locker goose runs migrations under.
//
// The wait is expressed as goose's probe interval and failure threshold
// rather than a deadline, so it is bounded but does not abort a run that is
// merely queued behind a long migration.
func newGuardedLocker(wait time.Duration, guard func(context.Context, *sql.Conn) error) (lock.SessionLocker, error) {
	const probe = 2 * time.Second

	attempts := uint64(wait / probe)
	if attempts < 1 {
		attempts = 1
	}

	inner, err := lock.NewPostgresSessionLocker(
		lock.WithLockID(migrationLockID),
		lock.WithLockTimeout(uint64(probe/time.Second), attempts),
		lock.WithUnlockTimeout(uint64(probe/time.Second), attempts),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build the migration locker: %w", err)
	}
	return &guardedLocker{inner: inner, guard: guard}, nil
}

// errLockUnavailable is returned when the migration lock could not be taken
// within the configured wait.
var errLockUnavailable = errors.New("another migration run holds the migration lock")

// withBoundedReadLock runs fn inside a read-only transaction holding the
// migration lock, giving up rather than queueing indefinitely.
//
// The transaction-scoped lock is deliberate: it is released by the commit or
// rollback, so there is no lifecycle to get wrong and nothing to leak if the
// caller's context is canceled mid-check. The bound matters because this runs
// during application startup — an unbounded wait would turn a long migration
// somewhere else into an indefinite outage here, which is the failure the
// timeouts exist to convert into a clear message.
func withBoundedReadLock(
	ctx context.Context,
	db *sql.DB,
	wait time.Duration,
	statementTimeout time.Duration,
	fn func(context.Context, *sql.Tx) error,
) error {
	deadline, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	const retryInterval = 250 * time.Millisecond
	for {
		err := tryBoundedReadLock(deadline, db, statementTimeout, fn)
		if !errors.Is(err, errLockUnavailable) {
			return err
		}

		select {
		case <-deadline.Done():
			return fmt.Errorf(
				"%w: gave up after %s. A migration is probably running elsewhere; "+
					"retry once it finishes",
				errLockUnavailable, wait,
			)
		case <-time.After(retryInterval):
		}
	}
}

func tryBoundedReadLock(
	ctx context.Context,
	db *sql.DB,
	statementTimeout time.Duration,
	fn func(context.Context, *sql.Tx) error,
) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("failed to begin the migration state transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only; rollback is the normal exit

	if err := setTimeout(ctx, tx, "statement_timeout", statementTimeout, scopeTransaction); err != nil {
		return err
	}

	var acquired bool
	if err := tx.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock($1)`, migrationLockID,
	).Scan(&acquired); err != nil {
		return fmt.Errorf("failed to request the migration lock: %w", err)
	}
	if !acquired {
		return errLockUnavailable
	}

	return fn(ctx, tx)
}

// setTimeout bounds how long a single statement may run. The migration lock
// bounds waiting for a turn; this bounds the turn itself, and they are
// different failures with different fixes.
//
// The scope argument is not a detail. PostgreSQL treats SET LOCAL outside a
// transaction as a warning and ignores it, so a session-scoped caller that
// used SET LOCAL would configure nothing and never find out — the timeout it
// believed it had set would simply never fire, while the run held the
// migration lock.
func setTimeout(ctx context.Context, ex executor, setting string, timeout time.Duration, scope timeoutScope) error {
	if timeout <= 0 {
		return nil
	}
	// Neither SET nor SET LOCAL takes parameters, so the value is formatted
	// in; it is a duration from configuration, rendered as whole milliseconds.
	statement := fmt.Sprintf("%s %s = %d", scope, setting, timeout.Milliseconds())
	if _, err := ex.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("failed to set %s: %w", setting, err)
	}
	return nil
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// timeoutScope picks between a session setting and a transaction-local one.
type timeoutScope string

const (
	// scopeSession applies for the life of the connection. Required outside
	// a transaction, where SET LOCAL silently does nothing.
	scopeSession timeoutScope = "SET"
	// scopeTransaction reverts at the end of the current transaction.
	scopeTransaction timeoutScope = "SET LOCAL"
)
