package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) prepareWorkspaceArchiveImportChunk(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil || source.Format != workspaceImportFormatNativeArchiveV2 ||
		source.ArchiveStorageKey == "" || source.ArchiveSHA256 == "" || source.ArchiveBytes < 1 {
		return workspaceImportStateErrorAt("archive-prepare-source")
	}
	if _, err := authorizeWorkspace(r.app, job.Owner, job.WorkspaceID, permissionWorkspaceWrite); err != nil {
		return err
	}
	if payload.ArchivePrepareUpload {
		if payload.ArchivePrepareBytes < source.ArchiveBytes {
			return r.assembleWorkspaceArchiveImportPart(job, payload, source)
		}
		return r.finishWorkspaceArchiveImportAssembly(job, payload, source)
	}
	exists, err := workspaceImportArchivePreparationMapExists(r.app.DataDir(), job.ID)
	if err != nil {
		return err
	}
	if !exists {
		archivePath := filepath.Join(
			r.app.DataDir(), workspaceImportDirectory, source.ArchiveStorageKey,
		)
		if err := createWorkspaceImportArchivePreparationMap(
			r.app.DataDir(), job.ID, archivePath, payload.People, payload.KeepIDs, loadAccountTimeZone(r.app, job.Owner),
		); err != nil {
			return err
		}
		return r.advanceWorkspaceArchivePreparationJob(job, payload)
	}
	db, state, err := openWorkspaceImportArchivePreparationMap(r.app.DataDir(), job.ID)
	if err != nil {
		return err
	}
	defer db.Close()
	switch state.Phase {
	case workspaceImportArchivePreparePhaseFileInventory:
		return r.inventoryWorkspaceArchiveFilesTurn(job, payload, db, state, source)
	case workspaceImportArchivePreparePhaseFileFinalize:
		return r.finalizeWorkspaceArchiveFileIndexTurn(job, payload, db, state)
	case workspaceImportArchivePreparePhaseEntityInventory:
		return r.inventoryWorkspaceArchiveEntitiesTurn(job, payload, db, state, source)
	case workspaceImportArchivePreparePhaseTextMaterialize:
		return r.materializeWorkspaceArchiveTextsTurn(job, payload, db, state)
	case workspaceImportArchivePreparePhaseTransform:
		return r.transformWorkspaceArchiveTurn(job, payload, db, state)
	case workspaceImportArchivePreparePhaseTransformFinish:
		return r.finalizeWorkspaceArchiveTransformTurn(job, payload, db, state)
	case workspaceImportArchivePreparePhaseDLP:
		return r.scanWorkspaceArchiveDLPTurn(job, payload, db, state, source)
	case workspaceImportArchivePreparePhasePublish:
		// A moved workspace keeps its IDs: one already here stops it before
		// anything is written.
		if payload.KeepIDs {
			if err := workspaceMoveIDsFree(r.app)(db); err != nil {
				return workspaceImportFailureAt(err, "archive-ids-free")
			}
		}
		return r.publishPreparedWorkspaceArchiveImport(job, payload, state, source)
	default:
		return workspaceImportStateErrorAt("archive-prepare-phase")
	}
}

func (r *durableJobRunner) commitWorkspaceArchivePreparationTurn(
	job storedJob,
	payload workspaceImportJobPayload,
	db *sql.DB,
	state workspaceImportArchivePreparationState,
) error {
	tx, err := db.Begin()
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-begin")
	}
	defer tx.Rollback()
	if err := saveWorkspaceImportArchivePreparationState(tx, state); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-map-commit")
	}
	return r.advanceWorkspaceArchivePreparationJob(job, payload)
}

func (r *durableJobRunner) advanceWorkspaceArchivePreparationJob(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
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
			return workspaceImportStateErrorAt("archive-prepare-job-frontier")
		}
		payload.CancelRequested = currentPayload.CancelRequested
		return r.updateWorkspaceImportPayload(
			txApp, current, payload, current.Cursor, current.Processed, true,
		)
	})
}

