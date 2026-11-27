package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedPageTrashEmptyJob struct {
	ID                  string `db:"id"`
	ActorID             string `db:"actor_id"`
	OwnerID             string `db:"owner_id"`
	WorkspaceID         string `db:"workspace_id"`
	OrganizationID      string `db:"organization_id"`
	RequestID           string `db:"request_id"`
	RequestHash         string `db:"request_hash"`
	Status              string `db:"status"`
	Phase               string `db:"phase"`
	Revision            int    `db:"revision"`
	SnapshotCreated     string `db:"snapshot_created"`
	SnapshotPageID      string `db:"snapshot_page_id"`
	DiscoveryCursor     string `db:"discovery_cursor"`
	DiscoveryPageID     string `db:"discovery_page_id"`
	DiscoveryAncestorID string `db:"discovery_ancestor_id"`
	CountsJSON          string `db:"counts_json"`
	CreatedAt           int64  `db:"created_at"`
	UpdatedAt           int64  `db:"updated_at"`
	CompletedAt         int64  `db:"completed_at"`
}

type storedPageTrashEmptyItem struct {
	JobID          string `db:"job_id"`
	PageID         string `db:"page_id"`
	ChildRequestID string `db:"child_request_id"`
	ChildJobID     string `db:"child_job_id"`
	Status         string `db:"status"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
}

type pageTrashEmptyAdmission struct {
	Job       storedPageTrashEmptyJob
	Admitted  bool
	Duplicate bool
}

type pageTrashEmptyProgressCounts struct {
	Discovered int `db:"discovered"`
	Admitted   int `db:"admitted"`
	Completed  int `db:"completed"`
}

type pageTrashEmptySnapshot struct {
	PageID  string `db:"page_id"`
	Created string `db:"created"`
}

func admitPageTrashEmpty(
	app core.App,
	actorID, workspaceID, requestID string,
) (pageTrashEmptyAdmission, error) {
	result := pageTrashEmptyAdmission{}
	requestHash := pageTrashEmptyRequestHash(workspaceID)
	err := app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadPageTrashEmptyByRequest(txApp, actorID, requestID)
		if err == nil {
			if existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
				return errTrashEmptyRequestIDReused
			}
			if err := reauthorizePageTrashEmpty(txApp, actorID, existing); err != nil {
				return err
			}
			result = pageTrashEmptyAdmission{
				Job: existing, Duplicate: existing.Status == trashEmptyStatusCompleted,
			}
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

		pending, err := loadPendingPageTrashEmptyByWorkspace(txApp, workspaceID)
		if err == nil {
			if pending.ActorID == actorID && pending.RequestID == requestID {
				result = pageTrashEmptyAdmission{Job: pending}
				return nil
			}
			return errTrashEmptyConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		snapshot := pageTrashEmptySnapshot{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]] AS [[page_id]], [[created]] FROM {{leafen_pages}}
				INDEXED BY [[idx_leafen_pages_export_window]]
			WHERE [[workspace]] = {:workspaceId} AND [[owner]] = {:ownerId}
				AND [[notion_import_staging]] = FALSE
			ORDER BY [[created]] DESC, [[id]] DESC LIMIT 1
		`).Bind(dbx.Params{
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
		}).One(&snapshot); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		job := storedPageTrashEmptyJob{
			ID: pageTrashEmptyJobID(actorID, requestID), ActorID: actorID,
			OwnerID: authorization.OwnerID, WorkspaceID: authorization.WorkspaceID,
			OrganizationID: authorization.Organization.OrganizationID,
			RequestID:      requestID, RequestHash: requestHash,
			Status: trashEmptyStatusPending, Phase: trashEmptyPhaseDiscovering,
			Revision: 1, CountsJSON: `{"roots":0}`, CreatedAt: now, UpdatedAt: now,
		}
		_, err = txApp.DB().Insert(pageTrashEmptyJobsTable, dbx.Params{
			"id": job.ID, "actor_id": job.ActorID, "owner_id": job.OwnerID,
			"workspace_id": job.WorkspaceID, "organization_id": job.OrganizationID,
			"request_id": job.RequestID, "request_hash": job.RequestHash,
			"status": job.Status, "phase": job.Phase, "revision": job.Revision,
			"snapshot_created": snapshot.Created, "snapshot_page_id": snapshot.PageID,
			"discovery_cursor": "", "discovery_page_id": "", "discovery_ancestor_id": "",
			"counts_json": job.CountsJSON,
			"created_at":  now, "updated_at": now, "completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		// Rows in trash leave with the pages: their retention ends now and
		// the trash retention runner deletes them.
		if err := expireDatabaseRowTrash(txApp, authorization.WorkspaceID, now); err != nil {
			return err
		}
		result = pageTrashEmptyAdmission{Job: job, Admitted: true}
		return nil
	})
	if err != nil && isSQLiteConstraintError(err) {
		if _, pendingErr := loadPendingPageTrashEmptyByWorkspace(app, workspaceID); pendingErr == nil {
			return pageTrashEmptyAdmission{}, errTrashEmptyConflict
		}
	}
	return result, err
}

