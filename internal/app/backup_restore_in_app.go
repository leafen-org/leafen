package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/osutils"
)

// Restoring a folder backup from the app (user decision 2026-10-03): the
// backup replaces what the server holds now, and what it held stays aside so
// the restore can be undone. The running server only puts the backup together
// and leaves a note; the next start, before the database opens, moves the
// current data aside and the restored data in. The work happens in the data
// folder's own backups folder: the same file system as the data, so every
// move is a rename even when the data folder is a mounted volume, and a place
// neither kind of backup takes.
const (
	restoreWorkDirName   = ".leafen-restore"
	restorePendingName   = "pending.json"
	restoreStateName     = "restored.json"
	restoreStagedName    = "staged"
	restoreBeforePrefix  = "before-"
	restoreUndonePrefix  = "undone-"
	restoreKindRestore   = "restore"
	restoreKindUndo      = "undo"
	restorePhaseReady    = "ready"
	restorePhaseMovingIn = "moving-in"
	// How long the data from before a restore stays to undo it, unless the
	// next scheduled backup succeeds first.
	restoreKeepFor = 7 * 24 * time.Hour
)

// restoreStaysPut is what belongs to this machine and stays through a
// restore: its downloaded program, the desktop app's updates and window, and
// its replica mark, or the note that its data moved elsewhere, the note
// of a move it took in (server_move.go), and the lock the serving process
// holds (server_data_lock.go), which moved aside would let a second take a
// new one. The replica's working state of the database goes aside with the
// database.
var restoreStaysPut = []string{"bin", "updates", "desktop.json", "window-background", replicaHolderName, replicaFencedName, serverMovedInName,
	serverDataLockName, setupRestoreSignInName}

var restoreFolderPattern = regexp.MustCompile(`^(staged|(before|undone)-[0-9]{13})$`)

