package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	// The server starts itself again where it can (a server on Linux or
	// macOS); an app's own server, and one on Windows, restores on the next
	// start the person makes.
	restoreRestartAutomatic = "automatic"
	restoreRestartManual    = "manual"
	restoreRestartDelay     = time.Second
)

// restartForRestore starts the server again to finish a restore; tests put
// a recorder in its place.
var restartForRestore = func(app core.App) error { return app.Restart() }

func registerScheduledBackupRestoreRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/backups/schedule/snapshots", listScheduledBackupSnapshots)
	event.Router.POST(apiRoot+"/admin/backups/schedule/restore", requestScheduledBackupRestore)
	event.Router.DELETE(apiRoot+"/admin/backups/schedule/restore", cancelScheduledBackupRestore)
	event.Router.POST(apiRoot+"/admin/backups/schedule/restore/undo", requestScheduledBackupRestoreUndo)
}

// restoreRestartMode says whether the server can start itself again.
func restoreRestartMode(app core.App) string {
	if runtime.GOOS == "windows" || app.Store().Has(localOwnerStoreKey) {
		return restoreRestartManual
	}
	return restoreRestartAutomatic
}

// listScheduledBackupSnapshots lists the backup folder's whole snapshots,
// newest first: when each was made, how much it holds, and whether it is
// sealed and opens with the schedule's password.
func listScheduledBackupSnapshots(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	schedule, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	list := administratorScheduledBackupSnapshotList{Snapshots: []administratorScheduledBackupSnapshot{}}
	if !schedule.targetSet() {
		return event.JSON(http.StatusOK, list)
	}
	secret, err := scheduledBackupSecretOf(event.App, schedule)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	// Why the backups can't be read says what the place answered
	// (backup_failure_reason.go).
	secrets := scheduledBackupSecrets(event.App, schedule)
	store, err := openScheduledBackupStore(event.Request.Context(), event.App, schedule)
	if err != nil {
		apiErrorBecause(event, backupFailureReason(err, secrets...), backupFailureCause(err))
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable,
			"Couldn't read the backups: "+backupFailureReason(err, secrets...)+".", err)
	}
	defer closeScheduledBackupStore(store)
	snapshots, err := listBackupSnapshotsIn(store, secret)
	if err != nil {
		apiErrorBecause(event, backupFailureReason(err, secrets...), backupFailureCause(err))
		return apiErrorFrom(event, http.StatusServiceUnavailable, snapshotListErrorCode(err), "Couldn't read the backups: "+backupFailureReason(err, secrets...)+".", err)
	}
	list.Snapshots = snapshots
	return event.JSON(http.StatusOK, list)
}

// listBackupSnapshotsIn lists a place's whole snapshots, newest first: when
// each was made, how much it holds, and whether it is sealed and opens with
// the secret given. A place with no backups yet lists none.
func listBackupSnapshotsIn(store scheduledBackupStore, secret scheduledBackupSecret) ([]administratorScheduledBackupSnapshot, error) {
	snapshots := []administratorScheduledBackupSnapshot{}
	root := scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupSnapshotsName)
	entries, err := store.List(root)
	if errors.Is(err, fs.ErrNotExist) {
		return snapshots, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		path := scheduledBackupStoreJoin(root, entry.Name)
		if !entry.Dir || !restoreSnapshotNameValid(entry.Name) || !scheduledBackupManifestPresent(store, path) {
			continue
		}
		item := administratorScheduledBackupSnapshot{Name: entry.Name}
		item.Sealed = !scheduledBackupStoreExists(store, scheduledBackupStoreJoin(path, scheduledBackupManifestName))
		if source, err := openScheduledBackupSnapshotWith(store, scheduledBackupRootName, path, "", secret); err == nil {
			item.Readable, item.CreatedAt, item.Files = true, source.manifest.CreatedAt, len(source.manifest.Files)
			item.Bytes = source.manifest.Database.Bytes
			for _, file := range source.manifest.Files {
				item.Bytes += file.Bytes
			}
		}
		snapshots = append(snapshots, item)
	}
	sort.Slice(snapshots, func(left, right int) bool { return snapshots[left].Name > snapshots[right].Name })
	return snapshots, nil
}

// snapshotListErrorCode: a place that answers oddly is one whose backups
// can't be read now, as one that doesn't answer.
func snapshotListErrorCode(err error) string {
	code := scheduledBackupErrorCode(err)
	if code == errorCodeScheduledBackupFailed {
		code = errorCodeScheduledBackupFolderUnavailable
	}
	return code
}

