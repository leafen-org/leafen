package app

import (
	"database/sql"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	sqliteDriver "modernc.org/sqlite"
)

// A read or a check that decides a refusal — not found, invalid, changed
// meanwhile, not one this person reaches — decides it by what it found: no
// row, a row that differs, a constraint the database kept. When what stopped
// it was the database itself — busy, locked, unable to open, full, any other
// SQLite failure, or a write that waited out its turn for the write
// connection — it found nothing, and telling the refusal says what isn't so
// (2026-11-18: a read the database failed was told as a 409 "Couldn't read
// the source block.", a 404 or "changed meanwhile"; a busy database never
// reached the busy answer). Such a failure goes on as itself, its cause
// kept, and is answered as the server's failure — told busy when the
// database stayed busy (api_error_busy.go) — while a real refusal keeps its
// words.

// storageFailure is whether err is the database failing rather than telling
// what it found.
func storageFailure(err error) bool {
	if err == nil || errors.Is(err, sql.ErrNoRows) || isSQLiteConstraintError(err) {
		return false
	}
	if waitedOutForWriteConnection(err) {
		return true
	}
	var sqliteError *sqliteDriver.Error
	if errors.As(err, &sqliteError) {
		return true
	}
	var combined dbx.Errors
	return errors.As(err, &combined) && slices.ContainsFunc(combined, storageFailure)
}

// refusedUnlessStorage is refusal for a read or check err stopped, unless
// the database failed it: then err itself, a failure.
func refusedUnlessStorage(refusal, err error) error {
	if storageFailure(err) {
		return err
	}
	return refusal
}

// mcpRefusedUnlessStorage is refusedUnlessStorage for an MCP tool: the
// database's failure is the tool's (errMCPToolFailed) with its cause, which
// mcpToolReasons words busy when the database stayed busy.
func mcpRefusedUnlessStorage(refusal, err error) error {
	if storageFailure(err) {
		return failedBecause(errMCPToolFailed, err)
	}
	return refusal
}
