package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A batch's pages are made as its documents go in, a few at a time, parents
// first (makeWorkspaceImportBatchPages).

type workspaceImportBatchPageRow struct {
	Position       int            `db:"position"`
	ParentPosition int            `db:"parent_position"`
	Title          string         `db:"title"`
	Name           string         `db:"name"`
	PageID         string         `db:"page_id"`
	ParentPageID   sql.NullString `db:"parent_page_id"`
	ParentReady    sql.NullBool   `db:"parent_ready"`
	// ParentRefusal is the code its parent's page was refused with, on an
	// earlier turn.
	ParentRefusal sql.NullString `db:"parent_refusal"`
}

// A database's row pages (an export's, archiveImportPlan.ts) are its rows,
// which its import makes: they are never made as pages here.
const workspaceImportBatchPageRowsSQL = `
	SELECT [[item]].[[position]], [[item]].[[parent_position]], [[item]].[[title]], [[item]].[[name]], [[item]].[[page_id]],
		[[parent]].[[page_id]] AS [[parent_page_id]], [[parent]].[[page_ready]] AS [[parent_ready]],
		[[parent]].[[error_code]] AS [[parent_refusal]]
	FROM {{leafen_workspace_import_batch_items}} AS [[item]]
	LEFT JOIN {{leafen_workspace_import_batch_items}} AS [[parent]]
		ON [[parent]].[[job_id]] = [[item]].[[job_id]] AND [[parent]].[[position]] = [[item]].[[parent_position]]
	WHERE [[item]].[[job_id]] = {:jobId} AND [[item]].[[page_ready]] = 0
		AND ([[item]].[[kind]] = 'folder' OR [[item]].[[kind]] = 'document' AND COALESCE([[parent]].[[kind]], '') <> 'database')
		AND ([[item]].[[state]] IN ('pending', 'received') OR ([[item]].[[state]] = 'failed' AND [[item]].[[error_code]] = ''))`

// loadWorkspaceImportBatchPagesFor are the pages not made yet that the items
// at these positions go into or under, folders first.
func loadWorkspaceImportBatchPagesFor(app core.App, jobID string, positions []int) ([]workspaceImportBatchPageRow, error) {
	rows := []workspaceImportBatchPageRow{}
	if len(positions) == 0 {
		return rows, nil
	}
	params := dbx.Params{"jobId": jobID}
	placeholders := make([]string, 0, len(positions))
	for index, position := range positions {
		key := fmt.Sprintf("position%d", index)
		params[key] = position
		placeholders = append(placeholders, "{:"+key+"}")
	}
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[wanted]] ([[position]]) AS (
			SELECT [[position]] FROM {{leafen_workspace_import_batch_items}}
			WHERE [[job_id]] = {:jobId} AND [[position]] IN (` + strings.Join(placeholders, ", ") + `)
			UNION
			SELECT [[below]].[[parent_position]] FROM {{leafen_workspace_import_batch_items}} AS [[below]]
			JOIN [[wanted]] ON [[wanted]].[[position]] = [[below]].[[position]]
			WHERE [[below]].[[job_id]] = {:jobId} AND [[below]].[[parent_position]] >= 0
		)` + workspaceImportBatchPageRowsSQL + `
			AND [[item]].[[position]] IN (SELECT [[position]] FROM [[wanted]])
		ORDER BY [[item]].[[position]]
	`).Bind(params).All(&rows)
	return rows, err
}

// loadWorkspaceImportBatchPagesLeft are the next pages no document went into
// or under, made as the batch ends: folders with nothing to import and the
// files that couldn't be read, which the log names.
func loadWorkspaceImportBatchPagesLeft(app core.App, jobID string) ([]workspaceImportBatchPageRow, error) {
	rows := []workspaceImportBatchPageRow{}
	err := app.DB().NewQuery(workspaceImportBatchPageRowsSQL + `
		ORDER BY [[item]].[[position]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "limit": workspaceImportBatchPagesPerTurn}).All(&rows)
	return rows, err
}

