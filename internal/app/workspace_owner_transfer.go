package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	workspaceOwnerTransferJobKind                 = "workspace-owner-transfer"
	workspaceOwnerTransferLockNamespace           = "workspace-owner-transfer"
	workspaceOwnerTransferSettlementLockNamespace = "workspace-owner-transfer-settlement"
	workspaceOwnerTransferMaxRetryDelay           = time.Minute
)

var (
	errWorkspaceOwnerTransferTargetInvalid  = errors.New(errorCodeWorkspaceOwnerTransferTargetInvalid)
	errWorkspaceOwnerTransferIdentityReused = errors.New(errorCodeWorkspaceOwnerTransferIdentityReused)
	errWorkspaceOwnerTransferRevision       = errors.New(errorCodeWorkspaceOwnerTransferRevisionConflict)
)

type workspaceOwnerTransferPayload struct {
	SchemaVersion        int    `json:"schemaVersion"`
	ActorID              string `json:"actorId"`
	WorkspaceID          string `json:"workspaceId"`
	RequestID            string `json:"requestId"`
	PreviousOwnerID      string `json:"previousOwnerId"`
	OwnerID              string `json:"ownerId"`
	BasePermissionCursor int    `json:"basePermissionCursor"`
	PermissionCursor     int    `json:"permissionCursor"`
}

type workspaceOwnerTransferAdmission struct {
	Job       storedJob
	Response  workspaceOwnerTransferResponse
	Duplicate bool
}

type administratorWorkspaceOwnershipTransfer struct {
	ActorID                  string
	PreviousOwnerID          string
	OwnerID                  string
	WorkspaceID              string
	RequestID                string
	ExpectedPermissionCursor *int
	AdmitSuccessor           bool
}

func transferWorkspaceOwnershipInTransaction(app core.App, input administratorWorkspaceOwnershipTransfer) (int, error) {
	var organizationID string
	if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}}
		WHERE [[id]]={:workspaceId} AND [[workspace_owner]]={:previousOwner} AND [[deletion_job_id]]=''`).Bind(dbx.Params{
		"workspaceId": input.WorkspaceID, "previousOwner": input.PreviousOwnerID,
	}).Row(&organizationID); err != nil {
		return 0, err
	}
	var targetRoles []storedPermissionRole
	if err := app.DB().NewQuery(`SELECT [[role]] FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:targetId} LIMIT 2`).Bind(dbx.Params{
		"workspaceId": input.WorkspaceID, "targetId": input.OwnerID,
	}).All(&targetRoles); err != nil {
		return 0, err
	}
	if input.OwnerID == input.PreviousOwnerID || len(targetRoles) > 1 ||
		(len(targetRoles) == 1 && targetRoles[0].Role != workspaceRoleMember) ||
		(len(targetRoles) == 0 && !input.AdmitSuccessor) {
		return 0, errWorkspaceOwnerTransferTargetInvalid
	}
	if len(targetRoles) == 0 {
		if err := admitAdministratorWorkspaceOwnershipSuccessor(app, organizationID, input.WorkspaceID, input.OwnerID, input.ActorID); err != nil {
			return 0, err
		}
	}
	cursor, err := currentWorkspacePermissionCursor(app, input.WorkspaceID)
	if err != nil {
		return 0, err
	}
	if input.ExpectedPermissionCursor != nil && cursor != *input.ExpectedPermissionCursor {
		return 0, errWorkspaceOwnerTransferRevision
	}
	now := time.Now().UTC().UnixMilli()
	if err := insertWorkspaceMembership(app, input.WorkspaceID, input.PreviousOwnerID, now); err != nil {
		return 0, err
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_workspaces}}
		SET [[workspace_owner]]={:targetId},[[updated]]=datetime('now')
		WHERE [[id]]={:workspaceId} AND [[workspace_owner]]={:previousOwner} AND [[deletion_job_id]]=''`).Bind(dbx.Params{
		"targetId": input.OwnerID, "workspaceId": input.WorkspaceID, "previousOwner": input.PreviousOwnerID,
	}).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return 0, errors.Join(permissionDenied(), err)
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:targetId} AND [[role]]='member'`).Bind(dbx.Params{
		"workspaceId": input.WorkspaceID, "targetId": input.OwnerID,
	}).Execute(); err != nil {
		return 0, err
	}
	noteWorkspaceSetChanges(input.OwnerID)
	permissionCursor, err := advanceWorkspacePermissionCursor(app, input.WorkspaceID, now)
	if err != nil {
		return 0, err
	}
	// People's Membership Type reads owner for one and member for the other.
	notePeopleMembershipChanged(app, input.WorkspaceID, input.ActorID, input.OwnerID, input.PreviousOwnerID)
	if err := recordOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: organizationID, WorkspaceID: input.WorkspaceID,
		ActorID: input.ActorID, Action: "workspace_owner.transfer", TargetType: "workspace_owner",
		TargetID: input.OwnerID, Metadata: map[string]any{"previousOwnerId": input.PreviousOwnerID}, OccurredAt: now,
	}); err != nil {
		return 0, err
	}
	for _, notification := range []struct{ userID, kind string }{
		{input.OwnerID, "ownership_received"}, {input.PreviousOwnerID, "ownership_transferred"},
	} {
		if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_workspace_owner_transfer_notifications}}
			([[id]],[[workspace_id]],[[user_id]],[[actor_id]],[[request_id]],[[kind]],[[created]])
			VALUES ({:id},{:workspaceId},{:userId},{:actorId},{:requestId},{:kind},{:created})`).Bind(dbx.Params{
			"id": security.RandomString(15), "workspaceId": input.WorkspaceID, "userId": notification.userID,
			"actorId": input.ActorID, "requestId": input.RequestID, "kind": notification.kind, "created": now,
		}).Execute(); err != nil {
			return 0, err
		}
	}
	return permissionCursor, nil
}

