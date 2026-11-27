package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Notion's deletion of a managed user (its help, read 2026-10-02): it is
// permanent after a 30-day grace period in which it may be cancelled and the
// person restored; their public and shared content stays. Here the person
// is suspended at once (organization_user_suspensions.go) and a job waits
// out the grace period. Then the workspaces they own in the organization
// pass to its owner, and the rest goes as their own account deletion does
// (account_deletion.go): their own organization's workspaces with them, the
// account anonymized. Someone whose organization holds other people's
// workspaces is left to the server's administrator, as there.

const (
	managedUserDeletionJobKind            = "managed-user-deletion"
	managedUserDeletionGrace              = 30 * 24 * time.Hour
	managedUserDeletionMaxAttempts        = 10
	errorCodeManagedUserDeletionBlocked   = "LEAFEN_MANAGED_USER_DELETION_BLOCKED"
	errorCodeManagedUserDeletionConfirm   = "LEAFEN_MANAGED_USER_DELETION_CONFIRMATION"
	errorCodeManagedUserDeletionStarted   = "LEAFEN_MANAGED_USER_DELETION_STARTED"
	errorCodeManagedUserDeletionScheduled = "LEAFEN_MANAGED_USER_DELETION_SCHEDULED"
)

var (
	errManagedUserDeletionBlocked   = errors.New(errorCodeManagedUserDeletionBlocked)
	errManagedUserDeletionConfirm   = errors.New(errorCodeManagedUserDeletionConfirm)
	errManagedUserDeletionStarted   = errors.New(errorCodeManagedUserDeletionStarted)
	errManagedUserDeletionScheduled = errors.New(errorCodeManagedUserDeletionScheduled)
)

type managedUserDeletionPayload struct {
	OrganizationID string `json:"organizationId"`
	UserID         string `json:"userId"`
}

type managedUserDeletionPreview struct {
	// The workspaces they own in the organization, which pass to its owner,
	// and their own, which go with them.
	Transferred []leafen.AccountDeletionWorkspace `json:"transferred"`
	Deleted     []leafen.AccountDeletionWorkspace `json:"deleted"`
	Blocker     string                            `json:"blocker"`
}

type storedManagedUserDeletion struct {
	JobID          string `db:"deletion_job_id"`
	KeptSuspension bool   `db:"kept_suspension"`
}

func managedUserDeletionPreviewFor(app core.App, organizationID, userID string) (managedUserDeletionPreview, error) {
	preview := managedUserDeletionPreview{Transferred: []leafen.AccountDeletionWorkspace{}, Deleted: []leafen.AccountDeletionWorkspace{}}
	account, err := leafen.AccountDeletionPreview(app, userID)
	if err != nil {
		return preview, err
	}
	preview.Blocker = account.Blocker
	if err := app.DB().NewQuery(`SELECT [[id]], [[name]] FROM {{leafen_workspaces}}
		WHERE [[organization]] = {:organizationId} AND [[workspace_owner]] = {:userId} AND [[deletion_job_id]] = ''
		ORDER BY [[name]], [[id]]`).Bind(dbx.Params{"organizationId": organizationID, "userId": userID}).
		All(&preview.Transferred); err != nil {
		return preview, err
	}
	transferred := map[string]bool{}
	for _, workspace := range preview.Transferred {
		transferred[workspace.ID] = true
	}
	for _, workspace := range account.DeletedWorkspaces {
		if !transferred[workspace.ID] {
			preview.Deleted = append(preview.Deleted, workspace)
		}
	}
	return preview, nil
}

func previewOrganizationManagedUserDeletion(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	if err := authorizeManagedUserTarget(event.App, authorization, userID); err != nil {
		return managedUsersError(event, err, "deletion-preview")
	}
	preview, err := managedUserDeletionPreviewFor(event.App, authorization.OrganizationID, userID)
	if err != nil {
		return managedUsersError(event, err, "deletion-preview")
	}
	return event.JSON(http.StatusOK, preview)
}