// pendingRestore is the note a requested restore, or its undoing, leaves for
// the next start: which folder of the work becomes the data, and where the
// data held now goes.
type pendingRestore struct {
	Kind        string `json:"kind"`
	Phase       string `json:"phase"`
	Snapshot    string `json:"snapshot,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Source      string `json:"source"`
	Keep        string `json:"keep"`
	RequestedAt int64  `json:"requestedAt"`
	// TakesOver is a restore that takes the replica over: this server holds
	// the data again, so the note that its data moved goes with it.
	TakesOver bool `json:"takesOver,omitzero"`
}

// restoredState is what the last restore left: when, from which snapshot,
// and the folder holding the data from before, while it is kept.
type restoredState struct {
	Snapshot   string `json:"snapshot"`
	RestoredAt int64  `json:"restoredAt"`
	Before     string `json:"before"`
}

// registerPendingRestore finishes a restore asked for before the server
// stopped, first of all, before anything opens the data.
func registerPendingRestore(pb *pocketbase.PocketBase) {
	pb.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id:       "leafenPendingRestore",
		Priority: -1000,
		Func: func(event *core.BootstrapEvent) error {
			now := time.Now()
			applied, err := applyPendingRestore(event.App.DataDir(), now)
			if err != nil {
				return fmt.Errorf("finishing the restore asked for before the server stopped: %w", err)
			}
			if err := event.Next(); err != nil {
				return err
			}
			// A restore here, its undo, or one the command line made puts
			// the data in another history: devices that knew this one
			// must not carry it back (data_epoch.go). Its next scheduled
			// backup keeps its time, however it was restored: 2026-11-19 a
			// folder snapshot restored by the command line backed up again
			// the moment its server first started.
			if applied || takeRestoreEpochMarker(event.App.DataDir()) {
				settleScheduledBackupAfterRestore(event.App, now)
				if _, err := renewDataEpoch(event.App, now); err != nil {
					return fmt.Errorf("marking the restored data's new history: %w", err)
				}
				event.App.Store().Set(dataRestoredAtStartStoreKey, true)
			}
			return nil
		},
	})
}

func restoreWorkDir(dataDir string) string {
	return filepath.Join(dataDir, core.LocalBackupsDirName, restoreWorkDirName)
}

func readRestoreFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// writeRestoreFile writes a note whole or not at all: a file beside it,
// synced, then renamed over it.
func writeRestoreFile(work, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeFileDurably(work, name, raw)
}

// writeFileDurably writes a file whole or not at all: a partial file synced,
// then renamed over it, and the folder synced.
func writeFileDurably(work, name string, raw []byte) error {
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	partial := filepath.Join(work, "."+name+".partial")
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(partial, filepath.Join(work, name)); err != nil {
		return err
	}
	return syncPath(work)
}

// applyPendingRestore finishes a restore, or its undoing, that a running
// server asked for: before the database opens it moves the data held now
// into the work folder and the chosen folder's data into its place. Each
// step is a rename, and the note says which step comes next, so a start cut
// off partway finishes on the next one. With nothing asked for it lets go of
// data from before a restore once it is no longer kept.
func applyPendingRestore(dataDir string, now time.Time) (bool, error) {
	work := restoreWorkDir(dataDir)
	pending := pendingRestore{}
	if err := readRestoreFile(filepath.Join(work, restorePendingName), &pending); errors.Is(err, fs.ErrNotExist) {
		return false, expireRestoreKeep(work, now)
	} else if err != nil {
		return false, fmt.Errorf("the restore note can't be read: %w", err)
	}
	if (pending.Kind != restoreKindRestore && pending.Kind != restoreKindUndo) ||
		!restoreFolderPattern.MatchString(pending.Source) || !restoreFolderPattern.MatchString(pending.Keep) ||
		pending.Source == pending.Keep {
		return false, errors.New("the restore note names folders it can't")
	}
	source, keep := filepath.Join(work, pending.Source), filepath.Join(work, pending.Keep)
	// A note over data that isn't all there moves nothing, or puts back
	// what it moved (restore_pending_broken.go).
	if broken, err := settleBrokenRestore(dataDir, work, pending, now); broken || err != nil {
		return false, err
	}
	if pending.Phase == restorePhaseReady {
		if err := os.MkdirAll(keep, 0o700); err != nil {
			return false, err
		}
		// Everything but the backups folder, which holds this work and
		// the server's own backups, and what belongs to this machine,
		// moves aside.
		if err := osutils.MoveDirContent(dataDir, keep, append([]string{core.LocalBackupsDirName}, restoreStaysPut...)...); err != nil {
			return false, fmt.Errorf("moving the current data aside: %w", err)
		}
		if err := errors.Join(syncPath(keep), syncPath(dataDir)); err != nil {
			return false, err
		}
		pending.Phase = restorePhaseMovingIn
		if err := writeRestoreFile(work, restorePendingName, pending); err != nil {
			return false, err
		}
	}
	if pending.Phase != restorePhaseMovingIn {
		return false, errors.New("the restore note names a step it can't")
	}
	// An older snapshot may hold some of what stays put: this machine's
	// own wins.
	for _, name := range restoreStaysPut {
		if err := os.RemoveAll(filepath.Join(source, name)); err != nil {
			return false, err
		}
	}
	// A start cut off once the source was all moved in and taken away
	// finds nothing left to move.
	_, err := os.Stat(source)
	sourceLeft := !errors.Is(err, fs.ErrNotExist)
	if sourceLeft {
		if err := osutils.MoveDirContent(source, dataDir, core.LocalBackupsDirName); err != nil {
			return false, fmt.Errorf("moving the restored data in: %w", err)
		}
	}
	// 2026-11-03 audit: an old primary that took the replica back kept the
	// note, and with its standby fenced too, neither server took changes.
	if pending.TakesOver {
		if err := os.Remove(filepath.Join(dataDir, replicaFencedName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	if err := syncPath(dataDir); err != nil {
		return false, err
	}
	if sourceLeft {
		if err := syncPath(source); err != nil {
			return false, err
		}
	}
	if pending.Kind == restoreKindRestore {
		if err := writeRestoreFile(work, restoreStateName, restoredState{
			Snapshot: pending.Snapshot, RestoredAt: now.UnixMilli(), Before: pending.Keep,
		}); err != nil {
			return false, err
		}
	} else {
		// Undone: the data from before is back, and the restored data goes.
		if err := os.Remove(filepath.Join(work, restoreStateName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		if err := os.RemoveAll(keep); err != nil {
			return false, err
		}
	}
	if err := os.Remove(source); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.Remove(filepath.Join(work, restorePendingName)); err != nil {
		return false, err
	}
	// Done, the word of an earlier one that couldn't finish goes.
	if err := os.Remove(filepath.Join(work, restoreFailedName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, syncPath(work)
}

// settleScheduledBackupAfterRestore keeps the next backup at its time: the
// restored data's schedule was read when its snapshot was taken, partway
// through that backup, and would back up at once — which would let go of
// the data from before the restore right away. Its record of failed runs
// starts anew too, however the data was restored or undone: the count, the
// day's mails (the backups' and the replica's) and the last failure's reason
// it holds were the runs before that backup — one that worked, for a
// snapshot — so the restored data's own next runs tell whether backups fail
// (2026-11-21: a snapshot made right after two failed runs came back with
// the band saying they kept failing). A backup refused for want of
// protection is no run and stays, as it is.
func settleScheduledBackupAfterRestore(app core.App, now time.Time) {
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[running_since]] = 0, [[run_requested]] = 0,
		[[next_run_at]] = {:now} + [[interval_hours]] * 3600000, [[failure_count]] = 0, [[alerted_at]] = 0,
		[[replication_alerted_at]] = 0,
		[[last_error_code]] = CASE WHEN [[last_error_code]] = {:refused} THEN [[last_error_code]] ELSE '' END
		WHERE [[id]] = 'default'`).
		Bind(dbx.Params{"now": now.UnixMilli(), "refused": errorCodeBackupProtectionRequired}).Execute(); err != nil {
		logDurableJobDiagnostic(app, errorCodeScheduledBackupRestoreFailed, newDiagnosticReference(), "",
			"scheduled-backup", "restore-settle", err)
	}
}

