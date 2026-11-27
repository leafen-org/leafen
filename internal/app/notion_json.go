package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionInvalid carries why a /v1 request is refused, as Notion's own
// validation errors say it; errors.Is still finds sentinel.
func notionInvalid(sentinel error, reason string) error {
	return notionAdminInvalid(sentinel, reason)
}

// notionPageValidationError says a refusal of a request's content as Notion's
// API does: as a validation error, but a file_upload it can't find as no
// object found (notionFileUploadMissing).
func notionPageValidationError(event *core.RequestEvent, message string) error {
	message = strings.TrimSpace(message)
	if strings.HasPrefix(message, notionFileUploadMissingPrefix) {
		return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound, message)
	}
	return notionCompatibilityError(event, http.StatusBadRequest, notionErrorValidation, message)
}

// notionGraphConflictError answers a content change's conflict as Notion's
// API does: a state the change needs first as a validation error, a change
// made meanwhile as conflict_error, one that met another as a write saved at
// the same moment that trying again gets past (seen 2026-11-14: ten of
// twelve writes to one page at once were answered so, where Leafen asked
// for the latest content again; notionConflictWhileSaving), as a write that
// waited out the database is answered (notionCompatibilityErrorWithData).
func notionGraphConflictError(event *core.RequestEvent, refusal *graphRequestError) error {
	if refusal.precondition {
		return notionPageValidationError(event, refusal.message)
	}
	message := refusal.message
	if _, met := notionGraphConflictsMet[refusal.code]; met {
		message = notionConflictWhileSaving
	}
	return notionCompatibilityError(event, http.StatusConflict, "conflict_error", message)
}

// notionGraphConflictsMet are the conflicts of a change that met another
// made meanwhile: the page's content, its state, a block's text, a view's
// settings or a dependency changed between the read and the write, which
// trying again gets past.
var notionGraphConflictsMet = map[string]struct{}{
	errorCodeGraphRevisionConflict: {}, errorCodeGraphPageCursorConflict: {}, errorCodeGraphTextTargetConflict: {},
	errorCodeGraphViewConflict: {}, errorCodeDatabaseDependencyStale: {},
}
