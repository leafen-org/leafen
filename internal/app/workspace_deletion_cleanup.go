package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceDeletionResidueLane struct {
	Table       string
	Key         string
	ParentTable string
	SharedOwner string
}

func advanceWorkspaceDeletion(
	app core.App,
	jobID string,
) (storedWorkspaceDeletionJob, error) {
	if err := validateWorkspaceDeletionDirectAuthorityInventory(app); err != nil {
		return storedWorkspaceDeletionJob{}, err
	}
	for step := 0; step < maxWorkspaceDeletionStepsPerRequest; step++ {
		job, err := loadWorkspaceDeletionByID(app, jobID)
		if err != nil {
			return storedWorkspaceDeletionJob{}, err
		}
		if job.Status == workspaceDeletionStatusCompleted {
			return job, nil
		}
		if err := advanceWorkspaceDeletionStep(app, job); err != nil {
			return storedWorkspaceDeletionJob{}, err
		}
	}
	return loadWorkspaceDeletionByID(app, jobID)
}

func advanceWorkspaceDeletionStep(app core.App, job storedWorkspaceDeletionJob) error {
	switch job.Phase {
	case workspaceDeletionPhasePages:
		return advanceWorkspaceDeletionPages(app, job)
	case workspaceDeletionPhaseFiles:
		return advanceWorkspaceDeletionFiles(app, job)
	case workspaceDeletionPhaseResidue:
		return advanceWorkspaceDeletionResidue(app, job)
	case workspaceDeletionPhaseFinal:
		// A deletion that reached its last step before a lane was added runs it first.
		if job.ResidueLane < workspaceDeletionLaneCount() {
			return advanceWorkspaceDeletionResidue(app, job)
		}
		return completeWorkspaceDeletion(app, job)
	case workspaceDeletionPhaseCompleted:
		return nil
	default:
		return errWorkspaceDeleteUnavailable
	}
}

// nextWorkspaceDeletionPage is the workspace's first page by id after the
// given one that is not a database's row, or "" when none is left; a row goes
// with its database's page. Pages go in id order, so the lookup starts after
// the page just deleted rather than at the start, which walked again past
// every row of the databases not yet reached that sorted before the next page
// (a query-plan follow-up, 2026-11-10: 65 ms a lookup on average and up to
// 0.27 s, in a workspace of 1,000 pages and 100,000 rows). One from the start
// comes last, so a page that sorted before is still found.
func nextWorkspaceDeletionPage(app core.App, workspaceID, after string) (string, error) {
	var pageID string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_pages}}
		WHERE [[workspace]] = {:workspaceId} AND [[data_source_id]] = '' AND [[id]] > {:after}
		ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "after": after}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) && after != "" {
		return nextWorkspaceDeletionPage(app, workspaceID, "")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}

func advanceWorkspaceDeletionFiles(app core.App, expected storedWorkspaceDeletionJob) error {
	handled, err := advanceWorkspaceDeletionCustomEmojis(app, expected)
	if err != nil || handled {
		return err
	}
	records, err := app.FindRecordsByFilter(
		filesCollection,
		"workspace = {:workspaceId}",
		"id", maxWorkspaceDeletionObjectsPerStep, 0,
		dbx.Params{"workspaceId": expected.WorkspaceID},
	)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return advanceWorkspaceDeletionExportArtifacts(app, expected)
	}
	for _, record := range records {
		if err := deleteProductFileStorage(app, record); err != nil {
			return err
		}
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		now := time.Now().UTC().UnixMilli()
		for _, record := range records {
			stored, err := txApp.FindRecordById(filesCollection, record.Id)
			if err != nil || stored.GetString("workspace") != job.WorkspaceID {
				return errors.Join(errWorkspaceDeleteConflict, err)
			}
			if err := releaseProductOrganizationStorage(
				context.Background(), txApp, stored.Id, now,
			); err != nil {
				return err
			}
			if err := txApp.Delete(stored); err != nil {
				return err
			}
		}
		job.FilesCount += len(records)
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
}

