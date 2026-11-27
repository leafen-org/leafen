package app

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/osutils"
)

// A restore's note is left only over data put together whole and made
// durable (leaveRestoreNote), and a start follows it only while that data is
// there: on Windows the put-together data's sync failed, the note was left
// anyway and the data taken away, and the next start moved the person's
// data aside and then couldn't start at all, "moving the restored data in:
// open …\staged: The system cannot find the file specified." (2026-11-19).
// A start that finds such a note moves nothing, or puts back what it moved,
// starts on the data it held, and says so: in its log and, until a restore
// finishes or a week passes, on the backup screen.

const (
	restoreFailedName = "failed.json"
	// restorePhasePuttingBack is a note whose restore couldn't finish, with
	// the data from before on its way back.
	restorePhasePuttingBack = "putting-back"
)

// restoreFailure is a restore, or its undoing, that couldn't finish.
type restoreFailure struct {
	Kind        string `json:"kind"`
	Snapshot    string `json:"snapshot"`
	RequestedAt int64  `json:"requestedAt"`
	FailedAt    int64  `json:"failedAt"`
}

// leaveRestoreNote makes the put-together data at staged durable and only
// then leaves the note the next start follows, last, as the start acts on
// it; the notes a step of first writes come after the sync too. A note the
// failure left goes again, so it never names data its caller takes away.
func leaveRestoreNote(staged, work string, pending pendingRestore, first ...func() error) error {
	if err := dropServerAddresses(staged); err != nil {
		return err
	}
	if err := syncRestoredTree(staged, syncPath); err != nil {
		return err
	}
	for _, step := range first {
		if err := step(); err != nil {
			return err
		}
	}
	if err := writeRestoreFile(work, restorePendingName, pending); err != nil {
		return errors.Join(err, removeIfThere(filepath.Join(work, restorePendingName)))
	}
	return nil
}

// settleBrokenRestore says whether the note's restore can't go on, its data
// to come in not all there, and if so moves the data from before back where
// it moved any, sets the note aside and records why.
func settleBrokenRestore(dataDir, work string, pending pendingRestore, now time.Time) (bool, error) {
	source, keep := filepath.Join(work, pending.Source), filepath.Join(work, pending.Keep)
	stays := append([]string{core.LocalBackupsDirName}, restoreStaysPut...)
	switch pending.Phase {
	case restorePhaseReady:
		if holdsServerData(source) {
			return false, nil
		}
	case restorePhaseMovingIn:
		// Its data.db is either still to come in or in already.
		if holdsServerData(source) || holdsServerData(dataDir) {
			return false, nil
		}
		// What came in of it goes back out; the note then says the data
		// from before comes back, so a start cut off partway goes on.
		if err := osutils.MoveDirContent(dataDir, source, stays...); err != nil {
			return true, fmt.Errorf("moving a restore that isn't all there back out: %w", err)
		}
		pending.Phase = restorePhasePuttingBack
		if err := writeRestoreFile(work, restorePendingName, pending); err != nil {
			return true, err
		}
	case restorePhasePuttingBack:
	default:
		return false, nil
	}
	if pending.Phase == restorePhasePuttingBack {
		if _, err := os.Stat(keep); !errors.Is(err, fs.ErrNotExist) {
			if err := osutils.MoveDirContent(keep, dataDir, stays...); err != nil {
				return true, fmt.Errorf("putting the data from before the restore back: %w", err)
			}
		}
		if err := errors.Join(syncPath(dataDir), removeIfThere(keep)); err != nil {
			return true, err
		}
	}
	if err := writeRestoreFile(work, restoreFailedName, restoreFailure{
		Kind: pending.Kind, Snapshot: pending.Snapshot, RequestedAt: pending.RequestedAt, FailedAt: now.UnixMilli(),
	}); err != nil {
		return true, err
	}
	// Put together here and not all there, it is of no use; data kept from
	// before an earlier restore stays to its time.
	if pending.Source == restoreStagedName {
		if err := os.RemoveAll(source); err != nil {
			return true, err
		}
	}
	if err := errors.Join(os.Remove(filepath.Join(work, restorePendingName)), syncPath(work)); err != nil {
		return true, err
	}
	log.Printf("leafen: the restore asked for before the server stopped couldn't finish, as the data put together for it "+
		"isn't all there; nothing was restored, and the server starts on the data it held before (%s)", work)
	return true, nil
}

// readRestoreFailure is a restore that couldn't finish, while the screen
// still tells it.
func readRestoreFailure(dataDir string) *restoreFailure {
	failure := restoreFailure{}
	if err := readRestoreFile(filepath.Join(restoreWorkDir(dataDir), restoreFailedName), &failure); err != nil {
		return nil
	}
	return &failure
}

// expireRestoreFailure lets go of the word of a restore that couldn't finish
// a week on, as of the data kept from before one.
func expireRestoreFailure(work string, now time.Time) error {
	failure := restoreFailure{}
	if err := readRestoreFile(filepath.Join(work, restoreFailedName), &failure); err != nil ||
		now.Sub(time.UnixMilli(failure.FailedAt)) <= restoreKeepFor {
		return nil
	}
	return removeIfThere(filepath.Join(work, restoreFailedName))
}

// removeIfThere removes a file or an empty folder, if there is one.
func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
