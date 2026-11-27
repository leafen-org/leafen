package app

import (
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Pages leave the trash for good 30 days after they went in, as in Notion.
// The trash retention runner takes a page whose time is up through the
// permanent deletion every deletion of a page takes, the pages under it with
// it, as the system (system_job_access.go), as database rows are deleted, in
// its workspace owner's name. Two things hold a page back:
//
//   - A page already in the trash when this arrived counts its 30 days from
//     the first run of the version that brought it (its migration's
//     applied_at), so an upgrade alone deletes nothing.
//   - An active legal hold keeps what it covers: a page, a block or a
//     database row whose page it holds — the whole organization, the
//     workspace, the page itself or its owner as custodian — stays in the
//     trash. A deletion that a hold on a page further down stops is undone
//     before anything went, and the page is looked at again a day later.

const (
	pageTrashRetention              = recoverableBlockTrashRetention
	pageTrashRetentionVersion       = 332
	pageTrashRetentionRequestPrefix = "trash-retention:"
	pageTrashRetentionHoldRetry     = 24 * time.Hour
	pageTrashRetentionConflictRetry = time.Hour
	// A turn advances one deletion this many times, each as far as one
	// permanent deletion request goes, and continues in the next turn.
	pageTrashRetentionAdvancesPerTurn = 8
)

// legalHoldRetainsPageSQL is true, in SQL, while an active legal hold covers
// the page: held whole, by its workspace, by the page or by its owner as
// custodian, as a permanent deletion checks. Each way is its own lookup of
// the organization's scopes by (organization, kind, target): the trash
// runner's reads, the trash's and the 30 days' lists judge it for every
// entry they pass, and one condition joining the kinds with OR read every
// scope of the organization for each (2026-11-18: 500 page scopes, about
// 0.08 ms an entry).
func legalHoldRetainsPageSQL(pageID, workspaceID string) string {
	held := func(kind, targetSQL string) string {
		return `EXISTS (
			SELECT 1 FROM {{leafen_workspaces}} AS [[hold_workspace]]
			JOIN {{leafen_organization_legal_hold_scopes}} AS [[hold_scope]]
				ON [[hold_scope]].[[organization_id]] = [[hold_workspace]].[[organization]]
				AND [[hold_scope]].[[kind]] = '` + kind + `' AND [[hold_scope]].[[target_id]] ` + targetSQL + `
			JOIN {{leafen_organization_legal_holds}} AS [[hold]]
				ON [[hold]].[[id]] = [[hold_scope]].[[hold_id]]
				AND [[hold]].[[organization_id]] = [[hold_scope]].[[organization_id]]
			WHERE [[hold_workspace]].[[id]] = ` + workspaceID + ` AND [[hold]].[[status]] = 'active')`
	}
	return `(` + held("all", `= ''`) + `
		OR ` + held("workspace", `= `+workspaceID) + `
		OR ` + held("page", `= `+pageID) + `
		OR ` + held("custodian", `IN (
			SELECT [[held_page]].[[owner]] FROM {{leafen_pages}} AS [[held_page]]
			WHERE [[held_page]].[[id]] = `+pageID+`)`) + `)`
}

// pageTrashRetentionStart is when pages already in the trash began counting
// their 30 days, when the retention came (migration 332, held in the schema
// baseline); none are deleted before it is known.
func pageTrashRetentionStart(app core.App) (int64, bool, error) {
	start, ok, err := productMigrationAppliedAt(app, pageTrashRetentionVersion)
	if err != nil || !ok {
		return 0, false, err
	}
	return start.UTC().UnixMilli(), true, nil
}

// pageTrashTimeSQL is when a page went into the trash, in milliseconds,
// written as idx_leafen_pages_trash_expiry indexes it (migration 824); a time
// that doesn't read as one is NULL.
const pageTrashTimeSQL = `CAST(unixepoch([[page]].[[trashed_at]], 'subsec') * 1000 AS INTEGER)`

// pageTrashDueSQL is when a trashed page's time is up, in milliseconds: 30
// days after it went into the trash or after counting began, whichever is
// later. A time that doesn't read as one is never due.
const pageTrashDueSQL = `(MAX(` + pageTrashTimeSQL + `, {:start}) + {:retention})`

// pageTrashRetentionCandidateWhereSQL keeps the trashed pages the runner may
// delete: their own pages outside database rows, in workspaces still there,
// not being deleted and not waiting.
const pageTrashRetentionCandidateWhereSQL = `
	JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]] = [[page]].[[workspace]]
	WHERE [[page]].[[in_trash]] = 1 AND [[page]].[[data_source_id]] = ''
		AND [[page]].[[deletion_job_id]] = '' AND [[workspace]].[[deletion_job_id]] = ''
		AND ` + pageTrashTimeSQL + ` IS NOT NULL
		AND NOT EXISTS (
			SELECT 1 FROM {{leafen_page_trash_retention_waits}} AS [[wait]]
			WHERE [[wait]].[[page_id]] = [[page]].[[id]] AND [[wait]].[[trashed_at]] = [[page]].[[trashed_at]]
				AND [[wait]].[[retry_at]] > {:now}
		)`

// pageTrashRetentionCandidateSQL reads one page among them by its id.
const pageTrashRetentionCandidateSQL = `
	FROM {{leafen_pages}} AS [[page]]` + pageTrashRetentionCandidateWhereSQL

// pageTrashRetentionQueueSQL reads them in the order they went into the
// trash, along idx_leafen_pages_trash_expiry, so a read ordered by that time
// stops at the first that fits.
const pageTrashRetentionQueueSQL = `
	FROM {{leafen_pages}} AS [[page]] INDEXED BY [[idx_leafen_pages_trash_expiry]]` + pageTrashRetentionCandidateWhereSQL

// pageTrashRetentionNext is the next time a page may go when none may now:
// the first trashed page past the cutoff (any, while counting began less
// than 30 days ago), a page's wait, or the end of a page's 30 days after a
// permanent deletion. Each is read in its index's order and stops at the
// first. A hold is judged when that time comes, by the reads of what is due,
// so a held page whose time is up is no next time (2026-11-18: it was, and
// the runner woke again at once, over and over, while a legal hold kept a
// trashed page past its 30 days).
func pageTrashRetentionNext(app core.App, params dbx.Params) (sql.NullInt64, error) {
	next := sql.NullInt64{}
	err := app.DB().NewQuery(`
		SELECT MIN([[due]]) FROM (
			SELECT * FROM (
				SELECT MAX(` + pageTrashTimeSQL + `, {:start}) + {:retention} AS [[due]]
				` + pageTrashRetentionQueueSQL + `
					AND ` + pageTrashTimeSQL + ` > {:after}
				ORDER BY ` + pageTrashTimeSQL + `, [[page]].[[id]] LIMIT 1
			)
			UNION ALL
			SELECT MIN([[retry_at]]) FROM {{leafen_page_trash_retention_waits}} WHERE [[retry_at]] > {:now}
			UNION ALL
			SELECT * FROM (
				SELECT [[kept]].[[purge_at]] FROM {{leafen_page_deletion_holds}} AS [[kept]]
				INDEXED BY [[idx_leafen_page_deletion_holds_purge]]
				WHERE [[kept]].[[purge_at]] > {:now} AND [[kept]].[[retry_at]] <= [[kept]].[[purge_at]]
				ORDER BY [[kept]].[[purge_at]] LIMIT 1
			)
			UNION ALL
			SELECT MIN([[kept]].[[retry_at]]) FROM {{leafen_page_deletion_holds}} AS [[kept]]
			INDEXED BY [[idx_leafen_page_deletion_holds_retry]]
			WHERE [[kept]].[[retry_at]] > [[kept]].[[purge_at]] AND [[kept]].[[retry_at]] > {:now}
		)
	`).Bind(params).Row(&next)
	return next, err
}

type pageTrashRetentionCandidate struct {
	PageID      string `db:"id"`
	TrashedAt   string `db:"trashed_at"`
	WorkspaceID string `db:"workspace"`
}

// retirePageTrash deletes, or goes on deleting, one page whose time in the
// trash is up. It reports whether more may be due now and, when none is,
// the next time one is.
func retirePageTrash(app core.App, rooms workspaceRoomInvalidator, now time.Time) (bool, sql.NullInt64, error) {
	nowMillis := now.UTC().UnixMilli()
	start, known, err := pageTrashRetentionStart(app)
	if err != nil || !known {
		return false, sql.NullInt64{}, err
	}
	job, found, err := pendingPageTrashRetentionJob(app, nowMillis)
	if err != nil {
		return false, sql.NullInt64{}, err
	}
	if !found {
		// A page whose 30 days after a permanent deletion are up goes next.
		released, err := releaseDuePageDeletionHold(app, nowMillis)
		if err != nil {
			return false, sql.NullInt64{}, err
		}
		if released {
			return true, sql.NullInt64{}, nil
		}
		// A page is due once 30 days have passed since it went into the trash
		// and since counting began: then its time is at or before the cutoff.
		cutoff := nowMillis - pageTrashRetention.Milliseconds()
		params := dbx.Params{
			"now": nowMillis, "start": start, "retention": pageTrashRetention.Milliseconds(),
			"cutoff": cutoff, "after": int64(math.MinInt64),
		}
		candidate := pageTrashRetentionCandidate{}
		err = sql.ErrNoRows
		if start <= cutoff {
			// The oldest page in the trash that no hold keeps goes first; the
			// read stops at it, past held pages only.
			params["after"] = cutoff
			err = app.DB().NewQuery(`
				SELECT [[page]].[[id]], [[page]].[[trashed_at]], [[page]].[[workspace]]
				` + pageTrashRetentionQueueSQL + `
					AND ` + pageTrashTimeSQL + ` <= {:cutoff}
					AND NOT ` + legalHoldRetainsPageSQL("[[page]].[[id]]", "[[page]].[[workspace]]") + `
				ORDER BY ` + pageTrashTimeSQL + `, [[page]].[[id]] LIMIT 1
			`).Bind(params).One(&candidate)
		}
		if errors.Is(err, sql.ErrNoRows) {
			// A wait whose page left the trash, or is gone, is done with.
			if _, err := app.DB().NewQuery(`
				DELETE FROM {{leafen_page_trash_retention_waits}} WHERE [[retry_at]] <= {:now} AND NOT EXISTS (
					SELECT 1 FROM {{leafen_pages}} AS [[page]]
					WHERE [[page]].[[id]] = {{leafen_page_trash_retention_waits}}.[[page_id]] AND [[page]].[[in_trash]] = 1
						AND [[page]].[[trashed_at]] = {{leafen_page_trash_retention_waits}}.[[trashed_at]])
			`).Bind(params).Execute(); err != nil {
				return false, sql.NullInt64{}, err
			}
			next, err := pageTrashRetentionNext(app, params)
			return false, next, err
		}
		if err != nil {
			return false, sql.NullInt64{}, err
		}
		err = holdPageTrashRetention(app, candidate, start, nowMillis)
		if errors.Is(err, errPageTrashRetentionNotDue) {
			return true, sql.NullInt64{}, nil
		}
		if err != nil {
			return true, sql.NullInt64{}, waitPageTrashRetention(app, candidate.PageID, candidate.TrashedAt, nowMillis, err)
		}
		if rooms != nil {
			if err := invalidatePermanentPageRooms(rooms, candidate.WorkspaceID); err != nil {
				return false, sql.NullInt64{}, err
			}
		}
		return true, sql.NullInt64{}, nil
	}
	if rooms != nil {
		if err := invalidatePermanentPageRooms(rooms, job.WorkspaceID); err != nil {
			return false, sql.NullInt64{}, err
		}
	}
	for advance := 0; advance < pageTrashRetentionAdvancesPerTurn; advance++ {
		advanced, err := advancePagePermanentDelete(app, job.ID)
		if err != nil {
			return true, sql.NullInt64{}, stopPageTrashRetention(app, job.ID, nowMillis, err)
		}
		if advanced.Status == pagePermanentDeleteStatusCompleted {
			break
		}
	}
	return true, sql.NullInt64{}, nil
}

var errPageTrashRetentionNotDue = errors.New("trashed page is no longer due")

// holdPageTrashRetention takes a page whose time is still up out of the trash
// into the 30 days after a permanent deletion, so a page restored or put back
// in the trash meanwhile is left as it now is.
func holdPageTrashRetention(
	app core.App, candidate pageTrashRetentionCandidate, start, now int64,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		var due int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) ` + pageTrashRetentionCandidateSQL + `
				AND [[page]].[[id]] = {:pageId} AND [[page]].[[trashed_at]] = {:trashedAt}
				AND ` + pageTrashDueSQL + ` <= {:now}
		`).Bind(dbx.Params{
			"pageId": candidate.PageID, "trashedAt": candidate.TrashedAt,
			"now": now, "start": start, "retention": pageTrashRetention.Milliseconds(),
		}).Row(&due); err != nil {
			return err
		}
		if due != 1 {
			return errPageTrashRetentionNotDue
		}
		workspace, err := txApp.FindRecordById(workspacesCollection, candidate.WorkspaceID)
		if err != nil {
			return err
		}
		_, _, err = holdPageForPermanentDeletion(
			txApp, systemJobSubjectFor(workspace), candidate.PageID,
			pageTrashRetentionRequestPrefix+candidate.PageID+":"+candidate.TrashedAt,
		)
		return err
	})
}

// pendingPageTrashRetentionJob is a deletion the runner began and hasn't
// finished, as after a restart, unless its page waits.
func pendingPageTrashRetentionJob(app core.App, now int64) (storedPagePermanentDeleteJob, bool, error) {
	var jobID string
	err := app.DB().NewQuery(`
		SELECT [[job]].[[id]] FROM {{leafen_page_permanent_delete_jobs}} AS [[job]]
		WHERE [[job]].[[status]] = 'pending' AND substr([[job]].[[request_id]], 1, {:prefixLength}) = {:prefix}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_page_trash_retention_waits}} AS [[wait]]
				WHERE [[wait]].[[page_id]] = [[job]].[[root_page_id]] AND [[wait]].[[retry_at]] > {:now}
			)
		ORDER BY [[job]].[[created_at]], [[job]].[[id]] LIMIT 1
	`).Bind(dbx.Params{
		"prefix": pageTrashRetentionRequestPrefix, "prefixLength": len(pageTrashRetentionRequestPrefix), "now": now,
	}).Row(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPagePermanentDeleteJob{}, false, nil
	}
	if err != nil {
		return storedPagePermanentDeleteJob{}, false, err
	}
	job, err := loadPagePermanentDeleteJobByID(app, jobID)
	return job, err == nil, err
}

// stopPageTrashRetention undoes a deletion stopped before any page went —
// by a legal hold on a page under it, or by another change there — and lets
// the page wait; a deletion already under way waits to go on.
func stopPageTrashRetention(app core.App, jobID string, now int64, cause error) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadPagePermanentDeleteJobByID(txApp, jobID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		page := struct {
			TrashedAt     string `db:"trashed_at"`
			DeletionJobID string `db:"deletion_job_id"`
		}{}
		if err := txApp.DB().NewQuery(`
			SELECT [[trashed_at]], [[deletion_job_id]] FROM {{leafen_pages}} WHERE [[id]] = {:pageId}
		`).Bind(dbx.Params{"pageId": current.RootPageID}).One(&page); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var cleaned int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_page_permanent_delete_items}} WHERE [[job_id]] = {:jobId} AND [[cleaned]] = 1
		`).Bind(dbx.Params{"jobId": current.ID}).Row(&cleaned); err != nil {
			return err
		}
		untouched := current.Status == pagePermanentDeleteStatusPending && cleaned == 0 &&
			(current.Phase == pagePermanentDeletePhaseDiscovering || current.Phase == pagePermanentDeletePhaseAuthorizing)
		if untouched {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_pages}} SET [[deletion_job_id]] = '' WHERE [[id]] = {:pageId} AND [[deletion_job_id]] = {:jobId}
			`).Bind(dbx.Params{"pageId": current.RootPageID, "jobId": current.ID}).Execute(); err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_page_permanent_delete_jobs}} WHERE [[id]] = {:jobId}
			`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
				return err
			}
		}
		return waitPageTrashRetention(txApp, current.RootPageID, page.TrashedAt, now, cause)
	})
}

// waitPageTrashRetention puts off a page's automatic deletion: a day for a
// legal hold, an hour for a change under way.
func waitPageTrashRetention(app core.App, pageID, trashedAt string, now int64, cause error) error {
	reason, delay := "conflict", pageTrashRetentionConflictRetry
	if errors.Is(cause, errLegalHoldPreventsPermanentDelete) {
		reason, delay = "legal_hold", pageTrashRetentionHoldRetry
	}
	logDurableJobDiagnostic(
		app, errorCodeJobRunnerFailed, newDiagnosticReference(), "",
		"page-trash.retention", reason, cause,
	)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_trash_retention_waits}} ([[page_id]], [[trashed_at]], [[retry_at]], [[reason]])
		VALUES ({:pageId}, {:trashedAt}, {:retryAt}, {:reason})
		ON CONFLICT ([[page_id]]) DO UPDATE SET [[trashed_at]] = excluded.[[trashed_at]],
			[[retry_at]] = excluded.[[retry_at]], [[reason]] = excluded.[[reason]]
	`).Bind(dbx.Params{
		"pageId": pageID, "trashedAt": trashedAt, "retryAt": now + delay.Milliseconds(), "reason": reason,
	}).Execute()
	return err
}
