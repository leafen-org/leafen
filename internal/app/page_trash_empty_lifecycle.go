package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageTrashEmptyCandidate struct {
	PageID   string `db:"page_id"`
	ParentID string `db:"parent_id"`
	InTrash  bool   `db:"in_trash"`
}

type pageTrashEmptyPageFence struct {
	DeletionJobID string `db:"deletion_job_id"`
}

func advancePageTrashEmpty(app core.App, jobID string) (storedPageTrashEmptyJob, error) {
	childTurns := 0
	job, err := loadPageTrashEmptyJobByID(app, jobID)
	if err != nil || job.Status == trashEmptyStatusCompleted {
		return job, err
	}
	if job.Phase == trashEmptyPhaseDiscovering {
		if err := discoverPageTrashEmptyRoots(app, jobID); err != nil {
			return storedPageTrashEmptyJob{}, err
		}
		job, err = loadPageTrashEmptyJobByID(app, jobID)
		if err != nil || job.Phase == trashEmptyPhaseDiscovering {
			return job, err
		}
	}
	if job.Phase == trashEmptyPhaseAdmitting {
		if childTurns >= maxTrashEmptyChildJobsPerRequest {
			return job, nil
		}
		if err := refusePageTrashEmptyUnderLegalHold(app, job); err != nil {
			return storedPageTrashEmptyJob{}, err
		}
		touched, err := admitNextPageTrashEmptyChild(app, job)
		if err != nil {
			return storedPageTrashEmptyJob{}, endRefusedPageTrashEmpty(app, job, err)
		}
		if err := transitionPageTrashEmptyAfterAdmission(app, job.ID); err != nil {
			return storedPageTrashEmptyJob{}, err
		}
		job, err = loadPageTrashEmptyJobByID(app, jobID)
		if err != nil || touched {
			return job, err
		}
		childTurns++
	}
	if job.Phase == trashEmptyPhaseDeleting {
		if childTurns >= maxTrashEmptyChildJobsPerRequest {
			return job, nil
		}
		if err := advanceNextPageTrashEmptyChild(app, job); err != nil {
			return storedPageTrashEmptyJob{}, err
		}
	}
	return loadPageTrashEmptyJobByID(app, jobID)
}

func discoverPageTrashEmptyRoots(app core.App, jobID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadPageTrashEmptyJobByID(txApp, jobID)
		if err != nil || job.Status == trashEmptyStatusCompleted ||
			job.Phase != trashEmptyPhaseDiscovering {
			return err
		}
		cursor, pageID, ancestorID := job.DiscoveryCursor,
			job.DiscoveryPageID, job.DiscoveryAncestorID
		roots := 0
		phase := trashEmptyPhaseDiscovering
		for step := 0; step < maxTrashEmptyDiscoveryStepsPerRequest &&
			roots < maxTrashEmptyRootsPerRequest; step++ {
			if pageID == "" {
				candidate, found, err := nextPageTrashEmptyCandidate(txApp, job, cursor)
				if err != nil {
					return err
				}
				if !found {
					phase = trashEmptyPhaseAdmitting
					break
				}
				pageID, ancestorID = candidate.PageID, candidate.ParentID
				if ancestorID == "" {
					if err := insertPageTrashEmptyRoot(txApp, job.ID, pageID); err != nil {
						return err
					}
					cursor, pageID = pageID, ""
					roots++
				}
				continue
			}

			ancestor, found, err := pageTrashEmptyAncestor(txApp, job, ancestorID)
			if err != nil {
				return err
			}
			if !found || ancestor.InTrash {
				if !found {
					if err := insertPageTrashEmptyRoot(txApp, job.ID, pageID); err != nil {
						return err
					}
					roots++
				}
				cursor, pageID, ancestorID = pageID, "", ""
				continue
			}
			ancestorID = ancestor.ParentID
			if ancestorID == "" {
				if err := insertPageTrashEmptyRoot(txApp, job.ID, pageID); err != nil {
					return err
				}
				cursor, pageID = pageID, ""
				roots++
			}
		}
		return updatePageTrashEmptyDiscovery(
			txApp, job.ID, cursor, pageID, ancestorID, phase,
		)
	})
}

