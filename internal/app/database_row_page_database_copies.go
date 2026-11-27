package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The databases of a database page's body that a copy of the body takes
// (database_template_body_databases.go) are copied by a job of their own,
// one after another, each by the copy a database's block takes
// (admitDatabaseTemplateCopy), after the copies of a template's sub-pages
// begin (database_row_page_sub_page_copies.go): the job is written with the rest of the body,
// so a server that stops meanwhile copies the rest once it is back. Until
// the job ends, the page says its databases are being copied into it
// (pageBusyCopyingDatabases), and its body shows as being filled, as
// Notion's does, saying "페이지 복제 중" while it copies a page's databases
// (seen 2026-11-14: a row's duplicate and a page made from a template alike;
// web lib/pageDatabaseCopies.ts). A write a database's copy fences off
// meanwhile waits on the device and goes once the copy is done: a change of
// its blocks is refused as busy (pageBusyError), its text closes its socket
// (ygoRoomAuthorization.writeGate).

const rowPageDatabaseCopyJobKind = "row-page-database-copy"

// pageBusyCopyingDatabases holds a page while its databases are copied into it.
const pageBusyCopyingDatabases = "copyingDatabases"

// rowPageDatabaseCopyWait is how long the job waits between its looks at a
// database's copy that can't begin yet: another copy or move holds a page
// it reads or writes, or the page is held by an operation (its page's copy).
// It is longer than a job's ordinary pause, so the job's turn ends there.
const rowPageDatabaseCopyWait = 300 * time.Millisecond

// rowPageDatabaseCopyMaxWaits is how many waits a database's copy takes
// before it is given up: some ten minutes, then once every two seconds,
// about an hour in all.
const (
	rowPageDatabaseCopyQuickWaits = 2000
	rowPageDatabaseCopyMaxWaits   = rowPageDatabaseCopyQuickWaits + 1500
	rowPageDatabaseCopySlowWait   = 2 * time.Second
)

type rowPageDatabaseCopyPayload struct {
	ActorID      string               `json:"actorId"`
	SourcePageID string               `json:"sourcePageId"`
	Databases    []rowPageOwnDatabase `json:"databases"`
	// Copies names each database copied so far by the block its copy took.
	Copies map[string]string `json:"copies,omitempty"`
	// Next is the database being copied, MoveJobID its copy once begun.
	Next      int    `json:"next"`
	MoveJobID string `json:"moveJobId,omitempty"`
	Waits     int    `json:"waits,omitzero"`
	// ImportTemplateRow says the source is a row an import made a template
	// of, which goes once its databases are copied (makeNotionImportTemplate),
	// with the owner of the import's records and the import, whose end
	// waits for the copy (notion_import_template_copies_wait.go).
	ImportTemplateRow bool   `json:"importTemplateRow,omitzero"`
	ImportOwnerID     string `json:"importOwnerId,omitempty"`
	ImportJobID       string `json:"importJobId,omitempty"`
	// SubPages are a template's sub-pages the body takes as copies, begun
	// before the databases; SubPageNext is the next to begin, SubPageJobs
	// the copies begun, which the job waits for before going on.
	SubPages    []rowPageSubPageCopy `json:"subPages,omitempty"`
	SubPageNext int                  `json:"subPageNext,omitzero"`
	SubPageJobs []string             `json:"subPageJobs,omitempty"`
}

// queueRowPageOwnDatabaseCopies writes the job that copies the databases
// and sub-pages into the page, beside what wrote the rest of its body; the
// runner hears of it once that is saved.
func queueRowPageOwnDatabaseCopies(
	app core.App, actorID, sourcePageID, targetPageID string, databases []rowPageOwnDatabase, subPages []rowPageSubPageCopy,
) error {
	if len(databases) == 0 && len(subPages) == 0 {
		return nil
	}
	target, err := app.FindRecordById(pagesCollection, targetPageID)
	if err != nil {
		return err
	}
	// The databases are read where the body was: in another workspace for a
	// copy into this one (block_copy_workspaces.go).
	source, err := app.FindRecordById(pagesCollection, sourcePageID)
	if err != nil {
		return err
	}
	if err := placeRowPageOwnDatabaseHolders(app, source.GetString("workspace"), sourcePageID, databases); err != nil {
		return err
	}
	if err := writeRowPageDatabaseCopyJob(app, actorID, sourcePageID, target.GetString("workspace"), targetPageID, databases, subPages); err != nil {
		return err
	}
	runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner)
	if !ok {
		return nil
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				runner.notify()
			}
			return nil
		})
		return nil
	}
	runner.notify()
	return nil
}

