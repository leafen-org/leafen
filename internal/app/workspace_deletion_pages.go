package app

import (
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A workspace's pages go one after another, each as a page emptied from the
// trash goes: its residue lane by lane (cleanPagePermanentDeleteResidueLane),
// then the page. A step goes on to the page's next lane, and past the page to
// the next page, while its window has room: a lane that leaves rows for
// another batch fills it, and so do maxWorkspaceDeletionRowsPerStep pages or
// workspaceDeletionPageStepTime spent, so a step's transaction stays short
// for the writers beside it. Most of a page's 52 lanes hold nothing, and a
// step each — a transaction and a write of the job — kept a staged bundle's
// workspace of 2,401 pages (an Obsidian vault moved again) deleting for 11
// minutes, 60 ms a page, while the move waited for its pages' IDs
// (2026-11-24).

// workspaceDeletionPageStepTime is how long a step goes on to further lanes
// and pages.
const workspaceDeletionPageStepTime = 10 * time.Millisecond

func advanceWorkspaceDeletionPages(app core.App, expected storedWorkspaceDeletionJob) error {
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		started := time.Now()
		for pages := 0; job.Phase == workspaceDeletionPhasePages && pages < maxWorkspaceDeletionRowsPerStep; {
			if pages > 0 && time.Since(started) >= workspaceDeletionPageStepTime {
				break
			}
			if job.PageID == "" {
				pageID, err := nextWorkspaceDeletionPage(txApp, job.WorkspaceID, "")
				if err != nil {
					return err
				}
				job.PageID, job.PageCleanupLane = pageID, 0
				if pageID == "" {
					job.Phase = workspaceDeletionPhaseFiles
				}
				continue
			}
			if job.PageCleanupLane < pagePermanentDeleteCleanupLaneCount {
				more, err := cleanPagePermanentDeleteResidueLane(txApp, job.PageID, job.PageCleanupLane)
				if err != nil {
					return err
				}
				if more {
					// A full batch: the lane goes on in the next step.
					break
				}
				job.PageCleanupLane++
				continue
			}
			if err := deleteWorkspacePageThroughPermanentDeleteOwner(txApp, job.WorkspaceID, job.PageID); err != nil {
				return err
			}
			next, err := nextWorkspaceDeletionPage(txApp, job.WorkspaceID, job.PageID)
			if err != nil {
				return err
			}
			job.PageID, job.PageCleanupLane = next, 0
			job.PagesCount++
			pages++
			if next == "" {
				job.Phase = workspaceDeletionPhaseFiles
			}
		}
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
}
