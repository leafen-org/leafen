package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A request that leaves out an input its operation needs is refused naming
// it, where its handler checks the input is given before anything else reads
// it (2026-11-26, a real agent: a name change without requestId was told
// "Check the profile." alone; a sweep of every operation sent empty found
// forty more told "Check the …", "That code isn't one this server gave" and
// the like, with nothing saying what was left out). The check stands in
// the handler's own steps — if input.X == "" { return refuseMissingInput(…) }
// — so the operation's description (scripts/native-operation-inputs) reads
// it as needed, and the refusal says the input and what it takes, with the
// input in the answer's fields (api_error_details.go).

// refuseMissingInput answers a request without an input its operation needs:
// "Check <field>: <what it takes>", the field named required.
func refuseMissingInput(event *core.RequestEvent, code, field, takes string) error {
	apiErrorAbout(event, nil, apiErrorField{Name: field, Problem: apiErrorFieldMissing})
	return apiError(event, http.StatusBadRequest, code, "Check "+field+": "+takes)
}

// refusedMissingInput is that refusal as an error, for a step that answers with
// the refusal its cause holds (api_error_refused_not_changed.go).
func refusedMissingInput(code, field, takes string) error {
	return refusedFor(http.StatusBadRequest, code, "Check "+field+": "+takes, apiErrorField{Name: field, Problem: apiErrorFieldMissing})
}