func (r *durableJobRunner) assembleWorkspaceArchiveImportPart(
	job storedJob,
	payload workspaceImportJobPayload,
	source storedWorkspaceImportSource,
) error {
	upload, err := loadProductFileUploadByID(r.app, job.Owner, job.ID)
	if err != nil || upload.Kind != fileUploadKindWorkspaceArchive ||
		upload.WorkspaceID != job.WorkspaceID || upload.Status != fileUploadStatusPending ||
		upload.TotalSize != source.ArchiveBytes || upload.SHA256 != source.ArchiveSHA256 ||
		upload.ReceivedBytes != upload.TotalSize {
		return workspaceImportStateErrorAt("archive-prepare-upload")
	}
	expectedOffset := int64(payload.ArchivePreparePart) * fileTransferPartBytes
	if payload.ArchivePreparePart < 0 || payload.ArchivePrepareBytes != expectedOffset ||
		expectedOffset >= upload.TotalSize {
		return workspaceImportStateErrorAt("archive-prepare-frontier")
	}
	part, err := loadProductFileUploadPart(r.app, upload.UploadID, payload.ArchivePreparePart)
	expectedBytes := min(fileTransferPartBytes, upload.TotalSize-expectedOffset)
	if err != nil || part.Size != expectedBytes || !sha256Pattern.MatchString(part.SHA256) {
		return workspaceImportStateErrorAt("archive-prepare-part")
	}

	directory := filepath.Join(r.app.DataDir(), workspaceImportDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-directory")
	}
	partialPath := workspaceImportArchiveAssemblyPath(directory, job.ID)
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-open")
	}
	info, statErr := file.Stat()
	if statErr != nil || info.Size() < payload.ArchivePrepareBytes {
		_ = file.Close()
		return workspaceImportStorageErrorAt(statErr, "archive-prepare-size")
	}
	if info.Size() > payload.ArchivePrepareBytes {
		if err := file.Truncate(payload.ArchivePrepareBytes); err != nil {
			_ = file.Close()
			return workspaceImportStorageErrorAt(err, "archive-prepare-truncate")
		}
	}
	if _, err := file.Seek(payload.ArchivePrepareBytes, io.SeekStart); err != nil {
		_ = file.Close()
		return workspaceImportStorageErrorAt(err, "archive-prepare-seek")
	}
	wholeHasher, err := workspaceImportArchivePreparationHasher(payload.ArchivePrepareHashState)
	if err != nil {
		_ = file.Close()
		return err
	}
	partHasher := sha256.New()
	fsys, err := r.app.NewFilesystem()
	if err != nil {
		_ = file.Close()
		return workspaceImportStorageErrorAt(err, "archive-prepare-filesystem")
	}
	reader, err := fsys.GetReader(part.StorageKey)
	if err != nil {
		_ = fsys.Close()
		_ = file.Close()
		return workspaceImportStorageErrorAt(err, "archive-prepare-part-open")
	}
	written, copyErr := io.CopyBuffer(
		io.MultiWriter(file, wholeHasher, partHasher),
		io.LimitReader(reader, expectedBytes+1),
		make([]byte, productFileDigestBufferSize),
	)
	closeErr := errors.Join(reader.Close(), fsys.Close(), file.Sync(), file.Close())
	if copyErr != nil || closeErr != nil || written != expectedBytes ||
		hex.EncodeToString(partHasher.Sum(nil)) != part.SHA256 {
		return workspaceImportStorageErrorAt(errors.Join(copyErr, closeErr), "archive-prepare-part-copy")
	}
	state, err := wholeHasher.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return workspaceImportStateErrorAt("archive-prepare-hash-state")
	}
	payload.ArchivePreparePart++
	payload.ArchivePrepareBytes += written
	payload.ArchivePrepareHashState = hex.EncodeToString(state)
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.Phase != workspaceImportPhaseArchivePrepare ||
			!currentPayload.ArchivePrepareUpload ||
			currentPayload.ArchivePreparePart != payload.ArchivePreparePart-1 ||
			currentPayload.ArchivePrepareBytes != payload.ArchivePrepareBytes-written {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-prepare-frontier-commit")
		}
		payload.CancelRequested = currentPayload.CancelRequested
		return r.updateWorkspaceImportPayload(
			txApp, current, payload, current.Cursor, current.Processed, true,
		)
	})
}

func workspaceImportArchivePreparationHasher(state string) (hash.Hash, error) {
	hasher := sha256.New()
	if state == "" {
		return hasher, nil
	}
	decoded, decodeErr := hex.DecodeString(state)
	if decodeErr != nil {
		return nil, workspaceImportStateErrorAt("archive-prepare-hash-state")
	}
	if err := hasher.(encoding.BinaryUnmarshaler).UnmarshalBinary(decoded); err != nil {
		return nil, workspaceImportStateErrorAt("archive-prepare-hash-state")
	}
	return hasher, nil
}

func workspaceImportArchiveAssemblyPath(directory, jobID string) string {
	return filepath.Join(directory, "."+jobID+".leafen.zip.assemble")
}