func nextPageTrashEmptyCandidate(
	app core.App,
	job storedPageTrashEmptyJob,
	afterPage string,
) (pageTrashEmptyCandidate, bool, error) {
	if job.SnapshotPageID == "" {
		return pageTrashEmptyCandidate{}, false, nil
	}
	candidate := pageTrashEmptyCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[id]] AS [[page_id]], [[parent_id]], [[in_trash]]
		FROM {{leafen_pages}} AS [[page]] INDEXED BY [[idx_leafen_pages_workspace_trash_id]]
		WHERE [[workspace]] = {:workspaceId} AND [[owner]] = {:ownerId}
			AND [[notion_import_staging]] = FALSE AND [[in_trash]] = 1
			AND [[id]] > {:afterPage} AND NOT ` + databasePageSignpostSQL + `
			AND (([[created]] < {:snapshotCreated})
				OR ([[created]] = {:snapshotCreated} AND [[id]] <= {:snapshotPageId}))
		ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{
		"workspaceId": job.WorkspaceID, "ownerId": job.OwnerID, "afterPage": afterPage,
		"snapshotCreated": job.SnapshotCreated, "snapshotPageId": job.SnapshotPageID,
	}).One(&candidate)
	if errors.Is(err, sql.ErrNoRows) {
		return pageTrashEmptyCandidate{}, false, nil
	}
	return candidate, err == nil, err
}

func pageTrashEmptyAncestor(
	app core.App,
	job storedPageTrashEmptyJob,
	pageID string,
) (pageTrashEmptyCandidate, bool, error) {
	ancestor := pageTrashEmptyCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[id]] AS [[page_id]], [[parent_id]], [[in_trash]]
		FROM {{leafen_pages}}
		WHERE [[id]] = {:pageId} AND [[workspace]] = {:workspaceId} AND [[owner]] = {:ownerId}
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": job.WorkspaceID, "ownerId": job.OwnerID,
	}).One(&ancestor)
	if errors.Is(err, sql.ErrNoRows) {
		return pageTrashEmptyCandidate{}, false, nil
	}
	return ancestor, err == nil, err
}

func insertPageTrashEmptyRoot(app core.App, jobID, pageID string) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().Insert(pageTrashEmptyItemsTable, dbx.Params{
		"job_id": jobID, "page_id": pageID,
		"child_request_id": pageTrashEmptyChildRequestID(jobID, pageID),
		"child_job_id":     "", "status": "pending",
		"created_at": now, "updated_at": now,
	}).Execute()
	return err
}

func admitNextPageTrashEmptyChild(
	app core.App,
	job storedPageTrashEmptyJob,
) (bool, error) {
	item, err := loadPageTrashEmptyItem(app, job.ID, "pending")
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pageFence := pageTrashEmptyPageFence{}
	err = app.DB().NewQuery(`
		SELECT [[deletion_job_id]] FROM {{leafen_pages}}
		WHERE [[id]] = {:pageId} AND [[workspace]] = {:workspaceId}
			AND [[owner]] = {:ownerId} AND [[in_trash]] = 1
	`).Bind(dbx.Params{
		"pageId": item.PageID, "workspaceId": job.WorkspaceID, "ownerId": job.OwnerID,
	}).One(&pageFence)
	if errors.Is(err, sql.ErrNoRows) {
		return true, updatePageTrashEmptyItem(app, job.ID, item.PageID, "", "completed")
	}
	if err != nil {
		return false, err
	}
	if pageDeletionHoldFence(pageFence.DeletionJobID) {
		return true, updatePageTrashEmptyItem(app, job.ID, item.PageID, "", "completed")
	}
	if pageFence.DeletionJobID != "" {
		child, err := loadPagePermanentDeleteJobByID(app, pageFence.DeletionJobID)
		if err != nil {
			return false, err
		}
		if child.RootPageID != item.PageID || child.WorkspaceID != job.WorkspaceID ||
			child.OwnerID != job.OwnerID {
			return false, errTrashEmptyConflict
		}
		status := "admitted"
		if child.Status == pagePermanentDeleteStatusCompleted {
			status = "completed"
		}
		return true, updatePageTrashEmptyItem(app, job.ID, item.PageID, child.ID, status)
	}
	childRequestID := pageTrashEmptyChildRequestID(job.ID, item.PageID)
	if item.ChildRequestID != childRequestID {
		return false, errTrashEmptyConflict
	}
	if _, _, err := holdPageForPermanentDeletion(app, personAccessSubject(job.ActorID), item.PageID, childRequestID); err != nil {
		if isPermissionDenied(err) {
			// Another member's private page in the trash isn't the person's to empty: it stays,
			// as Notion's trash shows each person only theirs (2026-11-05: one such page kept the
			// owner's emptying pending for good, retried every 3 seconds).
			return true, updatePageTrashEmptyItem(app, job.ID, item.PageID, "", "completed")
		}
		return false, err
	}
	return true, updatePageTrashEmptyItem(app, job.ID, item.PageID, "", "completed")
}