func reauthorizePageTrashEmpty(app core.App, actorID string, job storedPageTrashEmptyJob) error {
	authorization, err := authorizeWorkspace(app, actorID, job.WorkspaceID, permissionWorkspaceOwner)
	if err != nil {
		return err
	}
	if authorization.OwnerID != job.OwnerID ||
		authorization.Organization.OrganizationID != job.OrganizationID {
		return permissionDenied()
	}
	return nil
}

func loadPageTrashEmptyByRequest(
	app core.App,
	actorID, requestID string,
) (storedPageTrashEmptyJob, error) {
	return loadPageTrashEmptyJob(app,
		`[[actor_id]] = {:actorId} AND [[request_id]] = {:requestId}`,
		dbx.Params{"actorId": actorID, "requestId": requestID})
}

func loadPendingPageTrashEmptyByWorkspace(
	app core.App,
	workspaceID string,
) (storedPageTrashEmptyJob, error) {
	return loadPageTrashEmptyJob(app,
		`[[workspace_id]] = {:workspaceId} AND [[status]] = {:status} LIMIT 1`,
		dbx.Params{"workspaceId": workspaceID, "status": trashEmptyStatusPending})
}

func loadPageTrashEmptyJobByID(app core.App, jobID string) (storedPageTrashEmptyJob, error) {
	return loadPageTrashEmptyJob(app, `[[id]] = {:jobId}`, dbx.Params{"jobId": jobID})
}

func loadPageTrashEmptyJob(
	app core.App,
	where string,
	params dbx.Params,
) (storedPageTrashEmptyJob, error) {
	job := storedPageTrashEmptyJob{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[actor_id]], [[owner_id]], [[workspace_id]], [[organization_id]],
			[[request_id]], [[request_hash]], [[status]], [[phase]], [[revision]],
			[[snapshot_created]], [[snapshot_page_id]],
			[[discovery_cursor]], [[discovery_page_id]], [[discovery_ancestor_id]],
			[[counts_json]], [[created_at]], [[updated_at]], [[completed_at]]
		FROM {{leafen_trash_empty_jobs}} WHERE ` + where + `
	`).Bind(params).One(&job)
	return job, err
}

func pageTrashEmptySnapshotFenceActive(app core.App, workspaceID string) (bool, error) {
	var active int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_trash_empty_jobs}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = {:status}
				AND [[phase]] IN ({:discovering}, {:admitting}) LIMIT 1
		)
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "status": trashEmptyStatusPending,
		"discovering": trashEmptyPhaseDiscovering, "admitting": trashEmptyPhaseAdmitting,
	}).Row(&active)
	return active == 1, err
}

func loadPageTrashEmptyItem(
	app core.App,
	jobID, status string,
) (storedPageTrashEmptyItem, error) {
	item := storedPageTrashEmptyItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]], [[page_id]], [[child_request_id]], [[child_job_id]],
			[[status]], [[created_at]], [[updated_at]]
		FROM {{leafen_trash_empty_items}}
		WHERE [[job_id]] = {:jobId} AND [[status]] = {:status}
		ORDER BY [[page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "status": status}).One(&item)
	return item, err
}

func updatePageTrashEmptyItem(
	app core.App,
	jobID, pageID, childJobID, status string,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_trash_empty_items}}
			SET [[child_job_id]] = CASE WHEN {:childJobId} = '' THEN [[child_job_id]] ELSE {:childJobId} END,
				[[status]] = {:status}, [[updated_at]] = {:now}
			WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{
			"childJobId": childJobID, "status": status, "now": time.Now().UTC().UnixMilli(),
			"jobId": jobID, "pageId": pageID,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return errTrashEmptyConflict
		}
		return advancePageTrashEmptyRevision(txApp, jobID)
	})
}

func advancePageTrashEmptyRevision(app core.App, jobID string) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_trash_empty_jobs}}
		SET [[revision]] = [[revision]] + 1, [[updated_at]] = {:now}
		WHERE [[id]] = {:jobId} AND [[status]] = {:status}
	`).Bind(dbx.Params{
		"now": time.Now().UTC().UnixMilli(), "jobId": jobID,
		"status": trashEmptyStatusPending,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errTrashEmptyConflict
	}
	return nil
}

