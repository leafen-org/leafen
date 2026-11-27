package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func advanceBlockMove(ctx context.Context, app core.App, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A copy from another workspace copies the files it names first, each
	// outside a transaction, as a page's copy does (block_copy_workspaces.go).
	if copied, err := advanceBlockCopyFiles(ctx, app, jobID); err != nil || copied {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadBlockMoveJobByID(txApp, jobID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if job.Phase == blockMovePhaseRollingBack {
			return advanceBlockMoveRollback(txApp, job)
		}
		// A move whose every block has gone ends as in its last block's
		// window, whatever changed since (blockMoveEnding).
		ending, err := blockMoveEnding(txApp, job)
		if err != nil {
			return err
		}
		if !ending {
			if rollback, err := blockMoveMustGoBack(txApp, job); err != nil || rollback {
				if err != nil {
					return err
				}
				return beginBlockMoveRollback(txApp, job, errorCodeBlockMoveConflict)
			}
		}
		switch job.Phase {
		case blockMovePhaseDiscovering:
			done, err := discoverBlockMoveWindow(txApp, job)
			if err != nil {
				return err
			}
			if done {
				// A copy names the copies of the pages it carries before it
				// copies a block (block_copy_page_links.go).
				if job.Operation == blockMoveOperationCopy {
					if err := planBlockCopyPages(txApp, job); err != nil {
						return err
					}
				}
				return advanceBlockMovePhase(txApp, job, blockMovePhaseMoving)
			}
		case blockMovePhaseMoving:
			var done bool
			payload, sourceMove, payloadErr := loadDataSourceMovePayload(txApp, job.ID)
			switch {
			case payloadErr != nil:
				return payloadErr
			case sourceMove:
				done, err = advanceDataSourceMoveForward(txApp, job, payload)
			case job.Operation == blockMoveOperationCopy:
				done, err = advanceBlockCopyForward(txApp, job)
			default:
				done, err = advanceBlockMoveForward(txApp, job)
			}
			if err != nil {
				return err
			}
			if done {
				// Its blocks' windows are kept in the pages' histories at
				// once, before a copy's undo reads where they end.
				if !sourceMove {
					if err := recordBlockMoveHistory(txApp, job); err != nil {
						return err
					}
				}
				if err := completeBlockMove(txApp, job); err != nil {
					return err
				}
				// A finished copy, whose pages are open again, starts copying
				// the sub-pages whose blocks it copied.
				if job.Operation == blockMoveOperationCopy && !sourceMove {
					return copyBlockCopySubPages(txApp, job)
				}
				return nil
			}
		case blockMovePhaseCompleted, blockMovePhaseFailed:
			return nil
		default:
			return errBlockMoveConflict
		}
		return rescheduleBlockMove(txApp, job)
	})
}

func discoverBlockMoveWindow(app core.App, job storedBlockMoveJob) (bool, error) {
	// A window looks inside many blocks while it finds few, a copy's as a
	// move's (block_move_discovery.go).
	return discoverBlockMoveItems(app, job)
}

