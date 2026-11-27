package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// errorCodeRunnerDatabaseBusy names a background runner's turn that found
// the database's writes held — by another program, a backup or a long
// write — and left what it had to do where it was for its next turn: a
// wait, not the runner's failure (2026-11-11: the reminder runner said
// "LEAFEN_NOTIFICATION_DELIVERY_FAILED" while another program held the
// write lock, and its reminders went a minute after the lock let go).
const errorCodeRunnerDatabaseBusy = "LEAFEN_RUNNER_DATABASE_BUSY"

// runnerTurnBusyDelay is how soon a turn that found the database busy comes
// back, as a durable job whose turn found it so does.
const runnerTurnBusyDelay = durableJobStoreRetryDelay

// onlyDatabaseBusy reports whether everything that stopped a turn was the
// database being busy; a turn that met anything else failed.
func onlyDatabaseBusy(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		for _, child := range children {
			if child != nil && !onlyDatabaseBusy(child) {
				return false
			}
		}
		return len(children) > 0
	}
	return isSQLiteBusyError(err)
}

// onlyCalledOff reports whether everything that stopped a turn or a job was
// its work being called off: the server's shutdown cancels every background
// runner's context, and what the work had left stays for the next start. It
// is no failure (2026-11-17: a server stopped within a second of its start
// logged LEAFEN_NOTIFICATION_FANOUT_RETENTION_FAILED at error). A turn that
// met anything else besides — a time limit included — failed.
func onlyCalledOff(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		called := false
		for _, child := range joined.Unwrap() {
			if child == nil {
				continue
			}
			if !onlyCalledOff(child) {
				return false
			}
			called = true
		}
		return called
	}
	if own, ok := err.(interface{ Is(error) bool }); ok && own.Is(context.Canceled) {
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return onlyCalledOff(wrapped)
	}
	return err == context.Canceled
}

// logRunnerTurnError says what stopped a background runner's turn: under the
// runner's own failure code, or — when the database was only busy — as
// logRunnerTurnWait does. It reports whether the turn only waited.
func logRunnerTurnError(app core.App, failureCode, operation string, err error) bool {
	if runnerTurnWaited(app, operation, err, diagnosticSourcePosition(1)) {
		return true
	}
	logDurableJobDiagnostic(app, failureCode, newDiagnosticReference(), "", operation, "runner-turn", err)
	return false
}

// logRunnerTurnWait says a turn that found the database only busy waited, as
// a warning under errorCodeRunnerDatabaseBusy with the busy cause: what it
// had to do stays where it was for its next turn. It reports false, saying
// nothing, for a turn anything else stopped; the runner says that its own way.
func logRunnerTurnWait(app core.App, operation string, err error) bool {
	return runnerTurnWaited(app, operation, err, diagnosticSourcePosition(1))
}

// runnerTurnWaited is logRunnerTurnWait with the runner's place in its source.
func runnerTurnWaited(app core.App, operation string, err error, sourcePosition string) bool {
	if !onlyDatabaseBusy(err) {
		return false
	}
	defer func() {
		_ = recover()
	}()
	if app == nil {
		return true
	}
	attributes := []any{
		"diagnosticReference", newDiagnosticReference(),
		"code", errorCodeRunnerDatabaseBusy,
		"operation", safeDiagnosticToken(operation),
		"stage", "runner-turn",
		"sourcePosition", sourcePosition,
		"causeCode", persistentWriteDatabaseBusyCauseCode,
		"causeClass", fmt.Sprintf("%T", err),
	}
	app.Logger().Warn("leafen background turn waited", attributes...)
	if !app.IsDev() {
		productionDiagnosticLogger.Warn("leafen background turn waited", attributes...)
	}
	return true
}
