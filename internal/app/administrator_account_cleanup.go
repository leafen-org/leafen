package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	administratorAccountCleanupJobKind       = "administrator-account-cleanup"
	administratorAccountCleanupPayloadFormat = 1
	administratorAccountCleanupWaitingStatus = "waiting"
)

var (
	errAdministratorAccountOwnershipRequired = errors.New(errorCodeAdministratorAccountOwnershipRequired)
	errAdministratorAccountTransferInvalid   = errors.New(errorCodeAdministratorAccountTransferInvalid)
)

type administratorAccountCleanupPayload struct {
	SchemaVersion int    `json:"schemaVersion"`
	TargetUserID  string `json:"targetUserId"`
	Phase         string `json:"phase"`
	InventoryType string `json:"inventoryType"`
	Cursor        string `json:"cursor"`
	Revision      int    `json:"revision"`
	CleanupIndex  int    `json:"cleanupIndex"`
	// SelfDeletion marks the person's own deletion of their account, which
	// deletes the workspaces they own where an administrator's hands them on.
	SelfDeletion bool `json:"selfDeletion,omitzero"`
}

type storedAdministratorAccountTransferAssignment struct {
	ResourceType    string         `db:"resource_type"`
	ResourceID      string         `db:"resource_id"`
	ResourceName    string         `db:"resource_name"`
	SuccessorUserID sql.NullString `db:"successor_user_id"`
	Status          string         `db:"status"`
}

func registerAdministratorAccountCleanupRoutes(event *core.ServeEvent) {
	base := apiRoot + "/admin/accounts/{accountId}/cleanup"
	event.Router.POST(base, startAdministratorAccountCleanup)
	event.Router.GET(base+"/{jobId}", readAdministratorAccountCleanup)
	event.Router.PATCH(base+"/{jobId}/assignments", assignAdministratorAccountCleanupSuccessors)
	event.Router.POST(base+"/{jobId}/commit", commitAdministratorAccountCleanup)
	event.Router.POST(base+"/{jobId}/retry", retryAdministratorAccountCleanup)
}