// deferRowPageRemovalToDatabaseCopies leaves the taking away of a row an
// import made a template of to the copy of its databases into the template,
// when one was just written: the copy reads them from the row. It says
// whether one was; the import ends once it ends.
func deferRowPageRemovalToDatabaseCopies(app core.App, actorID, workspaceID, targetPageID, rowID, importOwnerID, importJobID string) (bool, error) {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]] = json_set([[payload_json]], '$.importTemplateRow', json('true'), '$.importOwnerId', {:importOwnerId},
			'$.importJobId', {:importJobId})
		WHERE [[owner]] = {:actorId} AND [[workspace_id]] = {:workspaceId} AND [[kind]] = {:kind}
			AND [[page_id]] = {:pageId} AND [[status]] = 'queued'
			AND json_extract([[payload_json]], '$.sourcePageId') = {:rowId}
	`).Bind(dbx.Params{
		"actorId": actorID, "workspaceId": workspaceID, "kind": rowPageDatabaseCopyJobKind,
		"pageId": targetPageID, "rowId": rowID, "importOwnerId": importOwnerID, "importJobId": importJobID,
	}).Execute()
	if err != nil {
		return false, err
	}
	deferred, err := result.RowsAffected()
	return deferred > 0, err
}

// rowPageDatabasesCopying says whether databases are being copied into the page.
func rowPageDatabasesCopying(app core.App, pageID string) (bool, error) {
	var copying int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_jobs}}
			WHERE [[page_id]]={:pageId} AND [[kind]]={:kind} AND [[status]]='queued')
	`).Bind(dbx.Params{"pageId": pageID, "kind": rowPageDatabaseCopyJobKind}).Row(&copying)
	return copying != 0, err
}

// pageHoldForMember is what holds a page its reader is shown: an operation
// under way when they can't edit it, and its databases being copied into it
// when they can, which it waits for as Notion's does.
func pageHoldForMember(app core.App, principalID, pageID string, canEdit bool) string {
	if canEdit {
		if copying, err := rowPageDatabasesCopying(app, pageID); err != nil || !copying {
			return ""
		}
	}
	return pageBusyForMember(app, principalID, pageID)
}

// applyRowPageDatabaseCopyJob takes the job one step: it begins the next
// database's copy, or drives the one begun, or ends.
func (r *durableJobRunner) applyRowPageDatabaseCopyJob(ctx context.Context, job storedJob) error {
	app := r.app
	payload := rowPageDatabaseCopyPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	if payload.Copies == nil {
		payload.Copies = map[string]string{}
	}
	if payload.SubPageNext < len(payload.SubPages) {
		return r.beginRowPageSubPageCopy(job, payload)
	}
	if done, err := rowPageSubPageCopiesDone(app, payload.SubPageJobs); err != nil {
		return err
	} else if !done && payload.Waits < rowPageDatabaseCopyMaxWaits {
		payload.Waits++
		return saveRowPageDatabaseCopyJob(app, job, payload, rowPageDatabaseCopyWait, false)
	} else if len(payload.SubPageJobs) > 0 {
		// What the body names of the sub-pages names their copies
		// (database_row_page_sub_page_copy_names.go).
		if err := nameRowPageSubPageCopies(app, payload.ActorID, job.PageID, payload.SubPageJobs); err != nil {
			app.Logger().Warn("row page sub-page copies not named", "page", job.PageID, "error", err)
		}
		payload.SubPageJobs, payload.Waits = nil, 0
	}
	if payload.Next >= len(payload.Databases) {
		return saveRowPageDatabaseCopyJob(app, job, payload, 0, true)
	}
	if payload.MoveJobID == "" {
		return r.beginRowPageDatabaseCopy(job, payload)
	}
	// The copy goes on in its own job's turns too (advanceBlockMove).
	if err := advanceBlockMove(ctx, app, payload.MoveJobID); err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	done, failed, err := rowPageDatabaseCopyDone(app, payload.MoveJobID)
	if err != nil {
		return err
	}
	if !done {
		return saveRowPageDatabaseCopyJob(app, job, payload, 0, false)
	}
	if failed {
		app.Logger().Warn("row page database copy failed", "page", job.PageID, "copy", payload.MoveJobID)
	} else {
		if database := payload.Databases[payload.Next]; database.Replaces {
			// The block that named the database copied goes (database_row_page_foreign_databases.go).
			if err := removeRowPageForeignDatabaseBlock(app, payload.ActorID, job.PageID, database.SourceBlockID); err != nil {
				app.Logger().Warn("row page database block not replaced", "page", job.PageID, "block", database.SourceBlockID, "error", err)
			}
		}
		// What the body names of the database names its copy
		// (database_row_page_database_copy_names.go).
		if err := nameRowPageDatabaseCopy(app, payload.ActorID, job.PageID, payload.MoveJobID); err != nil {
			app.Logger().Warn("row page database copy not named", "page", job.PageID, "copy", payload.MoveJobID, "error", err)
		}
	}
	payload.Next, payload.MoveJobID, payload.Waits = payload.Next+1, "", 0
	return saveRowPageDatabaseCopyJob(app, job, payload, 0, payload.Next >= len(payload.Databases))
}

