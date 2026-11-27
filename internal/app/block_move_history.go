package app

import "github.com/pocketbase/pocketbase/core"

// recordBlockMoveHistory keeps, in the histories of the pages a move or copy
// wrote, their blocks as they stand once its blocks have all gone, or all
// come back: one version for the whole move, as Notion's history shows it.
// A version a window, each keeping both pages whole, filled a toggle of
// 30,000 lines' move with some two thousand versions of 30,000 blocks each
// and took nearly all of every window (2026-11-23). The pages are held
// meanwhile, so no one's change comes between.
func recordBlockMoveHistory(app core.App, job storedBlockMoveJob) error {
	// A copy reads its source and writes only where it goes.
	if job.Operation != blockMoveOperationCopy && job.SourcePageID != job.DestinationPageID {
		if err := recordPageHistoryRewrite(app, job.SourcePageID, job.ActorID, true); err != nil {
			return err
		}
	}
	return recordPageHistoryRewrite(app, job.DestinationPageID, job.ActorID, true)
}

// blockMoveMustGoBack says whether a move goes back instead of on: its maker can no
// longer write where it goes, or read a copy's source in another workspace,
// or one of its pages is no longer where it was.
func blockMoveMustGoBack(app core.App, job storedBlockMoveJob) (bool, error) {
	if _, err := authorizeWorkspace(
		app, job.ActorID, job.WorkspaceID, permissionWorkspaceWrite,
	); err != nil {
		return isPermissionDenied(err), ignorePermissionDenied(err)
	}
	// A copy from another workspace still reads its source there.
	if job.crossWorkspace() {
		if _, err := authorizeWorkspace(
			app, job.ActorID, job.sourceWorkspace(), permissionWorkspaceRead,
		); err != nil {
			return isPermissionDenied(err), ignorePermissionDenied(err)
		}
	}
	current, err := blockMovePagesCurrent(app, job)
	return err == nil && !current, err
}

func ignorePermissionDenied(err error) error {
	if isPermissionDenied(err) {
		return nil
	}
	return err
}

// markBlockMoveEnding notes, in the window its last blocks go in, that a move
// has moved them all. The window that ends it, which keeps its pages'
// history, comes next; a change to its maker's access or its pages between
// the two leaves it ending, as when one window did both: going back then
// took its every block back and put the block it started from first on its
// page.
func markBlockMoveEnding(app core.App, job storedBlockMoveJob) error {
	return updateNativeCopyPayload(app, job.ID, func(value map[string]any) error {
		value["ending"] = true
		return nil
	})
}

// blockMoveEnding says whether a move has moved its every block and only ends.
func blockMoveEnding(app core.App, job storedBlockMoveJob) (bool, error) {
	if job.Phase != blockMovePhaseMoving || job.Operation != blockMoveOperationMove {
		return false, nil
	}
	placement, err := loadBlockMovePlacement(app, job.ID)
	return placement.Ending, err
}
