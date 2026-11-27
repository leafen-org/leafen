package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	sqliteDriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type sqliteOperationalClass string

const (
	sqliteOperationalNone        sqliteOperationalClass = ""
	sqliteOperationalBusy        sqliteOperationalClass = "busy"
	sqliteOperationalUnavailable sqliteOperationalClass = "unavailable"
)

func sqliteOperationalErrorClass(err error) sqliteOperationalClass {
	// A write that waited out its time for the write connection is busy as
	// one that waited out SQLite's for the lock (sqlite_write_gate.go), and
	// so is one that found a page's stream of updates at its backlog's end
	// (ygoMaximumPendingUpdates): its compaction, woken by every update
	// (notifyYgoCompactionAfterCommit), takes them a moment later, the write
	// kept nothing, and the same write goes through when sent again — every
	// answer that tells busy says so (2026-11-18: a page made under a page
	// whose backlog was full was answered 500 LEAFEN_PAGE_CREATE_FAILED).
	if isSQLiteBaseCode(err, sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED) || waitedOutForWriteConnection(err) ||
		ygoBacklogFull(err) {
		return sqliteOperationalBusy
	}
	if isSQLiteBaseCode(
		err,
		sqlite3.SQLITE_CANTOPEN,
		sqlite3.SQLITE_FULL,
		sqlite3.SQLITE_IOERR,
		sqlite3.SQLITE_READONLY,
	) {
		return sqliteOperationalUnavailable
	}
	return sqliteOperationalNone
}

// ygoBacklogFull says the error is, or carries, a page's stream of updates
// refusing one more (appendYgoStreamUpdateIfChanged), however the callers on
// the way wrapped it: in their words, or as the cause a failure told in its
// kind's words keeps (failedBecause).
func ygoBacklogFull(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), errorCodeYgoPendingBacklogFull) {
		return true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, inner := range wrapped.Unwrap() {
			if ygoBacklogFull(inner) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return ygoBacklogFull(wrapped.Unwrap())
	}
	return false
}

func isSQLiteBusyError(err error) bool {
	return sqliteOperationalErrorClass(err) == sqliteOperationalBusy
}

func isSQLiteStorageUnavailableError(err error) bool {
	return sqliteOperationalErrorClass(err) == sqliteOperationalUnavailable
}

func isSQLiteConstraintError(err error) bool {
	return isSQLiteBaseCode(err, sqlite3.SQLITE_CONSTRAINT)
}

func isSQLiteBaseCode(err error, codes ...int32) bool {
	var sqliteError *sqliteDriver.Error
	if errors.As(err, &sqliteError) {
		baseCode := sqliteError.Code() & 0xff
		for _, code := range codes {
			if baseCode == int(code) {
				return true
			}
		}
	}
	var combined dbx.Errors
	if !errors.As(err, &combined) {
		return false
	}
	for _, child := range combined {
		if isSQLiteBaseCode(child, codes...) {
			return true
		}
	}
	return false
}
