package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedWikiMutationItem struct {
	JobID           string `db:"job_id"`
	PageID          string `db:"page_id"`
	ParentID        string `db:"parent_id"`
	Depth           int    `db:"depth"`
	DiscoveryCursor string `db:"discovery_cursor"`
	Discovered      bool   `db:"discovered"`
	Applied         bool   `db:"applied"`
}

type wikiChild struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
}

func advanceWikiMutation(ctx context.Context, app core.App, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := loadWikiMutationByID(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err = loadWikiMutationByID(txApp, jobID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = authorizeWorkspace(
			txApp, job.ActorID, job.WorkspaceID, permissionWorkspaceWrite,
		); err != nil {
			return refusedUnlessStorage(errWikiConflict, err)
		}
		target, err := txApp.FindRecordById(pagesCollection, job.TargetPageID)
		if err != nil || target.GetString("wiki_active_job_id") != job.ID {
			return refusedUnlessStorage(errWikiConflict, err)
		}
		switch job.Phase {
		case wikiMutationPhaseDiscovering:
			done, discoverErr := discoverWikiMutationWindow(txApp, job)
			if discoverErr != nil {
				return discoverErr
			}
			if done {
				return advanceWikiMutationPhase(txApp, job, wikiMutationPhaseApplying)
			}
		case wikiMutationPhaseApplying:
			done, applyErr := applyWikiMutationWindow(job)
			if applyErr != nil {
				return applyErr
			}
			if done {
				return advanceWikiMutationPhase(txApp, job, wikiMutationPhasePublishing)
			}
		case wikiMutationPhasePublishing:
			return publishWikiMutation(txApp, job, target)
		case wikiMutationPhaseCleaning:
			return cleanWikiMutationWindow(txApp, job, target)
		case wikiMutationPhaseCompleted, wikiMutationPhaseFailed:
			return nil
		default:
			return errWikiConflict
		}
		return rescheduleWikiMutation(txApp, job.ID, job.Phase)
	})
}

func discoverWikiMutationWindow(app core.App, job storedWikiMutation) (bool, error) {
	item := storedWikiMutationItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[page_id]],[[parent_id]],[[depth]],[[discovery_cursor]],[[discovered]],[[applied]]
		FROM {{leafen_wiki_mutation_items}}
		WHERE [[job_id]]={:jobId} AND [[discovered]]=0
		ORDER BY [[depth]],[[page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&item)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	children := []wikiChild{}
	// The flags are read after the row (+): SQLite, which has no statistics,
	// took the (workspace, trash) index and read the workspace's every page.
	err = app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]] FROM {{leafen_pages}}
		WHERE [[workspace]]={:workspaceId} AND [[parent_id]]={:pageId}
			AND [[id]]>{:cursor} AND +[[notion_import_staging]]=0 AND +[[in_trash]]=0
			AND [[duplication_target_job_id]]='' ORDER BY [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": job.WorkspaceID, "pageId": item.PageID,
		"cursor": item.DiscoveryCursor, "limit": maxWikiMutationItemsPerTurn + 1,
	}).All(&children)
	if err != nil {
		return false, err
	}
	hasMore := len(children) > maxWikiMutationItemsPerTurn
	if hasMore {
		children = children[:maxWikiMutationItemsPerTurn]
	}
	now := time.Now().UTC().UnixMilli()
	if err = insertWikiMutationItems(app, job, item, children, now); err != nil {
		return false, err
	}
	nextCursor := item.DiscoveryCursor
	if len(children) > 0 {
		nextCursor = children[len(children)-1].ID
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutation_items}}
		SET [[discovery_cursor]]={:cursor},[[discovered]]={:done},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"cursor": nextCursor, "done": !hasMore, "now": now,
		"jobId": job.ID, "pageId": item.PageID,
	}).Execute()
	if err != nil {
		return false, err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutations}}
		SET [[item_count]]=[[item_count]]+{:added},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
	`).Bind(dbx.Params{"added": len(children), "now": now, "jobId": job.ID}).Execute()
	if err == nil {
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[total_items]]=[[total_items]]+{:added},[[updated]]={:now}
			WHERE [[id]]={:jobId} AND [[status]]='queued'
		`).Bind(dbx.Params{"added": len(children), "now": now, "jobId": job.ID}).Execute()
	}
	return false, err
}

