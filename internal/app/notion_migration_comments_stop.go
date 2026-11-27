package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The comments come last, once the content is done and in its place
// (notionMigrationRunOrder), and asking Notion for them can take long. The
// person may stop them then (2026-11-16: an hour of asking blocks showed no
// way to stop): what came stays, the run goes on to its check against
// Notion and its report, which says the comments stopped. Before the
// content shows, stopping cancels the import as before; while it shows,
// nothing stops it.

// notionImportCanCancel is whether a job's import can be stopped now: before
// its pages begin to show, or while a migration's run brings its comments.
func notionImportCanCancel(app core.App, job storedJob, payload notionImportJobPayload) bool {
	if job.Status != durableJobStatusQueued {
		return false
	}
	if !payload.Publishing {
		return true
	}
	return notionMigrationCommentsStoppable(app, job, payload)
}

// notionMigrationCommentsStoppable is a migration's run bringing comments
// that the person hasn't stopped yet.
func notionMigrationCommentsStoppable(app core.App, job storedJob, payload notionImportJobPayload) bool {
	if payload.MigrationID == "" || !payload.Restructuring || notionImportStage(app, job, payload) != "comments" {
		return false
	}
	var stopped bool
	err := app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[run_report_json]], '$.commentsStopped'), 0) FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": payload.MigrationID}).Row(&stopped)
	return err == nil && !stopped
}

// cancelOrStopNotionImport stops a migration's comments while its run
// brings them, and otherwise cancels the import (cancelNotionImportJob).
func (r *durableJobRunner) cancelOrStopNotionImport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	job, err := loadStoredJob(event.App, jobID, user.Id)
	payload := notionImportJobPayload{}
	if err != nil || job.Kind != notionImportJobKind || job.WorkspaceID != workspaceID ||
		json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || !notionMigrationCommentsStoppable(event.App, job, payload) {
		return r.cancelNotionImportJob(event)
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobControlFailed, "Couldn't stop the comments.", err)
	}
	// One statement, so a turn of the run writing its report meanwhile
	// can't lose it.
	if _, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_notion_migrations}} SET [[run_report_json]]=json_set([[run_report_json]], '$.commentsStopped', json('true'))
		WHERE [[id]]={:id} AND [[import_job_id]]={:jobId}
	`).Bind(dbx.Params{"id": payload.MigrationID, "jobId": job.ID}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobControlFailed, "Couldn't stop the comments.", err)
	}
	r.notify()
	response, err := notionImportResponseFor(event.Request.Context(), event.App, jobID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the import.", err)
	}
	return event.JSON(http.StatusOK, response)
}

// importNotionMigrationCommentsUntilStopped brings a turn's comments, or ends
// the comments step once the person stopped it.
func (r *durableJobRunner) importNotionMigrationCommentsUntilStopped(
	ctx context.Context, job storedJob, payload *notionImportJobPayload, people func(string) string,
) (bool, error) {
	var stopped bool
	if err := r.app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[run_report_json]], '$.commentsStopped'), 0) FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": payload.MigrationID}).Row(&stopped); err != nil {
		return false, err
	}
	if stopped {
		return false, nil
	}
	return r.importNotionMigrationComments(ctx, job, payload, people)
}
