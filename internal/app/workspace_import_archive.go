package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var errWorkspaceImportArchiveTargetLease = errors.New("workspace archive import target lease unavailable")

func (r *durableJobRunner) createWorkspaceArchiveImport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "archive-authorize")
	}
	dlpScanner, err := newOrganizationDLPContentScanner(
		event.App,
		authorization.Organization.OrganizationID,
	)
	if err != nil {
		return writeDLPPolicyError(event, err, "workspace-archive-import")
	}
	requestID := strings.TrimSpace(event.Request.Header.Get("x-leafen-request-id"))
	mediaType, _, mediaErr := mime.ParseMediaType(event.Request.Header.Get("content-type"))
	if requestID == "" || len([]rune(requestID)) > maxDurableJobRequestIDRunes || mediaErr != nil || mediaType != workspaceExportArchiveMIME {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceImportInvalid, "Check the Leafen archive details.")
	}
	if event.Request.ContentLength > workspaceImportArchiveDirectBytes {
		return workspaceArchiveResumableRequired(event)
	}

	jobID := security.RandomString(15)
	upload, err := stageWorkspaceImportArchiveUpload(
		event.App.DataDir(), jobID,
		io.LimitReader(event.Request.Body, workspaceImportArchiveDirectBytes+1),
	)
	if err != nil {
		return workspaceImportAPIError(event, err, "archive-upload")
	}
	if upload.Bytes > workspaceImportArchiveDirectBytes {
		_ = removeWorkspaceImportSourceFile(event.App.DataDir(), upload.StorageKey)
		return workspaceArchiveResumableRequired(event)
	}
	return r.admitWorkspaceArchivePreprocessing(
		event, user.Id, workspaceID, requestID, jobID, upload, dlpScanner, false, true,
	)
}

func workspaceArchiveResumableRequired(event *core.RequestEvent) error {
	return apiError(
		event,
		http.StatusRequestEntityTooLarge,
		errorCodeWorkspaceImportArchiveResumableRequired,
		"Import a large Leafen archive with a resumable upload.",
	)
}

// admitWorkspaceArchivePreprocessing admits a Leafen archive a person sent
// and answers with its job (startWorkspaceArchiveImport does the admitting).
func (r *durableJobRunner) admitWorkspaceArchivePreprocessing(
	event *core.RequestEvent,
	ownerID string,
	workspaceID string,
	requestID string,
	jobID string,
	upload workspaceImportArchiveUpload,
	dlpScanner organizationDLPContentScanner,
	prepareUpload bool,
	removeArchiveOnFailure bool,
) error {
	jobID, duplicate, err := r.startWorkspaceArchiveImport(
		event.App, ownerID, workspaceID, requestID, jobID, upload, dlpScanner, prepareUpload, removeArchiveOnFailure,
	)
	if err != nil {
		return workspaceArchiveAdmissionAPIError(event, err)
	}
	response, err := r.workspaceImportResponse(jobID, ownerID, workspaceID, duplicate)
	if err != nil {
		return workspaceImportAPIError(event, err, "archive-respond")
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

var errWorkspaceImportArchiveNotFresh = errors.New(errorCodeWorkspaceImportTargetNotFresh)

// workspaceArchiveAdmissionAPIError answers an archive admission's refusal as
// the archive routes always have.
func workspaceArchiveAdmissionAPIError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errWorkspaceImportArchiveNotFresh):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportTargetNotFresh, "A Leafen archive can only be imported into an empty workspace.")
	case errors.Is(err, errWorkspaceImportRequestReused):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportRequestIDReused, "This request ID was used for a different import.")
	}
	stage, cause := "archive-admit", err
	var admit *workspaceImportAdmitError
	if errors.As(err, &admit) {
		stage, cause = admit.stage, admit.err
	}
	if stage == "archive-admit" && isPermissionDenied(cause) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	return workspaceImportAPIError(event, cause, stage)
}