func advanceWorkspaceDeletionCustomEmojis(
	app core.App,
	expected storedWorkspaceDeletionJob,
) (bool, error) {
	handled := false
	err := app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		// In the order of the emojis' index by workspace, which reads only
		// this batch.
		result, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_custom_emojis}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_custom_emojis}}
				WHERE [[workspace_id]] = {:workspaceId}
				ORDER BY [[id]] LIMIT {:limit}
			)
		`).Bind(dbx.Params{
			"workspaceId": job.WorkspaceID,
			"limit":       maxWorkspaceDeletionRowsPerStep,
		}).Execute()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows == 0 {
			return err
		}
		handled = true
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
	return handled, err
}

func advanceWorkspaceDeletionExportArtifacts(
	app core.App,
	expected storedWorkspaceDeletionJob,
) error {
	artifacts := []storedWorkspaceExportArtifact{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]], [[workspace_id]], [[owner]], [[format]], [[filename]],
			[[content_type]], [[bytes]], [[sha256]], [[created]]
		FROM {{leafen_workspace_export_artifacts}}
		WHERE [[workspace_id]] = {:workspaceId}
		ORDER BY [[job_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": expected.WorkspaceID,
		"limit":       maxWorkspaceDeletionObjectsPerStep,
	}).All(&artifacts); err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return transitionWorkspaceDeletionPhase(
			app, expected, workspaceDeletionPhaseResidue,
		)
	}
	for _, artifact := range artifacts {
		if err := deleteWorkspaceExportArtifactStorage(app, artifact); err != nil {
			return err
		}
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		for _, artifact := range artifacts {
			result, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_workspace_export_artifacts}}
				WHERE [[job_id]] = {:jobId} AND [[workspace_id]] = {:workspaceId}
					AND [[filename]] = {:filename}
			`).Bind(dbx.Params{
				"jobId": artifact.JobID, "workspaceId": job.WorkspaceID,
				"filename": artifact.Filename,
			}).Execute()
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil || rows != 1 {
				return errors.Join(errWorkspaceDeleteConflict, err)
			}
		}
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
}

func advanceWorkspaceDeletionResidue(app core.App, expected storedWorkspaceDeletionJob) error {
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		if job.ResidueLane >= workspaceDeletionLaneCount() {
			job.Phase = workspaceDeletionPhaseFinal
			return persistWorkspaceDeletionJob(txApp, expected, job)
		}
		if job.ResidueLane >= len(workspaceDeletionResidueLanes) {
			next, err := clearWorkspaceDeletionCascadeLanes(
				txApp, job.WorkspaceID, job.ResidueLane-len(workspaceDeletionResidueLanes),
			)
			if err != nil {
				return err
			}
			job.ResidueLane = len(workspaceDeletionResidueLanes) + next
			return persistWorkspaceDeletionJob(txApp, expected, job)
		}
		lane := workspaceDeletionResidueLanes[job.ResidueLane]
		// Its database rows' own databases, queued for the trash runner,
		// go before the queue does (2026-10-24: dropped with the queue, they
		// stayed, and the workspace's last step refused).
		if lane.Table == rowPageContentCleanupTable {
			more, err := cleanRowPageContentSliceIn(txApp, job.WorkspaceID)
			if err != nil {
				return err
			}
			if more {
				return persistWorkspaceDeletionJob(txApp, expected, job)
			}
		}
		if lane.SharedOwner != "" {
			clean := cleanCompletedPagePermanentDeleteReceiptsForWorkspace
			switch lane.SharedOwner {
			case "workspace-search-projection":
				clean = cleanWorkspaceSearchProjection
			case "page-owned-leftovers":
				clean = cleanWorkspacePageLeftovers
			}
			more, err := clean(txApp, job.WorkspaceID, maxWorkspaceDeletionRowsPerStep)
			if err != nil {
				return err
			}
			if !more {
				job.ResidueLane++
			}
			return persistWorkspaceDeletionJob(txApp, expected, job)
		}
		statement := fmt.Sprintf(`
			DELETE FROM {{%s}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{%s}}
				WHERE [[%s]] = {:workspaceId}
				ORDER BY [[rowid]] LIMIT {:limit}
			)
		`, lane.Table, lane.Table, lane.Key)
		if lane.ParentTable != "" {
			childAccessIndex, ok := workspaceDeletionChildAccessIndex(lane.Table)
			if !ok {
				return errWorkspaceDeleteUnavailable
			}
			statement = fmt.Sprintf(`
				DELETE FROM {{%s}} WHERE [[rowid]] IN (
					SELECT [[child]].[[rowid]]
					FROM {{%s}} [[parent]] INDEXED BY [[%s]]
					CROSS JOIN {{%s}} [[child]] INDEXED BY [[%s]]
					WHERE [[parent]].[[workspace_id]] = {:workspaceId}
						AND [[child]].[[%s]] = [[parent]].[[id]]
					ORDER BY [[parent]].[[rowid]], [[child]].[[rowid]] LIMIT {:limit}
				)
			`, lane.Table, lane.ParentTable,
				workspaceDeletionScopeIndexName(lane.ParentTable),
				lane.Table, childAccessIndex, lane.Key)
		}
		result, err := txApp.DB().NewQuery(statement).Bind(dbx.Params{
			"workspaceId": job.WorkspaceID,
			"limit":       maxWorkspaceDeletionRowsPerStep,
		}).Execute()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows < maxWorkspaceDeletionRowsPerStep {
			job.ResidueLane++
		}
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
}