// requestScheduledBackupRestore puts the chosen snapshot together beside
// the data and leaves the note the next start follows, then starts the
// server again where it can.
func requestScheduledBackupRestore(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorScheduledBackupRestoreInput{}
	if err := event.BindBody(&input); err != nil || !restoreSnapshotNameValid(strings.TrimSpace(input.Snapshot)) {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "Choose a backup to restore.")
	}
	schedule, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if !schedule.targetSet() {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "Choose where backups go first.")
	}
	work := restoreWorkDir(event.App.DataDir())
	if pending, _, err := readRestoreStatus(event.App.DataDir()); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't read the restore state.", err)
	} else if pending != nil {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupRestorePending, "A restore waits for the server to start again.")
	}
	// One backup or restore at a time, with PocketBase's own as well; the
	// restore holds it until it ends, past the answer if need be.
	if event.App.Store().Has(core.StoreKeyActiveBackup) {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupBusy, "A backup or restore is running.")
	}
	event.App.Store().Set(core.StoreKeyActiveBackup, "leafen-restore")
	handed := false
	defer func() {
		if !handed {
			event.App.Store().Remove(core.StoreKeyActiveBackup)
		}
	}()
	secret, err := scheduledBackupSecretOf(event.App, schedule)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	name := strings.TrimSpace(input.Snapshot)
	// Read apart from the request, which may be answered first.
	store, err := openScheduledBackupStore(context.WithoutCancel(event.Request.Context()), event.App, schedule)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't read the backups.", err)
	}
	defer func() {
		if !handed {
			closeScheduledBackupStore(store)
		}
	}()
	source, err := openScheduledBackupSnapshotWith(store, scheduledBackupRootName,
		scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupSnapshotsName, name), "", secret)
	if err != nil {
		return apiError(event, http.StatusNotFound, errorCodeScheduledBackupSnapshotUnavailable, "That backup can't be opened.")
	}
	// A key's call asks its person once the backup opens
	// (user_key_acting_ask.go); whether it holds what its list says shows
	// only as it is put together.
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	// Put together apart from the request, showing how far it has got as a
	// backup does (backup_restore_background.go).
	handed = true
	return runRestoreAnswering(event, stagedRestore{
		operation: "scheduled-backup",
		stage: func(_ context.Context, progress *backupProgress) error {
			source.progress = progress
			return stageRestore(work, source, name, time.Now())
		},
		release: func() { closeScheduledBackupStore(store) },
		codeOf:  snapshotRestoreFailureCode,
		secrets: scheduledBackupSecrets(event.App, schedule),
		answer: func(err error, reason string) error {
			if err == nil {
				return answerRestoreRequest(event, schedule)
			}
			var refused restoreRefused
			message := "Couldn't put the backup together"
			switch code := snapshotRestoreFailureCode(err); {
			case errors.As(err, &refused):
				// The refusal says why itself.
				message, reason = "That backup can't be restored here: "+refused.message, ""
			case code == errorCodeScheduledBackupSnapshotUnavailable:
				message = "That backup doesn't hold what its list says"
			}
			return answerRestoreFailure(event, snapshotRestoreFailureCode(err), message, reason)
		},
	})
}

// snapshotRestoreFailureCode is why a snapshot couldn't be put together, as
// its request would have answered.
func snapshotRestoreFailureCode(err error) string {
	var refused restoreRefused
	if errors.As(err, &refused) {
		return refused.codeOr(errorCodeScheduledBackupSnapshotUnavailable)
	}
	if code := restorePlaceFailureCode(err); code != "" {
		return code
	}
	if errors.Is(err, errRestoreAssemble) {
		return errorCodeScheduledBackupSnapshotUnavailable
	}
	return errorCodeScheduledBackupRestoreFailed
}

// errRestoreAssemble is a backup that doesn't hold what its list says.
var errRestoreAssemble = errors.New("the backup doesn't hold what its list says")

// stageRestore puts a snapshot together in the restore's work folder and
// leaves the note the next start follows: the data held now goes aside, kept
// to undo the restore, and the snapshot's comes in. prepare, if given, works
// on the put-together data before the note is left.
func stageRestore(work string, source scheduledBackupSnapshotSource, name string, now time.Time, prepare ...func(staged string) error) error {
	staged := filepath.Join(work, restoreStagedName)
	if err := errors.Join(os.RemoveAll(staged), os.MkdirAll(staged, 0o700)); err != nil {
		return err
	}
	if _, err := source.assemble(staged); err != nil {
		_ = os.RemoveAll(staged)
		return errors.Join(errRestoreAssemble, err)
	}
	source.progress.step(backupProgressStepFinishing, 0, 0)
	if err := stagedRestoreRefusal(staged); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	// It comes in knowing it is the backup it was restored from
	// (restored_last_backup.go).
	if err := noteRestoredBackup(filepath.Join(staged, dataDatabaseFile), source, name); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	for _, step := range prepare {
		if err := step(staged); err != nil {
			_ = os.RemoveAll(staged)
			return err
		}
	}
	if err := leaveRestoreNote(staged, work, pendingRestore{
		Kind: restoreKindRestore, Phase: restorePhaseReady, Snapshot: name, SHA256: source.manifestSHA256,
		Source: restoreStagedName, Keep: restoreBeforePrefix + restoreStamp(now), RequestedAt: now.UnixMilli(),
	}); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	return nil
}

