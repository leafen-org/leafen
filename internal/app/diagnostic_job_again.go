package app

import (
	"context"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"
)

// A job's turn that failed in a way that passes is scheduled again, not
// failed, and the server's log says so once below an error: at info when
// Notion asked the connection to wait (its request limit, 429), which costs
// the job nothing, and as a warning for another failure that may pass — Notion
// or the way to it away for a while, the store busy — tried again within the
// job's attempts. "leafen durable job failed" at error is for a job that
// failed (2026-11-16: each time Notion asked a long move to wait, the log said
// at error that the job failed).
func logDurableJobScheduledAgain(
	app core.App, level slog.Level, code, reference, jobID, operation, stage, causeCode string, cause error,
) {
	defer func() {
		_ = recover()
	}()
	if app == nil {
		return
	}
	attributes := durableJobDiagnosticAttributes(
		diagnosticSourcePosition(1), code, reference, jobID, operation, stage, causeCode, cause,
	)
	app.Logger().Log(context.Background(), level, "leafen durable job scheduled again", attributes...)
	if !app.IsDev() {
		productionDiagnosticLogger.Log(context.Background(), level, "leafen durable job scheduled again", attributes...)
	}
}

// notionProviderWaitLevel is how a Notion import's turn scheduled again is
// logged: at info when Notion asked to wait, else as a warning.
func notionProviderWaitLevel(cause error) slog.Level {
	if notionProviderFailureOf(cause, false) == notionProviderAskedToWait {
		return slog.LevelInfo
	}
	return slog.LevelWarn
}