func transitionPageTrashEmptyAfterAdmission(app core.App, jobID string) error {
	if _, err := loadPageTrashEmptyItem(app, jobID, "pending"); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return touchPageTrashEmptyJob(
		app, jobID, trashEmptyPhaseDeleting, trashEmptyStatusPending, "",
	)
}

func advanceNextPageTrashEmptyChild(app core.App, job storedPageTrashEmptyJob) error {
	item, err := loadPageTrashEmptyItem(app, job.ID, "admitted")
	if errors.Is(err, sql.ErrNoRows) {
		return completePageTrashEmptyIfDrained(app, job.ID)
	}
	if err != nil {
		return err
	}
	child, err := advancePagePermanentDelete(app, item.ChildJobID)
	if err != nil {
		return err
	}
	if child.Status == pagePermanentDeleteStatusCompleted {
		if err := updatePageTrashEmptyItem(
			app, job.ID, item.PageID, child.ID, "completed",
		); err != nil {
			return err
		}
	} else if err := advancePageTrashEmptyRevision(app, job.ID); err != nil {
		return err
	}
	return completePageTrashEmptyIfDrained(app, job.ID)
}

func completePageTrashEmptyIfDrained(app core.App, jobID string) error {
	if _, err := loadPageTrashEmptyItem(app, jobID, "admitted"); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := loadPageTrashEmptyItem(app, jobID, "pending"); err == nil {
		return errTrashEmptyConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return touchPageTrashEmptyJob(
		app, jobID, trashEmptyPhaseCompleted, trashEmptyStatusCompleted, "",
	)
}

// refusePageTrashEmptyUnderLegalHold refuses the whole emptying before one of
// its pages goes when an active legal hold covers any of them, and ends the
// refused job: it isn't left pending for the deletion runner to finish once
// the hold is released (2026-11-05: an emptying refused with 412 deleted the
// held page two seconds after its hold was released, with nobody asking
// again). A job some of whose pages already went keeps the refusal per page.
func refusePageTrashEmptyUnderLegalHold(app core.App, job storedPageTrashEmptyJob) error {
	started := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{` + pageTrashEmptyItemsTable + `}}
		WHERE [[job_id]] = {:jobId} AND [[status]] != 'pending'`).
		Bind(dbx.Params{"jobId": job.ID}).Row(&started); err != nil {
		return err
	}
	if started > 0 {
		return nil
	}
	pageIDs := []string{}
	if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{` + pageTrashEmptyItemsTable + `}}
		WHERE [[job_id]] = {:jobId}`).Bind(dbx.Params{"jobId": job.ID}).Column(&pageIDs); err != nil {
		return err
	}
	if len(pageIDs) == 0 {
		return nil
	}
	held := assertNoActiveLegalHoldForPermanentDelete(app, job.OrganizationID, legalHoldDeletionTarget{
		WorkspaceID: job.WorkspaceID, PageIDs: pageIDs, CustodianUserIDs: []string{job.OwnerID},
	})
	if !errors.Is(held, errLegalHoldPreventsPermanentDelete) {
		return held
	}
	return endRefusedPageTrashEmpty(app, job, held)
}

// endRefusedPageTrashEmpty ends an emptying a legal hold refuses: the request
// says why, and nothing is left pending to go on once the hold is released or
// to keep every later emptying of the workspace waiting (2026-11-05: a refused
// emptying stayed pending, and each new one got "Another trash change or
// deletion is in progress."). Pages that already went stay gone; a passing
// error leaves the job for the runner as before.
func endRefusedPageTrashEmpty(app core.App, job storedPageTrashEmptyJob, refusal error) error {
	if !errors.Is(refusal, errLegalHoldPreventsPermanentDelete) {
		return refusal
	}
	if err := app.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().Delete(pageTrashEmptyItemsTable, dbx.HashExp{"job_id": job.ID}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().Delete(pageTrashEmptyJobsTable, dbx.HashExp{"id": job.ID}).Execute()
		return err
	}); err != nil {
		return errors.Join(refusal, err)
	}
	return refusal
}
