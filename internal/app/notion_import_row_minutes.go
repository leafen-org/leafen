package app

import "time"

// notionImportRowMinutes keeps the order of the rows Notion made in one
// minute. Notion's API gives a row's created time to the minute (seen
// 2026-11-12: six rows made a second apart all came as 11:33:00.000Z) and
// lists a database's rows newest first, by the time it keeps to the
// millisecond. The rows of one minute take created times in the order it
// lists them, the newest at the minute's end: the first 59 a second apart
// (…:59.999, …:58.999 … …:01.999), the rest in its first second a
// millisecond apart (…:00.999 down) — SQL reads a created time to the
// second, so ORDER BY createdTime keeps their order as Notion's own SQL,
// which has the real seconds, does (2026-11-16: a millisecond apart, the
// 80 rows of a synthetic import all read …:59 and the sort lost their
// order). A view with no row placed, which shows the newest first, and a
// sort by created time show them as Notion does, and each keeps the minute
// Notion gave. A connection's API gives no row's real second.
// A time finer than the minute, which Notion doesn't give but a source may,
// stays as it is for the first row that has it; rows listed after it with
// the very same time step back a millisecond each, within its minute, so
// rows that tie keep the order they are listed in too, rather than the one
// their ids happen to give (databaseNaturalOrder breaks a created-time tie
// by id). It runs on across a database's pages of rows, kept with the
// import's place in them.
type notionImportRowMinutes struct {
	Minute string `json:"minute,omitempty"`
	Count  int    `json:"count,omitempty"`
	Exact  string `json:"exact,omitempty"`
	Ties   int    `json:"ties,omitzero"`
}

// created is the created time a row of the run takes.
func (minutes *notionImportRowMinutes) created(at time.Time) time.Time {
	at = at.UTC()
	if minutes == nil {
		return at
	}
	if !at.Equal(at.Truncate(time.Minute)) {
		exact := at.Format(time.RFC3339Nano)
		if exact != minutes.Exact {
			minutes.Exact, minutes.Ties = exact, 0
			return at
		}
		minutes.Ties++
		room := int(at.Sub(at.Truncate(time.Minute)) / time.Millisecond)
		return at.Add(-time.Duration(min(minutes.Ties, room)) * time.Millisecond)
	}
	minute := at.Format(time.RFC3339)
	if minute != minutes.Minute {
		minutes.Minute, minutes.Count = minute, 0
	}
	newer := minutes.Count
	minutes.Count++
	if newer < 59 {
		return at.Add(time.Duration(59-newer)*time.Second + 999*time.Millisecond)
	}
	// Past its first second's milliseconds, rows share its first one.
	return at.Add(time.Duration(max(999-(newer-59), 0)) * time.Millisecond)
}
