package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A restore asked for in the app puts its backup together for as long as
// that takes, apart from the request (user 2026-11-22): a large backup comes
// down from a WebDAV server or S3 storage far slower than a screen waits for
// an answer — it gives a write up after 30 seconds, an agent's call or a
// tunnel not much later — and the request given up on took the restore down
// with it. The request answers once the backup is together, or after
// restoreAnswerWait with its progress; then the schedule's status shows how
// far it has got, and after it the restore waiting for the next start (the
// server starting again where it can) or lastRestore, where it stopped and
// why.
const lastRestoreStoreKey = "leafenLastRestore"

// restoreAnswerWait is how long a restore request waits for its backup to be
// put together before answering with its progress.
var restoreAnswerWait = 20 * time.Second

// stagedRestore is one restore asked for: the work, what it holds while it
// runs (released when it ends), the failure's code as the screen tells it,
// and the answer to give while the request still waits.
type stagedRestore struct {
	stage   func(ctx context.Context, progress *backupProgress) error
	release func()
	codeOf  func(err error) string
	// answer is the request's answer, with a failure's reason
	// (backup_failure_reason.go).
	answer func(err error, reason string) error
	// Where its diagnostic says it failed, and the place's secrets a reason
	// never says.
	operation string
	secrets   []string
}

// runRestoreAnswering runs a restore apart from the request that asked for
// it, which holds core.StoreKeyActiveBackup for it. The request answers as
// restore.answer does once the backup is together, or with its progress
// after restoreAnswerWait; one together after that answers no one, and
// success starts the server again where it starts itself. Either way a
// failure stays as lastRestore, with its reason, and in the diagnostic log.
func runRestoreAnswering(event *core.RequestEvent, restore stagedRestore) error {
	app := event.App
	app.Store().Remove(lastRestoreStoreKey)
	progress := startBackupProgress(app, backupProgressStoreKey, backupProgressKindRestore, "")
	var stageErr error
	reason := ""
	return runApartFromRequest(func() {
		stageErr = restore.stage(context.Background(), progress)
		ended := endBackupProgress(app, backupProgressStoreKey, progress)
		restore.release()
		if stageErr != nil {
			ended.ErrorCode, ended.ErrorReason = restore.codeOf(stageErr), backupFailureReason(stageErr, restore.secrets...)
			ended.ErrorCause = backupFailureCause(stageErr)
			reason = ended.ErrorReason
			app.Store().Set(lastRestoreStoreKey, ended)
			logBackupFailure(app, ended.ErrorCode, restore.operation, "restore-assemble", stageErr, ended.ErrorReason)
		}
		app.Store().Remove(core.StoreKeyActiveBackup)
	}, func() error {
		if stageErr != nil {
			apiErrorBecause(event, reason, backupFailureCause(stageErr))
		}
		return restore.answer(stageErr, reason)
	}, func() error {
		schedule, err := loadScheduledBackup(app)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
		}
		return event.JSON(http.StatusAccepted, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(app, schedule)})
	}, func() {
		if stageErr == nil {
			restartWhenRestoreWaits(app, restore.operation)
		}
	})
}

// runApartFromRequest runs work apart from the request waiting on it: answer
// answers once it ends within restoreAnswerWait, later answers when it
// hasn't, and after follows work that ended once the request was answered.
// What work sets is seen by answer and after.
func runApartFromRequest(work func(), answer, later func() error, after func()) error {
	var mu sync.Mutex
	answered := false
	done := make(chan struct{})
	go func() {
		work()
		mu.Lock()
		if !answered {
			close(done)
			mu.Unlock()
			return
		}
		mu.Unlock()
		after()
	}()
	timer := time.NewTimer(restoreAnswerWait)
	defer timer.Stop()
	select {
	case <-done:
		return answer()
	case <-timer.C:
	}
	mu.Lock()
	select {
	case <-done:
		mu.Unlock()
		return answer()
	default:
		answered = true
		mu.Unlock()
	}
	return later()
}

// restartWhenRestoreWaits starts the server again on a restore put together
// after its request was answered, where the server starts itself again.
func restartWhenRestoreWaits(app core.App, operation string) {
	if restoreRestartMode(app) != restoreRestartAutomatic {
		return
	}
	time.AfterFunc(restoreRestartDelay, func() {
		if err := restartForRestore(app); err != nil {
			logDurableJobDiagnostic(app, errorCodeScheduledBackupRestoreFailed, newDiagnosticReference(), "", operation, "restore-restart", err)
		}
	})
}

// lastRestoreOf is the last restore that stopped after its request was
// answered, nil for none since the server started.
func lastRestoreOf(app core.App) *administratorBackupProgress {
	if last, ok := app.Store().Get(lastRestoreStoreKey).(administratorBackupProgress); ok {
		return &last
	}
	return nil
}

// restorePlaceFailureCode is what the place, or this server's disk, said of
// a restore that stopped, where that is the reason: the place not answering,
// its address moved, its credentials refused, no room here. A content
// missing from it is the backup's own, not the place's.
func restorePlaceFailureCode(err error) string {
	switch code := scheduledBackupErrorCode(err); code {
	case errorCodeScheduledBackupPlaceUnreachable, errorCodeScheduledBackupPlaceMoved, errorCodeScheduledBackupWebDAVUnauthorized,
		errorCodeScheduledBackupS3Unauthorized, errorCodeScheduledBackupNoSpace:
		return code
	}
	return ""
}

// answerRestoreFailure answers a restore that stopped before its request
// was answered, with its reason after its message.
func answerRestoreFailure(event *core.RequestEvent, code, message, reason string) error {
	status := http.StatusUnprocessableEntity
	switch code {
	case errorCodeScheduledBackupPlaceUnreachable, errorCodeScheduledBackupPlaceMoved, errorCodeScheduledBackupWebDAVUnauthorized,
		errorCodeScheduledBackupS3Unauthorized, errorCodeScheduledBackupFolderUnavailable:
		status, message = http.StatusServiceUnavailable, "Couldn't read the backup from its place"
	case errorCodeScheduledBackupNoSpace:
		status, message = http.StatusInsufficientStorage, "There's no room on this server to put the backup together"
	case errorCodeScheduledBackupRestoreFailed:
		status = http.StatusInternalServerError
	}
	if reason != "" {
		message += ": " + reason
	}
	return apiError(event, status, code, message+".")
}
