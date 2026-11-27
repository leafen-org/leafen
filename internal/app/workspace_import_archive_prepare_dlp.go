package app

import (
	"database/sql"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) scanWorkspaceArchiveDLPTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
	archiveSource storedWorkspaceImportSource,
) error {
	authorization, err := authorizeWorkspace(r.app, job.Owner, job.WorkspaceID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	scanner, err := newOrganizationDLPContentScanner(r.app, authorization.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if state.DLPChunk < 0 {
		if err := scanner.enforce(map[string]any{
			"title": state.Title, "format": workspaceImportFormatNativeArchiveV2,
		}); err != nil {
			return err
		}
		state.DLPRevision = scanner.revision()
		state.DLPChunk = 0
		return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
	}
	if scanner.revision() != state.DLPRevision {
		return errDLPPolicyUnavailable
	}
	if state.DLPChunk >= state.OutputChunks {
		if state.DLPOffset != state.OutputBytes {
			return workspaceImportStateErrorAt("archive-dlp-frontier")
		}
		state.Phase = workspaceImportArchivePreparePhasePublish
		return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
	}
	source, err := workspaceImportArchivePreparedSource(job.ID, state, archiveSource)
	if err != nil {
		return err
	}
	chunk, nextOffset, err := loadWorkspaceImportStagedChunk(
		r.app.DataDir(), source, state.DLPOffset, state.DLPChunk,
	)
	if err != nil {
		return err
	}
	if err := scanner.enforce(chunk.Items); err != nil {
		return err
	}
	state.DLPOffset = nextOffset
	state.DLPChunk++
	if state.DLPChunk == state.OutputChunks {
		if state.DLPOffset != state.OutputBytes {
			return workspaceImportStateErrorAt("archive-dlp-frontier")
		}
		state.Phase = workspaceImportArchivePreparePhasePublish
	}
	return r.commitWorkspaceArchivePreparationTurn(job, payload, db, state)
}

func (r *durableJobRunner) publishPreparedWorkspaceArchiveImport(
	job storedJob,
	payload workspaceImportJobPayload,
	state workspaceImportArchivePreparationState,
	archiveSource storedWorkspaceImportSource,
) error {
	source, err := workspaceImportArchivePreparedSource(job.ID, state, archiveSource)
	if err != nil {
		return err
	}
	payload.Title = state.Title
	payload.ArchivePages = workspaceImportArchivePages(state.Descriptor)
	payload.Phase = workspaceImportPhaseStaged
	payload.SourceVerified = true
	payload.ArchiveFilesReady = source.FileCount == 0
	payload.ArchivePreparePart = 0
	payload.ArchivePrepareBytes = 0
	payload.ArchivePrepareHashState = ""
	encoded, err := encodeWorkspaceImportPayload(payload)
	if err != nil {
		return err
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.Phase != workspaceImportPhaseArchivePrepare ||
			currentPayload.ArchivePrepareUpload {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-prepare-publish-frontier")
		}
		authorization, err := authorizeWorkspace(
			txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		scanner, err := newOrganizationDLPContentScanner(
			txApp, authorization.Organization.OrganizationID,
		)
		if err != nil || scanner.revision() != state.DLPRevision {
			if err != nil {
				return err
			}
			return failedBecause(errDLPPolicyUnavailable, err)
		}
		var leaseCount int
		pageCount, err := workspaceArchiveTargetPages(txApp, current.WorkspaceID, false)
		if err != nil {
			return err
		}
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_archive_import_targets}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"workspaceId": current.WorkspaceID, "jobId": current.ID}).Row(&leaseCount); err != nil {
			return err
		}
		if pageCount != 0 || leaseCount != 1 {
			return workspaceImportTargetNotFreshError(nil, "archive-prepare-publish-freshness")
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_workspace_import_sources}}
			SET [[storage_key]] = {:storageKey}, [[sha256]] = {:sha256},
				[[source_bytes]] = {:sourceBytes}, [[chunk_count]] = {:chunkCount},
				[[chunk_hashes_json]] = {:chunkHashes},
				[[archive_sha256]] = {:archiveSHA256}, [[archive_bytes]] = {:archiveBytes},
				[[file_index_storage_key]] = {:fileIndexStorageKey},
				[[file_index_sha256]] = {:fileIndexSHA256},
				[[file_count]] = {:fileCount}, [[file_bytes]] = {:fileBytes}
			WHERE [[job_id]] = {:jobId} AND [[archive_storage_key]] = {:archiveStorageKey}
		`).Bind(dbx.Params{
			"jobId": current.ID, "storageKey": source.StorageKey,
			"sha256": source.SHA256, "sourceBytes": source.SourceBytes,
			"chunkCount": source.ChunkCount, "chunkHashes": source.ChunkHashesJSON,
			"archiveSHA256": source.ArchiveSHA256, "archiveBytes": source.ArchiveBytes,
			"fileIndexStorageKey": source.FileIndexStorageKey,
			"fileIndexSHA256":     source.FileIndexSHA256,
			"fileCount":           source.FileCount, "fileBytes": source.FileBytes,
			"archiveStorageKey": archiveSource.ArchiveStorageKey,
		}).Execute()
		if err != nil {
			return err
		}
		updated, _ := result.RowsAffected()
		if updated != 1 {
			return workspaceImportStateErrorAt("archive-prepare-publish-source")
		}
		if err := recordWorkspaceBundleLosses(txApp, current, payload, state.Descriptor.Losses); err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[total_items]] = {:total},
				[[chunk_count]] = [[chunk_count]] + 1,
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "payload": encoded,
			"total":     state.TotalItems,
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
}
