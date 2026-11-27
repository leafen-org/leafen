package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	workspaceMarkdownReplaceCleanupNew     = "replacement"
	workspaceMarkdownReplaceCleanupRestore = "restore"
)

func (r *durableJobRunner) cleanupWorkspaceMarkdownReplace(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	if payload.ReplaceCleanupStage == "" || payload.ReplaceCleanupStage == workspaceMarkdownReplaceCleanupNew {
		return r.cleanupWorkspaceMarkdownReplaceNewGraph(job, payload)
	}
	if payload.ReplaceCleanupStage == workspaceMarkdownReplaceCleanupRestore {
		return r.restoreWorkspaceMarkdownReplaceOldGraph(job, payload)
	}
	return workspaceImportStateErrorAt("replace-cleanup-stage")
}

func (r *durableJobRunner) cleanupWorkspaceMarkdownReplaceNewGraph(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	owned := workspaceMarkdownReplaceBlocks(job.ID)
	authorization, _, err := authorizeWorkspaceMarkdownReplaceCleanup(r.app, job)
	if err != nil {
		return err
	}
	rows := []workspaceMarkdownReplaceSnapshot{}
	err = r.app.DB().NewQuery(`
		SELECT [[block]].[[id]] AS [[block_id]], [[block]].[[parent_id]], [[block]].[[position]],
			[[block]].[[type]], [[block]].[[content]], '' AS [[text]], 'replacement' AS [[state]]
		FROM {{leafen_blocks}} AS [[block]]
		WHERE [[block]].[[owner]] = {:owner} AND [[block]].[[page_id]] = {:pageId}
			AND ` + owned.sql("[[block]].[[id]]") + `
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_blocks}} AS [[child]] INDEXED BY [[idx_leafen_blocks_page_order]]
				WHERE [[child]].[[owner]] = [[block]].[[owner]]
					AND [[child]].[[page_id]] = [[block]].[[page_id]]
					AND [[child]].[[parent_id]] = [[block]].[[id]]
					AND ` + owned.sql("[[child]].[[id]]") + `
			)
		ORDER BY [[block]].[[id]] LIMIT {:limit}
	`).Bind(owned.bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "pageId": job.PageID,
		"limit": workspaceImportCleanupTurnItems,
	})).All(&rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return r.advanceWorkspaceMarkdownReplaceCleanupStage(
			job, payload, workspaceMarkdownReplaceCleanupRestore,
		)
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
	hooks.Authorize = workspaceMarkdownReplaceCleanupAuthorization(job)
	digest := sha256.Sum256([]byte(strings.Join(identities, "\x00")))
	_, err = r.ygo.applyContentGraphMutation(job.Owner, job.PageID, graphMutationInput{
		MutationID: "workspace-import-replace-clean-new-" + job.ID + "-" + hex.EncodeToString(digest[:8]),
		Operations: operations,
	}, hooks)
	if err != nil {
		return err
	}
	return r.bumpWorkspaceMarkdownReplaceCleanup(job, payload)
}

