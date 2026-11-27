package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	workspaceMembershipRevocationJobKind       = "workspace-membership-revocation"
	workspaceMembershipRevocationPayloadFormat = 1
	workspaceMembershipRevocationLockNamespace = "workspace-membership-revocation"
	workspaceMembershipRevocationWindow        = 64
	workspaceMembershipRevocationMaxRetryDelay = time.Minute
)

type workspaceMembershipRevocationPayload struct {
	SchemaVersion    int    `json:"schemaVersion"`
	WorkspaceID      string `json:"workspaceId"`
	UserID           string `json:"userId"`
	PermissionCursor int    `json:"permissionCursor"`
}

type workspaceMembershipRevocationAdmission struct {
	Job      storedJob
	Response workspaceMembershipRevocationResponse
	Missing  bool
	// Removed says this call took the membership away, not an earlier one.
	Removed bool
}

type workspaceMembershipRevocationWindowJob struct {
	ID      string `db:"id"`
	Attempt int    `db:"attempt"`
}

func workspaceMembershipRevocationRequestID(workspaceID, userID string) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + userID))
	return "workspace-membership-revocation:" + hex.EncodeToString(digest[:])
}

func encodeWorkspaceMembershipRevocationPayload(
	workspaceID, userID string,
	permissionCursor int,
) (string, string, error) {
	payload, err := json.Marshal(workspaceMembershipRevocationPayload{
		SchemaVersion: workspaceMembershipRevocationPayloadFormat,
		WorkspaceID:   workspaceID, UserID: userID, PermissionCursor: permissionCursor,
	})
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(payload)
	return string(payload), hex.EncodeToString(digest[:]), nil
}

func decodeWorkspaceMembershipRevocationPayload(job storedJob) (workspaceMembershipRevocationPayload, error) {
	payload := workspaceMembershipRevocationPayload{}
	if job.Kind != workspaceMembershipRevocationJobKind ||
		json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.SchemaVersion != workspaceMembershipRevocationPayloadFormat ||
		payload.WorkspaceID == "" || payload.WorkspaceID != job.WorkspaceID ||
		payload.UserID == "" || payload.UserID != job.PageID ||
		payload.PermissionCursor <= 0 || strconv.Itoa(payload.PermissionCursor) != job.Cursor {
		return workspaceMembershipRevocationPayload{}, fmt.Errorf(
			"%s: invalid durable membership revocation payload", errorCodeJobRunnerFailed,
		)
	}
	return payload, nil
}

var errWorkspaceMemberLeaveManaged = errors.New(errorCodeWorkspaceMemberLeaveManaged)

func admitWorkspaceMembershipRevocation(
	app core.App,
	actorID, workspaceID, userID string,
) (workspaceMembershipRevocationAdmission, error) {
	admission := workspaceMembershipRevocationAdmission{}
	release := acquireProductOperationLock(workspaceMembershipRevocationLockNamespace, workspaceID)
	defer release()

	err := app.RunInTransaction(func(txApp core.App) error {
		// The owner removes a member; a member can leave.
		required := permissionWorkspaceOwner
		if actorID == userID {
			required = permissionWorkspaceRead
		}
		authorization, err := authorizeWorkspace(txApp, actorID, workspaceID, required)
		if err != nil {
			return err
		}
		if actorID == userID {
			managed, err := workspaceMemberProvisionedBySCIM(txApp, workspaceID, userID)
			if err != nil {
				return err
			}
			if managed {
				return errWorkspaceMemberLeaveManaged
			}
		}
		admission, err = revokeWorkspaceMembershipInTransaction(txApp, actorID, workspaceID, userID)
		if err != nil || !admission.Removed {
			return err
		}
		return recordWorkspaceMemberRemovalAudit(txApp, authorization.Organization.OrganizationID, actorID, workspaceID, userID)
	})
	return admission, err
}

// recordWorkspaceMemberRemovalAudit puts a member taken out of a workspace on
// its organization's audit log, as Notion's "Member removed" (its help
// "Workspace audit log", read 2026-11-13): who took them out and whom. One
// who leaves by themselves is there as having left, which Notion's log has
// no line of its own for (only its teamspaces' "Member left the teamspace").
func recordWorkspaceMemberRemovalAudit(app core.App, organizationID, actorID, workspaceID, userID string) error {
	action := "workspace_member.remove"
	if actorID == userID {
		action = "workspace_member.leave"
	}
	return recordOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: organizationID, WorkspaceID: workspaceID, ActorID: actorID,
		Action: action, TargetType: "workspace_member", TargetID: userID,
	})
}

