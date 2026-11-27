package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedWorkspaceDeletionJob struct {
	ID              string `db:"id"`
	ActorID         string `db:"actor_id"`
	OwnerID         string `db:"owner_id"`
	WorkspaceID     string `db:"workspace_id"`
	OrganizationID  string `db:"organization_id"`
	WorkspaceName   string `db:"workspace_name"`
	RequestID       string `db:"request_id"`
	RequestHash     string `db:"request_hash"`
	Status          string `db:"status"`
	Phase           string `db:"phase"`
	Revision        int    `db:"revision"`
	PageID          string `db:"page_id"`
	PageCleanupLane int    `db:"page_cleanup_lane"`
	ResidueLane     int    `db:"residue_lane"`
	PagesCount      int    `db:"pages_count"`
	FilesCount      int    `db:"files_count"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
	CompletedAt     int64  `db:"completed_at"`
}

type workspaceDeletionAdmission struct {
	Job            storedWorkspaceDeletionJob
	Admitted       bool
	Duplicate      bool
	AlreadyDeleted bool
}

func admitWorkspaceDeletion(
	app core.App,
	actorID, workspaceID, requestID, confirmationName string,
) (workspaceDeletionAdmission, error) {
	result := workspaceDeletionAdmission{}
	requestHash := workspaceDeletionRequestHash(workspaceID, confirmationName)
	err := app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadWorkspaceDeletionByRequest(txApp, actorID, requestID)
		if err == nil {
			if existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
				return errWorkspaceDeleteRequestIDReused
			}
			if existing.Status == workspaceDeletionStatusCompleted {
				result = workspaceDeletionAdmission{Job: existing, Duplicate: true}
				return nil
			}
			if err := reauthorizeWorkspaceDeletion(txApp, actorID, existing); err != nil {
				return err
			}
			result = workspaceDeletionAdmission{Job: existing}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		existing, err = loadWorkspaceDeletionByTarget(txApp, actorID, workspaceID)
		if err == nil {
			if existing.Status != workspaceDeletionStatusCompleted {
				return errWorkspaceDeleteConflict
			}
			if existing.WorkspaceName != confirmationName {
				return errWorkspaceDeleteConfirmationMismatch
			}
			result = workspaceDeletionAdmission{Job: existing, AlreadyDeleted: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		authorization, err := authorizeWorkspace(
			txApp, actorID, workspaceID, permissionWorkspaceOwner,
		)
		if err != nil {
			return err
		}
		if authorization.Workspace.GetString("name") != confirmationName {
			return errWorkspaceDeleteConfirmationMismatch
		}
		if err := assertNoActiveLegalHoldForPermanentDelete(
			txApp, authorization.Organization.OrganizationID,
			legalHoldDeletionTarget{WorkspaceID: workspaceID},
		); err != nil {
			return err
		}
		if err := preflightWorkspaceDeletion(txApp, workspaceID); err != nil {
			return err
		}

		now := time.Now().UTC().UnixMilli()
		jobID := workspaceDeletionJobID(actorID, requestID)
		job := storedWorkspaceDeletionJob{
			ID: jobID, ActorID: actorID, OwnerID: authorization.OwnerID,
			WorkspaceID:    workspaceID,
			OrganizationID: authorization.Organization.OrganizationID,
			WorkspaceName:  confirmationName, RequestID: requestID,
			RequestHash: requestHash, Status: workspaceDeletionStatusPending,
			Phase: workspaceDeletionPhasePages, Revision: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		_, err = txApp.DB().Insert(workspaceDeletionJobsTable, dbx.Params{
			"id": job.ID, "actor_id": job.ActorID, "owner_id": job.OwnerID,
			"workspace_id": job.WorkspaceID, "organization_id": job.OrganizationID,
			"workspace_name": job.WorkspaceName, "request_id": job.RequestID,
			"request_hash": job.RequestHash, "status": job.Status, "phase": job.Phase,
			"revision": job.Revision, "page_id": "", "page_cleanup_lane": 0,
			"residue_lane": 0, "pages_count": 0, "files_count": 0,
			"created_at": now, "updated_at": now, "completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		authorization.Workspace.Set("deletion_job_id", job.ID)
		if err := txApp.Save(authorization.Workspace); err != nil {
			return err
		}
		result = workspaceDeletionAdmission{Job: job, Admitted: true}
		return nil
	})
	return result, err
}

func preflightWorkspaceDeletion(app core.App, workspaceID string) error {
	// A table kept by workspace that the inventory doesn't name refuses the
	// deletion before it changes anything, rather than leaving that table's
	// rows behind (deletion_inventories_start.go).
	if err := validateWorkspaceDeletionDirectAuthorityInventory(app); err != nil {
		return err
	}
	pendingPageDelete, err := pendingPagePermanentDeleteForWorkspace(app, workspaceID)
	if err != nil {
		return err
	}
	if pendingPageDelete {
		return errWorkspaceDeleteConflict
	}
	var active int
	if err := app.DB().NewQuery(`
		SELECT
			EXISTS(SELECT 1 FROM {{leafen_jobs}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'queued' LIMIT 1)
			+ EXISTS(SELECT 1 FROM {{leafen_file_uploads}}
				WHERE [[workspace_id]] = {:workspaceId}
					AND [[status]] IN ('pending','settling','cleanup_pending') LIMIT 1)
			+ EXISTS(SELECT 1 FROM {{leafen_file_delete_receipts}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'pending' LIMIT 1)
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&active); err != nil {
		return err
	}
	if active != 0 {
		return errWorkspaceDeleteConflict
	}
	return nil
}

func reauthorizeWorkspaceDeletion(
	app core.App,
	actorID string,
	job storedWorkspaceDeletionJob,
) error {
	authorization, err := authorizeWorkspaceDeletion(app, actorID, job.WorkspaceID, job.ID)
	if err != nil {
		return err
	}
	if authorization.OwnerID != job.OwnerID ||
		authorization.Organization.OrganizationID != job.OrganizationID {
		return permissionDenied()
	}
	return nil
}

func loadWorkspaceDeletionByRequest(
	app core.App,
	actorID, requestID string,
) (storedWorkspaceDeletionJob, error) {
	return loadWorkspaceDeletionJob(app,
		`[[actor_id]] = {:actorId} AND [[request_id]] = {:requestId}`,
		dbx.Params{"actorId": actorID, "requestId": requestID})
}

func loadWorkspaceDeletionByTarget(
	app core.App,
	actorID, workspaceID string,
) (storedWorkspaceDeletionJob, error) {
	return loadWorkspaceDeletionJob(app,
		`[[actor_id]] = {:actorId} AND [[workspace_id]] = {:workspaceId}
		 ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT 1`,
		dbx.Params{"actorId": actorID, "workspaceId": workspaceID})
}

// loadDeletingWorkspaces lists the deletions a person started that haven't
// finished, for the app to carry on with when it opens: a deleting workspace
// is no longer theirs to open, so it isn't in their workspace list.
func loadDeletingWorkspaces(app core.App, actorID string) ([]deletingWorkspaceResponse, error) {
	rows := []struct {
		WorkspaceID string `db:"workspace_id"`
		Name        string `db:"workspace_name"`
		RequestID   string `db:"request_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]], [[workspace_name]], [[request_id]]
		FROM {{leafen_workspace_deletion_jobs}}
		WHERE [[actor_id]] = {:actorId} AND [[status]] = 'pending'
		ORDER BY [[created_at]], [[id]]
		LIMIT 50`).Bind(dbx.Params{"actorId": actorID}).All(&rows)
	deleting := make([]deletingWorkspaceResponse, 0, len(rows))
	for _, row := range rows {
		deleting = append(deleting, deletingWorkspaceResponse{WorkspaceID: row.WorkspaceID, Name: row.Name, RequestID: row.RequestID})
	}
	return deleting, err
}

func loadWorkspaceDeletionByID(
	app core.App,
	jobID string,
) (storedWorkspaceDeletionJob, error) {
	return loadWorkspaceDeletionJob(app, `[[id]] = {:jobId}`, dbx.Params{"jobId": jobID})
}

func loadWorkspaceDeletionJob(
	app core.App,
	where string,
	params dbx.Params,
) (storedWorkspaceDeletionJob, error) {
	job := storedWorkspaceDeletionJob{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[actor_id]], [[owner_id]], [[workspace_id]], [[organization_id]],
			[[workspace_name]], [[request_id]], [[request_hash]], [[status]], [[phase]],
			[[revision]], [[page_id]], [[page_cleanup_lane]], [[residue_lane]],
			[[pages_count]], [[files_count]], [[created_at]], [[updated_at]], [[completed_at]]
		FROM {{leafen_workspace_deletion_jobs}} WHERE ` + where + `
	`).Bind(params).One(&job)
	return job, err
}

func workspaceDeletionResponseForJob(
	job storedWorkspaceDeletionJob,
	duplicate, alreadyDeleted bool,
) workspaceDeletionResponse {
	return workspaceDeletionResponse{
		JobID: job.ID, WorkspaceID: job.WorkspaceID, Status: job.Status, Phase: job.Phase,
		Progress: workspaceDeletionProgress{
			Revision: job.Revision, Pages: job.PagesCount,
			Files: job.FilesCount, ResidueLane: job.ResidueLane,
		},
		Duplicate: duplicate, AlreadyDeleted: alreadyDeleted,
	}
}

func workspaceDeletionRequestHash(workspaceID, confirmationName string) string {
	digest := sha256.Sum256([]byte("workspace-delete\x00" + workspaceID + "\x00" + confirmationName))
	return hex.EncodeToString(digest[:])
}

func workspaceDeletionJobID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte(actorID + "\x00workspace-delete\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}
