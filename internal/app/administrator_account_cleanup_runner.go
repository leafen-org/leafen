package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type administratorOwnedResource struct {
	ID   string `db:"id"`
	Name string `db:"name"`
}

func (r *durableJobRunner) advanceAdministratorAccountCleanup(ctx context.Context, jobID string) error {
	job, err := loadStoredJob(r.app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	payload, err := decodeAdministratorAccountCleanupPayload(job)
	if err != nil {
		return err
	}
	release := acquireProductOperationLock(administratorAccountLockNamespace, payload.TargetUserID)
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if payload.Phase != administratorAccountCleanupPhaseInventory {
		// A turn that passed on a workspace or took away the person's
		// memberships has the workspace lists brought up to it at once.
		defer notifyBootstrapVisibilityProjectionAfterCommit(r.app)
	}
	switch payload.Phase {
	case administratorAccountCleanupPhaseInventory:
		return inventoryAdministratorAccountOwnership(r.app, job, payload)
	case administratorAccountCleanupPhaseWorkspaces:
		if payload.SelfDeletion {
			return r.deleteAccountWorkspace(ctx, job, payload)
		}
		return r.transferAdministratorAccountWorkspace(ctx, job, payload)
	case administratorAccountCleanupPhaseOrganization:
		return transferAdministratorAccountOrganization(r.app, job, payload)
	case administratorAccountCleanupPhaseAccount:
		// A table that keeps rows by person and says nothing of deleting an
		// account holds the cleanup before it deletes, rather than leaving
		// that table's rows behind (account_deletion_inventory.go).
		if err := validateAccountDeletionInventory(r.app); err != nil {
			return r.deferAdministratorAccountCleanup(ctx, job, err)
		}
		return cleanupAdministratorAccountData(r.app, job, payload)
	case administratorAccountCleanupPhaseCompleted:
		return nil
	default:
		return errors.New(errorCodeAdministratorAccountCleanupFailed)
	}
}

func inventoryAdministratorAccountOwnership(app core.App, job storedJob, payload administratorAccountCleanupPayload) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeAdministratorAccountCleanupPayload(current)
		if err != nil || payload.Phase != administratorAccountCleanupPhaseInventory {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_administrator_account_cleanup_assignments}}
			AS [[assignment]] WHERE [[assignment]].[[job_id]]={:jobId} AND [[assignment]].[[status]]<>'completed' AND NOT EXISTS (
				SELECT 1 FROM {{leafen_workspaces}} [[workspace]] WHERE [[assignment]].[[resource_type]]='workspace'
					AND [[workspace]].[[id]]=[[assignment]].[[resource_id]] AND [[workspace]].[[workspace_owner]]={:targetId}
				UNION ALL SELECT 1 FROM {{leafen_organizations}} [[organization]] WHERE [[assignment]].[[resource_type]]='organization'
					AND [[organization]].[[id]]=[[assignment]].[[resource_id]] AND [[organization]].[[owner]]={:targetId}
			)`).Bind(dbx.Params{"jobId": current.ID, "targetId": payload.TargetUserID}).Execute(); err != nil {
			return err
		}
		resources := []administratorOwnedResource{}
		table, ownerKey := "leafen_workspaces", "workspace_owner"
		if payload.InventoryType == administratorAccountResourceOrganization {
			table, ownerKey = "leafen_organizations", "owner"
		}
		query := `SELECT [[id]],[[name]] FROM {{` + table + `}} WHERE [[` + ownerKey + `]]={:targetId}
			AND [[id]]>{:cursor} ORDER BY [[id]] LIMIT {:limit}`
		if err := txApp.DB().NewQuery(query).Bind(dbx.Params{
			"targetId": payload.TargetUserID, "cursor": payload.Cursor,
			"limit": administratorAccountCleanupTurnLimit,
		}).All(&resources); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		for _, resource := range resources {
			if _, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_administrator_account_cleanup_assignments}}
				([[job_id]],[[resource_type]],[[resource_id]],[[resource_name]],[[successor_user_id]],[[status]],[[updated_at]])
				VALUES ({:jobId},{:resourceType},{:resourceId},{:resourceName},NULL,'unassigned',{:updated})
				ON CONFLICT ([[job_id]],[[resource_type]],[[resource_id]]) DO NOTHING`).Bind(dbx.Params{
				"jobId": current.ID, "resourceType": payload.InventoryType, "resourceId": resource.ID,
				"resourceName": resource.Name, "updated": now,
			}).Execute(); err != nil {
				return err
			}
		}
		if len(resources) == administratorAccountCleanupTurnLimit {
			payload.Cursor = resources[len(resources)-1].ID
		} else if payload.InventoryType == administratorAccountResourceWorkspace {
			payload.InventoryType = administratorAccountResourceOrganization
			payload.Cursor = ""
		} else {
			var total int
			if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_administrator_account_cleanup_assignments}}
				WHERE [[job_id]]={:jobId}`).Bind(dbx.Params{"jobId": current.ID}).Row(&total); err != nil {
				return err
			}
			cleanupTotal, err := countAdministratorAccountCleanupItems(txApp, payload.TargetUserID)
			if err != nil {
				return err
			}
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[total_items]]={:total}
				WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[payload_json]]={:payload}`).Bind(dbx.Params{
				"total": total + cleanupTotal, "id": current.ID,
				"kind": administratorAccountCleanupJobKind, "payload": current.PayloadJSON,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil || changed != 1 {
				return refusedUnlessStorage(errAdministratorAccountRevision, err)
			}
			if total > 0 {
				payload.Phase = administratorAccountCleanupPhaseAssignments
				return updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, administratorAccountCleanupWaitingStatus)
			}
			payload.Phase = administratorAccountCleanupPhaseAccount
		}
		return updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, durableJobStatusQueued)
	})
}