// workspaceMemberProvisionedBySCIM says whether the workspace's organization
// has its identity provider decide that this person belongs: someone it
// provisioned would only be put back by leaving, so they ask an administrator
// instead. People the provider does not manage, such as the owner who set the
// organization up, leave like anywhere else.
func workspaceMemberProvisionedBySCIM(app core.App, workspaceID, userID string) (bool, error) {
	var managed int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_scim_settings}} [[setting]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[organization]] = [[setting]].[[organization_id]]
		JOIN {{leafen_organization_memberships}} [[membership]]
			ON [[membership]].[[organization]] = [[setting]].[[organization_id]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[setting]].[[enabled]] = 1
			AND [[membership]].[[user]] = {:userId} AND [[membership]].[[scim_managed]] = 1`).
		Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&managed)
	return managed > 0, err
}

// revokeWorkspaceMembershipInTransaction takes a person's membership of a
// workspace away and queues the job that invalidates the workspace's rooms
// as the actor's, or finds the job an earlier revocation queued.
func revokeWorkspaceMembershipInTransaction(
	txApp core.App, actorID, workspaceID, userID string,
) (workspaceMembershipRevocationAdmission, error) {
	admission := workspaceMembershipRevocationAdmission{}
	result, err := txApp.DB().NewQuery(`
		DELETE FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId} AND [[role]]={:role}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": userID, "role": workspaceRoleMember,
	}).Execute()
	if err != nil {
		return admission, err
	}
	noteWorkspaceSetChanges(userID)
	changed, err := result.RowsAffected()
	if err != nil {
		return admission, err
	}
	requestID := workspaceMembershipRevocationRequestID(workspaceID, userID)
	if changed == 0 {
		job, readErr := loadStoredJobByRequest(txApp, actorID, requestID)
		if errors.Is(readErr, sql.ErrNoRows) {
			admission.Missing = true
			return admission, nil
		}
		if readErr != nil {
			return admission, readErr
		}
		payload, decodeErr := decodeWorkspaceMembershipRevocationPayload(job)
		if decodeErr != nil || (job.Status != durableJobStatusQueued && job.Status != durableJobStatusCompleted) {
			if decodeErr != nil {
				return admission, decodeErr
			}
			return admission, fmt.Errorf("%s: invalid durable membership revocation status", errorCodeJobRunnerFailed)
		}
		admission.Job = job
		admission.Response = workspaceMembershipRevocationResponse{
			WorkspaceID: workspaceID, UserID: userID, Role: workspaceRoleMember,
			PermissionCursor: payload.PermissionCursor, Revoked: true, JobID: job.ID, // its rooms' settling, at GET /jobs/{id}
		}
		return admission, nil
	}
	if changed != 1 {
		return admission, fmt.Errorf("%s: invalid membership delete count %d", errorCodeJobRunnerFailed, changed)
	}
	now := time.Now().UTC()
	cursor, err := advanceWorkspacePermissionCursor(txApp, workspaceID, now.UnixMilli())
	if err != nil {
		return admission, err
	}
	payload, requestHash, err := encodeWorkspaceMembershipRevocationPayload(workspaceID, userID, cursor)
	if err != nil {
		return admission, err
	}
	job, readErr := loadStoredJobByRequest(txApp, actorID, requestID)
	switch {
	case errors.Is(readErr, sql.ErrNoRows):
		jobID := security.RandomString(15)
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
				 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
				 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],[[chunk_count]])
			VALUES
				({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:userId},
				 'queued','','',{:notBefore},{:created},{:updated},
				 {:payload},{:cursor},0,1,0,0,0)
		`).Bind(dbx.Params{
			"id": jobID, "owner": actorID, "workspaceId": workspaceID,
			"requestId": requestID, "requestHash": requestHash,
			"kind": workspaceMembershipRevocationJobKind, "userId": userID,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
			"created":   now.UnixMilli(), "updated": now.UnixMilli(),
			"payload": payload, "cursor": strconv.Itoa(cursor),
		}).Execute()
		if err != nil {
			return admission, err
		}
	case readErr != nil:
		return admission, readErr
	default:
		if _, decodeErr := decodeWorkspaceMembershipRevocationPayload(job); decodeErr != nil {
			return admission, decodeErr
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[workspace_id]]={:workspaceId},[[request_hash]]={:requestHash},[[kind]]={:kind},
				[[page_id]]={:userId},[[status]]='queued',[[error_code]]='',[[diagnostic_reference]]='',
				[[not_before]]={:notBefore},[[updated]]={:updated},[[payload_json]]={:payload},
				[[cursor]]={:cursor},[[processed_items]]=0,[[total_items]]=1,[[attempt]]=0,
				[[max_attempts]]=0,[[chunk_count]]=0
			WHERE [[id]]={:id} AND [[owner]]={:owner} AND [[request_id]]={:requestId}
		`).Bind(dbx.Params{
			"id": job.ID, "owner": actorID, "workspaceId": workspaceID,
			"requestId": requestID, "requestHash": requestHash,
			"kind": workspaceMembershipRevocationJobKind, "userId": userID,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
			"payload": payload, "cursor": strconv.Itoa(cursor),
		}).Execute()
		if err != nil {
			return admission, err
		}
	}
	admission.Job, err = loadStoredJobByRequest(txApp, actorID, requestID)
	if err != nil {
		return admission, err
	}
	admission.Response = workspaceMembershipRevocationResponse{
		WorkspaceID: workspaceID, UserID: userID, Role: workspaceRoleMember,
		PermissionCursor: cursor, Revoked: true, JobID: admission.Job.ID,
	}
	admission.Removed = true
	// Leaving the organization's last ordinary workspace leaves its apps' too.
	return admission, revokeAppWorkspacesAfterLeaving(txApp, workspaceID, userID)
}