func (r *durableJobRunner) finishWorkspaceArchiveImportAssembly(
	job storedJob,
	payload workspaceImportJobPayload,
	source storedWorkspaceImportSource,
) error {
	if payload.ArchivePrepareBytes != source.ArchiveBytes {
		return workspaceImportStateErrorAt("archive-prepare-complete-size")
	}
	hasher, err := workspaceImportArchivePreparationHasher(payload.ArchivePrepareHashState)
	if err != nil || hex.EncodeToString(hasher.Sum(nil)) != source.ArchiveSHA256 {
		return workspaceImportStateErrorAt("archive-prepare-complete-digest")
	}
	directory := filepath.Join(r.app.DataDir(), workspaceImportDirectory)
	partialPath := workspaceImportArchiveAssemblyPath(directory, job.ID)
	finalPath := filepath.Join(directory, source.ArchiveStorageKey)
	if info, err := os.Stat(finalPath); err == nil {
		if info.Size() != source.ArchiveBytes {
			return workspaceImportStateErrorAt("archive-prepare-final-size")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return workspaceImportStorageErrorAt(err, "archive-prepare-final-stat")
	} else if err := os.Rename(partialPath, finalPath); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-prepare-finalize")
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if err := ensureWorkspaceArchiveUploadCleanup(r.app, job.Owner, job.ID); err != nil {
		return workspaceImportFailureAt(err, "archive-prepare-upload-cleanup")
	}
	payload.ArchivePrepareUpload = false
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.Phase != workspaceImportPhaseArchivePrepare ||
			!currentPayload.ArchivePrepareUpload ||
			currentPayload.ArchivePrepareBytes != source.ArchiveBytes {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-prepare-finalize-frontier")
		}
		payload.CancelRequested = currentPayload.CancelRequested
		return r.updateWorkspaceImportPayload(
			txApp, current, payload, current.Cursor, current.Processed, true,
		)
	})
}

func ensureWorkspaceArchiveUploadCleanup(app core.App, ownerID, uploadID string) error {
	upload, err := loadProductFileUploadByID(app, ownerID, uploadID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if upload.Status == fileUploadStatusCleanupPending {
		return nil
	}
	if upload.Kind != fileUploadKindWorkspaceArchive || upload.Status != fileUploadStatusPending {
		return errFileUploadUnavailable
	}
	return markWorkspaceArchiveUploadForCleanup(app, upload)
}

func (r *durableJobRunner) stagePreparedWorkspaceArchiveImport(
	job storedJob,
	payload workspaceImportJobPayload,
	archiveSource storedWorkspaceImportSource,
) error {
	stage, err := stageWorkspaceImportArchive(r.app.DataDir(), job.ID, workspaceImportArchiveIDSalt(job.ID, payload), workspaceImportArchiveUpload{
		StorageKey: archiveSource.ArchiveStorageKey,
		SHA256:     archiveSource.ArchiveSHA256,
		Bytes:      archiveSource.ArchiveBytes,
	}, payload.People, loadAccountTimeZone(r.app, job.Owner))
	if err != nil {
		return err
	}
	removeDerived := true
	defer func() {
		if removeDerived {
			_ = removeWorkspaceImportSourceFile(r.app.DataDir(), stage.Source.StorageKey)
			_ = removeWorkspaceImportSourceFile(r.app.DataDir(), stage.Source.FileIndexStorageKey)
		}
	}()
	authorization, err := authorizeWorkspace(r.app, job.Owner, job.WorkspaceID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	scanner, err := newOrganizationDLPContentScanner(r.app, authorization.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if err := enforceWorkspaceImportArchiveDLP(r.app.DataDir(), stage.Source, stage.Title, scanner); err != nil {
		return err
	}
	payload.Title = stage.Title
	payload.ArchivePages = stage.Pages
	payload.Phase = workspaceImportPhaseStaged
	payload.SourceVerified = true
	payload.ArchiveFilesReady = stage.Source.FileCount == 0
	payload.ArchivePreparePart = 0
	payload.ArchivePrepareBytes = 0
	payload.ArchivePrepareHashState = ""
	encoded, err := encodeWorkspaceImportPayload(payload)
	if err != nil {
		return err
	}
	err = r.app.RunInTransaction(func(txApp core.App) error {
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
			return workspaceImportStateErrorAt("archive-stage-frontier")
		}
		currentAuthorization, err := authorizeWorkspace(
			txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		if err := scanner.requireCurrentPolicy(
			txApp, currentAuthorization.Organization.OrganizationID,
		); err != nil {
			return err
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
			return workspaceImportTargetNotFreshError(nil, "archive-stage-freshness")
		}
		if _, err := txApp.DB().NewQuery(`
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
			"jobId": current.ID, "storageKey": stage.Source.StorageKey,
			"sha256": stage.Source.SHA256, "sourceBytes": stage.Source.SourceBytes,
			"chunkCount": stage.Source.ChunkCount, "chunkHashes": stage.Source.ChunkHashesJSON,
			"archiveSHA256": stage.Source.ArchiveSHA256, "archiveBytes": stage.Source.ArchiveBytes,
			"fileIndexStorageKey": stage.Source.FileIndexStorageKey,
			"fileIndexSHA256":     stage.Source.FileIndexSHA256,
			"fileCount":           stage.Source.FileCount, "fileBytes": stage.Source.FileBytes,
			"archiveStorageKey": archiveSource.ArchiveStorageKey,
		}).Execute(); err != nil {
			return err
		}
		if err := recordWorkspaceBundleLosses(txApp, current, payload, stage.Losses); err != nil {
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
			"total":     stage.TotalItems,
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
	if err != nil {
		return err
	}
	removeDerived = false
	return nil
}