func (r *durableJobRunner) transferAdministratorAccountWorkspace(ctx context.Context, job storedJob, payload administratorAccountCleanupPayload) error {
	assignment, found, err := nextAdministratorAccountTransfer(r.app, job.ID, administratorAccountResourceWorkspace)
	if err != nil {
		return err
	}
	if !found {
		return advanceAdministratorAccountCleanupPhase(r.app, job, payload, administratorAccountCleanupPhaseOrganization)
	}
	if r.rooms == nil {
		return r.deferAdministratorAccountCleanup(ctx, job, errors.New("workspace room invalidator is unavailable"))
	}
	if err := r.rooms.InvalidateWorkspace(assignment.ResourceID); err != nil {
		return r.deferAdministratorAccountCleanup(ctx, job, err)
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if err := validateAdministratorAccountCleanupSuccessor(txApp, payload.TargetUserID, administratorAccountTransferSelection{
			ResourceType: administratorAccountResourceWorkspace, ResourceID: assignment.ResourceID,
			SuccessorUserID: assignment.SuccessorUserID.String,
		}); err != nil {
			return err
		}
		if _, err := transferWorkspaceOwnershipInTransaction(txApp, administratorWorkspaceOwnershipTransfer{
			ActorID: job.Owner, PreviousOwnerID: payload.TargetUserID,
			OwnerID: assignment.SuccessorUserID.String, WorkspaceID: assignment.ResourceID,
			RequestID: job.RequestID + ":" + assignment.ResourceID, AdmitSuccessor: true,
		}); err != nil {
			return err
		}
		return completeAdministratorAccountTransfer(txApp, current, payload, assignment)
	})
}

func transferAdministratorAccountOrganization(app core.App, job storedJob, payload administratorAccountCleanupPayload) error {
	assignment, found, err := nextAdministratorAccountTransfer(app, job.ID, administratorAccountResourceOrganization)
	if err != nil {
		return err
	}
	if !found {
		return advanceAdministratorAccountCleanupPhase(app, job, payload, administratorAccountCleanupPhaseAccount)
	}
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if err := transferOrganizationOwnershipInTransaction(txApp, administratorOrganizationOwnershipTransfer{
			ActorID: job.Owner, PreviousOwnerID: payload.TargetUserID,
			OwnerID: assignment.SuccessorUserID.String, OrganizationID: assignment.ResourceID,
		}); err != nil {
			return err
		}
		return completeAdministratorAccountTransfer(txApp, current, payload, assignment)
	})
}

