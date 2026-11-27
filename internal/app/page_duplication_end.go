package app

import (
	"context"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How a copy ends, the runner's last turn (page_duplication_runner.go):
// completed, it records its effect, keeps the names of the people it brought
// from another workspace (workspace_carried_people.go), has its top read for
// the mentions nobody could be told of while it was being made, and lets go
// of the original and of itself; failed, it says why and lets go of the
// original (what it made goes by its cleanup, page_duplication_cleanup.go).

func completePageDuplication(app core.App, job storedPageDuplicationJob) error {
	if err := captureNativePageCopyUndo(app, job); err != nil {
		return err
	}
	if err := keepPageDuplicationCarriedPeople(app, job); err != nil {
		return err
	}
	// Until now nobody could open the copy's top, so nobody could be told of a
	// mention on it.
	if err := markReminderPagesDirty(context.Background(), app, []string{job.TargetRootPageID}, time.Now().UTC()); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_job_effects}} ([[job_id]],[[page_id]],[[revision]],[[sha256]],[[created]])
		VALUES ({:jobId},{:pageId},1,{:sha256},{:createdAt})
		ON CONFLICT ([[job_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": job.ID, "pageId": job.TargetRootPageID,
		"sha256": job.RequestHash, "createdAt": now,
	}).Execute(); err != nil {
		return err
	}
	// Finished, the copy stands on its own: nothing about it depends on this
	// job's row any more, which goes with the original if that is deleted for good.
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[duplication_target_job_id]]=''
		WHERE [[workspace]]={:workspaceId} AND ` + partialIndexKey(`[[duplication_target_job_id]]`, `={:jobId}`) + `
	`).Bind(dbx.Params{"workspaceId": job.WorkspaceID, "jobId": job.ID}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[phase]]='completed', [[revision]]=[[revision]]+1,
			[[updated_at]]={:updatedAt}, [[completed_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='publishing'
	`).Bind(dbx.Params{"jobId": job.ID, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='completed', [[cursor]]='completed',
			[[processed_items]]=[[total_items]], [[updated]]={:updatedAt}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"jobId": job.ID, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[duplication_source_job_id]]=''
		WHERE [[id]]={:sourcePageId} AND [[duplication_source_job_id]]={:jobId}
	`).Bind(dbx.Params{
		"sourcePageId": job.SourceRootPageID, "jobId": job.ID,
	}).Execute()
	return err
}

func failPageDuplicationInTransaction(
	app core.App,
	job storedPageDuplicationJob,
	errorCode string,
) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[phase]]='failed', [[error_code]]={:errorCode},
			[[revision]]=[[revision]]+1, [[updated_at]]={:updatedAt}, [[completed_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]] NOT IN ('completed','failed')
	`).Bind(dbx.Params{
		"jobId": job.ID, "errorCode": errorCode, "updatedAt": now,
	}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='failed', [[error_code]]={:errorCode}, [[updated]]={:updatedAt},
			[[attempt]]=[[max_attempts]]
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"jobId": job.ID, "errorCode": errorCode, "updatedAt": now,
	}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[duplication_source_job_id]]=''
		WHERE [[id]]={:sourcePageId} AND [[duplication_source_job_id]]={:jobId}
	`).Bind(dbx.Params{
		"sourcePageId": job.SourceRootPageID, "jobId": job.ID,
	}).Execute()
	return err
}