// applyWikiMutationWindow takes a step of a wiki job the database steps do
// not: a page becomes a wiki through content changes of the page, which the
// job runner makes (wiki_database_job.go), and undoing a wiki is done once
// those steps have made its database's pages pages again.
func applyWikiMutationWindow(job storedWikiMutation) (bool, error) {
	if job.Operation == wikiOperationUndo {
		return true, nil
	}
	return false, errWikiConflict
}

func publishWikiMutation(app core.App, job storedWikiMutation, target *core.Record) error {
	if job.Operation == wikiOperationUndo && target.GetBool("is_wiki") {
		target.Set("is_wiki", false)
		target.Set("wiki_revision", job.TargetRevision)
		if err := cancelWikiVerificationGeneration(app, target); err != nil {
			return err
		}
		return advanceWikiMutationPhase(app, job, wikiMutationPhaseApplying)
	}
	if job.Operation == wikiOperationConvert {
		target.Set("is_wiki", true)
		target.Set("wiki_revision", job.TargetRevision)
		target.Set("wiki_generation", job.TargetRevision)
	}
	target.Set("wiki_active_job_id", "")
	if err := app.Save(target); err != nil {
		return err
	}
	return completeWikiMutation(app, job, target)
}

func updateWikiMutationProgress(app core.App, jobID string, delta int) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutations}}
		SET [[item_count]]=MAX([[item_count]],[[processed_count]]+{:delta}),
			[[processed_count]]=[[processed_count]]+{:delta},[[revision]]=[[revision]]+1,[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[phase]]='applying'
	`).Bind(dbx.Params{"delta": delta, "now": now, "jobId": jobID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[total_items]]=MAX([[total_items]],[[processed_items]]+{:delta}),
			[[processed_items]]=[[processed_items]]+{:delta},[[updated]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"delta": delta, "now": now, "jobId": jobID}).Execute()
	return err
}

func advanceWikiMutationPhase(app core.App, job storedWikiMutation, phase string) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutations}}
		SET [[phase]]={:phase},[[revision]]=[[revision]]+1,[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[phase]]={:current}
	`).Bind(dbx.Params{
		"phase": phase, "now": now, "jobId": job.ID, "current": job.Phase,
	}).Execute(); err != nil {
		return err
	}
	return rescheduleWikiMutation(app, job.ID, phase)
}

func rescheduleWikiMutation(app core.App, jobID, cursor string) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[cursor]]={:cursor},[[not_before]]={:notBefore},
			[[chunk_count]]=[[chunk_count]]+1,[[updated]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"cursor": cursor, "notBefore": now + durableJobReadyDelay.Milliseconds(),
		"now": now, "jobId": jobID,
	}).Execute()
	return err
}

func completeWikiMutation(app core.App, job storedWikiMutation, target *core.Record) error {
	_, payload, err := captureWikiMutationResult(app, job, target)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	detailUpdate, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_mutations}}
		SET [[phase]]='completed',[[revision]]=[[revision]]+1,[[completed_at]]={:now},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[phase]]='publishing'
	`).Bind(dbx.Params{"now": now, "jobId": job.ID}).Execute()
	if err != nil {
		return err
	}
	changed, err := detailUpdate.RowsAffected()
	if err != nil || changed != 1 {
		return refusedUnlessStorage(errWikiConflict, err)
	}
	baseUpdate, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[status]]='completed',[[processed_items]]=[[total_items]],
			[[payload_json]]={:payload},[[not_before]]=0,[[updated]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"payload": payload, "now": now, "jobId": job.ID}).Execute()
	if err != nil {
		return err
	}
	changed, err = baseUpdate.RowsAffected()
	if err != nil || changed != 1 {
		return refusedUnlessStorage(errWikiConflict, err)
	}
	return nil
}