// beginRowPageDatabaseCopy begins the next database's copy at its place,
// after the database before it when that one went right before it; one that
// can't begin yet waits its turn.
func (r *durableJobRunner) beginRowPageDatabaseCopy(job storedJob, payload rowPageDatabaseCopyPayload) error {
	app := r.app
	database := payload.Databases[payload.Next]
	input := blockMoveInput{
		// Named by the job, so a turn after a stop finds the copy it began.
		RequestID:         job.ID + ":" + strconv.Itoa(payload.Next),
		DestinationPageID: job.PageID,
		PlaceParent:       database.ParentID,
		PlaceAfter:        database.AfterID,
	}
	if database.AfterDatabase != "" {
		input.PlaceAfter = payload.Copies[database.AfterDatabase]
	}
	if input.PlaceAfter == "" {
		input.PlaceAfter = blockCopyPlaceFirst
	}
	// A database another page holds is copied from there (placeRowPageOwnDatabaseHolders).
	fromPageID, fromBlockID := payload.SourcePageID, database.SourceBlockID
	if database.FromBlockID != "" {
		fromPageID, fromBlockID = database.FromPageID, database.FromBlockID
	}
	admission, err := admitDatabaseTemplateCopy(app, payload.ActorID, fromPageID, fromBlockID, input)
	if err != nil {
		held := errors.Is(err, errBlockMoveConflict)
		if !held && isPermissionDenied(err) {
			// Held by an operation under way (its page's copy), not out of reach.
			reason, reasonErr := pageBusyReasonOf(app, job.PageID, false)
			held = reasonErr == nil && reason != ""
		}
		if held && payload.Waits < rowPageDatabaseCopyMaxWaits {
			wait := rowPageDatabaseCopyWait
			if payload.Waits >= rowPageDatabaseCopyQuickWaits {
				wait = rowPageDatabaseCopySlowWait
			}
			payload.Waits++
			return saveRowPageDatabaseCopyJob(app, job, payload, wait, false)
		}
		app.Logger().Warn("row page database copy refused", "page", job.PageID, "block", database.SourceBlockID, "error", err)
		payload.Next, payload.Waits = payload.Next+1, 0
		return saveRowPageDatabaseCopyJob(app, job, payload, 0, payload.Next >= len(payload.Databases))
	}
	payload.MoveJobID, payload.Waits = admission.Job.ID, 0
	payload.Copies[database.SourceBlockID] = admission.Job.TargetRootBlockID
	return saveRowPageDatabaseCopyJob(app, job, payload, 0, false)
}

// rowPageDatabaseCopyDone says whether a database's copy has ended, with
// the pages its rows took, and whether it failed.
func rowPageDatabaseCopyDone(app core.App, moveJobID string) (bool, bool, error) {
	move, err := loadBlockMoveJobByID(app, moveJobID, "")
	if err != nil {
		return false, false, err
	}
	if move.Status == durableJobStatusFailed || move.Phase == blockMovePhaseFailed {
		return true, true, nil
	}
	if move.Phase != blockMovePhaseCompleted {
		return false, false, nil
	}
	var pending int
	err = app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_page_duplication_jobs}} WHERE ` +
		partialIndexKey(`[[parent_block_copy_job_id]]`, `={:id}`) + ` AND [[phase]] NOT IN ('completed','failed'))`).
		Bind(dbx.Params{"id": moveJobID}).Row(&pending)
	return pending == 0, false, err
}

// saveRowPageDatabaseCopyJob keeps where the job is, its next step after
// wait, or ends it.
func saveRowPageDatabaseCopyJob(app core.App, job storedJob, payload rowPageDatabaseCopyPayload, wait time.Duration, done bool) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	status := durableJobStatusQueued
	if done {
		status = durableJobStatusCompleted
	}
	if done && payload.ImportTemplateRow {
		// The row an import made the template of goes with the copy's end;
		// one gone already, or that can't go, stays as it is.
		if err := app.RunInTransaction(func(txApp core.App) error {
			if err := removeNotionImportTemplateRow(txApp, payload.ActorID, payload.ImportOwnerID, job.WorkspaceID, payload.SourcePageID); err != nil {
				return err
			}
			return retireNotionImportTemplateSubPages(txApp, payload.ImportOwnerID, job.WorkspaceID, payload.SourcePageID, job.PageID)
		}); err != nil {
			app.Logger().Warn("import template row not removed", "row", payload.SourcePageID, "error", err)
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]]={:payload}, [[status]]={:status}, [[processed_items]]={:processed},
			[[not_before]]={:notBefore}, [[updated]]={:updated}, [[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:id} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"payload": string(encoded), "status": status, "processed": payload.Next,
		"notBefore": now.Add(wait).UnixMilli(), "updated": now.UnixMilli(), "id": job.ID,
	}).Execute()
	if err == nil && done && payload.ImportTemplateRow {
		err = wakeNotionImportAfterTemplateCopy(app, payload.ImportJobID)
	}
	return err
}