// startWorkspaceArchiveImport admits a Leafen archive into an empty
// workspace as a job, or finds the one its request made before (duplicate).
func (r *durableJobRunner) startWorkspaceArchiveImport(
	app core.App,
	ownerID string,
	workspaceID string,
	requestID string,
	jobID string,
	upload workspaceImportArchiveUpload,
	dlpScanner organizationDLPContentScanner,
	prepareUpload bool,
	removeArchiveOnFailure bool,
) (string, bool, error) {
	return r.startWorkspaceArchiveImportWithPeople(app, ownerID, workspaceID, requestID, jobID, upload, dlpScanner,
		prepareUpload, removeArchiveOnFailure, nil, false, nil, nil)
}

// startWorkspaceArchiveImportWithPeople is startWorkspaceArchiveImport for a
// workspace moved from another Leafen server, with its people and the
// accounts here they were paired with (workspace_import_archive_people.go),
// keeping the bundle's IDs, and its pages' history to put in after them
// (workspace_move_history.go).
func (r *durableJobRunner) startWorkspaceArchiveImportWithPeople(
	app core.App,
	ownerID string,
	workspaceID string,
	requestID string,
	jobID string,
	upload workspaceImportArchiveUpload,
	dlpScanner organizationDLPContentScanner,
	prepareUpload bool,
	removeArchiveOnFailure bool,
	people []workspaceImportPerson,
	keepIDs bool,
	history *workspaceMoveHistorySource,
	access *workspaceMoveAccess,
) (string, bool, error) {
	removeArchive := removeArchiveOnFailure
	defer func() {
		if removeArchive {
			_ = removeWorkspaceImportSourceFile(app.DataDir(), upload.StorageKey)
			if history != nil {
				_ = removeWorkspaceImportSourceFile(app.DataDir(), history.StorageKey)
			}
		}
	}()
	requestHash := workspaceImportArchiveRequestHash(workspaceID, upload.SHA256)
	if existing, loadErr := loadStoredJobByRequest(app, ownerID, requestID); loadErr == nil {
		if existing.Kind != workspaceImportJobKind || existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
			return "", false, errWorkspaceImportRequestReused
		}
		return existing.ID, true, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return "", false, &workspaceImportAdmitError{stage: "archive-reconnect", err: loadErr}
	}
	if fresh, freshErr := workspaceImportArchiveTargetFresh(app, workspaceID); freshErr != nil {
		return "", false, &workspaceImportAdmitError{stage: "archive-freshness-preflight", err: freshErr}
	} else if !fresh {
		return "", false, errWorkspaceImportArchiveNotFresh
	}
	targetNotFresh := false
	identityReused := false
	duplicate := false
	chunkHashes, _ := json.Marshal([]string{upload.SHA256})
	jobPayload := workspaceImportJobPayload{
		SchemaVersion: 1, Format: workspaceImportFormatNativeArchiveV2,
		Title: "Leafen archive", Phase: workspaceImportPhaseArchivePrepare,
		ArchivePrepareUpload: prepareUpload, People: people, KeepIDs: keepIDs, History: history, MoveAccess: access,
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, authErr := authorizeWorkspace(txApp, ownerID, workspaceID, permissionWorkspaceWrite)
		if authErr != nil {
			return authErr
		}
		pageCount, err := workspaceArchiveTargetPages(txApp, workspaceID, false)
		if err != nil {
			return err
		}
		if pageCount != 0 {
			targetNotFresh = true
			return nil
		}
		if err := dlpScanner.requireCurrentPolicy(
			txApp,
			currentAuthorization.Organization.OrganizationID,
		); err != nil {
			return err
		}
		bundleConnectionID, bundlePageIDs, err := workspaceBundleStagingConnection(txApp, ownerID, workspaceID)
		if err != nil {
			return err
		}
		jobPayload.BundleConnectionID, jobPayload.PageIDs = bundleConnectionID, bundlePageIDs
		payload, err := json.Marshal(jobPayload)
		if err != nil {
			return err
		}
		now := r.clock.now().UTC()
		result, insertErr := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]],
				 [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, '',
				 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '0', 0, {:total}, 0, {:maxAttempts}, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": jobID, "owner": ownerID, "workspaceId": workspaceID, "requestId": requestID,
			"requestHash": requestHash, "kind": workspaceImportJobKind, "payload": string(payload),
			"total": 0, "maxAttempts": defaultDurableJobMaxAttempts,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "now": now.UnixMilli(),
		}).Execute()
		if insertErr != nil {
			return insertErr
		}
		inserted, insertErr := result.RowsAffected()
		if insertErr != nil {
			return insertErr
		}
		if inserted == 0 {
			existing, loadErr := loadStoredJobByRequest(txApp, ownerID, requestID)
			if loadErr != nil {
				return loadErr
			}
			if existing.Kind != workspaceImportJobKind || existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
				identityReused = true
				return nil
			}
			jobID, duplicate = existing.ID, true
			return nil
		}
		if bundleConnectionID != "" {
			if err := claimWorkspaceBundleStaging(txApp, bundleConnectionID, jobID); err != nil {
				return err
			}
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_archive_import_targets}} ([[workspace_id]], [[job_id]], [[created]])
			VALUES ({:workspaceId}, {:jobId}, {:created})
		`).Bind(dbx.Params{"workspaceId": workspaceID, "jobId": jobID, "created": now.UnixMilli()}).Execute(); err != nil {
			targetNotFresh = true
			return refusedUnlessStorage(errWorkspaceImportArchiveTargetLease, err)
		}
		_, insertErr = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_import_sources}}
				([[job_id]], [[schema_version]], [[format]], [[storage_key]], [[sha256]], [[source_bytes]],
				 [[chunk_count]], [[chunk_hashes_json]], [[archive_storage_key]], [[archive_sha256]], [[archive_bytes]],
				 [[file_index_storage_key]], [[file_index_sha256]], [[file_count]], [[file_bytes]], [[created]])
			VALUES
				({:jobId}, 3, {:format}, {:storageKey}, {:sha256}, {:sourceBytes},
				 {:chunkCount}, {:chunkHashes}, {:archiveStorageKey}, {:archiveSHA256}, {:archiveBytes},
				 {:fileIndexStorageKey}, {:fileIndexSHA256}, {:fileCount}, {:fileBytes}, {:created})
		`).Bind(dbx.Params{
			"jobId": jobID, "format": workspaceImportFormatNativeArchiveV2,
			"storageKey": jobID + ".preparing.ndjson",
			"sha256":     upload.SHA256, "sourceBytes": upload.Bytes,
			"chunkCount": 1, "chunkHashes": string(chunkHashes),
			"archiveStorageKey": upload.StorageKey, "archiveSHA256": upload.SHA256,
			"archiveBytes": upload.Bytes, "created": now.UnixMilli(),
			"fileIndexStorageKey": "", "fileIndexSHA256": "",
			"fileCount": 0, "fileBytes": 0,
		}).Execute()
		if insertErr != nil {
			return insertErr
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, workspaceID)
	})
	if targetNotFresh || errors.Is(err, errWorkspaceImportArchiveTargetLease) {
		return "", false, errWorkspaceImportArchiveNotFresh
	}
	if identityReused {
		return "", false, errWorkspaceImportRequestReused
	}
	if err != nil {
		return "", false, &workspaceImportAdmitError{stage: "archive-admit", err: err}
	}
	if !duplicate {
		removeArchive = false
	}
	r.notify()
	return jobID, duplicate, nil
}

func workspaceImportArchiveRequestHash(workspaceID, archiveSHA string) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + workspaceImportFormatNativeArchiveV2 + "\x00" + archiveSHA))
	return hex.EncodeToString(digest[:])
}

func workspaceImportArchiveTargetFresh(app core.App, workspaceID string) (bool, error) {
	pages, err := workspaceArchiveTargetPages(app, workspaceID, false)
	if err != nil {
		return false, err
	}
	var leases int
	err = app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspace_archive_import_targets}} WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&leases)
	return pages+leases == 0, err
}
