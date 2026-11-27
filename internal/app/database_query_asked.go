package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// databaseQueryAsAsked reads a query sent to the database query route as the
// one asking means it: an app's names as it wrote them
// (app_platform_data_names.go), and, when it names no time zone, its days —
// "today", "this week", a day's bounds — on the asker's clock
// (person_clock.go) rather than UTC's: an agent asking through leafen_call at
// 03:00 in Seoul for today's rows was given yesterday's (2026-11-26). The
// screen names its viewer's zone; an installed app's token names its own or
// reads UTC, as the app's data runner does.
func databaseQueryAsAsked(event *core.RequestEvent, userID string, input *databaseQueryRequest) {
	appQueryInMadeNames(event.App, event.Get(userKeyAuthorityKey), input)
	if strings.TrimSpace(input.TimeZone) == "" && !appTokenCall(event) {
		input.TimeZone = personLocation(event.App, userID).String()
	}
}