// scheduleOrganizationManagedUserDeletion suspends the person and queues
// their deletion for the end of the grace period, once the admin has typed
// their email.
func scheduleOrganizationManagedUserDeletion(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	input := struct {
		Email string `json:"email"`
	}{}
	if err := event.BindBody(&input); err != nil {
		return managedUsersError(event, errManagedUserDeletionConfirm, "deletion-bind")
	}
	release := leafen.AcquireProductOperationLock(leafen.AdministratorAccountLockNamespace, userID)
	defer release()
	change := func(txApp core.App) error {
		if err := authorizeManagedUserTarget(txApp, authorization, userID); err != nil {
			return err
		}
		user, err := txApp.FindRecordById("users", userID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(input.Email), user.Email()) {
			return errManagedUserDeletionConfirm
		}
		preview, err := managedUserDeletionPreviewFor(txApp, authorization.OrganizationID, userID)
		if err != nil {
			return err
		}
		if preview.Blocker != leafen.AccountDeletionBlockerNone {
			return errManagedUserDeletionBlocked
		}
		existing := storedManagedUserDeletion{}
		err = txApp.DB().NewQuery(`SELECT [[deletion_job_id]], [[kept_suspension]] FROM {{leafen_organization_user_suspensions}}
			WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId}`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "userId": userID,
		}).One(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		suspended := err == nil
		if existing.JobID != "" {
			return nil
		}
		now := time.Now().UTC()
		due := now.Add(managedUserDeletionGrace).UnixMilli()
		jobID := security.RandomString(15)
		payload, err := json.Marshal(managedUserDeletionPayload{OrganizationID: authorization.OrganizationID, UserID: userID})
		if err != nil {
			return err
		}
		requestID := "managed-user-deletion:" + userID + ":" + strconv.FormatInt(now.UnixMilli(), 10)
		digest := sha256.Sum256(payload)
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
				 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
				 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],[[chunk_count]])
			VALUES ({:id},{:owner},'',{:requestId},{:requestHash},{:kind},{:userId},
				'queued','','',{:due},{:now},{:now},{:payload},'',0,1,0,{:maxAttempts},0)
		`).Bind(dbx.Params{
			"id": jobID, "owner": authorization.PrincipalID, "requestId": requestID,
			"requestHash": hex.EncodeToString(digest[:]), "kind": managedUserDeletionJobKind, "userId": userID,
			"due": due, "now": now.UnixMilli(), "payload": string(payload), "maxAttempts": managedUserDeletionMaxAttempts,
		}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_organization_user_suspensions}}
				([[organization_id]], [[user_id]], [[suspended_by]], [[suspended_at]],
				 [[deletion_due_at]], [[deletion_job_id]], [[kept_suspension]])
			VALUES ({:organizationId}, {:userId}, {:actorId}, {:now}, {:due}, {:jobId}, 0)
			ON CONFLICT ([[organization_id]], [[user_id]]) DO UPDATE SET
				[[deletion_due_at]] = excluded.[[deletion_due_at]], [[deletion_job_id]] = excluded.[[deletion_job_id]],
				[[kept_suspension]] = 1
		`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "userId": userID, "actorId": authorization.PrincipalID,
			"now": now.UnixMilli(), "due": due, "jobId": jobID,
		}).Execute(); err != nil {
			return err
		}
		if !suspended {
			if _, err := leafen.RevokeAllBrowserSessionsCount(txApp, userID); err != nil {
				return err
			}
		}
		return recordManagedUserAudit(txApp, authorization, "managed_user.delete_schedule", userID,
			map[string]any{"dueAt": due, "transferred": len(preview.Transferred), "deleted": len(preview.Deleted)}, now.UnixMilli())
	}
	// A key's call asks its person only for a deletion that would be scheduled
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "deletion-schedule")
	}
	leafen.WorkspaceMembershipRevocationRunner(event.App, nil).Notify()
	return respondOrganizationManagedUser(event, authorization, userID)
}

