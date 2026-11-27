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

type storedPagePermanentDeleteJob struct {
	ID             string `db:"id"`
	ActorID        string `db:"actor_id"`
	OwnerID        string `db:"owner_id"`
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization_id"`
	RootPageID     string `db:"root_page_id"`
	RequestID      string `db:"request_id"`
	RequestHash    string `db:"request_hash"`
	Status         string `db:"status"`
	Phase          string `db:"phase"`
	Revision       int    `db:"revision"`
	CountsJSON     string `db:"counts_json"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
	CompletedAt    int64  `db:"completed_at"`
}

type storedPagePermanentDeleteItem struct {
	JobID           string `db:"job_id"`
	PageID          string `db:"page_id"`
	ParentPageID    string `db:"parent_page_id"`
	CustodianID     string `db:"custodian_id"`
	Depth           int    `db:"depth"`
	IsRoot          bool   `db:"is_root"`
	DiscoveryCursor string `db:"discovery_cursor"`
	Discovered      bool   `db:"discovered"`
	Authorized      bool   `db:"authorized"`
	CleanupLane     int    `db:"cleanup_lane"`
	Cleaned         bool   `db:"cleaned"`
}

type pagePermanentDeleteProgressCounts struct {
	Discovered int `db:"discovered"`
	Authorized int `db:"authorized"`
	Cleaned    int `db:"cleaned"`
}

func admitPagePermanentDelete(
	app core.App,
	subject pageAccessSubject,
	pageID, requestID string,
) (pagePermanentDeleteAdmission, error) {
	actorID := subject.actorID()
	requestHash := pagePermanentDeleteRequestHash(pageID)
	result := pagePermanentDeleteAdmission{}
	err := app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadPagePermanentDeleteByRequest(txApp, actorID, requestID)
		if err == nil {
			if existing.RootPageID != pageID || existing.RequestHash != requestHash {
				return errPagePermanentDeleteRequestIDReused
			}
			if err := reauthorizePagePermanentDeleteJob(txApp, subject, existing); err != nil {
				return err
			}
			result = pagePermanentDeleteAdmission{
				Job: existing, Duplicate: existing.Status == pagePermanentDeleteStatusCompleted,
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		existing, err = loadPagePermanentDeleteByRoot(txApp, actorID, pageID)
		if err == nil {
			if err := reauthorizePagePermanentDeleteJob(txApp, subject, existing); err != nil {
				return err
			}
			// Asked again, from another tab or after a reload, the deletion
			// under way goes on (it was refused as a conflict).
			if existing.Status != pagePermanentDeleteStatusCompleted {
				result = pagePermanentDeleteAdmission{Job: existing}
				return nil
			}
			result = pagePermanentDeleteAdmission{Job: existing, AlreadyDeleted: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		// A running copy reads this tree to its end; it may change, not vanish.
		copying, err := pageDuplicationSourceFenceActive(txApp, pageID)
		if err != nil {
			return err
		}
		if copying {
			return errPagePermanentDeleteConflict
		}
		authorization, err := subject.authorizePage(txApp, pageID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		leased, err := workspaceMarkdownReplacePageLeased(txApp, pageID, "")
		if err != nil {
			return err
		}
		if leased {
			return errPagePermanentDeleteConflict
		}
		// A page under a page in the trash is in the trash with it, and is
		// deleted forever on its own from there, as Notion's trash lists it
		// (database_source_trash_rows.go).
		if trashed, err := pageInTrashPath(txApp, pageID); err != nil {
			return err
		} else if !trashed {
			return errPagePermanentDeleteNotTrashed
		}
		if err := assertNoActiveLegalHoldForPermanentDelete(
			txApp, authorization.Workspace.Organization.OrganizationID,
			legalHoldDeletionTarget{
				WorkspaceID: authorization.Workspace.WorkspaceID,
				PageIDs:     []string{pageID}, CustodianUserIDs: []string{authorization.Page.GetString("owner")},
			},
		); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		jobID := pagePermanentDeleteJobID(actorID, requestID)
		_, err = txApp.DB().Insert(pagePermanentDeleteJobsTable, dbx.Params{
			"id": jobID, "actor_id": actorID,
			"owner_id":        authorization.Workspace.OwnerID,
			"workspace_id":    authorization.Workspace.WorkspaceID,
			"organization_id": authorization.Workspace.Organization.OrganizationID,
			"root_page_id":    pageID, "request_id": requestID, "request_hash": requestHash,
			"status": pagePermanentDeleteStatusPending,
			"phase":  pagePermanentDeletePhaseDiscovering, "revision": 1,
			"counts_json": `{"pages":1}`, "created_at": now, "updated_at": now,
			"completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		_, err = txApp.DB().Insert(pagePermanentDeleteItemsTable, dbx.Params{
			"job_id": jobID, "page_id": pageID,
			"parent_page_id": authorization.Page.GetString("parent_id"),
			"custodian_id":   authorization.Page.GetString("owner"),
			"depth":          0, "is_root": true, "discovery_cursor": "",
			"discovered": false, "authorized": false, "cleanup_lane": 0,
			"cleaned": false, "created_at": now, "updated_at": now,
		}).Execute()
		if err != nil {
			return err
		}
		authorization.Page.Set("deletion_job_id", jobID)
		if err := txApp.Save(authorization.Page); err != nil {
			return err
		}
		result = pagePermanentDeleteAdmission{
			Job: storedPagePermanentDeleteJob{
				ID: jobID, ActorID: actorID, OwnerID: authorization.Workspace.OwnerID,
				WorkspaceID:    authorization.Workspace.WorkspaceID,
				OrganizationID: authorization.Workspace.Organization.OrganizationID,
				RootPageID:     pageID, RequestID: requestID, RequestHash: requestHash,
				Status: pagePermanentDeleteStatusPending,
				Phase:  pagePermanentDeletePhaseDiscovering, Revision: 1,
				CountsJSON: `{"pages":1}`, CreatedAt: now, UpdatedAt: now,
			},
			Admitted: true,
		}
		return nil
	})
	return result, err
}

func reauthorizePagePermanentDeleteJob(
	app core.App,
	subject pageAccessSubject,
	job storedPagePermanentDeleteJob,
) error {
	authorization, err := subject.authorizeWorkspace(app, job.WorkspaceID, permissionWorkspaceOwner)
	if err != nil {
		return err
	}
	if authorization.OwnerID != job.OwnerID ||
		authorization.Organization.OrganizationID != job.OrganizationID {
		return permissionDenied()
	}
	return nil
}

func loadPagePermanentDeleteByRequest(
	app core.App,
	actorID, requestID string,
) (storedPagePermanentDeleteJob, error) {
	return loadPagePermanentDeleteJob(app, `[[actor_id]] = {:actorId} AND [[request_id]] = {:requestId}`,
		dbx.Params{"actorId": actorID, "requestId": requestID})
}

func loadPagePermanentDeleteByRoot(
	app core.App,
	actorID, rootPageID string,
) (storedPagePermanentDeleteJob, error) {
	return loadPagePermanentDeleteJob(app,
		`[[actor_id]] = {:actorId} AND [[root_page_id]] = {:rootPageId}
		 ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT 1`,
		dbx.Params{"actorId": actorID, "rootPageId": rootPageID})
}

func loadPagePermanentDeleteJobByID(
	app core.App,
	jobID string,
) (storedPagePermanentDeleteJob, error) {
	return loadPagePermanentDeleteJob(app, `[[id]] = {:jobId}`, dbx.Params{"jobId": jobID})
}

func loadPagePermanentDeleteJob(
	app core.App,
	where string,
	params dbx.Params,
) (storedPagePermanentDeleteJob, error) {
	job := storedPagePermanentDeleteJob{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[actor_id]], [[owner_id]], [[workspace_id]], [[organization_id]],
			[[root_page_id]], [[request_id]], [[request_hash]], [[status]], [[phase]],
			[[revision]], [[counts_json]], [[created_at]], [[updated_at]], [[completed_at]]
		FROM {{leafen_page_permanent_delete_jobs}}
		WHERE ` + where + `
	`).Bind(params).One(&job)
	return job, err
}

