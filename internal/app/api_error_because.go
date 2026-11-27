package app

import "github.com/pocketbase/pocketbase/core"

// A refusal that comes from what another server answered — a backup place
// that refused its sign-in, say — carries that answer (reason, with no
// secret in it) and its common cause (backup_failure_reason.go) beside its
// code, for the screen to show in its language and an agent to read
// (2026-11-22: a save refused by a WebDAV server showed only "couldn't
// write there").
const apiErrorBecauseKey = "leafenAPIErrorBecause"

type apiErrorBecauseOf struct {
	reason, cause string
}

// apiErrorBecause says, for the error a route answers next, what the other
// server answered and its common cause.
func apiErrorBecause(event *core.RequestEvent, reason, cause string) {
	event.Set(apiErrorBecauseKey, apiErrorBecauseOf{reason: reason, cause: cause})
}

// apiErrorBecauseSaid is what the route said of its next error, once.
func apiErrorBecauseSaid(event *core.RequestEvent) (string, string) {
	if event == nil {
		return "", ""
	}
	said, _ := event.Get(apiErrorBecauseKey).(apiErrorBecauseOf)
	event.Set(apiErrorBecauseKey, nil)
	return said.reason, said.cause
}