func pageTrashEmptyResponseForJob(
	app core.App,
	job storedPageTrashEmptyJob,
	duplicate bool,
) (trashEmptyResponse, error) {
	progress := pageTrashEmptyProgressCounts{}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) AS [[discovered]],
			COALESCE(SUM(CASE WHEN [[status]] IN ('admitted','completed') THEN 1 ELSE 0 END), 0) AS [[admitted]],
			COALESCE(SUM(CASE WHEN [[status]] = 'completed' THEN 1 ELSE 0 END), 0) AS [[completed]]
		FROM {{leafen_trash_empty_items}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&progress); err != nil {
		return trashEmptyResponse{}, err
	}
	return trashEmptyResponse{
		JobID: job.ID, WorkspaceID: job.WorkspaceID, Status: job.Status, Phase: job.Phase,
		Progress: trashEmptyProgress{
			Revision: job.Revision, Discovered: progress.Discovered,
			Admitted: progress.Admitted, Completed: progress.Completed,
		},
		Counts: trashEmptyCounts{Roots: progress.Discovered}, Duplicate: duplicate,
	}, nil
}

func updatePageTrashEmptyDiscovery(
	app core.App,
	jobID, cursor, pageID, ancestorID, phase string,
) error {
	var roots int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_trash_empty_items}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).Row(&roots); err != nil {
		return err
	}
	counts, err := json.Marshal(trashEmptyCounts{Roots: roots})
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_trash_empty_jobs}}
		SET [[phase]] = {:phase}, [[revision]] = [[revision]] + 1,
			[[discovery_cursor]] = {:cursor}, [[discovery_page_id]] = {:pageId},
			[[discovery_ancestor_id]] = {:ancestorId}, [[counts_json]] = {:counts},
			[[updated_at]] = {:now}
		WHERE [[id]] = {:jobId} AND [[status]] = {:status}
	`).Bind(dbx.Params{
		"phase": phase, "cursor": cursor, "pageId": pageID, "ancestorId": ancestorID,
		"counts": string(counts), "now": time.Now().UTC().UnixMilli(),
		"jobId": jobID, "status": trashEmptyStatusPending,
	}).Execute()
	return err
}

func touchPageTrashEmptyJob(app core.App, jobID, phase, status, cursor string) error {
	var roots int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_trash_empty_items}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).Row(&roots); err != nil {
		return err
	}
	counts, err := json.Marshal(trashEmptyCounts{Roots: roots})
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	completedAt := int64(0)
	if status == trashEmptyStatusCompleted {
		completedAt = now
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_trash_empty_jobs}}
		SET [[phase]] = {:phase}, [[status]] = {:status}, [[revision]] = [[revision]] + 1,
			[[discovery_cursor]] = CASE WHEN {:cursor} = '' THEN [[discovery_cursor]] ELSE {:cursor} END,
			[[counts_json]] = {:counts}, [[updated_at]] = {:now},
			[[completed_at]] = CASE WHEN {:completedAt} > 0 THEN {:completedAt} ELSE [[completed_at]] END
		WHERE [[id]] = {:jobId}
	`).Bind(dbx.Params{
		"phase": phase, "status": status, "cursor": cursor, "counts": string(counts),
		"now": now, "completedAt": completedAt, "jobId": jobID,
	}).Execute()
	return err
}

func pageTrashEmptyRequestHash(workspaceID string) string {
	digest := sha256.Sum256([]byte("page-trash-empty\x00" + workspaceID))
	return hex.EncodeToString(digest[:])
}

func pageTrashEmptyJobID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte(actorID + "\x00page-trash-empty\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}

func pageTrashEmptyChildRequestID(jobID, pageID string) string {
	return "trash-empty:" + jobID + ":" + pageID
}