func cleanupAdministratorAccountData(app core.App, job storedJob, payload administratorAccountCleanupPayload) error {
	// The last step gives the account a password no one knows, hashed before
	// the transaction (password_outside_transaction.go).
	unknown := preparedPassword{plain: security.RandomString(48)}
	if payload.CleanupIndex >= len(administratorAccountCleanupTables) {
		unknown = preparePassword(app, "users", unknown.plain)
	}
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if payload.CleanupIndex < len(administratorAccountCleanupTables) {
			item := administratorAccountCleanupTables[payload.CleanupIndex]
			if item.Table == organizationMembershipsCollection {
				if err := recordAccountDeletionAudit(txApp, current.ID, payload.TargetUserID); err != nil {
					return err
				}
			}
			query := `DELETE FROM {{` + item.Table + `}} WHERE [[rowid]] IN (` +
				administratorAccountCleanupLaneRows(item) + ` LIMIT {:limit})`
			result, err := txApp.DB().NewQuery(query).Bind(dbx.Params{
				"targetId": payload.TargetUserID, "limit": administratorAccountCleanupTurnLimit,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if changed < int64(administratorAccountCleanupTurnLimit) {
				payload.CleanupIndex++
			}
			if err := updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, durableJobStatusQueued); err != nil {
				return err
			}
			result, err = txApp.DB().NewQuery(`UPDATE {{leafen_jobs}}
				SET [[processed_items]]=[[processed_items]]+{:changed}
				WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued'`).Bind(dbx.Params{
				"changed": changed, "id": current.ID, "kind": administratorAccountCleanupJobKind,
			}).Execute()
			if err != nil {
				return err
			}
			updated, err := result.RowsAffected()
			if err != nil || updated != 1 {
				return refusedUnlessStorage(errAdministratorAccountRevision, err)
			}
			return nil
		}
		if _, err := revokeAllBrowserSessionsCount(txApp, payload.TargetUserID); err != nil {
			return err
		}
		user, err := txApp.FindRecordById("users", payload.TargetUserID)
		if err != nil {
			return err
		}
		for _, item := range administratorAccountLastStepTables {
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{` + item.Table + `}} WHERE [[` + item.Key + `]] = {:userId}`).
				Bind(dbx.Params{"userId": user.Id}).Execute(); err != nil {
				return err
			}
		}
		user.SetEmail(deletedAccountEmail(payload.TargetUserID))
		unknown.set(user)
		// No name is kept; readers see their own fallback for a person
		// who is no longer here.
		user.Set("name", "")
		user.Set("profile_avatar", "")
		user.SetVerified(false)
		if err := txApp.Save(user); err != nil {
			return err
		}
		// The empty organization someone who deleted their own account leaves
		// keeps no name they gave it.
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_organizations}} SET [[name]]={:name} WHERE [[owner]]={:targetId}`).Bind(dbx.Params{
			"name": deletedAccountOrganizationName, "targetId": payload.TargetUserID,
		}).Execute(); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		stateResult, err := txApp.DB().NewQuery(`UPDATE {{leafen_administrator_account_states}}
			SET [[status]]='deleted',[[revision]]=[[revision]]+1,[[updated_by]]={:actorId},[[updated_at]]={:updated}
			WHERE [[user_id]]={:targetId} AND [[status]]='deleting' AND [[cleanup_job_id]]={:jobId}`).Bind(dbx.Params{
			"actorId": job.Owner, "updated": now, "targetId": payload.TargetUserID, "jobId": job.ID,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := stateResult.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAdministratorAccountRevision, err)
		}
		payload.Phase = administratorAccountCleanupPhaseCompleted
		payload.Revision++
		encoded, err := encodeAdministratorAccountCleanupPayload(payload)
		if err != nil {
			return err
		}
		jobResult, err := txApp.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[status]]='completed',[[payload_json]]={:payload},
			[[processed_items]]=[[total_items]],[[not_before]]=0,[[updated]]={:updated},[[chunk_count]]=[[chunk_count]]+1
			WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued' AND [[payload_json]]={:previousPayload}`).Bind(dbx.Params{
			"payload": encoded, "updated": now, "id": current.ID, "kind": administratorAccountCleanupJobKind,
			"previousPayload": current.PayloadJSON,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err = jobResult.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAdministratorAccountRevision, err)
		}
		return nil
	})
}

// countAdministratorAccountCleanupItems counts the person's rows the lanes
// will delete. A lane whose table a later migration makes counts nothing
// when an earlier one admits a deletion (the retired agents' accounts,
// migration 664); the runner takes the lanes after every migration.
func countAdministratorAccountCleanupItems(app core.App, targetUserID string) (int, error) {
	tables, err := schemaTableNames(app)
	if err != nil {
		return 0, err
	}
	present := make(map[string]bool, len(tables))
	for _, table := range tables {
		present[table] = true
	}
	query := strings.Builder{}
	query.WriteString("SELECT 1")
	for _, item := range administratorAccountCleanupTables {
		if !present[item.Table] {
			continue
		}
		query.WriteString(" + (SELECT COUNT(*) FROM (")
		query.WriteString(administratorAccountCleanupLaneRows(item))
		query.WriteString("))")
	}
	var total int
	err = app.DB().NewQuery(query.String()).Bind(dbx.Params{"targetId": targetUserID}).Row(&total)
	return total, err
}

// administratorAccountCleanupLaneRows finds a lane's rows of the person by
// rowid: a workspace at a time where the table's index leads with its
// workspace and then the person.
func administratorAccountCleanupLaneRows(item administratorAccountCleanupTable) string {
	if item.Workspace != "" {
		return `SELECT [[lane]].[[rowid]] FROM {{leafen_workspaces}} [[lane_workspace]] CROSS JOIN {{` + item.Table + `}} [[lane]]
			WHERE [[lane]].[[` + item.Workspace + `]] = [[lane_workspace]].[[id]] AND [[lane]].[[` + item.Key + `]] = {:targetId}`
	}
	rows := `SELECT [[rowid]] FROM {{` + item.Table + `}} WHERE [[` + item.Key + `]] = {:targetId}`
	if item.Where != "" {
		rows += ` AND ` + item.Where
	}
	return rows
}

