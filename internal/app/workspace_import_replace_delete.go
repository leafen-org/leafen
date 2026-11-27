package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func (r *durableJobRunner) beginWorkspaceMarkdownReplaceDelete(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.ReplaceExistingPage ||
			currentPayload.Phase != workspaceImportPhaseApply || !currentPayload.ReadyToPublish ||
			current.Processed != current.Total {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-delete-readiness")
		}
		if currentPayload.CancelRequested {
			currentPayload.Phase = workspaceImportPhaseCleanup
		} else {
			if _, _, err := authorizeWorkspaceMarkdownReplace(txApp, current); err != nil {
				return err
			}
			currentPayload.Phase = workspaceImportPhaseReplaceDelete
		}
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

func (r *durableJobRunner) deleteWorkspaceMarkdownReplaceSnapshot(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	rows := []workspaceMarkdownReplaceSnapshot{}
	err := r.app.DB().NewQuery(`
		SELECT [[block_id]], [[parent_id]], [[position]], [[type]], [[content]], [[text]], [[state]]
		FROM {{leafen_workspace_import_replace_snapshots}} AS [[snapshot]]
		WHERE [[snapshot]].[[job_id]] = {:jobId} AND [[snapshot]].[[state]] = 'snapshot'
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_workspace_import_replace_snapshots}} AS [[child]]
				WHERE [[child]].[[job_id]] = [[snapshot]].[[job_id]]
					AND [[child]].[[parent_id]] = [[snapshot]].[[block_id]]
					AND [[child]].[[state]] = 'snapshot'
			)
		ORDER BY [[snapshot]].[[block_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "limit": workspaceImportMutationTurnItems,
	}).All(&rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		var remaining int
		if err := r.app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_import_replace_snapshots}}
			WHERE [[job_id]] = {:jobId} AND [[state]] = 'snapshot'
		`).Bind(dbx.Params{"jobId": job.ID}).Row(&remaining); err != nil {
			return err
		}
		if remaining != 0 {
			return workspaceImportStateErrorAt("replace-delete-orphan")
		}
		return r.finishWorkspaceMarkdownReplace(job, payload)
	}
	operations := make([]graphOperation, 0, len(rows))
	identities := make([]string, 0, len(rows))
	for _, row := range rows {
		operations = append(operations, graphOperation{
			Kind: graphOperationKindDeleteBlock,
			Deletion: &graphBlockDelete{
				BlockID: row.BlockID,
			},
		})
		identities = append(identities, row.BlockID)
	}
	hooks, err := workspaceMarkdownReplaceMutationHooks(job, operations)
	if err != nil {
		return err
	}
	hooks.AfterApply = func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
		for _, row := range rows {
			result, updateErr := txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_import_replace_snapshots}} SET [[state]] = 'removed'
				WHERE [[job_id]] = {:jobId} AND [[block_id]] = {:blockId} AND [[state]] = 'snapshot'
			`).Bind(dbx.Params{"jobId": job.ID, "blockId": row.BlockID}).Execute()
			if updateErr != nil {
				return updateErr
			}
			updated, updateErr := result.RowsAffected()
			if updateErr != nil || updated != 1 {
				if updateErr != nil {
					return updateErr
				}
				return workspaceImportStateErrorAt("replace-delete-snapshot-state")
			}
		}
		return nil
	}
	digest := sha256.Sum256([]byte(strings.Join(identities, "\x00")))
	_, err = r.ygo.applyContentGraphMutation(job.Owner, job.PageID, graphMutationInput{
		MutationID: "workspace-import-replace-delete-" + job.ID + "-" + hex.EncodeToString(digest[:8]),
		Operations: operations,
	}, hooks)
	if err != nil {
		return err
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.Phase != workspaceImportPhaseReplaceDelete {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-delete-phase")
		}
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

func (r *durableJobRunner) finishWorkspaceMarkdownReplace(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	if _, err := loadStoredWorkspaceImportSource(r.app, job.ID); err != nil {
		return workspaceImportStateErrorAt("replace-finish-source")
	}
	err := r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.ReplaceExistingPage ||
			currentPayload.Phase != workspaceImportPhaseReplaceDelete || currentPayload.CancelRequested ||
			!currentPayload.ReadyToPublish || current.Processed != current.Total {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-finish-readiness")
		}
		if _, _, err := authorizeWorkspaceMarkdownReplace(txApp, current); err != nil {
			return err
		}
		var oldRows int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_import_replace_snapshots}}
			WHERE [[job_id]] = {:jobId} AND [[state]] = 'snapshot'
		`).Bind(dbx.Params{"jobId": current.ID}).Row(&oldRows); err != nil {
			return err
		}
		if oldRows != 0 {
			return workspaceImportStateErrorAt("replace-finish-old-graph")
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_import_replace_snapshots}} WHERE [[job_id]] = {:jobId};
			DELETE FROM {{leafen_workspace_import_replace_leases}} WHERE [[job_id]] = {:jobId};
		`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		currentPayload.Phase = workspaceImportPhasePublishCleanup
		currentPayload.TerminalErrorCode, currentPayload.TerminalErrorDetail = "", ""
		encoded, err := encodeWorkspaceImportPayload(currentPayload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[error_code]] = '', [[diagnostic_reference]] = '',
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "payload": encoded,
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
	return workspaceImportPublicationRetry(err)
}
