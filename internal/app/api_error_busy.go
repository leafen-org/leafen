package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A request that failed because the database stayed busy — a write that
// waited out its time for the write connection or for SQLite's lock
// (sqlite_write_gate.go) — wrote none of what it waited to write: it is
// answered as one the server had no turn for (serverBusy), 503 with
// Retry-After, whatever failure the route would have said, and the app sends
// it again once the write holding the database ends (apiTransport.ts), as
// Notion's API answers such a write (notion_compatibility_routes.go). A route
// that says busy in its own words keeps them. One that kept a transaction of
// its own before is answered as it was: sending it again would do that part
// twice — the same rule as a write the server's stop ended
// (request_write_stop.go). 2026-11-18: an app's window opened on a file
// stayed on "앱을 여는 중…" while a slow write held the database and then
// failed, its session refused with 500 LEAFEN_APP_PLATFORM_FAILED.
func busyRequestAnswer(event *core.RequestEvent, status int, code, message string, cause error) (int, string, string) {
	if requestKeptWrite(event) {
		return status, code, message
	}
	return busyAnswer(event.Response.Header(), status, code, message, cause)
}

// busyAnswer is that rule for a failure that kept nothing it wrote, answered
// with header: a 500 whose cause is a database that stayed busy is the
// shared busy answer, 503 with Retry-After.
func busyAnswer(header http.Header, status int, code, message string, cause error) (int, string, string) {
	if status != http.StatusInternalServerError || !isSQLiteBusyError(cause) {
		return status, code, message
	}
	header.Set("Retry-After", requestAdmissionRetryAfter)
	return http.StatusServiceUnavailable, errorCodeServerBusy, serverBusyMessage
}

// requestKeptWrite is whether one of a request's transactions was kept.
func requestKeptWrite(event *core.RequestEvent) bool {
	note, _ := event.Get(requestWriteNoteKey).(*requestWriteNote)
	return note != nil && note.committed.Load()
}