func pendingPagePermanentDeleteForWorkspace(app core.App, workspaceID string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_page_permanent_delete_jobs}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = {:status}
			LIMIT 1
		)
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "status": pagePermanentDeleteStatusPending,
	}).Row(&found)
	return found == 1, err
}

func advancePagePermanentDelete(
	app core.App,
	jobID string,
) (storedPagePermanentDeleteJob, error) {
	err := app.RunInTransaction(func(txApp core.App) error {
		for step := 0; step < maxPagePermanentDeleteItemsPerRequest; step++ {
			job, err := loadPagePermanentDeleteJobByID(txApp, jobID)
			if err != nil {
				return err
			}
			if job.Status == pagePermanentDeleteStatusCompleted {
				return nil
			}
			if err := advancePagePermanentDeleteStep(txApp, job); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return storedPagePermanentDeleteJob{}, err
	}
	return loadPagePermanentDeleteJobByID(app, jobID)
}

func pagePermanentDeleteResponseForJob(
	app core.App,
	job storedPagePermanentDeleteJob,
	duplicate, alreadyDeleted bool,
) (pagePermanentDeleteResponse, error) {
	progress := pagePermanentDeleteProgressCounts{}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) AS [[discovered]],
			COALESCE(SUM(CASE WHEN [[authorized]] = 1 THEN 1 ELSE 0 END), 0) AS [[authorized]],
			COALESCE(SUM(CASE WHEN [[cleaned]] = 1 THEN 1 ELSE 0 END), 0) AS [[cleaned]]
		FROM {{leafen_page_permanent_delete_items}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&progress); err != nil {
		return pagePermanentDeleteResponse{}, err
	}
	return pagePermanentDeleteResponse{
		JobID: job.ID, RootPageID: job.RootPageID, Status: job.Status, Phase: job.Phase,
		Progress: pagePermanentDeleteProgress{
			Revision: job.Revision, Discovered: progress.Discovered,
			Authorized: progress.Authorized, Cleaned: progress.Cleaned,
		},
		Counts:    pagePermanentDeleteCounts{Pages: progress.Discovered},
		Duplicate: duplicate, AlreadyDeleted: alreadyDeleted,
	}, nil
}

func touchPagePermanentDeleteJob(
	app core.App,
	jobID string,
	phase, status string,
) error {
	var pages int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_permanent_delete_items}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).Row(&pages); err != nil {
		return err
	}
	counts, err := json.Marshal(pagePermanentDeleteCounts{Pages: pages})
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	completedAt := int64(0)
	if status == pagePermanentDeleteStatusCompleted {
		completedAt = now
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_permanent_delete_jobs}}
		SET [[phase]] = {:phase}, [[status]] = {:status}, [[revision]] = [[revision]] + 1,
			[[counts_json]] = {:counts}, [[updated_at]] = {:now},
			[[completed_at]] = CASE WHEN {:completedAt} > 0 THEN {:completedAt} ELSE [[completed_at]] END
		WHERE [[id]] = {:jobId}
	`).Bind(dbx.Params{
		"phase": phase, "status": status, "counts": string(counts),
		"now": now, "completedAt": completedAt, "jobId": jobID,
	}).Execute()
	return err
}

func pagePermanentDeleteRequestHash(pageID string) string {
	digest := sha256.Sum256([]byte("page-permanent-delete\x00" + pageID))
	return hex.EncodeToString(digest[:])
}

func pagePermanentDeleteJobID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte(actorID + "\x00page-permanent-delete\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}
