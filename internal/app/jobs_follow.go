package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// GET /jobs/{id} follows every long job an answer names by its jobId
// (2026-11-26: agents following a trash emptying and a workspace deletion
// there, as its summary "Follow a long job until it is done" says, were told
// "Job not found."). A job of the job table answers as it always has —
// moves, copies, wiki changes, exports, imports, backups and the rest keep
// theirs there. A job another table keeps answers what its own read answers,
// for whoever may read it there: a database change's and a file cleanup's
// own read runs here with the job's place; a trash emptying, a page's
// permanent deletion and a workspace's deletion have no read of their own,
// so this is theirs, for the person who started them; so is a page's or a
// row's deletion for good held for its 30 days, answered done at once
// (its jobId held:<page>, page_deletion_hold.go). A workspace's deletion
// goes on as it is followed, as the screen carries it on when it opens
// (useAuthenticatedBootstrap's resume): nothing else carries it on.

// jobFollower reads one kind of job another table keeps by its ID.
type jobFollower struct {
	// table keeps the kind's jobs, by their id.
	table string
	read  func(event *core.RequestEvent, rooms workspaceRoomInvalidator, user *core.Record, jobID string) error
}

var jobFollowers = []jobFollower{
	{table: "leafen_trash_empty_jobs", read: followTrashEmptyJob},
	{table: "leafen_page_permanent_delete_jobs", read: followPagePermanentDeleteJob},
	{table: workspaceDeletionJobsTable, read: followWorkspaceDeletionJob},
	{table: "leafen_database_mutation_jobs", read: followJobAtItsRead("leafen_database_mutation_jobs", "page_id", "pageId", readDatabaseMutationDeletion)},
	{table: "leafen_file_manual_cleanup_jobs", read: followJobAtItsRead("leafen_file_manual_cleanup_jobs", "workspace_id", "workspaceId", readProductFileManualCleanup)},
}

// followOtherJob answers GET /jobs/{id} for an ID the job table doesn't
// hold: the job another table keeps under it, or none.
func (r *durableJobRunner) followOtherJob(event *core.RequestEvent, user *core.Record, id string) error {
	if pageID, held := strings.CutPrefix(id, pageDeletionHoldFencePrefix); held {
		return followHeldDeletion(event, user, pageID)
	}
	// An ID of 32 hex digits comes with a UUID's dashes (native_api_id_aliases.go).
	ids := []string{id}
	if compact := strings.ReplaceAll(id, "-", ""); compact != id && len(compact) == 32 {
		ids = append(ids, compact)
	}
	for _, follower := range jobFollowers {
		for _, jobID := range ids {
			var found int
			err := event.App.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{` + follower.table + `}} WHERE [[id]] = {:id})`).
				Bind(dbx.Params{"id": jobID}).Row(&found)
			if err != nil {
				return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the job.", err)
			}
			if found == 1 {
				return follower.read(event, r.rooms, user, jobID)
			}
		}
	}
	return jobNotFollowed(event)
}

// followJobAtItsRead answers a job at its own read, the place it is kept
// under — a column of its job — named as that read's path names it.
func followJobAtItsRead(
	table, column, pathName string, read func(*core.RequestEvent) error,
) func(*core.RequestEvent, workspaceRoomInvalidator, *core.Record, string) error {
	return func(event *core.RequestEvent, _ workspaceRoomInvalidator, _ *core.Record, jobID string) error {
		place := ""
		if err := event.App.DB().NewQuery(`SELECT [[` + column + `]] FROM {{` + table + `}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": jobID}).Row(&place); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the job.", err)
		}
		event.Request.SetPathValue(pathName, place)
		event.Request.SetPathValue("jobId", jobID)
		return read(event)
	}
}

// jobNotFollowed is a job its follower may not read: not found, as its
// own read answers one.
func jobNotFollowed(event *core.RequestEvent) error {
	return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Job not found.")
}

// followTrashEmptyJob reads a trash emptying for whoever started it; the
// deletion runner carries it on (page_deletion_runner.go).
func followTrashEmptyJob(event *core.RequestEvent, _ workspaceRoomInvalidator, user *core.Record, jobID string) error {
	job, err := loadPageTrashEmptyJobByID(event.App, jobID)
	if err == nil && job.ActorID != user.Id {
		return jobNotFollowed(event)
	}
	if err != nil {
		return pageTrashEmptyError(event, err, "follow")
	}
	response, err := pageTrashEmptyResponseForJob(event.App, job, false)
	if err != nil {
		return pageTrashEmptyError(event, err, "follow-response")
	}
	return event.JSON(http.StatusOK, response)
}

// followPagePermanentDeleteJob reads a page's permanent deletion for whoever
// started it; the deletion runner carries it on.
func followPagePermanentDeleteJob(event *core.RequestEvent, _ workspaceRoomInvalidator, user *core.Record, jobID string) error {
	job, err := loadPagePermanentDeleteJobByID(event.App, jobID)
	if err == nil && job.ActorID != user.Id {
		return jobNotFollowed(event)
	}
	if err != nil {
		return pagePermanentDeleteError(event, err, "follow")
	}
	response, err := pagePermanentDeleteResponseForJob(event.App, job, false, false)
	if err != nil {
		return pagePermanentDeleteError(event, err, "follow-response")
	}
	return event.JSON(http.StatusOK, response)
}

// followWorkspaceDeletionJob reads a workspace's deletion for whoever
// started it, carrying it on a few steps while they may still delete it, as
// sending its request again does.
func followWorkspaceDeletionJob(event *core.RequestEvent, rooms workspaceRoomInvalidator, user *core.Record, jobID string) error {
	job, err := loadWorkspaceDeletionByID(event.App, jobID)
	if err == nil && job.ActorID != user.Id {
		return jobNotFollowed(event)
	}
	if err != nil {
		return workspaceDeletionError(event, err, "follow")
	}
	if job.Status != workspaceDeletionStatusCompleted && reauthorizeWorkspaceDeletion(event.App, user.Id, job) == nil {
		release := acquireProductOperationLock("permanent-workspace-delete", job.WorkspaceID)
		defer release()
		if rooms != nil {
			if err := rooms.InvalidateWorkspace(job.WorkspaceID); err != nil {
				return workspaceDeletionError(event, errors.Join(errWorkspaceDeleteUnavailable, err), "follow-invalidate")
			}
		}
		if job, err = advanceWorkspaceDeletion(event.App, jobID); err != nil {
			return workspaceDeletionError(event, err, "follow-advance")
		}
	}
	return event.JSON(http.StatusOK, workspaceDeletionResponseForJob(job, false, false))
}

// followHeldDeletion reads a page's or a row's deletion for good in its 30
// days, for whoever deleted it: done, as its answer said.
func followHeldDeletion(event *core.RequestEvent, user *core.Record, pageID string) error {
	hold, err := loadPageDeletionHold(event.App, pageID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && hold.ActorID != user.Id {
		return jobNotFollowed(event)
	}
	if err != nil {
		return pagePermanentDeleteError(event, err, "follow-held")
	}
	return event.JSON(http.StatusOK, pagePermanentDeleteResponseForHold(hold, false))
}