func startAdministratorAccountCleanup(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	actor, _ := authenticatedUser(event)
	targetID := strings.TrimSpace(event.Request.PathValue("accountId"))
	input := administratorAccountCleanupStartInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "cleanup-bind")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	// What the request lacks is said as such, as an account's other changes
	// say it (administratorAccountRequestRefusal).
	if targetID == actor.Id {
		return administratorAccountMutationError(event, errAdministratorAccountSelfMutation, "cleanup-self")
	}
	if refused := administratorAccountRequestRefusal(targetID, input.RequestID, input.ExpectedRevision); refused != nil {
		return administratorAccountMutationError(event, refused, "cleanup-validate")
	}
	// A table that keeps rows by person and says nothing of deleting an
	// account refuses the deletion before it changes anything
	// (account_deletion_inventory.go).
	if err := validateAccountDeletionInventory(event.App); err != nil {
		return administratorAccountMutationError(event, err, "cleanup-inventory")
	}
	hash := administratorAccountCleanupRequestHash(targetID, input.ExpectedRevision)
	release := acquireProductOperationLock(administratorAccountLockNamespace, targetID)
	defer release()
	job := storedJob{}
	duplicate := false
	change := func(txApp core.App) error {
		existing, err := loadStoredJobByRequest(txApp, actor.Id, input.RequestID)
		if err == nil {
			if existing.Kind != administratorAccountCleanupJobKind || existing.PageID != targetID || existing.RequestHash != hash {
				return errAdministratorAccountIdentityReused
			}
			job, duplicate = existing, true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state, err := loadAdministratorAccountState(txApp, targetID)
		if err != nil {
			return err
		}
		if state.Revision != input.ExpectedRevision || state.Status == administratorAccountStatusDeleting || state.Status == administratorAccountStatusDeleted {
			return errAdministratorAccountRevision
		}
		if err := assertNoLegalHoldOnAccount(txApp, targetID); err != nil {
			return err
		}
		job.ID = security.RandomString(15)
		payload := administratorAccountCleanupPayload{
			SchemaVersion: administratorAccountCleanupPayloadFormat, TargetUserID: targetID,
			Phase:         administratorAccountCleanupPhaseInventory,
			InventoryType: administratorAccountResourceWorkspace, Revision: 1,
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := revokeAllBrowserSessionsCount(txApp, targetID); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`UPDATE {{leafen_administrator_account_states}}
			SET [[status]]='deleting',[[revision]]=[[revision]]+1,[[request_id]]={:requestId},[[request_hash]]={:requestHash},
				[[cleanup_job_id]]={:jobId},[[updated_by]]={:actorId},[[updated_at]]={:updated}
			WHERE [[user_id]]={:targetId} AND [[revision]]={:revision}`).Bind(dbx.Params{
			"requestId": input.RequestID, "requestHash": hash, "jobId": job.ID, "actorId": actor.Id,
			"updated": now, "targetId": targetID, "revision": input.ExpectedRevision,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAdministratorAccountRevision, err)
		}
		if _, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_jobs}}
			([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],[[status]],
			 [[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],[[payload_json]],
			 [[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],[[chunk_count]])
			VALUES ({:id},{:owner},'',{:requestId},{:requestHash},{:kind},{:targetId},'queued','','',{:notBefore},
			 {:now},{:now},{:payload},'',0,0,0,3,0)`).Bind(dbx.Params{
			"id": job.ID, "owner": actor.Id, "requestId": input.RequestID, "requestHash": hash,
			"kind": administratorAccountCleanupJobKind, "targetId": targetID,
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "now": now, "payload": string(encoded),
		}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(administratorAccountAuditTable, dbx.Params{
			"id": core.GenerateDefaultRandomId(), "actor_id": actor.Id, "target_user_id": targetID,
			"action": "delete-start", "from_status": state.Status, "to_status": administratorAccountStatusDeleting,
			"revision": input.ExpectedRevision + 1, "request_id": input.RequestID, "created_at": now,
			"target_email": deletionAuditEmail(txApp, targetID),
		}).Execute(); err != nil {
			return err
		}
		job, err = loadStoredJob(txApp, job.ID, actor.Id)
		return err
	}
	// A key's call asks its person only for a cleanup that would start
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		job, duplicate = storedJob{}, false
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-admission")
	}
	response, err := administratorAccountCleanupResponse(event.App, job, "")
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-response")
	}
	notifyAdministratorAccountCleanupRunner(event.App)
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readAdministratorAccountCleanup(event *core.RequestEvent) error {
	job, ok := authorizeAdministratorAccountCleanup(event)
	if !ok {
		return nil
	}
	response, err := administratorAccountCleanupResponse(event.App, job, strings.TrimSpace(event.Request.URL.Query().Get("cursor")))
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-read")
	}
	return event.JSON(http.StatusOK, response)
}

func assignAdministratorAccountCleanupSuccessors(event *core.RequestEvent) error {
	job, ok := authorizeAdministratorAccountCleanup(event)
	if !ok {
		return nil
	}
	input := administratorAccountCleanupAssignmentInput{}
	if err := event.BindBody(&input); err != nil || len(input.Assignments) == 0 || len(input.Assignments) > administratorAccountCleanupTurnLimit {
		return administratorAccountMutationError(event, errAdministratorAccountTransferInvalid, "cleanup-assignment-bind")
	}
	release := acquireProductOperationLock(administratorAccountLockNamespace, job.PageID)
	defer release()
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil {
			return err
		}
		payload, err := decodeAdministratorAccountCleanupPayload(current)
		if err != nil || current.Status != administratorAccountCleanupWaitingStatus ||
			payload.Phase != administratorAccountCleanupPhaseAssignments || payload.Revision != input.ExpectedRevision {
			return errAdministratorAccountRevision
		}
		for _, assignment := range input.Assignments {
			if err := validateAdministratorAccountCleanupSuccessor(txApp, payload.TargetUserID, assignment); err != nil {
				return err
			}
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_administrator_account_cleanup_assignments}}
				SET [[successor_user_id]]={:successor},[[status]]='assigned',[[updated_at]]={:updated}
				WHERE [[job_id]]={:jobId} AND [[resource_type]]={:resourceType} AND [[resource_id]]={:resourceId}
					AND [[status]] IN ('unassigned','assigned')`).Bind(dbx.Params{
				"successor": assignment.SuccessorUserID, "updated": time.Now().UTC().UnixMilli(), "jobId": current.ID,
				"resourceType": assignment.ResourceType, "resourceId": assignment.ResourceID,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil || changed != 1 {
				return refusedUnlessStorage(errAdministratorAccountTransferInvalid, err)
			}
		}
		return updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, administratorAccountCleanupWaitingStatus)
	})
	if err != nil {
		return administratorAccountCleanupError(event, err, "cleanup-assign")
	}
	current, _ := loadStoredJob(event.App, job.ID, job.Owner)
	response, err := administratorAccountCleanupResponse(event.App, current, "")
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-assignment-response")
	}
	return event.JSON(http.StatusOK, response)
}

