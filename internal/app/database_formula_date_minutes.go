package app

import "time"

// databaseFormulaMinute is a time at the start of its minute. Formula times
// keep whole minutes, as Notion's: a time read from text, now() and
// fromTimestamp() drop their seconds and milliseconds (seen 2026-11-15
// through its API and on screen: timestamp(now()) % 60000 is 0,
// formatDate(now(), "ss.SSS") "00.000", fromTimestamp(1700000012345) is
// 22:13 UTC and its timestamp() 1699999980000, parseDate("2026-11-20T10:30:45")
// 10:30, so dateBetween() of times 12 seconds apart in seconds is 0), as
// dateAdd() of hours, minutes and seconds keeps them, and as the editor's
// (formulaDates.ts).
func databaseFormulaMinute(at time.Time) time.Time {
	milliseconds := at.UnixMilli()
	return time.UnixMilli(milliseconds - (milliseconds%60000+60000)%60000).In(at.Location())
}
