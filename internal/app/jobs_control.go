package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) retry(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("id"))
	outcome, err := r.retryOwnedStoredJob(event.App, jobID, user.Id, pageCheckpointJobKind, "", nil, nil)
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Job not found.")
	}
	if outcome.unavailable {
		return apiError(event, http.StatusConflict, errorCodeJobRetryUnavailable, "This operation can't be retried.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobControlFailed, "Couldn't restart the job.", err)
	}
	r.notify()
	response, err := r.response(jobID, user.Id, false)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the job.", err)
	}
	return event.JSON(http.StatusAccepted, response)
}

func (r *durableJobRunner) cancelJob(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("id"))
	outcome, err := r.cancelOwnedStoredJob(event.App, jobID, user.Id, pageCheckpointJobKind, "", nil)
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Job not found.")
	}
	if outcome.unavailable {
		return apiError(event, http.StatusConflict, errorCodeJobCancelUnavailable, "This operation can't be canceled.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobControlFailed, "Couldn't cancel the job.", err)
	}
	response, err := r.response(jobID, user.Id, false)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the job.", err)
	}
	return event.JSON(http.StatusOK, response)
}

type storedJobControlOutcome struct {
	missing     bool
	unavailable bool
}

func ownedJobMatches(job storedJob, expectedKind, workspaceID string) bool {
	return (expectedKind == "" || job.Kind == expectedKind) &&
		(workspaceID == "" || job.WorkspaceID == workspaceID)
}

// retryOwnedStoredJob and cancelOwnedStoredJob write through app, the
// request's own (request_write_stop.go): a person's retry or cancel stops
// with the server as the rest of their request's writes do.
func (r *durableJobRunner) retryOwnedStoredJob(
	app core.App,
	jobID, owner, expectedKind, workspaceID string,
	currentAuthorization func(core.App) error,
	additionalCheck func(storedJob) bool,
) (storedJobControlOutcome, error) {
	outcome := storedJobControlOutcome{}
	err := app.RunInTransaction(func(txApp core.App) error {
		if currentAuthorization != nil {
			if err := currentAuthorization(txApp); err != nil {
				return err
			}
		}
		job, err := loadStoredJob(txApp, jobID, owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !ownedJobMatches(job, expectedKind, workspaceID)) {
			outcome.missing = true
			return nil
		}
		if err != nil {
			return err
		}
		// A person's retry gives a failed job its attempts again: the runner's
		// own retries are spent while the service it reads is away, and the
		// "다시 시도" the person is offered is the way back (2026-11-05: an
		// import that failed its three attempts while Notion was unreachable
		// for three minutes refused every retry).
		if job.Status != durableJobStatusFailed ||
			(additionalCheck != nil && !additionalCheck(job)) {
			outcome.unavailable = true
			return nil
		}
		now := r.clock.now()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'queued', [[error_code]] = '', [[attempt]] = 0,
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'failed'
				AND [[attempt]] = {:attempt}
		`).Bind(dbx.Params{
			"id": job.ID, "owner": owner, "attempt": job.Attempt,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
	if err == nil && expectedKind == notionImportJobKind {
		notifyNotionImportRetentionAfterCommit(r.app)
	}
	return outcome, err
}

func (r *durableJobRunner) cancelOwnedStoredJob(
	app core.App,
	jobID, owner, expectedKind, workspaceID string,
	currentAuthorization func(core.App) error,
) (storedJobControlOutcome, error) {
	outcome := storedJobControlOutcome{}
	err := app.RunInTransaction(func(txApp core.App) error {
		if currentAuthorization != nil {
			if err := currentAuthorization(txApp); err != nil {
				return err
			}
		}
		job, err := loadStoredJob(txApp, jobID, owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !ownedJobMatches(job, expectedKind, workspaceID)) {
			outcome.missing = true
			return nil
		}
		if err != nil {
			return err
		}
		if job.Status == durableJobStatusCanceled {
			return nil
		}
		if job.Status != durableJobStatusQueued {
			outcome.unavailable = true
			return nil
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'canceled', [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{"id": job.ID, "owner": owner, "updated": r.clock.now().UnixMilli()}).Execute()
		if err == nil && expectedKind == workspaceExportJobKind {
			err = cleanupWorkspaceExportCapturedState(txApp, job.ID)
		}
		return err
	})
	if err == nil && expectedKind == notionImportJobKind {
		notifyNotionImportRetentionAfterCommit(r.app)
	}
	return outcome, err
}