// expireRestoreKeep lets go of the data from before a restore once it has
// been kept long enough, and of any folder an earlier restore left.
func expireRestoreKeep(work string, now time.Time) error {
	if err := expireRestoreFailure(work, now); err != nil {
		return err
	}
	state := restoredState{}
	err := readRestoreFile(filepath.Join(work, restoreStateName), &state)
	if errors.Is(err, fs.ErrNotExist) {
		return removeRestoreLeftovers(work, "")
	}
	if err != nil {
		return err
	}
	if state.Before != "" && now.Sub(time.UnixMilli(state.RestoredAt)) > restoreKeepFor {
		return releaseRestoreKeep(work, state)
	}
	return removeRestoreLeftovers(work, state.Before)
}

// releaseRestoreKeep removes the data from before a restore; the restore can
// no longer be undone.
func releaseRestoreKeep(work string, state restoredState) error {
	if state.Before == "" {
		return nil
	}
	if !restoreFolderPattern.MatchString(state.Before) {
		return errors.New("the restore record names a folder it can't")
	}
	if err := os.RemoveAll(filepath.Join(work, state.Before)); err != nil {
		return err
	}
	state.Before = ""
	if err := writeRestoreFile(work, restoreStateName, state); err != nil {
		return err
	}
	return removeRestoreLeftovers(work, "")
}

// removeRestoreLeftovers removes the work's folders no note names: a
// restore put together and never asked for, or data a restore left behind.
func removeRestoreLeftovers(work, kept string) error {
	entries, err := os.ReadDir(work)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == kept || !restoreFolderPattern.MatchString(name) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(work, name)); err != nil {
			return err
		}
	}
	return nil
}

// readRestoreStatus is what the screen shows of restores: one waiting for
// the next start, and the last one with how long it can be undone.
func readRestoreStatus(dataDir string) (pending *pendingRestore, state *restoredState, err error) {
	work := restoreWorkDir(dataDir)
	waiting := pendingRestore{}
	if err := readRestoreFile(filepath.Join(work, restorePendingName), &waiting); err == nil {
		pending = &waiting
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	last := restoredState{}
	if err := readRestoreFile(filepath.Join(work, restoreStateName), &last); err == nil {
		if last.Before != "" {
			if _, statErr := os.Stat(filepath.Join(work, last.Before)); statErr != nil {
				last.Before = ""
			}
		}
		state = &last
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	return pending, state, nil
}

func restoreStamp(now time.Time) string {
	return fmt.Sprintf("%013d", now.UnixMilli())
}

func restoreSnapshotNameValid(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && filepath.Base(name) == name &&
		!strings.ContainsAny(name, `/\`) && len(name) <= 255
}
