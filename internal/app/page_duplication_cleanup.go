package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func beginPageDuplicationCleanupInTransaction(
	app core.App,
	job storedPageDuplicationJob,
	errorCode string,
) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[phase]]='cleaning', [[error_code]]={:errorCode},
			[[revision]]=[[revision]]+1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]] NOT IN ('cleaning','completed','failed')
	`).Bind(dbx.Params{
		"jobId": job.ID, "errorCode": errorCode, "updatedAt": now,
	}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[cursor]]='cleaning', [[attempt]]=[[max_attempts]],
			[[not_before]]={:notBefore}, [[updated]]={:updatedAt}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"jobId": job.ID, "notBefore": now + durableJobReadyDelay.Milliseconds(),
		"updatedAt": now,
	}).Execute()
	return err
}

func cleanPageDuplicationFailureWindow(
	app core.App,
	job storedPageDuplicationJob,
) (bool, error) {
	item := storedPageDuplicationItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_id]],[[target_page_id]],[[cleanup_lane]],[[cleaned]]
		FROM {{leafen_page_duplication_items}}
			INDEXED BY [[idx_leafen_page_duplication_cleanup_work]]
		WHERE [[job_id]]={:jobId} AND [[page_created]]=1 AND [[cleaned]]=0
		ORDER BY [[depth]] DESC,[[target_page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&item)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if item.CleanupLane < pagePermanentDeleteCleanupLaneCount {
		more, err := cleanPagePermanentDeleteResidueLane(app, item.TargetPageID, item.CleanupLane)
		if err != nil {
			return false, err
		}
		if !more {
			_, err = app.DB().NewQuery(`
				UPDATE {{leafen_page_duplication_items}}
				SET [[cleanup_lane]]=[[cleanup_lane]]+1, [[updated_at]]={:updatedAt}
				WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
			`).Bind(dbx.Params{
				"jobId": job.ID, "sourcePageId": item.SourcePageID,
				"updatedAt": time.Now().UTC().UnixMilli(),
			}).Execute()
		}
		return false, err
	}
	target, err := app.FindRecordById(pagesCollection, item.TargetPageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		target.Set("icon", "")
		target.Set("icon_type", "none")
		target.Set("cover", "")
		if err := app.Save(target); err != nil {
			return false, err
		}
		if err := replaceProductFilePageReferences(
			app, job.WorkspaceID, target.Id, "", "none", "",
		); err != nil {
			return false, err
		}
		if err := app.Delete(target); err != nil {
			return false, err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[cleaned]]=1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID,
		"updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return false, err
}
