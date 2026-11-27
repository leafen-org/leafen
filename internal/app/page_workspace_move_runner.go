package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A move goes through its tables a window at a time
// (pageWorkspaceMoveLanes), each step one short write: a window changes at
// most pageWorkspaceMoveWindowRows rows, and a step stops taking windows
// past pageWorkspaceMoveStepBudget, so a page edited elsewhere meanwhile
// waits for one window at most. Where it stands (its lane, and in the
// search index's lane the page it reached) is kept with each step, so a
// restart goes on from there; a window takes only rows still in the
// workspace it leaves, so one done twice changes nothing. Going back takes
// the same lanes the other way, from where it stopped.

const (
	pageWorkspaceMoveWindowRows      = 256
	pageWorkspaceMoveSearchPages     = 32
	pageWorkspaceMoveStepBudget      = 40 * time.Millisecond
	pageWorkspaceMoveStepMostWindows = 64
)

func advancePageWorkspaceMove(ctx context.Context, app core.App, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		move, err := loadPageWorkspaceMove(txApp, jobID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		job, err := loadStoredJob(txApp, jobID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		switch move.Phase {
		case pageWorkspaceMovePhaseMoving:
			if lost, err := pageWorkspaceMoveDestinationLost(txApp, move); err != nil || lost {
				if err != nil {
					return err
				}
				return beginPageWorkspaceMoveRollback(txApp, move, errorCodePageWorkspaceMoveAccessLost)
			}
			done, err := stepPageWorkspaceMoveLanes(txApp, &move, false)
			if err != nil {
				return err
			}
			if done {
				return finishPageWorkspaceMove(txApp, move)
			}
		case pageWorkspaceMovePhaseRollingBack:
			done, err := stepPageWorkspaceMoveLanes(txApp, &move, true)
			if err != nil {
				return err
			}
			if done {
				return failPageWorkspaceMove(txApp, move)
			}
		default:
			return nil
		}
		return reschedulePageWorkspaceMove(txApp, jobID)
	})
}

// pageWorkspaceMoveDestinationLost says the mover may no longer add pages
// where the move goes: a failure that takes the move back.
func pageWorkspaceMoveDestinationLost(app core.App, move storedPageWorkspaceMove) (bool, error) {
	target, err := authorizeWorkspace(app, move.ActorID, move.TargetWorkspaceID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	err = pageWorkspaceMoveDestinationWritable(app, move, target)
	if errors.Is(err, errPageWorkspaceMoveConflict) || isPermissionDenied(err) || destinationRefused(err) {
		return true, nil
	}
	return false, err
}

// stepPageWorkspaceMoveLanes takes windows from where the move stands,
// forward or back, until its budget; done once every lane is.
func stepPageWorkspaceMoveLanes(app core.App, move *storedPageWorkspaceMove, back bool) (bool, error) {
	started := time.Now()
	directions, err := pageWorkspaceMoveDirections(app, *move, back)
	if err != nil {
		return false, err
	}
	done := false
	for windows := 0; windows < pageWorkspaceMoveStepMostWindows && time.Since(started) < pageWorkspaceMoveStepBudget; windows++ {
		if (!back && move.Lane >= len(pageWorkspaceMoveLanes)) || (back && move.Lane < 0) {
			done = true
			break
		}
		lane := pageWorkspaceMoveLanes[move.Lane]
		changed, cursor, full, err := pageWorkspaceMoveWindow(app, *move, lane, directions, back)
		if err != nil {
			return false, err
		}
		move.MovedRows += changed
		move.Cursor = cursor
		if !full {
			move.Cursor = ""
			if back {
				move.Lane--
			} else {
				move.Lane++
			}
		}
	}
	if (!back && move.Lane >= len(pageWorkspaceMoveLanes)) || (back && move.Lane < 0) {
		done = true
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_workspace_moves}}
		SET [[lane]] = {:lane}, [[cursor]] = {:cursor}, [[moved_rows]] = {:moved},
			[[revision]] = [[revision]] + 1, [[updated_at]] = {:now}
		WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{
		"lane": move.Lane, "cursor": move.Cursor, "moved": move.MovedRows,
		"now": time.Now().UTC().UnixMilli(), "jobId": move.JobID,
	}).Execute()
	return done, err
}

