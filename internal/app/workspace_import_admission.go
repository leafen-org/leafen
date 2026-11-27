package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Admitting an import: a person's request answered over HTTP, made into a job.

var errWorkspaceImportRequestReused = errors.New(errorCodeWorkspaceImportRequestIDReused)

// workspaceImportAdmitError says where an admission stopped, as its
// diagnostic names it.
type workspaceImportAdmitError struct {
	stage string
	err   error
}

func (failure *workspaceImportAdmitError) Error() string { return failure.err.Error() }

func (failure *workspaceImportAdmitError) Unwrap() error { return failure.err }

// workspaceImportAdmissionAPIError answers an admission's refusal as the
// import routes always have.
func workspaceImportAdmissionAPIError(event *core.RequestEvent, err error, dlpOperation string) error {
	if errors.Is(err, errWorkspaceImportRequestReused) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportRequestIDReused, "This request ID was used for a different import.")
	}
	stage, cause := "admit", err
	var admit *workspaceImportAdmitError
	if errors.As(err, &admit) {
		stage, cause = admit.stage, admit.err
	}
	switch stage {
	case "existing-page-own":
		if stableWorkspaceImportFailureCode(cause) != errorCodeWorkspaceImportInvalid {
			return workspaceImportAPIError(event, cause, stage)
		}
		// A refusal that says what was wrong says it.
		message := "Check the import content."
		if detail := workspaceImportFailureDetail(cause); detail != "" {
			message = "Check the import content: " + detail + "."
		}
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceImportInvalid, message)
	case "policy-preflight", "admit":
		if isPermissionDenied(cause) {
			return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
		}
		if isDLPPolicyDecisionError(cause) {
			return writeDLPPolicyError(event, cause, dlpOperation)
		}
	}
	return workspaceImportAPIError(event, cause, stage)
}

