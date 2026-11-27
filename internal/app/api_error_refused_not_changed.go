package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A change refused for what it is — a place in another workspace, an access
// the person lacks, a setting that keeps it — is answered as that refusal,
// with its own status, code and words, whatever the route's error switch
// would make of it: never as a change made meanwhile (409 "changed. Try
// again."), which sends its caller to retry what can't succeed, and never as
// a failure (2026-11-26: an agent moving a row into a database of another
// workspace was told "The database row changed. Try again." and learned why
// only from another tool). A check that refuses so returns refusedFor(…) in
// place of its route's conflict error; the one error writer
// (writeAPIErrorWithReference) finds it in the cause the route answers with
// (apiErrorFrom, apiErrorWithCause, writeAPIErrorWithReference) and answers
// it, so a route's switch that doesn't know it, sending it to its 500, still
// says it. Its words follow the agent requests' shape: "Check <field>: …"
// for what the request named, the reason and the way that does it.

type refusedForReason struct {
	status  int
	code    string
	message string
	// fields name what in the request the refusal is about, given beside the
	// fields the writer reads from the body (api_error_details.go).
	fields []apiErrorField
}

func (refusal *refusedForReason) Error() string { return refusal.code }

// refusedFor is a refusal answered with this status, code and message, and
// these fields named beside them.
func refusedFor(status int, code, message string, fields ...apiErrorField) error {
	return &refusedForReason{status: status, code: code, message: message, fields: fields}
}

// refusalAnswer is the answer of a failure whose cause holds such a refusal:
// the refusal's; any other keeps the route's.
func refusalAnswer(event *core.RequestEvent, status int, code, message string, cause error) (int, string, string) {
	var refusal *refusedForReason
	if cause == nil || !errors.As(cause, &refusal) {
		return status, code, message
	}
	if len(refusal.fields) > 0 && event != nil {
		apiErrorAbout(event, nil, refusal.fields...)
	}
	return refusal.status, refusal.code, refusal.message
}