func workspaceMembershipRevocationRunner(
	app core.App,
	rooms workspaceRoomInvalidator,
) *durableJobRunner {
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		return runner
	}
	runner := newDurableJobRunner(app)
	runner.bindRoomInvalidator(rooms)
	return runner
}

func loadWorkspaceMembershipRevocationWindow(
	ctx context.Context,
	app core.App,
	workspaceID string,
) ([]workspaceMembershipRevocationWindowJob, error) {
	jobs := []workspaceMembershipRevocationWindowJob{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[attempt]]
		FROM {{leafen_jobs}} INDEXED BY [[idx_leafen_jobs_membership_revocation_window]]
		WHERE [[workspace_id]]={:workspaceId} AND [[kind]]={:kind} AND [[status]]='queued'
		ORDER BY [[created]], [[id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID,
		"kind":        workspaceMembershipRevocationJobKind,
		"limit":       workspaceMembershipRevocationWindow,
	}).All(&jobs)
	return jobs, err
}

func workspaceMembershipRevocationJobSet(
	jobs []workspaceMembershipRevocationWindowJob,
) (string, dbx.Params, error) {
	if len(jobs) == 0 || len(jobs) > workspaceMembershipRevocationWindow {
		return "", nil, fmt.Errorf("%s: invalid membership revocation window", errorCodeJobRunnerFailed)
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(jobs))
	seen := make(map[string]struct{}, len(jobs))
	for index, job := range jobs {
		if job.ID == "" {
			return "", nil, fmt.Errorf("%s: invalid membership revocation job identity", errorCodeJobRunnerFailed)
		}
		if _, exists := seen[job.ID]; exists {
			return "", nil, fmt.Errorf("%s: duplicate membership revocation job identity", errorCodeJobRunnerFailed)
		}
		seen[job.ID] = struct{}{}
		name := fmt.Sprintf("job%d", index)
		params[name] = job.ID
		placeholders = append(placeholders, "{:"+name+"}")
	}
	return strings.Join(placeholders, ","), params, nil
}

func (r *durableJobRunner) completeWorkspaceMembershipRevocationWindow(
	ctx context.Context,
	workspaceID string,
	jobs []workspaceMembershipRevocationWindowJob,
) error {
	jobIDs, params, err := workspaceMembershipRevocationJobSet(jobs)
	if err != nil {
		return err
	}
	params["kind"] = workspaceMembershipRevocationJobKind
	params["workspaceId"] = workspaceID
	params["updated"] = r.clock.now().UnixMilli()
	result, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='completed',[[error_code]]='',[[diagnostic_reference]]='',
			[[processed_items]]=1,[[not_before]]=0,[[updated]]={:updated}
		WHERE [[id]] IN (` + jobIDs + `) AND [[workspace_id]]={:workspaceId}
			AND [[kind]]={:kind} AND [[status]]='queued'
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != int64(len(jobs)) {
		return fmt.Errorf("%s: membership revocation completion conflict", errorCodeJobRunnerFailed)
	}
	return nil
}

