package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The day a view falls on in analytics — a page's, a database row page's, a
// database opened alone's, a member's in workspace analytics, and the day a
// search is counted on — is the viewer's: the date on their clock
// (personLocation: the zone they chose, or their device's, kept while
// "위치를 사용하여 자동으로 시간대 설정" is on, else the server's) when they
// opened it. Notion keeps its page analytics so (seen 2026-11-26 in Seoul,
// in two tabs: views at 03:47, 03:50 and 04:04, still the 25th in UTC, came
// back from getPageAnalytics as ds "2026-11-26"; neither the visit nor the
// read names a zone, and the account's time zone is Asia/Seoul). A visitor
// without an account, on a public page, has the server's clock. The moments
// kept beside a day — when it was viewed, until when it is kept — stay in UTC.
//
// A reader's range ends on their own today, on the same clock
// (pageAnalyticsToday), so a view in Seoul before 09:00 is today's, not
// yesterday's as UTC's day had it (2026-11-26). Notion's chart lays its days
// out by UTC, so before 09:00 in Seoul it ends on yesterday at 0 and leaves
// today's views off though its total counts them (seen twice 2026-11-26);
// Leafen's chart ends on the reader's today instead.

// pageAnalyticsNow is now in UTC, for the moments kept, and on the viewer's
// clock, whose date is the day the view falls on.
func pageAnalyticsNow(app core.App, viewerID string) (time.Time, time.Time) {
	now := time.Now().UTC()
	return now, now.In(personLocation(app, viewerID))
}

// pageAnalyticsDayOf is the day a moment on the viewer's clock falls on.
func pageAnalyticsDayOf(onClock time.Time) string {
	return onClock.Format(time.DateOnly)
}

// pageAnalyticsToday is a reader's today, on their clock.
func pageAnalyticsToday(app core.App, readerID string, now time.Time) time.Time {
	return now.In(personLocation(app, readerID))
}