func nextAdministratorAccountTransfer(app core.App, jobID, resourceType string) (storedAdministratorAccountTransferAssignment, bool, error) {
	row := storedAdministratorAccountTransferAssignment{}
	err := app.DB().NewQuery(`SELECT [[resource_type]],[[resource_id]],[[resource_name]],[[successor_user_id]],[[status]]
		FROM {{leafen_administrator_account_cleanup_assignments}}
		WHERE [[job_id]]={:jobId} AND [[resource_type]]={:resourceType} AND [[status]]='assigned'
		ORDER BY [[resource_id]] LIMIT 1`).Bind(dbx.Params{"jobId": jobID, "resourceType": resourceType}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

func completeAdministratorAccountTransfer(app core.App, job storedJob, payload administratorAccountCleanupPayload, assignment storedAdministratorAccountTransferAssignment) error {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_administrator_account_cleanup_assignments}}
		SET [[status]]='completed',[[updated_at]]={:updated}
		WHERE [[job_id]]={:jobId} AND [[resource_type]]={:resourceType} AND [[resource_id]]={:resourceId} AND [[status]]='assigned'`).Bind(dbx.Params{
		"updated": time.Now().UTC().UnixMilli(), "jobId": job.ID,
		"resourceType": assignment.ResourceType, "resourceId": assignment.ResourceID,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return refusedUnlessStorage(errAdministratorAccountRevision, err)
	}
	payload.Revision++
	if err := updateAdministratorAccountCleanupPayload(app, job, payload, payload.Revision, durableJobStatusQueued); err != nil {
		return err
	}
	progress, err := app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[processed_items]]=[[processed_items]]+1
			WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued'`).Bind(dbx.Params{
		"id": job.ID, "kind": administratorAccountCleanupJobKind,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err = progress.RowsAffected()
	if err != nil || changed != 1 {
		return refusedUnlessStorage(errAdministratorAccountRevision, err)
	}
	return nil
}

func advanceAdministratorAccountCleanupPhase(app core.App, job storedJob, payload administratorAccountCleanupPayload, phase string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeAdministratorAccountCleanupPayload(current)
		if err != nil {
			return err
		}
		if phase == administratorAccountCleanupPhaseAccount {
			remaining, err := countAdministratorAccountCleanupItems(txApp, payload.TargetUserID)
			if err != nil {
				return err
			}
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[total_items]]=[[processed_items]]+{:remaining}
				WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued' AND [[payload_json]]={:payload}`).Bind(dbx.Params{
				"remaining": remaining, "id": current.ID, "kind": administratorAccountCleanupJobKind,
				"payload": current.PayloadJSON,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil || changed != 1 {
				return refusedUnlessStorage(errAdministratorAccountRevision, err)
			}
		}
		payload.Phase, payload.Cursor = phase, ""
		return updateAdministratorAccountCleanupPayload(txApp, current, payload, payload.Revision+1, durableJobStatusQueued)
	})
}

func (r *durableJobRunner) deferAdministratorAccountCleanup(ctx context.Context, job storedJob, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := r.clock.now()
	attempt := job.Attempt + 1
	status, notBefore := durableJobStatusQueued, now.Add(time.Duration(attempt)*time.Second).UnixMilli()
	if attempt >= job.MaxAttempts {
		status, notBefore = durableJobStatusFailed, int64(0)
	}
	// The job keeps the reference; the log line it finds says why.
	reference := newDiagnosticReference()
	logDurableJobDiagnostic(r.app, errorCodeAdministratorAccountCleanupFailed, reference, job.ID, administratorAccountCleanupJobKind, status, cause)
	result, err := r.app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[attempt]]={:attempt},[[status]]={:status},
		[[not_before]]={:notBefore},[[error_code]]={:errorCode},[[diagnostic_reference]]={:reference},[[updated]]={:updated}
		WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued'`).WithContext(ctx).Bind(dbx.Params{
		"attempt": attempt, "status": status, "notBefore": notBefore, "errorCode": errorCodeAdministratorAccountCleanupFailed,
		"reference": reference, "updated": now.UnixMilli(), "id": job.ID,
		"kind": administratorAccountCleanupJobKind,
	}).Execute()
	if err != nil {
		return errors.Join(cause, err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return errors.Join(cause, errAdministratorAccountRevision, err)
	}
	return nil
}

func encodeAdministratorAccountCleanupPayload(payload administratorAccountCleanupPayload) (string, error) {
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}
