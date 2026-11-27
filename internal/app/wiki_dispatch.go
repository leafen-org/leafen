package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func startWikiMutation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := wikiMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return wikiAPIError(event, bodyRefusal(errWikiInvalid, "wiki change", err), "bind")
	}
	if pageID == "" {
		return wikiAPIError(event, wikiRefused("the page ID in the path is empty"), "bind")
	}
	// A wiki made without names from the web takes its maker's language.
	if input.Operation == wikiOperationConvert && input.Names == nil {
		names := generatedNamesFor(withRequestLanguage(event.Request.Context(), event.Request), event.App, user.Id).Wiki
		input.Names = &names
	}
	admission, err := admitWikiMutation(event.App, user.Id, pageID, input)
	if err != nil {
		return wikiAPIError(event, err, "admit")
	}
	response, err := wikiMutationResponseForJob(admission.Job, admission.Duplicate)
	if err != nil {
		return wikiAPIError(event, err, "response")
	}
	if runner, ok := event.App.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	status := http.StatusAccepted
	if response.Status != wikiMutationStatusPending {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readWikiMutation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	if jobID == "" {
		return wikiAPIError(event, wikiRefused("the wiki mutation ID in the path is empty"), "validate-read")
	}
	// A change it can't read is one not found: a job, not a wiki.
	job, err := loadWikiMutationByID(event.App, jobID, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Wiki change not found.")
	}
	if err != nil {
		return wikiAPIError(event, err, "read")
	}
	if _, err := authorizeWorkspace(
		event.App, user.Id, job.WorkspaceID, permissionWorkspaceRead,
	); err != nil {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Wiki change not found.")
	}
	response, err := wikiMutationResponseForJob(job, false)
	if err != nil {
		return wikiAPIError(event, err, "response-read")
	}
	return event.JSON(http.StatusOK, response)
}

func wikiAPIError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable,
		errorCodeWikiUnavailable, "Couldn't complete the wiki operation."
	switch {
	case errors.Is(err, errWikiInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeWikiInvalid, "Check the wiki request."
	case errors.Is(err, errWikiNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeWikiNotFound, "Wiki or page not found."
	case errors.Is(err, errWikiRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeWikiRequestIDReused, "This request ID was used for a different operation."
	case errors.Is(err, errWikiRevisionChanged):
		status, code, message = http.StatusConflict,
			errorCodeWikiRevisionChanged, "The wiki state changed. Load it again."
	case errors.Is(err, errWikiConflict):
		status, code, message = http.StatusConflict,
			errorCodeWikiConflict, "The operation can't be applied in the current wiki state."
	}
	if status == http.StatusBadRequest || status == http.StatusConflict {
		message = refusalMessage(message, refusalReason(err))
	}
	reference := newDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference,
		"wiki", stage, code, err, diagnosticSourcePosition(1),
	)
}
