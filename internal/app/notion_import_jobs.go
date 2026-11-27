package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

func (r *durableJobRunner) createNotionImportJob(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportCreateFailed, "Couldn't start the import.", err)
	}

	var input notionImportJobInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput,
			refusalMessage("Check the import details.", bodyRefusalReason("Notion import request", err)))
	}
	input.ConnectionID = strings.TrimSpace(input.ConnectionID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	rootIDs, rootKinds, valid := normalizeNotionRoots(input.RootIDs, input.RootKinds)
	if problem := notionImportInputProblem(input, valid); problem != "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, refusalMessage("Check the import details.", problem))
	}
	input.RootIDs = rootIDs
	input.RootKinds = rootKinds
	if _, err := loadNotionConnection(event.App, input.ConnectionID, user.Id, workspaceID); errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportConnectionNotFound, "Connection not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportCreateFailed, "Couldn't read the connection.", err)
	}

	requestHash := notionImportJobRequestHash(workspaceID, input)
	jobID := security.RandomString(15)
	duplicate := false
	identityReused := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, authorizeErr := authorizeWorkspace(
			txApp, user.Id, workspaceID, permissionWorkspaceWrite,
		)
		if authorizeErr != nil {
			return authorizeErr
		}
		payload, marshalErr := json.Marshal(notionImportJobPayload{
			ConnectionID: input.ConnectionID, RootIDs: input.RootIDs, RootKinds: input.RootKinds,
			AuthorityOwnerID: currentAuthorization.OwnerID, Phase: notionImportPhaseScope,
			notionImportUnmentionedSweep: notionImportUnmentionedSweepFromStart(),
		})
		if marshalErr != nil {
			return marshalErr
		}
		existing, loadErr := loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if loadErr == nil {
			if existing.RequestHash != requestHash || existing.Kind != notionImportJobKind {
				if !notionImportLegacyPageRequestMatches(existing, workspaceID, input) {
					identityReused = true
					return nil
				}
			}
			jobID, duplicate = existing.ID, true
			return nil
		}
		if !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		}
		inserted, insertErr := insertNotionImportJob(txApp, jobID, user.Id, workspaceID, input.RequestID, requestHash, payload)
		if insertErr != nil {
			return insertErr
		}
		if !inserted {
			existing, loadErr = loadStoredJobByRequest(txApp, user.Id, input.RequestID)
			if loadErr != nil {
				return loadErr
			}
			if existing.RequestHash != requestHash || existing.Kind != notionImportJobKind {
				if !notionImportLegacyPageRequestMatches(existing, workspaceID, input) {
					identityReused = true
					return nil
				}
			}
			jobID, duplicate = existing.ID, true
		}
		return nil
	})
	if identityReused {
		return apiError(event, http.StatusConflict, errorCodeJobIdentityReused, "The job request ID was used for a different request.")
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportCreateFailed, "Couldn't start the import.", err)
	}
	r.notify()
	response, err := notionImportResponseFor(event.Request.Context(), r.app, jobID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the import.", err)
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

// insertNotionImportJob queues an import job and its report, unless the
// person's request ID already names a job.
func insertNotionImportJob(
	app core.App, jobID, ownerID, workspaceID, requestID, requestHash string, payload []byte,
) (bool, error) {
	now := time.Now().UTC()
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_jobs}}
			([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
			 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]],
			 [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
		VALUES
			({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, '',
			 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, 0, 0, {:maxAttempts}, 0)
		ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"id": jobID, "owner": ownerID, "workspaceId": workspaceID,
		"requestId": requestID, "requestHash": requestHash, "kind": notionImportJobKind,
		"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "now": now.UnixMilli(),
		"payload": string(payload), "maxAttempts": defaultDurableJobMaxAttempts,
	}).Execute()
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return false, err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_reports}} ([[job_id]], [[updated]])
		VALUES ({:jobId}, {:updated})
	`).Bind(dbx.Params{"jobId": jobID, "updated": now.UnixMilli()}).Execute()
	return err == nil, err
}

func (r *durableJobRunner) retryNotionImportJob(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	outcome, err := r.retryNotionImport(event.Request.Context(), event.App, jobID, user.Id, workspaceID)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportNotFound, "Import not found.")
	}
	if outcome.unavailable {
		return apiError(event, http.StatusConflict, errorCodeJobRetryUnavailable, "This import can't be retried.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobControlFailed, "Couldn't restart the import.", err)
	}
	r.notify()
	response, err := notionImportResponseFor(event.Request.Context(), r.app, jobID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the import.", err)
	}
	return event.JSON(http.StatusAccepted, response)
}

// cancelNotionImportJob stops an import before its pages begin to show. What it
// made stays hidden until retention clears it, and running the plan again goes
// on from it.
func (r *durableJobRunner) cancelNotionImportJob(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	outcome, err := r.cancelOwnedStoredJob(
		event.App, jobID, user.Id, notionImportJobKind, workspaceID,
		func(txApp core.App) error {
			if _, authorizeErr := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite); authorizeErr != nil {
				return authorizeErr
			}
			job, loadErr := loadStoredJob(txApp, jobID, user.Id)
			if errors.Is(loadErr, sql.ErrNoRows) {
				return nil
			}
			if loadErr != nil {
				return loadErr
			}
			payload := notionImportJobPayload{}
			if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
				return err
			}
			if job.Status != durableJobStatusCanceled && payload.Publishing {
				return errNotionImportCancelTooLate
			}
			return nil
		},
	)
	if errors.Is(err, errNotionImportCancelTooLate) {
		outcome.unavailable = true
		err = nil
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportNotFound, "Import not found.")
	}
	if outcome.unavailable {
		return apiError(event, http.StatusConflict, errorCodeJobCancelUnavailable, "This import can't be canceled anymore.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobControlFailed, "Couldn't cancel the import.", err)
	}
	// A migration's notices lose their time left (notion_migration_time_said.go).
	settleNotionMigrationNoticeHeadsOfJob(r.app, jobID, user.Id)
	response, err := notionImportResponseFor(event.Request.Context(), r.app, jobID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the import.", err)
	}
	return event.JSON(http.StatusOK, response)
}

func (r *durableJobRunner) getNotionImportJob(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	var response notionImportJob
	err := withAuthorizedNotionImportRead(event.Request.Context(), event.App, user.Id, workspaceID, func(txApp core.App) error {
		var readErr error
		response, readErr = notionImportResponseFor(event.Request.Context(), txApp, jobID, user.Id, workspaceID)
		return readErr
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportNotFound, "Import not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the import.", err)
	}
	return event.JSON(http.StatusOK, response)
}

func notionImportResponse(app core.App, id, owner, workspaceID string) (notionImportJob, error) {
	return notionImportResponseFor(context.Background(), app, id, owner, workspaceID)
}

// notionImportResponseFor is an import's state for the owner whose own
// request ctx carries, its time left said in its language.
func notionImportResponseFor(ctx context.Context, app core.App, id, owner, workspaceID string) (notionImportJob, error) {
	job, err := loadStoredJob(app, id, owner)
	if err != nil || job.Kind != notionImportJobKind || job.WorkspaceID != workspaceID {
		if err == nil {
			err = sql.ErrNoRows
		}
		return notionImportJob{}, err
	}
	payload := notionImportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return notionImportJob{}, err
	}
	var stored storedNotionImportReport
	if err := app.DB().NewQuery(`
		SELECT [[pages]], [[blocks]], [[databases]], [[rows]], [[files_copied]], [[files_omitted]]
		FROM {{leafen_notion_import_reports}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&stored); err != nil {
		return notionImportJob{}, err
	}
	// What didn't come whole, one tally whose sums are the counts; a
	// migration's run counts what the migration still hasn't fully moved, as
	// its report lists it (notion_import_not_whole.go).
	notWhole, err := notionImportNotWholeOf(app, job.ID, payload.MigrationID)
	if err != nil {
		return notionImportJob{}, err
	}
	// Once it ended, each item names the first pages it was on, as its finish
	// screen and row say them (notion_import_issue_places.go).
	if job.Status != durableJobStatusQueued {
		if err := notionImportNotWholeWithPages(app, job.ID, payload.MigrationID, notWhole); err != nil {
			return notionImportJob{}, err
		}
	}
	blockIssues, propertyIssues, viewIssues := notionImportNotWholeCounts(notWhole)
	status := job.Status
	if status == durableJobStatusQueued && job.ChunkCount > 0 {
		status = notionImportStatusRunning
	}
	stage, step := notionImportJobStageStep(app, job, payload)
	var progress *notionImportProgress
	if job.Status == durableJobStatusQueued {
		if progress, err = notionImportProgressOfFor(ctx, app, job, payload); err != nil {
			return notionImportJob{}, err
		}
	}
	// A run that stopped names the reference its failure was logged under,
	// which its card shows where Leafen itself failed (notionImportFailure.ts).
	reference := ""
	if job.Status == durableJobStatusFailed {
		reference = job.DiagnosticReference
	}
	return notionImportJob{
		ID: job.ID, WorkspaceID: job.WorkspaceID, Status: status, Phase: payload.Phase,
		ProcessedItems: job.Processed, TotalItems: job.Total, FilesPending: len(payload.PendingFileIDs), ErrorCode: job.ErrorCode,
		DiagnosticReference: reference,
		CanCancel:           notionImportCanCancel(app, job, payload), Stage: stage, Step: step,
		CommentBlocksDone: payload.CommentBlocksDone, CommentBlocksTotal: payload.CommentBlocksTotal,
		CommentPagesDone: payload.CommentPagesDone, CommentPagesTotal: payload.CommentPagesTotal,
		Report: notionImportReport{
			Pages: stored.Pages, Blocks: stored.Blocks, Databases: stored.Databases, Rows: stored.Rows,
			FilesCopied: stored.FilesCopied, FilesOmitted: stored.FilesOmitted, BlockIssues: blockIssues,
			PropertyIssues: propertyIssues, ViewIssues: viewIssues, NotWhole: notWhole,
		},
		Progress: progress,
	}, nil
}