// requestScheduledBackupRestoreUndo asks the next start to put back the
// data from before the last restore, while it is kept.
func requestScheduledBackupRestoreUndo(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	schedule, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	pending, state, err := readRestoreStatus(event.App.DataDir())
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't read the restore state.", err)
	}
	if pending != nil {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupRestorePending, "A restore waits for the server to start again.")
	}
	if state == nil || state.Before == "" {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupUndoUnavailable, "The data from before the restore is no longer kept.")
	}
	// A key's call asks its person once the data to put back is kept
	// (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	now := time.Now()
	if err := writeRestoreFile(restoreWorkDir(event.App.DataDir()), restorePendingName, pendingRestore{
		Kind: restoreKindUndo, Phase: restorePhaseReady, Snapshot: state.Snapshot,
		Source: state.Before, Keep: restoreUndonePrefix + restoreStamp(now), RequestedAt: now.UnixMilli(),
	}); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't ask for the undo.", err)
	}
	return answerRestoreRequest(event, schedule)
}

// cancelScheduledBackupRestore takes back a restore, or its undoing, the
// server has not started again for yet.
func cancelScheduledBackupRestore(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	schedule, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	work := restoreWorkDir(event.App.DataDir())
	pending, _, err := readRestoreStatus(event.App.DataDir())
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't read the restore state.", err)
	}
	// A key's call asks its person once the restore state reads
	// (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if pending != nil {
		if err := os.Remove(filepath.Join(work, restorePendingName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't undo the restore.", err)
		}
		if pending.Kind == restoreKindRestore {
			_ = os.RemoveAll(filepath.Join(work, restoreStagedName))
		}
	}
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, schedule)})
}

// answerRestoreRequest answers with the schedule as it now stands and, where
// the server starts itself again, does so once the answer has gone.
func answerRestoreRequest(event *core.RequestEvent, schedule storedScheduledBackup) error {
	projected := projectScheduledBackupWithRestore(event.App, schedule)
	if err := event.JSON(http.StatusAccepted, administratorScheduledBackupResult{Schedule: projected}); err != nil {
		return err
	}
	if projected.RestoreRestart == restoreRestartAutomatic {
		app := event.App
		time.AfterFunc(restoreRestartDelay, func() {
			if err := restartForRestore(app); err != nil {
				logDurableJobDiagnostic(app, errorCodeScheduledBackupRestoreFailed, newDiagnosticReference(), "",
					"scheduled-backup", "restore-restart", err)
			}
		})
	}
	return nil
}

// projectScheduledBackupWithRestore is the schedule with what the screen
// shows of restores: one waiting for the next start, and the last one with
// how long it can be undone.
func projectScheduledBackupWithRestore(app core.App, stored storedScheduledBackup) administratorScheduledBackup {
	projected := projectScheduledBackup(stored)
	// What runs now, as far as it has got (backup_progress.go), and a
	// restore that stopped after its request was answered.
	projected.Progress = runningBackupProgress(app, backupProgressStoreKey).snapshot()
	projected.LastRestore = lastRestoreOf(app)
	projected.TurnOnSuggested = scheduledBackupTurnOnSuggested(app, stored, time.Now())
	projected.Replication = projectReplication(app, stored)
	projected.Standby = projectStandby(app, stored)
	pending, state, err := readRestoreStatus(app.DataDir())
	if err != nil {
		return projected
	}
	projected.RestoreRestart = restoreRestartMode(app)
	if pending != nil {
		projected.RestorePending, projected.RestorePendingSnapshot = pending.Kind, pending.Snapshot
	} else if failure := readRestoreFailure(app.DataDir()); failure != nil {
		// One that couldn't finish at the last start (restore_pending_broken.go).
		projected.RestoreFailed, projected.RestoreFailedSnapshot = failure.Kind, failure.Snapshot
		projected.RestoreFailedAt = time.UnixMilli(failure.FailedAt).UTC().Format(time.RFC3339)
	}
	if state != nil {
		restoredAt := time.UnixMilli(state.RestoredAt).UTC()
		projected.RestoredAt, projected.RestoredSnapshot = restoredAt.Format(time.RFC3339), state.Snapshot
		if state.Before != "" {
			projected.RestoreUndoUntil = restoredAt.Add(restoreKeepFor).Format(time.RFC3339)
		}
	}
	return projected
}

// releaseRestoreKeepAfterBackup lets go of the data from before a restore
// once a scheduled backup has succeeded after it (user decision 2026-10-03).
func releaseRestoreKeepAfterBackup(app core.App) error {
	_, state, err := readRestoreStatus(app.DataDir())
	if err != nil || state == nil || state.Before == "" {
		return err
	}
	return releaseRestoreKeep(restoreWorkDir(app.DataDir()), *state)
}
