package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceExportFilesTable = "leafen_workspace_export_files"

func storeWorkspaceExportFileSnapshots(
	app core.App,
	jobID string,
	snapshots []workspaceExportFileSnapshot,
) error {
	if len(snapshots) == 0 {
		return nil
	}
	for _, snapshot := range snapshots {
		if !workspaceExportFileRecordIDPattern.MatchString(snapshot.RecordID) ||
			snapshot.StorageKey == "" || snapshot.Name == "" || snapshot.ContentType == "" ||
			snapshot.Bytes <= 0 || !sha256Pattern.MatchString(snapshot.SHA256) ||
			(snapshot.Scope != "icons" && snapshot.Scope != "covers" && snapshot.Scope != "uploads") {
			return newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
		}
	}
	encoded, err := json.Marshal(snapshots)
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_export_files}}
			([[job_id]], [[record_id]], [[storage_key]], [[name]], [[content_type]], [[bytes]], [[sha256]], [[scope]])
		SELECT {:jobId},
			json_extract([[value]], '$.recordId'), json_extract([[value]], '$.storageKey'),
			json_extract([[value]], '$.name'), json_extract([[value]], '$.contentType'),
			json_extract([[value]], '$.bytes'), json_extract([[value]], '$.sha256'),
			json_extract([[value]], '$.scope')
		FROM json_each({:snapshots}) WHERE TRUE
		ON CONFLICT ([[job_id]], [[record_id]]) DO UPDATE SET
			[[scope]] = CASE
				WHEN [[leafen_workspace_export_files]].[[scope]] = excluded.[[scope]]
					THEN excluded.[[scope]]
				ELSE 'uploads'
			END
		WHERE [[leafen_workspace_export_files]].[[storage_key]] = excluded.[[storage_key]]
			AND [[leafen_workspace_export_files]].[[name]] = excluded.[[name]]
			AND [[leafen_workspace_export_files]].[[content_type]] = excluded.[[content_type]]
			AND [[leafen_workspace_export_files]].[[bytes]] = excluded.[[bytes]]
			AND [[leafen_workspace_export_files]].[[sha256]] = excluded.[[sha256]]
	`).Bind(dbx.Params{"jobId": jobID, "snapshots": string(encoded)}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != int64(len(snapshots)) {
		return newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged)
	}
	return nil
}

func loadWorkspaceExportFileWindow(
	app core.App,
	jobID string,
	cursor string,
	limit int,
) ([]workspaceExportFileSnapshot, error) {
	if limit < 1 {
		return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	rows := []workspaceExportFileSnapshot{}
	err := app.DB().NewQuery(`
		SELECT [[record_id]], [[storage_key]], [[name]], [[content_type]], [[bytes]], [[sha256]], [[scope]]
		FROM {{leafen_workspace_export_files}}
		WHERE [[job_id]] = {:jobId} AND [[record_id]] > {:cursor}
		ORDER BY [[record_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "cursor": cursor, "limit": limit}).All(&rows)
	return rows, err
}