// pageWorkspaceMoveDirection is what a window sets and from what.
type pageWorkspaceMoveDirection struct {
	fromWorkspace, toWorkspace string
	fromOwner, toOwner         string
	toOrganization             string
}

func pageWorkspaceMoveDirections(app core.App, move storedPageWorkspaceMove, back bool) (pageWorkspaceMoveDirection, error) {
	source, err := findRecordRemembered(app, workspacesCollection, move.SourceWorkspaceID)
	if err != nil {
		return pageWorkspaceMoveDirection{}, err
	}
	target, err := findRecordRemembered(app, workspacesCollection, move.TargetWorkspaceID)
	if err != nil {
		return pageWorkspaceMoveDirection{}, err
	}
	direction := pageWorkspaceMoveDirection{
		fromWorkspace: move.SourceWorkspaceID, toWorkspace: move.TargetWorkspaceID,
		fromOwner: move.SourceOwnerID, toOwner: move.TargetOwnerID,
		toOrganization: target.GetString("organization"),
	}
	if back {
		direction = pageWorkspaceMoveDirection{
			fromWorkspace: move.TargetWorkspaceID, toWorkspace: move.SourceWorkspaceID,
			fromOwner: move.TargetOwnerID, toOwner: move.SourceOwnerID,
			toOrganization: source.GetString("organization"),
		}
	}
	return direction, nil
}

