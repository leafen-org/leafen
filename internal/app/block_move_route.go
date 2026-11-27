package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const blockMoveMaxBodyBytes = 4096

var (
	errBlockMoveInvalid         = &blockMoveFailure{code: errorCodeBlockMoveInvalid}
	errBlockMoveNotFound        = &blockMoveFailure{code: errorCodeBlockMoveNotFound}
	errBlockMoveRequestIDReused = &blockMoveFailure{code: errorCodeBlockMoveRequestIDReused}
	errBlockMoveConflict        = &blockMoveFailure{code: errorCodeBlockMoveConflict}
)

type blockMoveFailure struct {
	code string
}

func (failure *blockMoveFailure) Error() string {
	return failure.code
}

func startBlockMove(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	sourcePageID := strings.TrimSpace(event.Request.PathValue("sourcePageId"))
	blockID := strings.TrimSpace(event.Request.PathValue("blockId"))
	input := blockMoveInput{}
	if event.Request.ContentLength > blockMoveMaxBodyBytes {
		return blockMoveError(event, errBlockMoveInvalid, "bind")
	}
	if err := event.BindBody(&input); err != nil {
		return blockMoveError(event, errBlockMoveInvalid, "bind")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	// The body's pages and blocks are taken by the IDs an address names, a
	// UUID without its dashes as with them, as the path's are
	// (native_api_id_aliases.go): an agent copying a block an <unknown> line
	// names gives them so (notionMarkdownUnknownCopyWay).
	input.DestinationPageID = nativeIDWithDashes(strings.TrimSpace(input.DestinationPageID))
	input.Operation = strings.TrimSpace(input.Operation)
	input.PlaceAfter = nativeIDWithDashes(strings.TrimSpace(input.PlaceAfter))
	input.PlaceParent = nativeIDWithDashes(strings.TrimSpace(input.PlaceParent))
	if sourcePageID == "" || blockID == "" || input.RequestID == "" ||
		input.DestinationPageID == "" ||
		len([]rune(input.RequestID)) > maxBlockMoveRequestIDRunes ||
		len(sourcePageID) > maxWorkspacePermissionIdentifierLength ||
		len(input.DestinationPageID) > maxWorkspacePermissionIdentifierLength ||
		len(blockID) > maxGraphIdentifierLength ||
		len(input.PlaceAfter) > maxGraphIdentifierLength || len(input.PlaceParent) > maxGraphIdentifierLength {
		return blockMoveError(event, errBlockMoveInvalid, "validate")
	}
	admission, err := admitBlockMove(event.App, user.Id, sourcePageID, blockID, input)
	if err != nil {
		return blockMoveError(event, err, "admit")
	}
	response, err := blockMoveResponseForJob(event.App, admission.Job, admission.Duplicate)
	if err != nil {
		return blockMoveError(event, err, "response")
	}
	if runner, ok := event.App.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	status := http.StatusAccepted
	if response.Status == blockMoveStatusCompleted || response.Status == blockMoveStatusFailed {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readBlockMove(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	if jobID == "" {
		return blockMoveError(event, errBlockMoveInvalid, "validate-read")
	}
	job, err := loadBlockMoveJobByID(event.App, jobID, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return blockMoveError(event, errBlockMoveNotFound, "read")
	}
	if err != nil {
		return blockMoveError(event, err, "read")
	}
	if _, err := authorizeWorkspace(
		event.App, user.Id, job.WorkspaceID, permissionWorkspaceWrite,
	); err != nil {
		return blockMoveError(event, errBlockMoveNotFound, "authorize-read")
	}
	response, err := blockMoveResponseForJob(event.App, job, false)
	if err != nil {
		return blockMoveError(event, err, "response-read")
	}
	return event.JSON(http.StatusOK, response)
}

func blockMoveError(event *core.RequestEvent, err error, stage string) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteBlockMove, stage,
	); ok {
		return storageError
	}
	status, code, message, refused := blockMoveRefusal(err)
	switch {
	case refused:
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeBlockMoveNotFound, "Block move or copy job not found."
	default:
		status, code, message = http.StatusServiceUnavailable,
			errorCodeBlockMoveUnavailable, "Couldn't move or copy the blocks."
	}
	reference := newDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference, "block_move", stage,
		code, err, diagnosticSourcePosition(1),
	)
}

// blockMoveRefusal is how a block move's or copy's refusal answers,
// wherever it is refused: on its own routes, and in a content change that
// starts one, as a page's block whose body moves into the page or back
// (writeGraphError; 2026-11-16: a refused return of a page's body to its
// block answered a content change with a 500).
func blockMoveRefusal(err error) (status int, code, message string, refused bool) {
	switch {
	case errors.Is(err, errBlockMoveInvalid):
		return http.StatusBadRequest, errorCodeBlockMoveInvalid, "Check the block move or copy request.", true
	case errors.Is(err, errBlockMoveNotFound):
		return http.StatusNotFound, errorCodeBlockMoveNotFound, "Block move or copy job not found.", true
	case errors.Is(err, errBlockMoveRequestIDReused):
		return http.StatusConflict, errorCodeBlockMoveRequestIDReused, "This request ID was used for a different block operation.", true
	case errors.Is(err, errBlockMoveConflict):
		return http.StatusConflict, errorCodeBlockMoveConflict, "Blocks can't be moved or copied between these pages.", true
	}
	return 0, "", "", false
}
