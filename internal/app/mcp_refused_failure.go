package app

import (
	"errors"
	"net/http"
	"strings"
)

// A call whose tool met a refusal with its reason — a value the write
// checks refused, a change the content refused for what it asks, a request
// refused for a reason (api_error_refused_not_changed.go), an account that
// can't change it — and passed it on as its own failure says the refusal,
// at the end of the call, whichever of the tools' many failure sites it came
// through: told "Leafen couldn't … Try once more", an agent sent the same
// call again and again, and found the cause only by taking things out one at
// a time (2026-11-26: a column with one odd option refused every write of
// it so). A failure for what may pass — busy, stopped, a page held — is said
// as before; any other failure keeps its words.

// mcpRefusedFailure is a call's failure (failure, its tool's error) as the
// refusal its cause holds, when its words are a failure's (mcpStopFailures);
// any other is as it was.
func mcpRefusedFailure(failure error, text string) string {
	code, _, _ := strings.Cut(strings.TrimSpace(text), ":")
	if !mcpStopFailures[code] || failure == nil {
		return text
	}
	if refused := mcpRefusalOf(failure); refused != nil {
		return refused.Error()
	}
	return text
}

// mcpRefusalOf is the refusal a failure's cause holds, as the tool error it
// says itself as; nil for none.
func mcpRefusalOf(failure error) error {
	var answered *refusedForReason
	var refusal *requestRefusal
	var graph *graphRequestError
	var notion *notionDatabaseRowPageRequestError
	switch {
	case errors.As(failure, &answered) && strings.TrimSpace(answered.message) != "":
		if answered.status == http.StatusForbidden || answered.status == http.StatusNotFound || answered.status == http.StatusUnauthorized {
			return mcpDenied("%s", answered.message)
		}
		return mcpInvalid("%s", answered.message)
	case errors.As(failure, &refusal) && strings.TrimSpace(refusal.reason) != "" && !mcpRefusalMayPass(refusal.kind):
		return mcpInvalid("%s", refusal.reason)
	case errors.As(failure, &graph) && graph.status == http.StatusBadRequest && strings.TrimSpace(graph.message) != "":
		return mcpGraphReason(errMCPToolInvalid, graph)
	case errors.As(failure, &notion) && strings.TrimSpace(notion.message) != "":
		return mcpInvalid("%s", notion.message)
	case isPermissionDenied(failure):
		return errMCPCantChange
	}
	return nil
}

// mcpRefusalMayPass is whether a refusal's kind is a change met meanwhile —
// a conflict, a revision changed — which a call made again on what is there
// now may pass, so it keeps its failure's words.
func mcpRefusalMayPass(kind error) bool {
	words := strings.ToLower(kind.Error())
	return strings.Contains(words, "conflict") || strings.Contains(words, "changed")
}
