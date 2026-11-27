package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// pageDuplicationMaxBodyBytes holds a request naming its copy with a title as
// long as a page's, four bytes a character, beside its other fields.
const pageDuplicationMaxBodyBytes = 4*maxGraphTitleRunes + 4096

var (
	errPageDuplicationInvalid           = &pageDuplicationFailure{code: errorCodePageDuplicationInvalid}
	errPageDuplicationNotFound          = &pageDuplicationFailure{code: errorCodePageDuplicationNotFound}
	errPageDuplicationRequestIDReused   = &pageDuplicationFailure{code: errorCodePageDuplicationRequestIDReused}
	errPageDuplicationConflict          = &pageDuplicationFailure{code: errorCodePageDuplicationConflict}
	errPageDuplicationDestinationLocked = &pageDuplicationFailure{code: errorCodePageDuplicationDestinationLocked}
	errPageDuplicationUnsupported       = &pageDuplicationFailure{code: errorCodePageDuplicationUnsupportedContent}
	// The page copied, or where its copy goes, is in the trash.
	errPageDuplicationInTrash = &pageDuplicationFailure{code: errorCodePageInTrash}
)

type pageDuplicationFailure struct {
	code string
}

func (failure *pageDuplicationFailure) Error() string {
	return failure.code
}

func startPageDuplication(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	sourcePageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := pageDuplicationInput{}
	body, err := io.ReadAll(event.Request.Body)
	if err != nil {
		return pageDuplicationError(event, errPageDuplicationInvalid, "bind")
	}
	event.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err := event.BindBody(&input); err != nil {
		return pageDuplicationError(event, errPageDuplicationInvalid, "bind")
	}
	if besideItsSource(body, input) {
		if source, err := event.App.FindRecordById(pagesCollection, sourcePageID); err == nil {
			input.DestinationParentID = source.GetString("parent_id")
		}
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.DestinationParentID = strings.TrimSpace(input.DestinationParentID)
	input.Title = strings.TrimSpace(input.Title)
	input.DestinationTeamspaceID = strings.TrimSpace(input.DestinationTeamspaceID)
	// A copy to the person's private pages goes to their top, in this workspace.
	if input.ToPrivatePages && (input.DestinationParentID != "" || input.DestinationTeamspaceID != "" ||
		input.DestinationWorkspaceID != "") {
		return pageDuplicationError(event, errPageDuplicationInvalid, "validate")
	}
	if sourcePageID == "" || input.RequestID == "" ||
		len([]rune(input.RequestID)) > maxPageDuplicationRequestIDRunes ||
		len(input.DestinationParentID) > maxWorkspacePermissionIdentifierLength ||
		len(input.DestinationTeamspaceID) > maxWorkspacePermissionIdentifierLength ||
		len([]rune(input.Title)) > maxGraphTitleRunes {
		return pageDuplicationError(event, errPageDuplicationInvalid, "validate")
	}

	admission, err := admitPageDuplication(event.App, user.Id, sourcePageID, input)
	if err != nil {
		return pageDuplicationError(event, err, "admit")
	}
	response, err := pageDuplicationResponseForJob(admission.Job, admission.Duplicate)
	if err != nil {
		return pageDuplicationError(event, err, "response")
	}
	if runner, ok := event.App.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	status := http.StatusAccepted
	if response.Status == pageDuplicationStatusCompleted || response.Status == pageDuplicationStatusFailed {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readPageDuplication(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	if jobID == "" {
		return pageDuplicationError(event, errPageDuplicationInvalid, "validate-read")
	}
	job, err := loadPageDuplicationJobByID(event.App, jobID, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return pageDuplicationError(event, errPageDuplicationNotFound, "read")
	}
	if errors.Is(err, errPageDuplicationNotFound) {
		return pageDuplicationError(event, err, "read")
	}
	if err != nil {
		return pageDuplicationError(event, err, "read")
	}
	if err := authorizePageDuplicationTarget(
		event.App, user.Id, job,
	); err != nil {
		return pageDuplicationError(event, errPageDuplicationNotFound, "authorize-read")
	}
	response, err := pageDuplicationResponseForJob(job, false)
	if err != nil {
		return pageDuplicationError(event, err, "response-read")
	}
	return event.JSON(http.StatusOK, response)
}

func pageDuplicationError(event *core.RequestEvent, err error, stage string) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWritePageDuplication, stage,
	); ok {
		return storageError
	}
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return writeSharingSecurityBlockedError(event)
	}
	status, code, message := http.StatusServiceUnavailable,
		errorCodePageDuplicationUnavailable, "Couldn't duplicate the page."
	switch {
	case errors.Is(err, errPageDuplicationInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodePageDuplicationInvalid, "Check the page duplication request."
	case errors.Is(err, errPageDuplicationNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodePageDuplicationNotFound, "Page duplication job not found."
	case errors.Is(err, errPageDuplicationRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodePageDuplicationRequestIDReused, "This request ID was used for a different duplication."
	case errors.Is(err, errPageDuplicationConflict):
		status, code, message = http.StatusConflict,
			errorCodePageDuplicationConflict, "The page can't be duplicated at the current location."
	case errors.Is(err, errPageDuplicationInTrash):
		status, code, message = http.StatusConflict, errorCodePageInTrash, pageInTrashMessage
	case errors.Is(err, errPageDuplicationDestinationLocked):
		status, code, message = http.StatusConflict,
			errorCodePageDuplicationDestinationLocked, "A page can't be duplicated into a locked page."
	case errors.Is(err, errPageDuplicationUnsupported):
		status, code, message = http.StatusUnprocessableEntity,
			errorCodePageDuplicationUnsupportedContent, "Some content can't be duplicated safely yet."
	}
	reference := newDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference, "page_duplication", stage,
		code, err, diagnosticSourcePosition(1),
	)
}

// besideItsSource says whether a copy that names no place goes beside its
// page, in the same parent, as Notion's duplicate does — its menu's and its
// MCP's notion-duplicate-page, which takes no place (2026-11-16: a copy asked
// for with no destinationParentId went to the top of the workspace). The app
// names the parent it copies into, its source's for a duplicate.
func besideItsSource(body []byte, input pageDuplicationInput) bool {
	named := struct {
		DestinationParentID *string `json:"destinationParentId"`
	}{}
	if json.Unmarshal(body, &named) != nil || named.DestinationParentID != nil {
		return false
	}
	return !input.ToPrivatePages && input.DestinationWorkspaceID == "" && input.DestinationTeamspaceID == ""
}