func (r *durableJobRunner) advanceWorkspaceMarkdownReplaceCleanupStage(
	job storedJob,
	payload workspaceImportJobPayload,
	stage string,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.ReplaceExistingPage || currentPayload.Phase != workspaceImportPhaseCleanup ||
			currentPayload.ReplaceCleanupStage != payload.ReplaceCleanupStage {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-cleanup-frontier")
		}
		currentPayload.ReplaceCleanupStage = stage
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

func (r *durableJobRunner) bumpWorkspaceMarkdownReplaceCleanup(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.ReplaceExistingPage || currentPayload.Phase != workspaceImportPhaseCleanup ||
			currentPayload.ReplaceCleanupStage != payload.ReplaceCleanupStage {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-cleanup-bump")
		}
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

func (r *durableJobRunner) restoreWorkspaceMarkdownReplaceOldGraph(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	rows := []workspaceMarkdownReplaceSnapshot{}
	err := r.app.DB().NewQuery(`
		SELECT [[snapshot]].[[block_id]], [[snapshot]].[[parent_id]], [[snapshot]].[[position]],
			[[snapshot]].[[type]], [[snapshot]].[[content]], [[snapshot]].[[text]], [[snapshot]].[[state]]
		FROM {{leafen_workspace_import_replace_snapshots}} AS [[snapshot]]
		WHERE [[snapshot]].[[job_id]] = {:jobId} AND [[snapshot]].[[state]] = 'removed'
			AND (
				[[snapshot]].[[parent_id]] = '' OR EXISTS (
					SELECT 1 FROM {{leafen_workspace_import_replace_snapshots}} AS [[parent]]
					WHERE [[parent]].[[job_id]] = [[snapshot]].[[job_id]]
						AND [[parent]].[[block_id]] = [[snapshot]].[[parent_id]]
						AND [[parent]].[[state]] IN ('snapshot', 'restored')
				)
			)
		ORDER BY [[snapshot]].[[block_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "limit": workspaceImportCleanupTurnItems,
	}).All(&rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		var remaining int
		if err := r.app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_import_replace_snapshots}}
			WHERE [[job_id]] = {:jobId} AND [[state]] = 'removed'
		`).Bind(dbx.Params{"jobId": job.ID}).Row(&remaining); err != nil {
			return err
		}
		if remaining != 0 {
			return workspaceImportStateErrorAt("replace-restore-orphan")
		}
		return r.finishWorkspaceMarkdownReplaceCleanup(job, payload)
	}
	operations := make([]graphOperation, 0, len(rows))
	identities := make([]string, 0, len(rows))
	for _, row := range rows {
		var parentID *string
		if row.ParentID != "" {
			value := row.ParentID
			parentID = &value
		}
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: row.BlockID, ParentID: parentID, Position: row.Position,
				Type: row.Type, Content: json.RawMessage(row.Content),
			},
		})
		identities = append(identities, row.BlockID)
	}
	hooks, err := workspaceMarkdownReplaceMutationHooks(job, operations)
	if err != nil {
		return err
	}
	hooks.Authorize = workspaceMarkdownReplaceCleanupAuthorization(job)
	hooks.WorkspaceImportReplaceRollback = true
	hooks.AfterApply = func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
		documents := map[string]*notionYgoDocument{}
		document, err := loadNotionYgoDocument(txApp, job.PageID, documents)
		if err != nil {
			return err
		}
		document.doc.Transact(func(txn *crdt.Transaction) {
			for _, row := range rows {
				if row.Text != "" {
					txn.GetText(ygoBlockTextRootPrefix+row.BlockID).Insert(txn, 0, row.Text, nil)
				}
			}
		})
		for _, row := range rows {
			result, updateErr := txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_import_replace_snapshots}} SET [[state]] = 'restored'
				WHERE [[job_id]] = {:jobId} AND [[block_id]] = {:blockId} AND [[state]] = 'removed'
			`).Bind(dbx.Params{"jobId": job.ID, "blockId": row.BlockID}).Execute()
			if updateErr != nil {
				return updateErr
			}
			updated, updateErr := result.RowsAffected()
			if updateErr != nil || updated != 1 {
				if updateErr != nil {
					return updateErr
				}
				return workspaceImportStateErrorAt("replace-restore-snapshot-state")
			}
		}
		return saveNotionYgoDocuments(txApp, job.Owner, documents)
	}
	digest := sha256.Sum256([]byte(strings.Join(identities, "\x00")))
	_, err = r.ygo.applyContentGraphMutation(job.Owner, job.PageID, graphMutationInput{
		MutationID: "workspace-import-replace-restore-" + job.ID + "-" + hex.EncodeToString(digest[:8]),
		Operations: operations,
	}, hooks)
	if err != nil {
		return err
	}
	return r.bumpWorkspaceMarkdownReplaceCleanup(job, payload)
}

func (r *durableJobRunner) finishWorkspaceMarkdownReplaceCleanup(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := removeWorkspaceImportSourceFile(r.app.DataDir(), source.StorageKey); err != nil {
			return err
		}
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.ReplaceExistingPage || currentPayload.Phase != workspaceImportPhaseCleanup ||
			currentPayload.ReplaceCleanupStage != workspaceMarkdownReplaceCleanupRestore {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-cleanup-finish")
		}
		authorization, _, err := authorizeWorkspaceMarkdownReplaceCleanup(txApp, current)
		if err != nil {
			return err
		}
		var residue int
		owned := workspaceMarkdownReplaceBlocks(current.ID)
		if err := txApp.DB().NewQuery(`
			SELECT
				(SELECT COUNT(*) FROM {{leafen_blocks}}
				 WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
					AND ` + owned.sql("[[id]]") + `)
				+
				(SELECT COUNT(*) FROM {{leafen_workspace_import_replace_snapshots}}
				 WHERE [[job_id]] = {:jobId} AND [[state]] = 'removed')
		`).Bind(owned.bind(dbx.Params{
			"owner": authorization.Workspace.OwnerID, "pageId": current.PageID, "jobId": current.ID,
		})).Row(&residue); err != nil {
			return err
		}
		if residue != 0 {
			return workspaceImportStateErrorAt("replace-cleanup-residue")
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_import_sources}} WHERE [[job_id]] = {:jobId};
			DELETE FROM {{leafen_workspace_import_replace_snapshots}} WHERE [[job_id]] = {:jobId};
			DELETE FROM {{leafen_workspace_import_replace_leases}} WHERE [[job_id]] = {:jobId};
		`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		status := durableJobStatusCanceled
		currentPayload.Phase = workspaceImportPhaseCancelled
		if currentPayload.TerminalErrorCode != "" {
			status = durableJobStatusFailed
			currentPayload.Phase = workspaceImportPhaseFailed
		}
		encoded, err := json.Marshal(currentPayload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[payload_json]] = {:payload},
				[[error_code]] = {:errorCode}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "status": status,
			"payload": string(encoded), "errorCode": currentPayload.TerminalErrorCode, "updated": now,
		}).Execute()
		if err != nil {
			return err
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, current.WorkspaceID)
	})
}