// startWorkspaceImport admits an import as a job, or finds the one its
// request made before (duplicate).
func (r *durableJobRunner) startWorkspaceImport(
	app core.App,
	ownerID string,
	workspaceID string,
	admission workspaceImportAdmission,
) (string, bool, error) {
	input := admission.Input
	requestHash := workspaceImportAdmissionHash(workspaceID, admission)
	if existing, loadErr := loadStoredJobByRequest(app, ownerID, input.RequestID); loadErr == nil {
		if existing.Kind != workspaceImportJobKind || existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
			return "", false, errWorkspaceImportRequestReused
		}
		return existing.ID, true, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return "", false, &workspaceImportAdmitError{stage: "reconnect", err: loadErr}
	}
	if err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		currentAuthorization, authErr := authorizeWorkspaceImportAdmission(
			txApp, ownerID, workspaceID, admission.TargetPageID, admission.Append,
		)
		if authErr != nil {
			return authErr
		}
		if placeErr := authorizeWorkspaceImportPlace(txApp, ownerID, workspaceID, input); placeErr != nil {
			return placeErr
		}
		return enforceOrganizationDLPContent(
			txApp, currentAuthorization.Organization.OrganizationID,
			workspaceImportDLPContent(input),
		)
	}); err != nil {
		return "", false, &workspaceImportAdmitError{stage: "policy-preflight", err: err}
	}
	jobID := security.RandomString(15)
	items, mergeRows, err := parseWorkspaceImportAdmission(app, ownerID, jobID, admission)
	if err != nil {
		return "", false, &workspaceImportAdmitError{stage: "existing-page-own", err: err}
	}
	var inline []string
	source, err := stageWorkspaceImportSource(app.DataDir(), jobID, input.Format, workspaceImportInlineDatabasesOf(items, &inline))
	if err != nil {
		return "", false, &workspaceImportAdmitError{stage: "stage", err: err}
	}
	removeStaged := true
	defer func() {
		if removeStaged {
			_ = removeWorkspaceImportSourceFile(app.DataDir(), source.StorageKey)
		}
	}()

	duplicate := false
	identityReused := false
	payloadVersion := 1
	rootPageIDs := []string(nil)
	if admission.Append {
		payloadVersion = 2
		rootPageIDs = []string{admission.TargetPageID}
	} else if admission.Replace {
		payloadVersion = 3
		rootPageIDs = []string{admission.TargetPageID}
	}
	if admission.MergeDataSourceID != "" {
		payloadVersion = 4
	}
	placeTitle := ""
	if admission.FillsPlace {
		if placeTitle, err = workspaceImportPlaceTitle(app, admission.TargetPageID); err != nil {
			return "", false, &workspaceImportAdmitError{stage: "place", err: err}
		}
	}
	payload, _ := json.Marshal(workspaceImportJobPayload{
		SchemaVersion: payloadVersion, Format: input.Format, Title: input.Title,
		ParentPageID: input.ParentPageID, TeamspaceID: input.TeamspaceID, DataSourceID: input.DataSourceID,
		Phase: workspaceImportPhaseStaged, RootPageID: admission.TargetPageID,
		RootPageIDs: rootPageIDs, AppendToExistingPage: admission.Append,
		ReplaceExistingPage: admission.Replace,
		MergeDataSourceID:   admission.MergeDataSourceID, MergeRows: mergeRows,
		MergeNewPropertyIDs: workspaceCSVMergeNewPropertyIDs(admission.MergeNewProperties),
		PageIDs:             true,
		BatchJobID:          admission.BatchJobID, ReservedRootPageID: admission.ReservedRootPageID,
		FillsPlace: admission.FillsPlace, PlaceTitle: placeTitle, InlineDatabases: inline,
	})
	err = app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, authErr := authorizeWorkspaceImportAdmission(
			txApp, ownerID, workspaceID, admission.TargetPageID, admission.Append,
		)
		if authErr != nil {
			return authErr
		}
		if placeErr := authorizeWorkspaceImportPlace(txApp, ownerID, workspaceID, input); placeErr != nil {
			return placeErr
		}
		if admission.TargetPageID != "" && !admission.Replace {
			leased, leaseErr := workspaceMarkdownReplacePageLeased(txApp, admission.TargetPageID, "")
			if leaseErr != nil {
				return leaseErr
			}
			if leased {
				return workspaceImportTargetNotFreshError(nil, "existing-page-lease")
			}
		}
		if dlpErr := enforceOrganizationDLPContent(
			txApp, currentAuthorization.Organization.OrganizationID,
			workspaceImportDLPContent(input),
		); dlpErr != nil {
			return dlpErr
		}
		now := r.clock.now().UTC()
		result, insertErr := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]],
				 [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:pageId},
				 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '0', 0, {:total}, 0, {:maxAttempts}, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": jobID, "owner": ownerID, "workspaceId": workspaceID, "requestId": input.RequestID,
			"requestHash": requestHash, "kind": workspaceImportJobKind, "payload": string(payload),
			"pageId": admission.TargetPageID,
			"total":  source.Items, "maxAttempts": defaultDurableJobMaxAttempts,
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
			existing, loadErr := loadStoredJobByRequest(txApp, ownerID, input.RequestID)
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
		if admission.Replace {
			pageAuthorization, pageErr := authorizePage(
				txApp, ownerID, admission.TargetPageID, permissionWorkspaceWrite,
			)
			if pageErr != nil || pageAuthorization.Workspace.WorkspaceID != workspaceID {
				if pageErr == nil {
					pageErr = permissionDenied()
				}
				return pageErr
			}
			if pageAuthorization.Page.GetInt("revision") != admission.TargetRevision ||
				pageAuthorization.Page.GetInt("cursor") != admission.TargetCursor {
				return workspaceImportTargetNotFreshError(nil, "replace-admission-fence")
			}
			if leaseErr := insertWorkspaceMarkdownReplaceLease(
				txApp, jobID, pageAuthorization, now.UnixMilli(),
			); leaseErr != nil {
				return workspaceImportTargetNotFreshError(leaseErr, "replace-lease")
			}
		}
		source.JobID = jobID
		_, insertErr = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_import_sources}}
				([[job_id]], [[schema_version]], [[format]], [[storage_key]], [[sha256]], [[source_bytes]],
				 [[chunk_count]], [[chunk_hashes_json]], [[created]])
			VALUES
				({:jobId}, {:schemaVersion}, {:format}, {:storageKey}, {:sha256}, {:sourceBytes},
				 {:chunkCount}, {:chunkHashes}, {:created})
		`).Bind(dbx.Params{
			"jobId": source.JobID, "schemaVersion": source.SchemaVersion, "format": source.Format,
			"storageKey": source.StorageKey, "sha256": source.SHA256, "sourceBytes": source.SourceBytes,
			"chunkCount": source.ChunkCount, "chunkHashes": source.ChunkHashesJSON, "created": now.UnixMilli(),
		}).Execute()
		if insertErr != nil {
			return insertErr
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, workspaceID)
	})
	if identityReused {
		return "", false, errWorkspaceImportRequestReused
	}
	if err != nil {
		return "", false, &workspaceImportAdmitError{stage: "admit", err: err}
	}
	if !duplicate {
		removeStaged = false
	}
	return jobID, duplicate, nil
}

// admitWorkspaceImport admits an import a person asked for and answers with
// its job (startWorkspaceImport does the admitting).
func (r *durableJobRunner) admitWorkspaceImport(
	event *core.RequestEvent,
	ownerID string,
	workspaceID string,
	admission workspaceImportAdmission,
) error {
	jobID, duplicate, err := r.startWorkspaceImport(event.App, ownerID, workspaceID, admission)
	if err != nil {
		return workspaceImportAdmissionAPIError(event, err, "workspace-import")
	}
	if admission.Replace && !duplicate {
		var invalidateErr error
		if r.rooms == nil {
			invalidateErr = workspaceImportStateErrorAt("replace-room-owner")
		} else {
			invalidateErr = r.rooms.InvalidateWorkspace(workspaceID)
		}
		if invalidateErr != nil {
			reference := newDiagnosticReference()
			code := stableWorkspaceImportFailureCode(invalidateErr)
			handled, failureErr := r.scheduleWorkspaceImportFailure(event.App, jobID, reference, code, workspaceImportFailureDetail(invalidateErr), false)
			if handled {
				logDurableJobDiagnostic(
					r.app, code, reference, jobID,
					"workspace-import.replace", "room-invalidation", invalidateErr,
				)
			}
			r.notify()
			if failureErr != nil {
				invalidateErr = errors.Join(invalidateErr, failureErr)
			}
			return writeAPIErrorWithReference(
				event, http.StatusServiceUnavailable, code,
				"Couldn't start the page replacement.", reference,
				"workspace-import", "replace-room-invalidation", code,
				invalidateErr, diagnosticSourcePosition(1),
			)
		}
	}
	r.notify()
	response, err := r.workspaceImportResponse(jobID, ownerID, workspaceID, duplicate)
	if err != nil {
		return workspaceImportAPIError(event, err, "respond")
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}
