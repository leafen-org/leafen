package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// Some addresses carry a credential in their query — a collaboration
// socket's session token, an app hook's token, a one-time ticket — because
// a browser socket or an outside service can't send a header. The request
// log records the address after the handler has read it, so this replaces
// those values first: the log keeps that there was a token, not the token.
var requestLogRedactedQuery = []string{"token", "ticket"}

func redactRequestLogQuery(event *core.RequestEvent) error {
	err := event.Next()
	if event.Request == nil || event.Request.URL == nil || event.Request.URL.RawQuery == "" {
		return err
	}
	query := event.Request.URL.Query()
	changed := false
	for _, name := range requestLogRedactedQuery {
		if query.Has(name) {
			query.Set(name, "redacted")
			changed = true
		}
	}
	if changed {
		event.Request.URL.RawQuery = query.Encode()
	}
	return err
}