// restoreOrganizationManagedUser cancels a deletion still waiting out its
// grace period; someone suspended before it stays suspended.
func restoreOrganizationManagedUser(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	release := leafen.AcquireProductOperationLock(leafen.AdministratorAccountLockNamespace, userID)
	defer release()
	change := func(txApp core.App) error {
		scheduled := storedManagedUserDeletion{}
		err := txApp.DB().NewQuery(`SELECT [[deletion_job_id]], [[kept_suspension]] FROM {{leafen_organization_user_suspensions}}
			WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId}`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "userId": userID,
		}).One(&scheduled)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && scheduled.JobID == "") {
			return authorizeManagedUserTarget(txApp, authorization, userID)
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[status]] = 'canceled', [[not_before]] = 0, [[updated]] = {:now}
			WHERE [[id]] = {:id} AND [[kind]] = {:kind} AND [[status]] IN ('queued', 'failed')`).Bind(dbx.Params{
			"now": now, "id": scheduled.JobID, "kind": managedUserDeletionJobKind,
		}).Execute()
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return errors.Join(err, errManagedUserDeletionStarted)
		}
		query := `DELETE FROM {{leafen_organization_user_suspensions}} WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId}`
		if scheduled.KeptSuspension {
			query = `UPDATE {{leafen_organization_user_suspensions}} SET [[deletion_due_at]] = 0, [[deletion_job_id]] = '', [[kept_suspension]] = 0
				WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId}`
		}
		if _, err := txApp.DB().NewQuery(query).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "userId": userID,
		}).Execute(); err != nil {
			return err
		}
		return recordManagedUserAudit(txApp, authorization, "managed_user.delete_cancel", userID, nil, now)
	}
	// A key's call asks its person only for a restore that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "deletion-restore")
	}
	return respondOrganizationManagedUser(event, authorization, userID)
}

// advanceManagedUserDeletion carries out a deletion whose grace period is
// over. Anything that can't finish now is tried again later; a person whose
// organization came to hold other people's workspaces stays suspended.
func advanceManagedUserDeletion(r *leafen.DurableJobRunner, ctx context.Context, jobID string) error {
	job, err := leafen.LoadStoredJob(r.App(), jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != leafen.DurableJobStatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	payload := managedUserDeletionPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || payload.UserID == "" || payload.OrganizationID == "" {
		return settleManagedUserDeletion(r, ctx, job.ID, leafen.DurableJobStatusFailed, leafen.ErrorCodeJobRunnerFailed)
	}
	release := leafen.AcquireProductOperationLock(leafen.AdministratorAccountLockNamespace, payload.UserID)
	defer release()
	var scheduled int
	if err := r.App().DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_user_suspensions}}
		WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId} AND [[deletion_job_id]] = {:jobId}`).Bind(dbx.Params{
		"organizationId": payload.OrganizationID, "userId": payload.UserID, "jobId": job.ID,
	}).Row(&scheduled); err != nil {
		return err
	}
	if scheduled != 1 {
		return settleManagedUserDeletion(r, ctx, job.ID, leafen.DurableJobStatusCanceled, "")
	}
	var owner string
	if err := r.App().DB().NewQuery(`SELECT [[owner]] FROM {{leafen_organizations}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": payload.OrganizationID}).Row(&owner); err != nil {
		return err
	}
	transferred := []string{}
	if err := r.App().DB().NewQuery(`SELECT [[id]] FROM {{leafen_workspaces}}
		WHERE [[organization]] = {:organizationId} AND [[workspace_owner]] = {:userId} AND [[deletion_job_id]] = ''
		ORDER BY [[id]]`).Bind(dbx.Params{"organizationId": payload.OrganizationID, "userId": payload.UserID}).
		Column(&transferred); err != nil {
		return err
	}
	for _, workspaceID := range transferred {
		if r.Rooms() == nil {
			return deferManagedUserDeletion(r, ctx, job, errors.New("workspace room invalidator is unavailable"))
		}
		if err := r.Rooms().InvalidateWorkspace(workspaceID); err != nil {
			return deferManagedUserDeletion(r, ctx, job, err)
		}
		if err := r.App().RunInTransaction(func(txApp core.App) error {
			_, err := leafen.TransferWorkspaceOwnershipInTransaction(txApp, leafen.AdministratorWorkspaceOwnershipTransfer{
				ActorID: job.Owner, PreviousOwnerID: payload.UserID, OwnerID: owner, WorkspaceID: workspaceID,
				RequestID: job.RequestID + ":" + workspaceID, AdmitSuccessor: true,
			})
			return err
		}); err != nil {
			return deferManagedUserDeletion(r, ctx, job, err)
		}
	}
	deleted := []string{}
	blocked := false
	err = r.App().RunInTransaction(func(txApp core.App) error {
		preview, err := leafen.AccountDeletionPreview(txApp, payload.UserID)
		if err != nil {
			return err
		}
		state, err := leafen.LoadAdministratorAccountState(txApp, payload.UserID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		audit := leafen.OrganizationAuthorization{OrganizationID: payload.OrganizationID, PrincipalID: job.Owner}
		if preview.Blocker != leafen.AccountDeletionBlockerNone || state.Status != leafen.AdministratorAccountStatusActive {
			blocked = true
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_organization_user_suspensions}}
				SET [[deletion_due_at]] = 0, [[deletion_job_id]] = '', [[kept_suspension]] = 0
				WHERE [[organization_id]] = {:organizationId} AND [[user_id]] = {:userId}`).Bind(dbx.Params{
				"organizationId": payload.OrganizationID, "userId": payload.UserID,
			}).Execute(); err != nil {
				return err
			}
			if err := recordManagedUserAudit(txApp, audit, "managed_user.delete_blocked", payload.UserID,
				map[string]any{"blocker": preview.Blocker, "accountStatus": state.Status}, now); err != nil {
				return err
			}
			return settleManagedUserDeletionIn(r, txApp, job.ID, leafen.DurableJobStatusFailed, errorCodeManagedUserDeletionBlocked)
		}
		requestID := "managed-user-deletion:" + job.ID
		deleted, err = leafen.AdmitAccountWorkspaceDeletions(txApp, payload.UserID, requestID)
		if err != nil {
			return err
		}
		// The administrator who scheduled it deletes the account, on the
		// server's and the organizations' deletion lines too: this
		// organization's one line is the account's "사용자 삭제됨", as its
		// cleanup writes it (the workspaces passed on and deleted have lines
		// of their own).
		if err := leafen.AdmitAccountDeletionBy(txApp, payload.UserID, job.Owner, requestID, state, len(deleted)); err != nil {
			return err
		}
		if err := leafen.RecordManagedAccountDeletionAudit(txApp, payload.OrganizationID, payload.UserID); err != nil {
			return err
		}
		return settleManagedUserDeletionIn(r, txApp, job.ID, leafen.DurableJobStatusCompleted, "")
	})
	if err != nil {
		return deferManagedUserDeletion(r, ctx, job, err)
	}
	for _, workspaceID := range deleted {
		if r.Rooms() != nil {
			_ = r.Rooms().InvalidateWorkspace(workspaceID)
		}
	}
	if !blocked {
		leafen.NotifyAdministratorAccountCleanupRunner(r.App())
	}
	return nil
}