// pageWorkspaceMoveWindow changes one window of a lane's rows and says how
// many, where it got to, and whether the window was full (the lane may have
// more).
func pageWorkspaceMoveWindow(
	app core.App, move storedPageWorkspaceMove, lane pageWorkspaceMoveTable, direction pageWorkspaceMoveDirection, back bool,
) (int, string, bool, error) {
	params := dbx.Params{
		"jobId": move.JobID, "kind": lane.Kind, "from": direction.fromWorkspace, "to": direction.toWorkspace,
		"fromOwner": direction.fromOwner, "toOwner": direction.toOwner, "toOrganization": direction.toOrganization,
		"limit": pageWorkspaceMoveWindowRows,
	}
	kind := ""
	if lane.Kind != pageWorkspaceMoveItemAny {
		kind = ` AND [[item]].[[kind]] = {:kind}`
	}
	if lane.Rule == pageWorkspaceMoveRuleReindex {
		// The search index takes its pages again once the move ends either way.
		if back {
			return 0, "", false, nil
		}
		if lane.Table == "leafen_search_pages_fts" {
			return clearPageWorkspaceMoveSearchIndex(app, move)
		}
		result, err := app.DB().NewQuery(`
			DELETE FROM {{` + lane.Table + `}} WHERE [[rowid]] IN (
				SELECT [[row]].[[rowid]] FROM {{leafen_page_workspace_move_items}} AS [[item]]
				JOIN {{` + lane.Table + `}} AS [[row]] ON [[row]].[[` + lane.Key + `]] = [[item]].[[item_id]]
				WHERE [[item]].[[job_id]] = {:jobId}` + kind + ` LIMIT {:limit})
		`).Bind(params).Execute()
		return pageWorkspaceMoveWindowResult(result, err)
	}
	sets, scope := []string{}, ""
	if lane.Workspace != "" {
		sets = append(sets, `[[`+lane.Workspace+`]] = {:to}`)
		scope = ` AND [[row]].[[` + lane.Workspace + `]] = {:from}`
	}
	if move.ownersDiffer() {
		for _, owner := range lane.Owners {
			sets = append(sets, `[[`+owner+`]] = CASE WHEN [[`+owner+`]] = {:fromOwner} THEN {:toOwner} ELSE [[`+owner+`]] END`)
		}
		if lane.Workspace == "" && len(lane.Owners) > 0 {
			scope = ` AND [[row]].[[` + lane.Owners[0] + `]] = {:fromOwner}`
		}
	}
	if lane.Organization != "" {
		sets = append(sets, `[[`+lane.Organization+`]] = {:toOrganization}`)
	}
	if scope == "" || len(sets) == 0 {
		return 0, "", false, nil
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{` + lane.Table + `}} SET ` + strings.Join(sets, ", ") + ` WHERE [[rowid]] IN (
			SELECT [[row]].[[rowid]] FROM {{leafen_page_workspace_move_items}} AS [[item]]
			JOIN {{` + lane.Table + `}} AS [[row]] ON [[row]].[[` + lane.Key + `]] = [[item]].[[item_id]]
			WHERE [[item]].[[job_id]] = {:jobId}` + kind + scope + ` LIMIT {:limit})
	`).Bind(params).Execute()
	return pageWorkspaceMoveWindowResult(result, err)
}

func pageWorkspaceMoveWindowResult(result sql.Result, err error) (int, string, bool, error) {
	if err != nil {
		return 0, "", false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, "", false, err
	}
	return int(changed), "", changed >= pageWorkspaceMoveWindowRows, nil
}

// clearPageWorkspaceMoveSearchIndex takes the next moved pages out of the
// search index, which finds them in their new workspace once they are
// indexed there (finishPageWorkspaceMove).
func clearPageWorkspaceMoveSearchIndex(app core.App, move storedPageWorkspaceMove) (int, string, bool, error) {
	pages := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[item_id]] FROM {{leafen_page_workspace_move_items}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] = 'page' AND [[item_id]] > {:cursor}
		ORDER BY [[item_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": move.JobID, "cursor": move.Cursor, "limit": pageWorkspaceMoveSearchPages}).Column(&pages); err != nil {
		return 0, "", false, err
	}
	for _, pageID := range pages {
		for {
			more, err := deleteWorkspaceSearchFTSPage(app, pageID)
			if err != nil {
				return 0, "", false, err
			}
			if !more {
				break
			}
		}
	}
	cursor := move.Cursor
	if len(pages) > 0 {
		cursor = pages[len(pages)-1]
	}
	return len(pages), cursor, len(pages) >= pageWorkspaceMoveSearchPages, nil
}

func reschedulePageWorkspaceMove(app core.App, jobID string) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[not_before]]=MAX([[not_before]], {:notBefore}),[[updated]]={:updatedAt},
			[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"notBefore": now, "updatedAt": now, "jobId": jobID}).Execute()
	return err
}

// beginPageWorkspaceMoveRollback turns the move back from where it stands:
// its lanes the other way, from the one it was in.
func beginPageWorkspaceMoveRollback(app core.App, move storedPageWorkspaceMove, errorCode string) error {
	now := time.Now().UTC().UnixMilli()
	lane := min(move.Lane, len(pageWorkspaceMoveLanes)-1)
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_workspace_moves}}
		SET [[phase]] = 'rolling_back', [[lane]] = {:lane}, [[cursor]] = '', [[error_code]] = {:errorCode},
			[[revision]] = [[revision]] + 1, [[updated_at]] = {:now}
		WHERE [[job_id]] = {:jobId} AND [[phase]] = 'moving'
	`).Bind(dbx.Params{"lane": lane, "errorCode": errorCode, "now": now, "jobId": move.JobID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]]={:now},[[updated]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"now": now, "jobId": move.JobID}).Execute()
	return err
}

