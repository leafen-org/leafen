package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A content graph request's refusal, and how a graph route answers an error:
// the refusal's own status and code, the store's busy or unavailable state,
// a data loss prevention decision, or the route's fallback.

type graphRequestError struct {
	status         int
	code           string
	message        string
	reference      string
	operation      string
	stage          string
	causeCode      string
	sourcePosition string
	// reason is what to fix, the field as a content change names it and why
	// (request_refusal.go), which the content graph's own route says after
	// message (told); a route that made the change itself, from a body of its
	// own, an import or Markdown, says message alone, as the operations it
	// names aren't the caller's.
	reason string
	// precondition marks a conflict that is a state the change waits on —
	// a row restored first, a wiki undone first — not a change made
	// meanwhile: Leafen's own routes answer it 409 like any conflict, and
	// Notion's API as the validation error Notion gives such a refusal (that
	// the block is archived and is restored first), keeping 409
	// conflict_error for data collisions.
	precondition bool
}

// told is the refusal's words with what to fix, as the content graph's
// mutation route answers a change its caller wrote.
func (e *graphRequestError) told() string {
	return refusalMessage(e.message, e.reason)
}

func (e *graphRequestError) Error() string {
	return e.code
}

func graphInputError(code, message string) error {
	return &graphRequestError{status: http.StatusBadRequest, code: code, message: message}
}

// graphRefused is a content change refused with its words and what to fix:
// "Check the content order." and "position is empty" (request_refusal.go).
func graphRefused(code, message, format string, args ...any) error {
	return &graphRequestError{
		status: http.StatusBadRequest, code: code, message: message, reason: fmt.Sprintf(format, args...),
	}
}

// graphRefusalAt says where in the request a refused field is — at
// "operations[2].block." the reason "position is empty" names
// operations[2].block.position — or leaves err as it is.
func graphRefusalAt(err error, at string) error {
	var refusal *graphRequestError
	if !errors.As(err, &refusal) || refusal.reason == "" {
		return err
	}
	placed := *refusal
	placed.reason = at + refusal.reason
	return &placed
}

func graphConflictError(code, message string) error {
	return &graphRequestError{status: http.StatusConflict, code: code, message: message}
}

// graphPreconditionError is a conflict with a state the change needs first,
// said as what to do first.
func graphPreconditionError(code, message string) error {
	return &graphRequestError{status: http.StatusConflict, code: code, message: message, precondition: true}
}

func graphNotFoundError() error {
	return &graphRequestError{
		status:  http.StatusNotFound,
		code:    errorCodePageNotFound,
		message: "Page not found.",
	}
}

// graphBlockNotFoundError is a block the page it names doesn't hold.
func graphBlockNotFoundError() error {
	return &graphRequestError{
		status:  http.StatusNotFound,
		code:    errorCodeBlockNotFound,
		message: "Block not found on this page.",
	}
}

const (
	graphDatabaseBusyCauseCode       = "GRAPH_DATABASE_BUSY"
	graphStorageUnavailableCauseCode = "GRAPH_STORAGE_UNAVAILABLE"
)

func graphStorageAPIError(event *core.RequestEvent, err error, stage string) (error, bool) {
	switch sqliteOperationalErrorClass(err) {
	case sqliteOperationalBusy:
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeGraphDatabaseBusy,
			"The page content store is locked by another operation.", "content_graph", stage,
			graphDatabaseBusyCauseCode, err,
		), true
	case sqliteOperationalUnavailable:
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeGraphStorageUnavailable,
			"The page content store is temporarily unavailable.", "content_graph", stage,
			graphStorageUnavailableCauseCode, err,
		), true
	default:
		return nil, false
	}
}

// writeGraphChangeError answers a content change its caller wrote, as
// writeGraphError does, saying what to fix where the refusal names it.
func writeGraphChangeError(event *core.RequestEvent, err error, stage, fallbackCode, fallbackMessage string) error {
	var requestError *graphRequestError
	if errors.As(err, &requestError) && requestError.reason != "" && requestError.reference == "" {
		return apiError(event, requestError.status, requestError.code, requestError.told())
	}
	return writeGraphError(event, err, stage, fallbackCode, fallbackMessage)
}

func writeGraphError(event *core.RequestEvent, err error, stage, fallbackCode, fallbackMessage string) error {
	if errors.Is(err, context.Canceled) {
		return apiErrorAndFlush(event, http.StatusServiceUnavailable, fallbackCode, fallbackMessage)
	}
	if storageError, ok := graphStorageAPIError(event, err, stage); ok {
		return storageError
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "content-graph-"+stage)
	}
	var requestError *graphRequestError
	if errors.As(err, &requestError) {
		if requestError.reference != "" {
			return writeAPIErrorWithReference(
				event, requestError.status, requestError.code, requestError.message,
				requestError.reference, requestError.operation, requestError.stage,
				requestError.causeCode, requestError, requestError.sourcePosition,
			)
		}
		return apiError(event, requestError.status, requestError.code, requestError.message)
	}
	if status, code, message, refused := blockMoveRefusal(err); refused {
		return apiError(event, status, code, message)
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, fallbackCode, fallbackMessage,
		"content_graph", stage, fallbackCode, err,
	)
}