func registerWorkspaceOwnerTransferRoutes(
	event *core.ServeEvent,
	roomInvalidator workspaceRoomInvalidator,
) {
	event.Router.POST(apiRoot+workspaceOwnerTransferStartPath, func(request *core.RequestEvent) error {
		return startWorkspaceOwnerTransfer(request, roomInvalidator)
	}).Bind(apis.BodyLimit(workspaceOwnerTransferMaxBodyBytes))
	event.Router.GET(apiRoot+workspaceOwnerTransferReadPath, readWorkspaceOwnerTransfer)
}

func startWorkspaceOwnerTransfer(
	event *core.RequestEvent,
	roomInvalidator workspaceRoomInvalidator,
) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := workspaceOwnerTransferInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceOwnerTransferInvalid, "Check the ownership transfer request.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.TargetUserID = strings.TrimSpace(input.TargetUserID)
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		input.RequestID == "" || len(input.RequestID) > maxWorkspaceOwnerTransferRequestIDLength ||
		input.TargetUserID == "" || len(input.TargetUserID) > maxWorkspacePermissionIdentifierLength ||
		input.BasePermissionCursor < 0 {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceOwnerTransferInvalid, "Check the ownership transfer request.")
	}
	// A key's call asks its person only for a transfer that would start
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := admitWorkspaceOwnerTransfer(txApp, user.Id, workspaceID, input)
		return err
	})
	admission := workspaceOwnerTransferAdmission{}
	if err == nil {
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		admission, err = admitWorkspaceOwnerTransfer(event.App, user.Id, workspaceID, input)
	}
	if isPermissionDenied(err) {
		return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owner hands it to a new owner.", workspaceNotFound(event))
	}
	if errors.Is(err, errWorkspaceOwnerTransferTargetInvalid) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceOwnerTransferTargetInvalid, "Choose a different workspace member as the owner.")
	}
	if errors.Is(err, errWorkspaceOwnerTransferIdentityReused) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceOwnerTransferIdentityReused, "This request was already used with a different meaning.")
	}
	if errors.Is(err, errWorkspaceOwnerTransferRevision) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceOwnerTransferRevisionConflict, "The workspace permissions changed. Check them again.")
	}
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(event, err, persistentWriteWorkspaceOwnerTransfer, "admission"); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceOwnerTransferFailed, "Couldn't transfer ownership.", err)
	}
	runner := workspaceMembershipRevocationRunner(event.App, roomInvalidator)
	completed, err := runner.advanceWorkspaceOwnerTransfer(event.Request.Context(), admission.Job.ID)
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(event, err, persistentWriteWorkspaceOwnerTransfer, "room-invalidation-settlement"); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceOwnerTransferFailed, "Couldn't apply the new owner permissions.", err)
	}
	status := http.StatusAccepted
	if completed {
		status = http.StatusOK
		admission.Response.Status = workspaceOwnerTransferStatusCompleted
	} else {
		runner.notify()
	}
	admission.Response.Duplicate = admission.Duplicate
	return event.JSON(status, admission.Response)
}