func settleManagedUserDeletion(r *leafen.DurableJobRunner, ctx context.Context, jobID, status, errorCode string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return settleManagedUserDeletionIn(r, r.App(), jobID, status, errorCode)
}

func settleManagedUserDeletionIn(r *leafen.DurableJobRunner, app core.App, jobID, status, errorCode string) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[status]] = {:status}, [[error_code]] = {:errorCode},
			[[processed_items]] = 1, [[not_before]] = 0, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[kind]] = {:kind} AND [[status]] = 'queued'`).Bind(dbx.Params{
		"status": status, "errorCode": errorCode, "updated": leafen.DurableJobClockNow(r.Clock()).UnixMilli(),
		"id": jobID, "kind": managedUserDeletionJobKind,
	}).Execute()
	return err
}

func deferManagedUserDeletion(r *leafen.DurableJobRunner, ctx context.Context, job leafen.StoredJob, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := leafen.DurableJobClockNow(r.Clock())
	attempt := job.Attempt + 1
	status, notBefore := leafen.DurableJobStatusQueued, now.Add(time.Duration(attempt)*time.Minute).UnixMilli()
	if attempt >= job.MaxAttempts {
		status, notBefore = leafen.DurableJobStatusFailed, int64(0)
	}
	reference := leafen.NewDiagnosticReference()
	leafen.LogDurableJobDiagnostic(r.App(), leafen.ErrorCodeJobRunnerFailed, reference, job.ID, "managed-user.deletion", "retry", cause)
	_, err := r.App().DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[attempt]] = {:attempt}, [[status]] = {:status},
			[[not_before]] = {:notBefore}, [[error_code]] = {:errorCode}, [[diagnostic_reference]] = {:reference}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[kind]] = {:kind} AND [[status]] = 'queued'`).Bind(dbx.Params{
		"attempt": attempt, "status": status, "notBefore": notBefore, "errorCode": leafen.ErrorCodeJobRunnerFailed,
		"reference": reference, "updated": now.UnixMilli(), "id": job.ID, "kind": managedUserDeletionJobKind,
	}).Execute()
	return err
}
