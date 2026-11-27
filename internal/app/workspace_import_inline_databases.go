package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An export's page draws a database it holds inline as the database's table
// (Notion's HTML export and Leafen's), and the database's rows come as a CSV
// in the page's folder, which a batch brings first, as a full-page database
// under the page. Brought back, the database stands inline where its page
// drew it, with its rows, properties and views, and no page of its own: the
// page's text names it by a line linking it marked inline
// ([](leafen-page-link:inline), htmlImportDatabases.ts), where its page's
// block stands, as a sub-page's does; once the text is in, the import turns
// it inline there, as the block menu's "인라인으로 전환" does
// (databaseTurn), its emptied page going to the trash. Notion's own HTML
// import brings its export's inline database as a table of text columns in
// place and the CSV's database apart (seen 2026-11-15); Leafen keeps the one
// database instead. The batch's record of the database then names the page
// it stands in, so the ZIP's page mappings never name the trashed page.

const (
	// workspaceImportInlineDatabaseMoveWait is how long a turn moves a
	// database before it lets the next job in, and
	// workspaceImportInlineDatabaseMoveDelay how long it waits after one
	// still moving.
	workspaceImportInlineDatabaseMoveWait  = 5 * time.Second
	workspaceImportInlineDatabaseMoveDelay = time.Second
)

// workspaceImportInlineDatabasesOf takes the marks of the databases a page
// holds inline out of its staged items into found, each once, in order.
func workspaceImportInlineDatabasesOf(items workspaceImportItemSource, found *[]string) workspaceImportItemSource {
	return func(emit func(workspaceImportStagedItem) error) error {
		*found = (*found)[:0]
		return items(func(item workspaceImportStagedItem) error {
			if item.InlineDatabase == "" {
				return emit(item)
			}
			if !slices.Contains(*found, item.InlineDatabase) {
				*found = append(*found, item.InlineDatabase)
			}
			return nil
		})
	}
}

// turnWorkspaceImportDatabasesInline turns the first database the import's
// page holds inline, the next in a turn of its own. One still moving is
// looked at again after a while; one that can't turn stays the full-page
// database it came as.
func (r *durableJobRunner) turnWorkspaceImportDatabasesInline(ctx context.Context, job storedJob, payload workspaceImportJobPayload) error {
	pageID := payload.InlineDatabases[0]
	moving, err := r.turnWorkspaceImportDatabaseInline(ctx, job, payload, pageID)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil && workspaceImportBatchMayWait(err):
		moving = true
	case err != nil:
		r.app.Logger().Warn("workspace import database not turned inline", "jobId", job.ID, "pageId", pageID, "error", err)
	}
	delay := workspaceImportTurnDelay
	if moving {
		delay = workspaceImportInlineDatabaseMoveDelay
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		latest, err := decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		if latest.Phase != workspaceImportPhasePublishCleanup || len(latest.InlineDatabases) == 0 || latest.InlineDatabases[0] != pageID {
			return nil
		}
		if !moving {
			latest.InlineDatabases = latest.InlineDatabases[1:]
		}
		encoded, err := json.Marshal(latest)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "owner": job.Owner, "payload": string(encoded),
			"notBefore": now + delay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
}

// turnWorkspaceImportDatabaseInline turns one database inline from where an
// earlier turn left it, and reports a move still going. Its page is the
// full-page database the batch made of its CSV right under the import's
// page: the page becomes a page, its database block moves to the end of the
// import's page by the editor's block move, and from there to where its
// page's block stands (databaseTurn.placeInline). A database whose move
// failed stays its page's.
func (r *durableJobRunner) turnWorkspaceImportDatabaseInline(
	ctx context.Context, job storedJob, payload workspaceImportJobPayload, pageID string,
) (bool, error) {
	parentID := workspaceImportPrimaryRootPageID(payload)
	page, err := r.app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if parentID == "" || page.GetString("parent_id") != parentID || page.GetString("workspace") != job.WorkspaceID {
		return false, nil
	}
	if page.GetBool("in_trash") {
		return false, r.mapWorkspaceImportInlineDatabase(payload, pageID, parentID)
	}
	turn := workspaceImportDatabaseTurnOf(job)
	requestID := "workspace-import-inline-database:" + job.ID + ":" + pageID
	moveID := ""
	if move, err := loadStoredJobByRequest(r.app, job.Owner, requestID); err == nil {
		moveID = move.ID
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	} else {
		blockID, err := workspaceImportDatabaseBlockOf(r.app, pageID)
		if err != nil || blockID == "" {
			return false, err
		}
		if page.GetString("kind") == pageKindDatabase {
			if err := turn.setKind(ctx, r.ygo, pageID, pageKindPage); err != nil {
				return false, err
			}
		}
		admission, err := admitBlockMove(r.app, job.Owner, pageID, blockID, blockMoveInput{
			RequestID: requestID, DestinationPageID: parentID, Operation: blockMoveOperationMove,
		})
		if err != nil {
			return false, errors.Join(err, turn.setKind(ctx, r.ygo, pageID, pageKindDatabase))
		}
		moveID = admission.Job.ID
	}
	phase, err := driveDataSourceMove(ctx, r.app, moveID, workspaceImportInlineDatabaseMoveWait)
	switch {
	case err != nil:
		return false, err
	case phase == blockMovePhaseFailed:
		return false, errors.Join(errors.New("the database's move failed"), turn.setKind(ctx, r.ygo, pageID, pageKindDatabase))
	case phase != blockMovePhaseCompleted:
		r.notify()
		return true, nil
	}
	move, err := loadBlockMoveJobByID(r.app, moveID, "")
	if err != nil {
		return false, err
	}
	if err := turn.placeInline(ctx, r.app, r.ygo, parentID, pageID, move.TargetRootBlockID); err != nil {
		return false, err
	}
	return false, r.mapWorkspaceImportInlineDatabase(payload, pageID, parentID)
}

// workspaceImportDatabaseBlockOf is a full-page database's block: its page's
// one database, at the top of its body; none for a page holding otherwise.
func workspaceImportDatabaseBlockOf(app core.App, pageID string) (string, error) {
	blocks := []string{}
	err := app.DB().NewQuery(`
		SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[page_id]] = {:pageId} LIMIT 2
	`).Bind(dbx.Params{"pageId": pageID}).Column(&blocks)
	if err != nil || len(blocks) != 1 {
		return "", err
	}
	var top bool
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId} AND [[id]] = {:blockId} AND [[parent_id]] = '')
	`).Bind(dbx.Params{"pageId": pageID, "blockId": blocks[0]}).Row(&top); err != nil || !top {
		return "", err
	}
	return blocks[0], nil
}

// mapWorkspaceImportInlineDatabase points the batch's record of a database
// turned inline at the page it stands in.
func (r *durableJobRunner) mapWorkspaceImportInlineDatabase(payload workspaceImportJobPayload, pageID, parentID string) error {
	if payload.BatchJobID == "" {
		return nil
	}
	_, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_workspace_import_batch_items}} SET [[page_id]] = {:parentId}
		WHERE [[job_id]] = {:batchId} AND [[page_id]] = {:pageId} AND [[kind]] = 'database'
	`).Bind(dbx.Params{"batchId": payload.BatchJobID, "pageId": pageID, "parentId": parentID}).Execute()
	return err
}