func commitAdministratorAccountCleanup(event *core.RequestEvent) error {
	job, ok := authorizeAdministratorAccountCleanup(event)
	if !ok {
		return nil
	}
	input := administratorAccountCleanupCommitInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "cleanup-commit-bind")
	}
	release := acquireProductOperationLock(administratorAccountLockNamespace, job.PageID)
	defer release()
	change := func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil {
			return err
		}
		payload, err := decodeAdministratorAccountCleanupPayload(current)
		if err != nil || current.Status != administratorAccountCleanupWaitingStatus || payload.Revision != input.ExpectedRevision {
			return errAdministratorAccountRevision
		}
		var unassigned int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_administrator_account_cleanup_assignments}}
			WHERE [[job_id]]={:jobId} AND [[status]]='unassigned'`).Bind(dbx.Params{"jobId": current.ID}).Row(&unassigned); err != nil {
			return err
		}
		if unassigned != 0 {
			return errAdministratorAccountOwnershipRequired
		}
		matches, err := administratorAccountOwnershipMatches(txApp, current.ID, payload.TargetUserID)
		if err != nil {
			return err
		}
		if !matches {
			payload.Phase = administratorAccountCleanupPhaseInventory
			payload.InventoryType = administratorAccountResourceWorkspace
			payload.Cursor = ""
			return updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, durableJobStatusQueued)
		}
		payload.Phase = administratorAccountCleanupPhaseWorkspaces
		return updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, durableJobStatusQueued)
	}
	// A key's call asks its person only for a cleanup that would go on
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return administratorAccountCleanupError(event, err, "cleanup-commit")
	}
	current, _ := loadStoredJob(event.App, job.ID, job.Owner)
	response, err := administratorAccountCleanupResponse(event.App, current, "")
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-commit-response")
	}
	notifyAdministratorAccountCleanupRunner(event.App)
	return event.JSON(http.StatusAccepted, response)
}

func retryAdministratorAccountCleanup(event *core.RequestEvent) error {
	job, ok := authorizeAdministratorAccountCleanup(event)
	if !ok {
		return nil
	}
	input := administratorAccountCleanupCommitInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "cleanup-retry-bind")
	}
	if input.ExpectedRevision < 1 {
		return administratorAccountMutationError(event, errAdministratorCleanupRevisionMissing, "cleanup-retry-validate")
	}
	release := acquireProductOperationLock(administratorAccountLockNamespace, job.PageID)
	defer release()
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil {
			return err
		}
		payload, err := decodeAdministratorAccountCleanupPayload(current)
		if err != nil || payload.Revision != input.ExpectedRevision {
			return errAdministratorAccountRevision
		}
		if current.Status == durableJobStatusQueued {
			return nil
		}
		if current.Status != durableJobStatusFailed {
			return errAdministratorAccountRevision
		}
		nextStatus := durableJobStatusQueued
		nextPayload := current.PayloadJSON
		if payload.Phase == administratorAccountCleanupPhaseWorkspaces || payload.Phase == administratorAccountCleanupPhaseOrganization {
			nextStatus = administratorAccountCleanupWaitingStatus
			payload.Phase = administratorAccountCleanupPhaseAssignments
			payload.Cursor = ""
			payload.Revision++
			nextPayload, err = encodeAdministratorAccountCleanupPayload(payload)
			if err != nil {
				return err
			}
		}
		notBefore := time.Now().UTC().Add(durableJobReadyDelay).UnixMilli()
		if nextStatus == administratorAccountCleanupWaitingStatus {
			notBefore = 0
		}
		result, err := txApp.DB().NewQuery(`UPDATE {{leafen_jobs}}
			SET [[status]]={:status},[[error_code]]='',[[diagnostic_reference]]='',[[attempt]]=0,
				[[not_before]]={:notBefore},[[payload_json]]={:nextPayload},[[cursor]]='',[[updated]]={:updated}
			WHERE [[id]]={:id} AND [[owner]]={:owner} AND [[kind]]={:kind} AND [[status]]='failed'
				AND [[payload_json]]={:payload}`).Bind(dbx.Params{
			"status": nextStatus, "notBefore": notBefore, "nextPayload": nextPayload, "updated": time.Now().UTC().UnixMilli(),
			"id": current.ID, "owner": current.Owner, "kind": administratorAccountCleanupJobKind,
			"payload": current.PayloadJSON,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAdministratorAccountRevision, err)
		}
		return nil
	})
	if err != nil {
		return administratorAccountCleanupError(event, err, "cleanup-retry")
	}
	current, err := loadStoredJob(event.App, job.ID, job.Owner)
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-retry-response")
	}
	response, err := administratorAccountCleanupResponse(event.App, current, "")
	if err != nil {
		return administratorAccountMutationError(event, err, "cleanup-retry-response")
	}
	status := http.StatusOK
	if current.Status == durableJobStatusQueued {
		notifyAdministratorAccountCleanupRunner(event.App)
		status = http.StatusAccepted
	}
	return event.JSON(status, response)
}

func authorizeAdministratorAccountCleanup(event *core.RequestEvent) (storedJob, bool) {
	if !authorizeAdministratorRequest(event) {
		return storedJob{}, false
	}
	targetID := strings.TrimSpace(event.Request.PathValue("accountId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	job, err := loadStoredJob(event.App, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (job.Kind != administratorAccountCleanupJobKind || job.PageID != targetID)) {
		_ = apiError(event, http.StatusNotFound, errorCodeAdministratorAccountNotFound, "Account cleanup job not found.")
		return storedJob{}, false
	}
	if err != nil {
		_ = administratorAccountMutationError(event, err, "cleanup-authorize")
		return storedJob{}, false
	}
	return job, true
}

func administratorAccountCleanupRequestHash(targetID string, revision int) string {
	digest := sha256.Sum256([]byte(targetID + "\x00" + strconv.Itoa(revision)))
	return hex.EncodeToString(digest[:])
}

func decodeAdministratorAccountCleanupPayload(job storedJob) (administratorAccountCleanupPayload, error) {
	payload := administratorAccountCleanupPayload{}
	if job.Kind != administratorAccountCleanupJobKind || json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.SchemaVersion != administratorAccountCleanupPayloadFormat || payload.TargetUserID != job.PageID ||
		payload.Revision < 1 || payload.Phase == "" {
		return payload, errors.New(errorCodeAdministratorAccountCleanupFailed)
	}
	return payload, nil
}

func updateAdministratorAccountCleanupPayload(app core.App, job storedJob, payload administratorAccountCleanupPayload, revision int, status string) error {
	payload.Revision = revision
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[payload_json]]={:payload},[[cursor]]={:cursor},
		[[status]]={:status},[[not_before]]={:notBefore},[[updated]]={:updated},[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]={:previousStatus}
			AND [[payload_json]]={:previousPayload}`).Bind(dbx.Params{
		"payload": string(encoded), "cursor": payload.Cursor, "status": status,
		"notBefore": time.Now().UTC().Add(durableJobReadyDelay).UnixMilli(), "updated": time.Now().UTC().UnixMilli(),
		"id": job.ID, "kind": administratorAccountCleanupJobKind,
		"previousStatus": job.Status, "previousPayload": job.PayloadJSON,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return refusedUnlessStorage(errAdministratorAccountRevision, err)
	}
	return nil
}

func notifyAdministratorAccountCleanupRunner(app core.App) {
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
}

func administratorAccountCleanupError(event *core.RequestEvent, err error, stage string) error {
	if errors.Is(err, errAdministratorAccountOwnershipRequired) {
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountOwnershipRequired, "Choose a successor for every owned resource.")
	}
	if errors.Is(err, errAdministratorAccountTransferInvalid) {
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountTransferInvalid, "Check the ownership transfer target again.")
	}
	return administratorAccountMutationError(event, err, stage)
}

// The runner owns inventoryAdministratorAccountOwnership and calls the shared
// transferWorkspaceOwnershipInTransaction / transferOrganizationOwnershipInTransaction
// primitives before revokeAllBrowserSessionsCount and the deleted tombstone.