// makeWorkspaceImportBatchPageRows makes these pages of the batch, parents
// first. A parent held for a while has the batch wait for it, this page and
// the ones after it made on a later turn (workspace_import_held.go). A place
// that went to the trash on the way (pageCreationParentInTrash) refuses its
// pages as the trash, and the pages under them say so too, as the batch's
// documents placed there are refused (workspaceImportTargetGone): none is
// made inside the trash.
func (r *durableJobRunner) makeWorkspaceImportBatchPageRows(
	ctx context.Context,
	job storedJob,
	payload workspaceImportJobPayload,
	rows []workspaceImportBatchPageRow,
) error {
	refused, hold := map[int]string{}, newWorkspaceImportBatchHold(payload, r.clock.now())
	// A parent may be made in this same turn, right before its pages.
	made, waiting := map[int]bool{}, false
	if err := r.app.RunInTransaction(func(txApp core.App) error {
		bodies := map[string]*workspaceImportBodyMentions{}
	turn:
		for _, row := range rows {
			input := pageCreationAdmissionInput{
				RequestID:      fmt.Sprintf("workspace-import-batch-%s-%d", job.ID, row.Position),
				WorkspaceID:    job.WorkspaceID,
				Title:          row.Title,
				PreparedPageID: row.PageID,
			}
			switch {
			case row.ParentPosition >= 0 && !made[row.ParentPosition] && !(row.ParentReady.Valid && row.ParentReady.Bool):
				refused[row.Position] = errorCodePageCreateParentConflict
				if refused[row.ParentPosition] == errorCodePageInTrash || row.ParentRefusal.String == errorCodePageInTrash {
					refused[row.Position] = errorCodePageInTrash
				}
				hold.words[row.Position] = hold.words[row.ParentPosition]
				continue
			case row.ParentPosition >= 0:
				input.ParentID = row.ParentPageID.String
				if err := placeWorkspaceImportBatchPage(txApp, job.ID, row, &input, bodies); err != nil {
					return err
				}
			case payload.ParentPageID != "":
				input.ParentID = payload.ParentPageID
			default:
				input.TeamspaceID = payload.TeamspaceID
			}
			input.Imported = true
			_, status, err := admitPageCreationTransaction(txApp, job.Owner, input)
			switch {
			case isDLPPolicyDecisionError(err):
				refused[row.Position] = stableWorkspaceImportFailureCode(err)
			case err != nil:
				return err
			case status == pageCreationWorkspaceMissing || status == pageCreationParentDenied:
				refused[row.Position] = errorCodePermissionDenied
			case status == pageCreationParentInTrash:
				refused[row.Position] = errorCodePageInTrash
			case status != pageCreationApplied && hold.waits(row.Position, pageHeldForMember(txApp, job.Owner, input.ParentID)):
				waiting = true
				break turn
			case status != pageCreationApplied:
				refused[row.Position] = errorCodePageCreateParentConflict
			default:
				made[row.Position] = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// A page made under a wiki's page becomes the wiki's, a batch's top page
	// a database's row (settleWorkspaceImportBatchPage); asked again on a stop.
	if r.ygo != nil {
		for _, row := range rows {
			if !made[row.Position] {
				continue
			}
			if err := r.settleWorkspaceImportBatchPage(ctx, job, payload, row); err != nil {
				return err
			}
		}
	}
	delay := workspaceImportTurnDelay
	if waiting {
		delay = durableJobReadyDelay
	}
	return r.scheduleWorkspaceImportBatch(job, delay, func(txApp core.App, current *workspaceImportJobPayload) error {
		hold.keep(current)
		for _, row := range rows {
			code, failed := refused[row.Position]
			if !failed && !made[row.Position] {
				// Waiting for its held parent.
				continue
			}
			statement, params := `
				UPDATE {{leafen_workspace_import_batch_items}}
				SET [[page_ready]] = 1, [[state]] = CASE WHEN [[kind]] = 'folder' THEN 'imported' ELSE [[state]] END
				WHERE [[job_id]] = {:jobId} AND [[position]] = {:position}`, dbx.Params{"jobId": job.ID, "position": row.Position}
			if failed {
				// A held parent's words, the batch done waiting for it.
				statement = `
					UPDATE {{leafen_workspace_import_batch_items}} SET [[state]] = 'failed', [[error_code]] = {:code},
						[[message]] = CASE WHEN {:message} = '' THEN [[message]] ELSE {:message} END
					WHERE [[job_id]] = {:jobId} AND [[position]] = {:position}`
				params["code"], params["message"] = code, hold.words[row.Position]
			}
			if _, err := txApp.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}