func completeWorkspaceDeletion(app core.App, expected storedWorkspaceDeletionJob) error {
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		if err := reauthorizeWorkspaceDeletion(txApp, job.ActorID, job); err != nil {
			return err
		}
		workspace, err := txApp.FindRecordById(workspacesCollection, job.WorkspaceID)
		if err != nil || workspace.GetString("deletion_job_id") != job.ID {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		// A page or file written behind its phase sends the deletion back to
		// that phase, and rows written again behind their lane send it back
		// to that lane (workspace_deletion_lane_return.go).
		phase, err := workspaceDeletionPhaseToReturnTo(txApp, job.WorkspaceID)
		if err != nil {
			return err
		}
		if phase != "" {
			job.Phase = phase
			job.PageID = ""
			job.PageCleanupLane = 0
			return persistWorkspaceDeletionJob(txApp, expected, job)
		}
		lane, err := workspaceDeletionLaneToReturnTo(txApp, job.WorkspaceID)
		if err != nil {
			return err
		}
		if lane >= 0 {
			job.Phase = workspaceDeletionPhaseResidue
			job.ResidueLane = lane
			return persistWorkspaceDeletionJob(txApp, expected, job)
		}
		now := time.Now().UTC().UnixMilli()
		if err := bumpOrganizationWorkspaceSetRevision(txApp, job.OrganizationID); err != nil {
			return err
		}
		if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID:             organizationAuditID(job.OrganizationID, "workspace.deleted", job.WorkspaceID+"\x00"+job.ID),
			OrganizationID: job.OrganizationID, WorkspaceID: job.WorkspaceID,
			ActorID: job.ActorID, Action: "workspace.deleted",
			TargetType: "workspace", TargetID: job.WorkspaceID,
			Metadata: map[string]any{
				"jobId": job.ID, "name": job.WorkspaceName,
				"pages": job.PagesCount, "files": job.FilesCount,
			},
			OccurredAt: now,
		}); err != nil {
			return err
		}
		// Its cascades find the cascade lanes' tables already cleared.
		if err := txApp.Delete(workspace); err != nil {
			return err
		}
		notifyBootstrapVisibilityProjectionAfterCommit(txApp)
		if err := validateWorkspaceDeletionTerminalResidue(
			txApp, job.WorkspaceID,
		); err != nil {
			return err
		}
		job.Status = workspaceDeletionStatusCompleted
		job.Phase = workspaceDeletionPhaseCompleted
		job.CompletedAt = now
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
}

func transitionWorkspaceDeletionPhase(
	app core.App,
	expected storedWorkspaceDeletionJob,
	phase string,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadWorkspaceDeletionByID(txApp, expected.ID)
		if err != nil || !sameWorkspaceDeletionRevision(job, expected) {
			return errors.Join(errWorkspaceDeleteConflict, err)
		}
		job.Phase = phase
		return persistWorkspaceDeletionJob(txApp, expected, job)
	})
}

func persistWorkspaceDeletionJob(
	app core.App,
	expected, next storedWorkspaceDeletionJob,
) error {
	now := time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_workspace_deletion_jobs}}
		SET [[status]] = {:status}, [[phase]] = {:phase}, [[revision]] = [[revision]] + 1,
			[[page_id]] = {:pageId}, [[page_cleanup_lane]] = {:pageCleanupLane},
			[[residue_lane]] = {:residueLane}, [[pages_count]] = {:pagesCount},
			[[files_count]] = {:filesCount}, [[updated_at]] = {:updatedAt},
			[[completed_at]] = {:completedAt}
		WHERE [[id]] = {:jobId} AND [[actor_id]] = {:actorId}
			AND [[workspace_id]] = {:workspaceId} AND [[revision]] = {:revision}
			AND [[phase]] = {:expectedPhase} AND [[status]] = {:expectedStatus}
	`).Bind(dbx.Params{
		"status": next.Status, "phase": next.Phase, "pageId": next.PageID,
		"pageCleanupLane": next.PageCleanupLane, "residueLane": next.ResidueLane,
		"pagesCount": next.PagesCount, "filesCount": next.FilesCount,
		"updatedAt": now, "completedAt": next.CompletedAt, "jobId": expected.ID,
		"actorId": expected.ActorID, "workspaceId": expected.WorkspaceID,
		"revision": expected.Revision, "expectedPhase": expected.Phase,
		"expectedStatus": expected.Status,
	}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.Join(errWorkspaceDeleteConflict, err)
	}
	return nil
}

func sameWorkspaceDeletionRevision(left, right storedWorkspaceDeletionJob) bool {
	return left.ID == right.ID && left.ActorID == right.ActorID &&
		left.WorkspaceID == right.WorkspaceID && left.Status == right.Status &&
		left.Phase == right.Phase && left.Revision == right.Revision
}