func advanceBlockMoveForward(app core.App, job storedBlockMoveJob) (bool, error) {
	databasesDone, err := advanceBlockMoveDatabaseForward(app, job)
	if err != nil || !databasesDone {
		return false, err
	}
	items, err := nextBlockMoveForwardWindow(app, job)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return true, nil
	}
	if len(items) > maxBlockMoveItemsPerTurn {
		items = items[:maxBlockMoveItemsPerTurn]
	}
	if err := moveBlockMoveWindow(app, job, items); err != nil {
		return false, err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[moved_count]]=[[moved_count]]+{:moved},[[revision]]=[[revision]]+1,
			[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='moving'
	`).Bind(dbx.Params{
		"moved": len(items), "updatedAt": now, "jobId": job.ID,
	}).Execute(); err != nil {
		return false, err
	}
	// The move ends in a window of its own, the one that keeps its pages'
	// history (recordBlockMoveHistory): with the last blocks', a long page's
	// two were one write of seconds (2026-11-23).
	var remaining int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]=0
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&remaining); err != nil {
		return false, err
	}
	if remaining == 0 {
		return false, markBlockMoveEnding(app, job)
	}
	return false, nil
}

func advanceBlockMoveRollback(app core.App, job storedBlockMoveJob) error {
	if job.Operation == blockMoveOperationCopy {
		return advanceBlockCopyRollback(app, job)
	}
	items := []storedBlockMoveItem{}
	if err := app.DB().NewQuery(`
		SELECT [[item]].[[job_id]],[[item]].[[block_id]],[[item]].[[target_block_id]],[[item]].[[parent_id]],
			[[item]].[[position]],[[item]].[[depth]],[[item]].[[discovery_cursor]],
			[[item]].[[discovered]],[[item]].[[moved]]
		FROM {{leafen_block_move_items}} [[item]]
		WHERE [[item]].[[job_id]]={:jobId} AND [[item]].[[moved]]=1
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_block_move_items}} [[parent]]
				WHERE [[parent]].[[job_id]]=[[item]].[[job_id]]
					AND [[parent]].[[block_id]]=[[item]].[[parent_id]] AND [[parent]].[[moved]]=1
			)
		ORDER BY [[item]].[[depth]],[[item]].[[position]],[[item]].[[block_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "limit": maxBlockMoveItemsPerTurn + 1,
	}).All(&items); err != nil {
		return err
	}
	if len(items) == 0 {
		done, err := advanceBlockMoveDatabaseRollback(app, job)
		if err != nil {
			return err
		}
		if done {
			return failBlockMove(app, job)
		}
		return rescheduleBlockMove(app, job)
	}
	if len(items) > maxBlockMoveItemsPerTurn {
		items = items[:maxBlockMoveItemsPerTurn]
	}
	if err := rollbackBlockMoveWindow(app, job, items); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[moved_count]]=[[moved_count]]-{:moved},[[revision]]=[[revision]]+1,
			[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='rolling_back'
	`).Bind(dbx.Params{
		"moved": len(items), "updatedAt": now, "jobId": job.ID,
	}).Execute(); err != nil {
		return err
	}
	var remaining int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]=1
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		// Every block is back: the pages' histories keep them so.
		if err := recordBlockMoveHistory(app, job); err != nil {
			return err
		}
		job.MovedCount = 0
	}
	return rescheduleBlockMove(app, job)
}

func beginBlockMoveRollback(app core.App, job storedBlockMoveJob, errorCode string) error {
	now := time.Now().UTC().UnixMilli()
	var movedDatabases int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_block_move_database_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]=1
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&movedDatabases); err != nil {
		return err
	}
	if job.MovedCount == 0 && movedDatabases == 0 {
		job.ErrorCode = errorCode
		return failBlockMove(app, job)
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[phase]]='rolling_back',[[error_code]]={:errorCode},
			[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]] NOT IN ('completed','failed')
	`).Bind(dbx.Params{
		"errorCode": errorCode, "updatedAt": now, "jobId": job.ID,
	}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]]={:notBefore},[[updated]]={:updatedAt}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"notBefore": now, "updatedAt": now, "jobId": job.ID}).Execute()
	return err
}

func advanceBlockMovePhase(app core.App, job storedBlockMoveJob, phase string) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[phase]]={:phase},[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]={:currentPhase}
	`).Bind(dbx.Params{
		"phase": phase, "updatedAt": now, "jobId": job.ID, "currentPhase": job.Phase,
	}).Execute()
	return err
}

func rescheduleBlockMove(app core.App, job storedBlockMoveJob) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[not_before]]=MAX([[not_before]], {:notBefore}),[[updated]]={:updatedAt},
			[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"notBefore": now, "updatedAt": now, "jobId": job.ID}).Execute()
	return err
}

func completeBlockMove(app core.App, job storedBlockMoveJob) error {
	if err := captureNativeBlockCopyUndo(app, job); err != nil {
		return err
	}
	if err := keepBlockCopyCarriedPeople(app, job); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[phase]]='completed',[[revision]]=[[revision]]+1,
			[[updated_at]]={:updatedAt},[[completed_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='moving'
	`).Bind(dbx.Params{"updatedAt": now, "jobId": job.ID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='completed',[[processed_items]]={:processed},[[total_items]]={:processed},
			[[updated]]={:updatedAt},[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"processed": job.DiscoveredCount, "updatedAt": now, "jobId": job.ID,
	}).Execute()
	if err != nil {
		return err
	}
	if err := endBlockCopyFiles(app, job); err != nil {
		return err
	}
	return continueBlockMoveReturns(app, job)
}

func failBlockMove(app core.App, job storedBlockMoveJob) error {
	now := time.Now().UTC().UnixMilli()
	errorCode := job.ErrorCode
	if errorCode == "" {
		errorCode = errorCodeBlockMoveUnavailable
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[phase]]='failed',[[moved_count]]=0,[[error_code]]={:errorCode},
			[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt},[[completed_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]] NOT IN ('completed','failed')
	`).Bind(dbx.Params{
		"errorCode": errorCode, "updatedAt": now, "jobId": job.ID,
	}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='failed',[[error_code]]={:errorCode},[[updated]]={:updatedAt},
			[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"errorCode": errorCode, "updatedAt": now, "jobId": job.ID,
	}).Execute()
	if err != nil {
		return err
	}
	if err := endBlockCopyFiles(app, job); err != nil {
		return err
	}
	job.Phase = blockMovePhaseFailed
	return continueBlockMoveReturns(app, job)
}