// failPageWorkspaceMove ends a move taken back: its pages are where they
// were, indexed for search there again, and let go.
func failPageWorkspaceMove(app core.App, move storedPageWorkspaceMove) error {
	if err := reindexPageWorkspaceMovePages(app, move, move.SourceWorkspaceID); err != nil {
		return err
	}
	if err := notePageWorkspaceMoveListed(app, move, move.SourceWorkspaceID); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	errorCode := move.ErrorCode
	if errorCode == "" {
		errorCode = errorCodePageWorkspaceMoveUnavailable
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_workspace_moves}}
		SET [[phase]] = 'failed', [[error_code]] = {:errorCode}, [[revision]] = [[revision]] + 1,
			[[updated_at]] = {:now}, [[completed_at]] = {:now}
		WHERE [[job_id]] = {:jobId} AND [[phase]] = 'rolling_back'
	`).Bind(dbx.Params{"errorCode": errorCode, "now": now, "jobId": move.JobID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='failed',[[error_code]]={:errorCode},[[updated]]={:now},[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"errorCode": errorCode, "now": now, "jobId": move.JobID}).Execute()
	if err == nil {
		recheckCollaborationAccessAfterCommit(app)
	}
	return err
}

// reindexPageWorkspaceMovePages has the search index take the moved pages
// again, in the workspace they are in.
func reindexPageWorkspaceMovePages(app core.App, move storedPageWorkspaceMove, workspaceID string) error {
	pages := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[item_id]] FROM {{leafen_page_workspace_move_items}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] = 'page' ORDER BY [[item_id]]
	`).Bind(dbx.Params{"jobId": move.JobID}).Column(&pages); err != nil {
		return err
	}
	targets := make([]searchDirtyTarget, 0, len(pages))
	for _, pageID := range pages {
		targets = append(targets, searchDirtyTarget{PageID: pageID, WorkspaceID: workspaceID})
	}
	for start := 0; start < len(targets); start += 500 {
		if err := markSearchDirtyTargets(context.Background(), app, targets[start:min(start+500, len(targets))]); err != nil {
			return err
		}
	}
	return nil
}

// schedulePageWorkspaceMoveFailure has a move whose step keeps failing go
// back once its attempts are spent, as a block move does
// (scheduleBlockMoveFailure); one going back tries again.
func schedulePageWorkspaceMoveFailure(app core.App, jobID, reference string, applyErr error) (bool, error) {
	job, err := loadStoredJob(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Kind != pageWorkspaceMoveJobKind) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	move, err := loadPageWorkspaceMove(app, jobID)
	if err != nil {
		return true, err
	}
	now := time.Now().UTC().UnixMilli()
	if move.Phase == pageWorkspaceMovePhaseMoving && job.Attempt+1 >= job.MaxAttempts {
		err = app.RunInTransaction(func(txApp core.App) error {
			if err := beginPageWorkspaceMoveRollback(txApp, move, errorCodePageWorkspaceMoveUnavailable); err != nil {
				return err
			}
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_jobs}} SET [[attempt]]=0,[[diagnostic_reference]]={:reference},[[updated]]={:now}
				WHERE [[id]]={:jobId}
			`).Bind(dbx.Params{"jobId": jobID, "reference": reference, "now": now}).Execute()
			return err
		})
		if err == nil {
			logDurableJobDiagnostic(app, errorCodePageWorkspaceMoveUnavailable, reference, jobID,
				"page-workspace-move.apply", "rollback-scheduled", applyErr)
		}
		return true, err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[attempt]]=MIN([[attempt]]+1, [[max_attempts]]-1),[[diagnostic_reference]]={:reference},
			[[not_before]]={:notBefore},[[updated]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"jobId": jobID, "reference": reference, "notBefore": now + durableJobReadyDelay.Milliseconds(), "now": now,
	}).Execute()
	if err == nil {
		logDurableJobDiagnostic(app, errorCodePageWorkspaceMoveUnavailable, reference, jobID,
			"page-workspace-move.apply", "retry-scheduled", applyErr)
	}
	return true, err
}