func (r *durableJobRunner) advanceWorkspaceMembershipRevocation(
	ctx context.Context,
	jobID string,
) (bool, error) {
	job, err := loadStoredJob(r.app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err != nil {
		return false, err
	}
	if job.Status == durableJobStatusCompleted {
		return true, nil
	}
	if job.Status != durableJobStatusQueued {
		return false, fmt.Errorf("%s: invalid membership revocation job status", errorCodeJobRunnerFailed)
	}
	payload, err := decodeWorkspaceMembershipRevocationPayload(job)
	if err != nil {
		return false, err
	}

	release := acquireProductOperationLock(workspaceMembershipRevocationLockNamespace, payload.WorkspaceID)
	defer release()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, err := loadStoredJob(r.app, jobID, "")
	if err != nil {
		return false, err
	}
	if current.Status == durableJobStatusCompleted {
		return true, nil
	}
	if current.Status != durableJobStatusQueued {
		return false, fmt.Errorf("%s: invalid membership revocation job status", errorCodeJobRunnerFailed)
	}
	currentPayload, err := decodeWorkspaceMembershipRevocationPayload(current)
	if err != nil {
		return false, err
	}
	if currentPayload.WorkspaceID != payload.WorkspaceID {
		return false, fmt.Errorf("%s: changed membership revocation workspace", errorCodeJobRunnerFailed)
	}
	jobs, err := loadWorkspaceMembershipRevocationWindow(ctx, r.app, payload.WorkspaceID)
	if err != nil {
		return false, err
	}
	if len(jobs) == 0 {
		return false, fmt.Errorf("%s: queued membership revocation window is empty", errorCodeJobRunnerFailed)
	}
	targetSelected := false
	for _, selected := range jobs {
		if selected.ID == jobID {
			targetSelected = true
			break
		}
	}
	var invalidateErr error
	if r.rooms == nil {
		invalidateErr = errors.New("workspace membership room invalidator is unavailable")
	} else {
		invalidateErr = invalidateWorkspaceMembershipRooms(r.rooms, payload.WorkspaceID)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if invalidateErr != nil {
		reference := newDiagnosticReference()
		if err := r.scheduleWorkspaceMembershipRevocationRetry(
			ctx, payload.WorkspaceID, jobs, reference,
		); err != nil {
			return false, errors.Join(invalidateErr, err)
		}
		logDurableJobDiagnostic(
			r.app, errorCodeYgoRoomInvalidationFailed, reference, job.ID,
			"workspace-membership.revoke", "room-invalidation-retry", invalidateErr,
		)
		return false, nil
	}
	if err := r.completeWorkspaceMembershipRevocationWindow(
		ctx, payload.WorkspaceID, jobs,
	); err != nil {
		return false, err
	}
	if len(jobs) == workspaceMembershipRevocationWindow {
		r.notify()
	}
	return targetSelected, nil
}

func (r *durableJobRunner) scheduleWorkspaceMembershipRevocationRetry(
	ctx context.Context,
	workspaceID string,
	jobs []workspaceMembershipRevocationWindowJob,
	reference string,
) error {
	jobIDs, params, err := workspaceMembershipRevocationJobSet(jobs)
	if err != nil {
		return err
	}
	attempt := 1
	for _, job := range jobs {
		if job.Attempt >= attempt {
			attempt = job.Attempt + 1
		}
	}
	now := r.clock.now()
	params["errorCode"] = errorCodeYgoRoomInvalidationFailed
	params["reference"] = reference
	params["attempt"] = attempt
	params["notBefore"] = now.Add(workspaceMembershipRevocationRetryDelay(attempt)).UnixMilli()
	params["updated"] = now.UnixMilli()
	params["kind"] = workspaceMembershipRevocationJobKind
	params["workspaceId"] = workspaceID
	result, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[error_code]]={:errorCode},[[diagnostic_reference]]={:reference},
			[[attempt]]={:attempt},[[not_before]]={:notBefore},[[updated]]={:updated}
		WHERE [[id]] IN (` + jobIDs + `) AND [[workspace_id]]={:workspaceId}
			AND [[kind]]={:kind} AND [[status]]='queued'
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != int64(len(jobs)) {
		return fmt.Errorf("%s: membership revocation retry conflict", errorCodeJobRunnerFailed)
	}
	return nil
}

func workspaceMembershipRevocationRetryDelay(attempt int) time.Duration {
	delay := durableJobReadyDelay
	for current := 1; current < attempt && delay < workspaceMembershipRevocationMaxRetryDelay; current++ {
		delay *= 2
		if delay > workspaceMembershipRevocationMaxRetryDelay {
			return workspaceMembershipRevocationMaxRetryDelay
		}
	}
	return delay
}
