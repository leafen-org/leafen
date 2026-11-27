package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A first-setup restore puts its backup together apart from the request
// that asked for it, as a restore asked for in the app does
// (backup_restore_background.go; 2026-11-22: a large backup coming down from
// WebDAV or S3 was cut off when the screen stopped waiting for the answer
// after 30 seconds). The request answers once the backup is together, or
// after restoreAnswerWait with its progress and a watch the screen follows
// it by (POST /setup/restore/progress) to the restart, or to why it stopped.

const setupRestoreWatchStoreKey = "leafenSetupRestoreWatch"

// setupRestoreWatch is the restore a screen follows: by a token its request
// was answered with, until it is together or stopped.
type setupRestoreWatch struct {
	mu      sync.Mutex
	token   string
	done    bool
	failure *setupRestoreFailure
}

// setupRestoreFailure is why a first-setup restore stopped, as its request
// would have answered: the code, the message and what the place or the
// system said.
type setupRestoreFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
	// Its common cause (backup_failure_reason.go).
	Cause string `json:"cause,omitempty"`
}

type setupRestoreProgressInput struct {
	Watch string `json:"watch"`
}

// setupRestoreProgressAnswer is how far the restore has got: its progress
// while it goes; done once it waits for the server to start again; or why it
// stopped.
type setupRestoreProgressAnswer struct {
	Progress *administratorBackupProgress `json:"progress"`
	Done     bool                         `json:"done"`
	Failure  *setupRestoreFailure         `json:"failure,omitempty"`
}

// requestSetupRestoreApart restores what the screen chose apart from its
// request, the store already open, and starts the server again on it
// (stageSetupRestore).
func requestSetupRestoreApart(event *core.RequestEvent, input setupRestoreInput, place storedScheduledBackup, store scheduledBackupStore) error {
	app := event.App
	// The pass this browser signs in with, kept beside the data once the
	// restore is together (setup_restore_sign_in.go).
	pass, err := newSetupRestoreSignInPass()
	if err != nil {
		app.Logger().Warn("leafen setup restore sign-in pass failed", "error", err)
		pass = ""
	}
	token, err := newSetupRestoreSignInPass()
	if err != nil {
		closeScheduledBackupStore(store)
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't restore the backup.", err)
	}
	watch := &setupRestoreWatch{token: token}
	app.Store().Set(setupRestoreWatchStoreKey, watch)
	restart := restoreRestartMode(app)
	var stageErr error
	return runApartFromRequest(func() {
		stageErr = stageSetupRestore(context.Background(), app, input, place, store)
		closeScheduledBackupStore(store)
		if stageErr == nil && pass != "" {
			if err := leaveSetupRestoreSignIn(app.DataDir(), mcpTokenHash(pass), time.Now()); err != nil {
				// The restore goes ahead; the browser signs in from a device instead.
				app.Logger().Warn("leafen setup restore sign-in pass failed", "error", err)
			}
		}
		watch.mu.Lock()
		watch.done, watch.failure = stageErr == nil, setupRestoreFailureOf(stageErr)
		watch.mu.Unlock()
	}, func() error {
		if stageErr != nil {
			return answerSetupRestoreError(event, stageErr)
		}
		event.Response.Header().Set("Cache-Control", "no-store")
		if err := event.JSON(http.StatusAccepted, setupRestoreResult{Restart: restart, SignIn: pass}); err != nil {
			return err
		}
		restartAfterSetupRestore(app, restart)
		return nil
	}, func() error {
		event.Response.Header().Set("Cache-Control", "no-store")
		return event.JSON(http.StatusAccepted, setupRestoreResult{Restart: restart, SignIn: pass, Watch: token,
			Progress: runningBackupProgress(app, backupProgressStoreKey).snapshot()})
	}, func() {
		if stageErr == nil {
			restartAfterSetupRestore(app, restart)
		}
	})
}

// setupRestoreFailureOf is a stopped restore as its request would answer it.
func setupRestoreFailureOf(err error) *setupRestoreFailure {
	if err == nil {
		return nil
	}
	failure := &setupRestoreFailure{Code: errorCodeScheduledBackupRestoreFailed, Message: "Couldn't restore the backup."}
	var refusal *setupRestoreRefusal
	if errors.As(err, &refusal) {
		failure.Code, failure.Message = refusal.code, refusal.message
		err = refusal.cause
	}
	failure.Reason, failure.Cause = backupFailureReason(err), backupFailureCause(err)
	return failure
}

// readSetupRestoreProgress answers the screen that holds the restore's
// watch with how far it has got. It needs no setup code: the watch came
// with the answer to the request the setup code let through.
func readSetupRestoreProgress(event *core.RequestEvent) error {
	input := setupRestoreProgressInput{}
	if err := event.BindBody(&input); err != nil || input.Watch == "" {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "Send the restore's watch.")
	}
	watch, _ := event.App.Store().Get(setupRestoreWatchStoreKey).(*setupRestoreWatch)
	if watch == nil || subtle.ConstantTimeCompare([]byte(watch.token), []byte(input.Watch)) != 1 {
		return apiError(event, http.StatusNotFound, errorCodeScheduledBackupSnapshotUnavailable, "There's no restore to follow.")
	}
	watch.mu.Lock()
	answer := setupRestoreProgressAnswer{Done: watch.done, Failure: watch.failure}
	watch.mu.Unlock()
	if !answer.Done && answer.Failure == nil {
		answer.Progress = runningBackupProgress(event.App, backupProgressStoreKey).snapshot()
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, answer)
}