func normalizeNotionRoots(ids, kinds []string) ([]string, []string, bool) {
	if len(ids) == 0 || (len(kinds) != 0 && len(kinds) != len(ids)) {
		return nil, nil, false
	}
	seen := make(map[string]struct{}, len(ids))
	resultIDs := make([]string, 0, len(ids))
	resultKinds := make([]string, 0, len(ids))
	for index, value := range ids {
		value = canonicalNotionID(value)
		if value == "" || len(value) > maxWorkspacePermissionIdentifierLength {
			return nil, nil, false
		}
		if _, exists := seen[value]; exists {
			return nil, nil, false
		}
		kind := "page"
		if len(kinds) > 0 {
			kind = strings.TrimSpace(kinds[index])
		}
		if kind != "page" && kind != "database" {
			return nil, nil, false
		}
		seen[value] = struct{}{}
		resultIDs = append(resultIDs, value)
		resultKinds = append(resultKinds, kind)
	}
	return resultIDs, resultKinds, true
}

// canonicalNotionID writes a Notion ID as Notion returns it — lower case,
// with dashes — however it was given (a page link's ID has none), so a root
// matches the object the provider reads for it.
func canonicalNotionID(value string) string {
	value = strings.TrimSpace(value)
	plain := strings.ToLower(strings.ReplaceAll(value, "-", ""))
	if len(plain) != 32 || strings.Trim(plain, "0123456789abcdef") != "" {
		return value
	}
	return plain[:8] + "-" + plain[8:12] + "-" + plain[12:16] + "-" + plain[16:20] + "-" + plain[20:]
}

func notionImportJobRequestHash(workspaceID string, input notionImportJobInput) string {
	encodedIDs, _ := json.Marshal(input.RootIDs)
	encodedKinds, _ := json.Marshal(input.RootKinds)
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + input.ConnectionID + "\x00" + string(encodedIDs) + "\x00" + string(encodedKinds) + "\x00" + input.RequestID + "\x00true"))
	return hex.EncodeToString(digest[:])
}

func notionImportLegacyPageRequestMatches(existing storedJob, workspaceID string, input notionImportJobInput) bool {
	if existing.Kind != notionImportJobKind {
		return false
	}
	for _, kind := range input.RootKinds {
		if kind != "page" {
			return false
		}
	}
	encodedIDs, _ := json.Marshal(input.RootIDs)
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + input.ConnectionID + "\x00" + string(encodedIDs) + "\x00" + input.RequestID + "\x00true"))
	return existing.RequestHash == hex.EncodeToString(digest[:])
}
