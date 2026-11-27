package app

import (
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A time the server writes for a person — in a sentence they read, a mail, a
// moved comment's first line, or a time an agent is handed to tell them — is
// on that person's clock: their own time zone (Settings › 언어 및 시간), else
// the server's (personLocation), its offset written so it reads the same
// anywhere, and never UTC by default (2026-11-16: a moved Notion comment
// opened "2026-11-16 10:49 UTC" on a Korean screen, and an agent told its
// person a sign-in code held "until 13:05 UTC"). Times made for programs —
// Notion's own API shapes, the web client's fields it shows in the reader's
// zone, the server's log — keep their formats.
//
// The Notion-shaped MCP tools (notion-*) hand an agent theirs so too — a
// date's time, a page's creation and last edit, the moment a fetch read, a
// comment's, a search hit's, a version's, a download's end, in fetch, a
// query's rows and SQL alike — in Notion's MCP's form, to the millisecond,
// on the clock of the person the connection works as (mcpAuthority.Clock):
// "2026-11-24T13:00:00.000+09:00", "Z" on a UTC clock. Notion's MCP gives
// them in UTC (seen 2026-09-30, 2026-11-11, 2026-11-13), and an agent read a
// 13:00 appointment in Seoul, given as 04:00Z, as 04:00 (2026-11-24); the
// same moment with the person's offset reads right to a person and to a
// program alike. A day stays a day, a time on no clock as written, and a
// time sent with any offset or Z is that moment. SQL reads them as the
// clock's wall-clock time (databaseSQLTimeLayout), and a query's days — a
// date filter's, "this week" and the rest, a search's date range — are the
// clock's days too, so a filter by the person's day finds what is on it.
// So are the days a person or an agent names by a word without a zone:
// "today" at a view's range end (addDateCondition), a database query that
// names no zone (databaseQueryAsAsked), a template's @today
// (newDatabaseTemplateUse), a starter's first example date
// (shiftDatabaseStarterDates), a fetched page's formulas
// (loadDatabaseRowPageComputedValuesFor), and an automation's view, target
// and formulas on the clock of the person it runs as
// (interactiveAutomationFormulas.clock) — never UTC's, which in Seoul is
// yesterday until 09:00 (2026-11-26). The day a view or a search falls on in
// analytics is its viewer's, and an analytics range, a workspace digest's week
// among them, ends on its reader's today (page_analytics_clock.go).

// personClockMillisLayout is a time a Notion-shaped tool gives.
const personClockMillisLayout = "2006-01-02T15:04:05.000Z07:00"

// personClockMillis is a time a Notion-shaped tool gives, on the clock.
func personClockMillis(at time.Time, location *time.Location) string {
	return at.In(location).Format(personClockMillisLayout)
}

// personClockMillisOf puts a stored RFC 3339 time on the clock as a
// Notion-shaped tool gives it; anything else stays as it is.
func personClockMillisOf(text string, location *time.Location) string {
	if at, err := time.Parse(time.RFC3339Nano, strings.Replace(strings.TrimSpace(text), " ", "T", 1)); err == nil {
		return personClockMillis(at, location)
	}
	return text
}

// personClockWritten is a date part an agent writes, as the moment it names:
// a time without an offset on the person's clock — "2026-11-25T10:00:00",
// tomorrow's 10 a.m. to a person in Seoul, was read as UTC's and shown at
// 19:00 (2026-11-24), as Notion's API reads one (seen 2026-11-24:
// "…T10:00:00.000+00:00"); a time with an offset or Z, and a day, as written.
// SQL's own form of the time, the person's wall clock with a space, is read
// so too: SQL's "2026-11-26 09:30:00" written back as SQL gave it was read
// as UTC's, nine hours on in Seoul (2026-11-26).
func personClockWritten(text string, location *time.Location) string {
	trimmed := strings.TrimSpace(text)
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04"} {
		if wall, err := time.ParseInLocation(layout, trimmed, location); err == nil {
			return personClockMillis(wall, location)
		}
	}
	return text
}

// personClockDateValue is a formula's or rollup's value as a Notion-shaped
// tool gives it: a date's start and end with their offset on the clock, and
// no zone beside them, in a list of values too; anything else as it is.
func personClockDateValue(value any, location *time.Location) any {
	switch typed := value.(type) {
	case []any:
		listed := make([]any, len(typed))
		for index, item := range typed {
			listed[index] = personClockDateValue(item, location)
		}
		return listed
	case map[string]any:
		date := make(map[string]any, len(typed))
		restamped := false
		for key, item := range typed {
			date[key] = item
			if text, ok := item.(string); ok && (key == "start" || key == "end") {
				if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
					date[key], restamped = personClockMillisOf(text, location), true
				}
			}
		}
		if restamped {
			delete(date, "timeZone")
			delete(date, "time_zone")
		}
		return date
	}
	return value
}

// clock is the clock a connection's tools write times on: its person's,
// loaded with it (authenticateMCPRequest); UTC for one made without it, a
// Leafen app's own (app_platform_data_runner.go).
func (authority mcpAuthority) clock() *time.Location {
	if authority.Clock == nil {
		return time.UTC
	}
	return authority.Clock
}

// personLocation is a person's own time zone, else the server's.
func personLocation(app core.App, userID string) *time.Location {
	return personZoneLocation(loadAccountTimeZone(app, userID))
}

// personZoneLocation is the zone a person chose, kept by name, else the
// server's — for a zone kept apart from the person's settings, such as a
// moved archive's map (workspace_import_archive_clock.go).
func personZoneLocation(zone string) *time.Location {
	if zone != "" {
		if location, err := time.LoadLocation(zone); err == nil {
			return location
		}
	}
	return time.Local
}

// personClockLayout writes a time in a sentence: to the minute, with its offset.
const personClockLayout = "2006-01-02 15:04 (UTC-07:00)"

// personClockText is a time as a sentence writes it, on the person's clock.
func personClockText(at time.Time, location *time.Location) string {
	return at.In(location).Format(personClockLayout)
}

// personClockStamp is a time handed to an agent: RFC 3339 on the person's
// clock, the same moment as in UTC with the person's offset.
func personClockStamp(at time.Time, location *time.Location) string {
	return at.In(location).Format(time.RFC3339)
}

// personClockZone names the person's clock for an agent: the zone they chose,
// or the server's offset where they chose none.
func personClockZone(location *time.Location, at time.Time) string {
	if location == time.Local {
		return at.In(location).Format("UTC-07:00")
	}
	return location.String()
}

// personClockRestamp puts an RFC 3339 time on the person's clock; anything
// else, an empty time among them, stays as it is.
func personClockRestamp(text string, location *time.Location) string {
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return personClockStamp(at, location)
	}
	return text
}