func readWorkspaceOwnerTransfer(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	job, err := loadStoredJob(event.App, jobID, user.Id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (job.WorkspaceID != workspaceID || job.Kind != workspaceOwnerTransferJobKind)) {
		return apiError(event, http.StatusNotFound, errorCodeOwnerTransferNotFound, "Ownership transfer request not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceOwnerTransferFailed, "Couldn't load the ownership transfer state.", err)
	}
	response, err := workspaceOwnerTransferResponseFromJob(job, true)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceOwnerTransferFailed, "Couldn't load the ownership transfer state.", err)
	}
	return event.JSON(http.StatusOK, response)
}

func workspaceOwnerTransferRequestHash(workspaceID string, input workspaceOwnerTransferInput) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + input.TargetUserID + "\x00" + strconv.Itoa(input.BasePermissionCursor)))
	return hex.EncodeToString(digest[:])
}

func encodeWorkspaceOwnerTransferPayload(payload workspaceOwnerTransferPayload) (string, error) {
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

func decodeWorkspaceOwnerTransferPayload(job storedJob) (workspaceOwnerTransferPayload, error) {
	payload := workspaceOwnerTransferPayload{}
	if job.Kind != workspaceOwnerTransferJobKind || json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.SchemaVersion != workspaceOwnerTransferPayloadFormat || payload.ActorID != job.Owner ||
		payload.WorkspaceID == "" || payload.WorkspaceID != job.WorkspaceID || payload.RequestID != job.RequestID ||
		payload.PreviousOwnerID == "" || payload.OwnerID == "" || payload.PermissionCursor <= 0 ||
		strconv.Itoa(payload.PermissionCursor) != job.Cursor {
		return workspaceOwnerTransferPayload{}, fmt.Errorf("%s: invalid durable owner transfer payload", errorCodeJobRunnerFailed)
	}
	return payload, nil
}

func workspaceOwnerTransferResponseFromJob(job storedJob, duplicate bool) (workspaceOwnerTransferResponse, error) {
	payload, err := decodeWorkspaceOwnerTransferPayload(job)
	if err != nil {
		return workspaceOwnerTransferResponse{}, err
	}
	status := workspaceOwnerTransferStatusPending
	if job.Status == durableJobStatusCompleted {
		status = workspaceOwnerTransferStatusCompleted
	} else if job.Status != durableJobStatusQueued {
		return workspaceOwnerTransferResponse{}, fmt.Errorf("%s: invalid owner transfer status", errorCodeJobRunnerFailed)
	}
	return workspaceOwnerTransferResponse{
		JobID: job.ID, RequestID: payload.RequestID, WorkspaceID: payload.WorkspaceID,
		PreviousOwnerID: payload.PreviousOwnerID, OwnerID: payload.OwnerID,
		PermissionCursor: payload.PermissionCursor, Status: status, Duplicate: duplicate,
	}, nil
}

func admitWorkspaceOwnerTransfer(
	app core.App,
	actorID, workspaceID string,
	input workspaceOwnerTransferInput,
) (workspaceOwnerTransferAdmission, error) {
	admission := workspaceOwnerTransferAdmission{}
	release := acquireProductOperationLock(workspaceOwnerTransferLockNamespace, workspaceID)
	defer release()
	err := app.RunInTransaction(func(txApp core.App) error {
		requestHash := workspaceOwnerTransferRequestHash(workspaceID, input)
		existing, readErr := loadStoredJobByRequest(txApp, actorID, input.RequestID)
		if readErr == nil {
			if existing.RequestHash != requestHash || existing.Kind != workspaceOwnerTransferJobKind {
				return errWorkspaceOwnerTransferIdentityReused
			}
			response, err := workspaceOwnerTransferResponseFromJob(existing, true)
			admission = workspaceOwnerTransferAdmission{Job: existing, Response: response, Duplicate: true}
			return err
		}
		if !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		if _, err := authorizeWorkspace(txApp, actorID, workspaceID, permissionWorkspaceOwner); err != nil {
			return err
		}
		expectedCursor := input.BasePermissionCursor
		permissionCursor, err := transferWorkspaceOwnershipInTransaction(txApp, administratorWorkspaceOwnershipTransfer{
			ActorID: actorID, PreviousOwnerID: actorID, OwnerID: input.TargetUserID,
			WorkspaceID: workspaceID, RequestID: input.RequestID,
			ExpectedPermissionCursor: &expectedCursor, AdmitSuccessor: false,
		})
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		payloadJSON, err := encodeWorkspaceOwnerTransferPayload(workspaceOwnerTransferPayload{
			SchemaVersion: workspaceOwnerTransferPayloadFormat, ActorID: actorID, WorkspaceID: workspaceID,
			RequestID: input.RequestID, PreviousOwnerID: actorID, OwnerID: input.TargetUserID,
			BasePermissionCursor: input.BasePermissionCursor, PermissionCursor: permissionCursor,
		})
		if err != nil {
			return err
		}
		jobID := security.RandomString(15)
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],[[status]],
				 [[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],[[payload_json]],
				 [[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],[[chunk_count]])
			VALUES ({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:targetId},'queued',
				'', '', {:notBefore},{:now},{:now},{:payload},{:cursor},0,1,0,0,0)
		`).Bind(dbx.Params{
			"id": jobID, "owner": actorID, "workspaceId": workspaceID, "requestId": input.RequestID,
			"requestHash": requestHash, "kind": workspaceOwnerTransferJobKind, "targetId": input.TargetUserID,
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "now": now,
			"payload": payloadJSON, "cursor": strconv.Itoa(permissionCursor),
		}).Execute(); err != nil {
			return err
		}
		job, err := loadStoredJob(txApp, jobID, actorID)
		if err != nil {
			return err
		}
		response, err := workspaceOwnerTransferResponseFromJob(job, false)
		admission = workspaceOwnerTransferAdmission{Job: job, Response: response}
		return err
	})
	return admission, err
}

func (r *durableJobRunner) advanceWorkspaceOwnerTransfer(ctx context.Context, jobID string) (bool, error) {
	job, err := loadStoredJob(r.app, jobID, "")
	if err != nil {
		return false, err
	}
	if job.Status == durableJobStatusCompleted {
		return true, nil
	}
	payload, err := decodeWorkspaceOwnerTransferPayload(job)
	if err != nil || job.Status != durableJobStatusQueued {
		return false, errors.Join(err, fmt.Errorf("%s: invalid owner transfer job state", errorCodeJobRunnerFailed))
	}
	release := acquireProductOperationLock(workspaceOwnerTransferSettlementLockNamespace, payload.WorkspaceID)
	defer release()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, err := loadStoredJob(r.app, jobID, "")
	if err != nil || current.Status == durableJobStatusCompleted {
		return current.Status == durableJobStatusCompleted, err
	}
	if current.Status != durableJobStatusQueued {
		return false, fmt.Errorf("%s: invalid owner transfer job state", errorCodeJobRunnerFailed)
	}
	invalidateErr := errors.New("workspace owner transfer room invalidator is unavailable")
	if r.rooms != nil {
		invalidateErr = r.rooms.InvalidateWorkspace(payload.WorkspaceID)
	}
	now := r.clock.now()
	if invalidateErr != nil {
		reference := newDiagnosticReference()
		attempt := current.Attempt + 1
		_, scheduleErr := r.app.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[error_code]]={:errorCode},[[diagnostic_reference]]={:reference},
				[[attempt]]={:attempt},[[not_before]]={:notBefore},[[updated]]={:updated}
			WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued'
		`).WithContext(ctx).Bind(dbx.Params{
			"errorCode": errorCodeYgoRoomInvalidationFailed, "reference": reference, "attempt": attempt,
			"notBefore": now.Add(workspaceOwnerTransferRetryDelay(attempt)).UnixMilli(),
			"updated":   now.UnixMilli(), "id": jobID, "kind": workspaceOwnerTransferJobKind,
		}).Execute()
		if scheduleErr != nil {
			return false, errors.Join(invalidateErr, scheduleErr)
		}
		logDurableJobDiagnostic(r.app, errorCodeYgoRoomInvalidationFailed, reference, jobID,
			"workspace-owner.transfer", "room-invalidation-retry", invalidateErr)
		return false, nil
	}
	result, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[status]]='completed',[[error_code]]='',[[diagnostic_reference]]='',
			[[processed_items]]=1,[[not_before]]=0,[[updated]]={:updated}
		WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued'
	`).WithContext(ctx).Bind(dbx.Params{
		"updated": now.UnixMilli(), "id": jobID, "kind": workspaceOwnerTransferJobKind,
	}).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func workspaceOwnerTransferRetryDelay(attempt int) time.Duration {
	delay := durableJobReadyDelay
	for current := 1; current < attempt && delay < workspaceOwnerTransferMaxRetryDelay; current++ {
		delay *= 2
	}
	if delay > workspaceOwnerTransferMaxRetryDelay {
		return workspaceOwnerTransferMaxRetryDelay
	}
	return delay
}
