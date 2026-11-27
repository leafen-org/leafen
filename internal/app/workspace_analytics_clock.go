package app

import (
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
)

// Workspace analytics reads every day on one clock, its reader's
// (workspaceAnalyticsLocation): the window ends on their today, a view falls
// on its viewer's day (page_analytics_clock.go), the screen's "created in the
// last N days" starts at their midnight (workspaceAnalyticsParams), and an
// edit falls on the day it was made on their clock — by the offset their zone
// had at that moment, so in New York an edit at 00:30 a week before the
// clocks went back stays on its own day rather than taking today's offset and
// the day before (2026-11-26).

// workspaceAnalyticsOffset is an offset the reader's zone has from a moment on.
type workspaceAnalyticsOffset struct {
	FromMillis int64
	Seconds    int
}

// maxWorkspaceAnalyticsOffsets bounds the changes a window reads; a zone
// changes its clock twice a year at most, and a window is a year at most.
const maxWorkspaceAnalyticsOffsets = 16

// workspaceAnalyticsOffsets are the offsets the zone of `from` has between
// `from` and `to`, each from the moment it began, the first from `from`.
func workspaceAnalyticsOffsets(from, to time.Time) []workspaceAnalyticsOffset {
	offsets := []workspaceAnalyticsOffset{}
	for at := from; ; {
		_, seconds := at.Zone()
		offsets = append(offsets, workspaceAnalyticsOffset{FromMillis: at.UnixMilli(), Seconds: seconds})
		_, end := at.ZoneBounds()
		if end.IsZero() || !end.After(at) || !end.Before(to) || len(offsets) == maxWorkspaceAnalyticsOffsets {
			return offsets
		}
		at = end
	}
}

// workspaceAnalyticsDaySQL is the day ("YYYY-MM-DD") a column of Unix
// milliseconds falls on, on the window's clock, and the parameters it binds.
func workspaceAnalyticsDaySQL(column string, window workspaceAnalyticsWindow) (string, dbx.Params) {
	params := dbx.Params{}
	offset := "0"
	for index, change := range window.Offsets {
		name := "dayOffset" + strconv.Itoa(index)
		params[name] = change.Seconds
		if index == 0 {
			offset = "{:" + name + "}"
			continue
		}
		since := "dayOffsetFrom" + strconv.Itoa(index)
		params[since] = change.FromMillis
		offset = "CASE WHEN " + column + " >= {:" + since + "} THEN {:" + name + "} ELSE " + offset + " END"
	}
	return "strftime('%Y-%m-%d', " + column + " / 1000 + " + offset + ", 'unixepoch')", params
}
