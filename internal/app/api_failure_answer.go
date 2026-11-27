package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// failureAnswer is what a failed request is answered, whatever shape its
// answer takes: one refused as its page not found while an operation holds
// the page is held (page_held_failure_answer.go), one its client left is the
// client's (499), one a stopping server ended is said so (server_stopping.go),
// and one whose write the stop ended with none of it kept
// (request_write_stop.go) or whose database stayed busy (api_error_busy.go)
// is told busy — 503 with Retry-After, the shared busy code and words — so
// the app sends it again. The error writer answers
// every JSON failure through it (diagnostic.go); a route that answers in a
// shape of its own — an empty body, plain text, a page, an OAuth error —
// goes through answeredFailure.
func failureAnswer(event *core.RequestEvent, status int, code, message string, cause error) (int, string, string) {
	// A page an operation holds a while is held, not gone (page_held_failure_answer.go).
	status, code, message = heldPageFailureAnswer(event, status, code, message)
	status = requestFailureStatus(event, status)
	status, code, message = stoppedRequestWriteAnswer(event, status, code, message)
	return busyRequestAnswer(event, status, code, message, cause)
}

// answeredFailure is failureAnswer for a failure a route answers in a shape
// of its own, logged as the error writer logs one — with its cause's class,
// at sourcePosition — since that shape has no log line of its own. The
// route answers the status, and the code and words where its shape has
// room for them; told busy, they are the shared busy code and words
// (2026-11-18: an app's frame, an app endpoint, an OAuth token request and
// the Notion migration's format page answered a busy database 500 with no
// cause in the log).
func answeredFailure(
	event *core.RequestEvent, status int, code, message string, cause error, sourcePosition string,
) (int, string, string) {
	answered, answeredCode, answeredMessage := failureAnswer(event, status, code, message, cause)
	logAPIDiagnostic(
		event, answered, answeredCode, newDiagnosticReference(), diagnosticOperation(event), "response",
		code, cause, sourcePosition,
	)
	return answered, answeredCode, answeredMessage
}

// failedStatus is the status of a failure answered with an empty body,
// through answeredFailure — 500, or 503 with Retry-After for a database
// that stayed busy.
func failedStatus(event *core.RequestEvent, status int, code string, cause error) int {
	answered, _, _ := answeredFailure(event, status, code, "", cause, diagnosticSourcePosition(1))
	return answered
}

// answeredBusy is whether a failure's answer is the shared busy one.
func answeredBusy(code string) bool {
	return code == errorCodeServerBusy
}
