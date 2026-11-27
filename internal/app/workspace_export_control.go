package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func cleanupWorkspaceExportCapturedState(app core.App, jobID string) error {
	for _, table := range []string{
		"leafen_workspace_export_scope_pages",
		"leafen_workspace_export_scope_blocks",
		"leafen_workspace_export_chunks",
		"leafen_workspace_export_files",
		"leafen_job_chunks",
		"leafen_workspace_export_artifacts",
	} {
		if _, err := app.DB().NewQuery("DELETE FROM {{" + table + "}} WHERE [[job_id]] = {:jobId}").
			Bind(dbx.Params{"jobId": jobID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func (r *durableJobRunner) retryWorkspaceExport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	outcome, err := r.retryOwnedStoredJob(
		event.App, jobID, user.Id, workspaceExportJobKind, workspaceID,
		func(txApp core.App) error {
			return enforceSharingSecurityWorkspaceExports(txApp, user.Id, workspaceID)
		},
		func(job storedJob) bool { return job.ErrorCode != errorCodeWorkspaceExportSourceChanged },
	)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceExportNotFound, "Export not found.")
	}
	if outcome.unavailable {
		return apiError(event, http.StatusConflict, errorCodeJobRetryUnavailable, "This export can't be retried.")
	}
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return writeSharingSecurityBlockedError(event)
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportControlFailed, "Couldn't restart the export.", err)
	}
	r.notify()
	response, err := r.workspaceExportResponse(jobID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	return event.JSON(http.StatusAccepted, workspaceExportJobResult{Job: response})
}

func (r *durableJobRunner) cancelWorkspaceExport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	outcome, err := r.cancelOwnedStoredJob(
		event.App, jobID, user.Id, workspaceExportJobKind, workspaceID,
		func(txApp core.App) error {
			_, authorizeErr := authorizeWorkspaceToExport(txApp, user.Id, workspaceID, permissionWorkspaceRead)
			return authorizeErr
		},
	)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceExportNotFound, "Export not found.")
	}
	if outcome.unavailable {
		return apiError(event, http.StatusConflict, errorCodeJobCancelUnavailable, "This export can't be canceled.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportControlFailed, "Couldn't cancel the export.", err)
	}
	response, err := r.workspaceExportResponse(jobID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	return event.JSON(http.StatusOK, workspaceExportJobResult{Cancelled: true, Job: response})
}
