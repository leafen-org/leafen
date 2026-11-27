package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedPageDuplicationFileState struct {
	JobID             string `db:"job_id"`
	SourcePageCursor  string `db:"source_page_cursor"`
	OwnerKindCursor   string `db:"owner_kind_cursor"`
	OwnerIDCursor     string `db:"owner_id_cursor"`
	PropertyIDCursor  string `db:"property_id_cursor"`
	FileIDCursor      string `db:"file_id_cursor"`
	InventoryPageOpen bool   `db:"inventory_page_open"`
	InventoryComplete bool   `db:"inventory_complete"`
	FilesComplete     bool   `db:"files_complete"`
}

type pageDuplicationFileReference struct {
	SourcePageID string `db:"source_page_id"`
	TargetPageID string `db:"target_page_id"`
	OwnerKind    string `db:"owner_kind"`
	OwnerID      string `db:"owner_id"`
	PropertyID   string `db:"property_id"`
	SourceFileID string `db:"source_file_id"`
}

func initializePageDuplicationFileState(app core.App, jobID string, now int64) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_duplication_file_state}}
			([[job_id]],[[created_at]],[[updated_at]])
		VALUES ({:jobId},{:now},{:now})
		ON CONFLICT ([[job_id]]) DO NOTHING
	`).Bind(dbx.Params{"jobId": jobID, "now": now}).Execute()
	return err
}

func pageDuplicationFilesReady(app core.App, jobID string) (bool, error) {
	state := storedPageDuplicationFileState{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[inventory_complete]],[[files_complete]]
		FROM {{leafen_page_duplication_file_state}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).One(&state)
	return state.InventoryComplete && state.FilesComplete, err
}

func loadPageDuplicationFileState(app core.App, jobID string) (storedPageDuplicationFileState, error) {
	state := storedPageDuplicationFileState{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_cursor]],[[owner_kind_cursor]],[[owner_id_cursor]],
			[[property_id_cursor]],[[file_id_cursor]],[[inventory_page_open]],
			[[inventory_complete]],[[files_complete]]
		FROM {{leafen_page_duplication_file_state}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).One(&state)
	return state, err
}

func validatePageDuplicationFileJob(
	app core.App,
	jobID string,
	phase string,
) (storedPageDuplicationJob, error) {
	job, err := loadPageDuplicationJobByID(app, jobID, "")
	if err != nil || job.Status != durableJobStatusQueued || job.Phase != phase {
		return job, errors.Join(errPageDuplicationConflict, err)
	}
	if err := authorizePageDuplicationTarget(
		app, job.ActorID, job,
	); err != nil {
		return job, refusedUnlessStorage(errPageDuplicationConflict, err)
	}
	if err := job.sourceReadable(app); err != nil {
		return job, err
	}
	source, err := app.FindRecordById(pagesCollection, job.SourceRootPageID)
	if err != nil || source.GetString("duplication_source_job_id") != job.ID {
		return job, errors.Join(errPageDuplicationConflict, err)
	}
	return job, nil
}
