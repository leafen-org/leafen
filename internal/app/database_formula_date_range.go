package app

import "time"

// dateRange is a range from one date to another, on one clock as a date
// property's range is: its start's when the start has a time, else its
// end's. Two days make a range of days. Given a time at one end and a day at
// the other, the day takes a time on that clock — a start the day's first
// minute, an end its last, 23:59 — and a time kept on another clock keeps
// its wall-clock time on the range's, as Notion's (seen 2026-11-15 through
// its API in UTC and on screen in Seoul: dateRange(now(), d) of a day d ends
// at 23:59 of d, dateRange(d, now()) starts at 00:00, dateRange(t, d) of a
// 10:30 +09:00 time ends at 23:59 +09:00, dateRange(now(), t) of a 10:30
// New York time ends at 10:30 on the viewer's clock). The editor's is the
// same (formulaDates.ts dateRange).
func (environment *databaseFormulaEnvironment) dateRange(startValue, endValue any) any {
	start, startOK := environment.momentOf(startValue)
	end, endOK := environment.momentOf(endValue)
	// An end that can't be read stays Notion's invalid date, the other as it is.
	if (startOK || environment.unreadableDate(startValue)) && (endOK || environment.unreadableDate(endValue)) && (!startOK || !endOK) {
		text := func(moment databaseFormulaMoment, ok bool) string {
			if !ok {
				return databaseFormulaInvalidDate
			}
			return moment.text()
		}
		zone := start.zone
		if !startOK {
			zone = end.zone
		}
		return databaseFormulaDate{Text: text(start, startOK) + "/" + text(end, endOK), Zone: zone}
	}
	if !startOK || !endOK || !start.valid() || !end.valid() {
		return ""
	}
	if start.hasTime || end.hasTime {
		clock := start
		if !start.hasTime {
			clock = end
		}
		start = environment.onRangeClock(start, clock, 0, 0)
		end = environment.onRangeClock(end, clock, 23, 59)
		if !start.valid() || !end.valid() {
			return ""
		}
	}
	// A range names one time zone, its clock's, as a date property does.
	return databaseFormulaDate{Text: start.text() + "/" + end.text(), Zone: start.zone}
}

// onRangeClock is one end of a range on the range's clock: a day at the
// given time of day, a time at its wall-clock time there.
func (environment *databaseFormulaEnvironment) onRangeClock(moment, clock databaseFormulaMoment, hour, minute int) databaseFormulaMoment {
	if moment.hasTime && moment.own == clock.own {
		return moment
	}
	wall := moment.clock()
	if !moment.hasTime {
		wall = time.Date(wall.Year(), wall.Month(), wall.Day(), hour, minute, 0, 0, time.UTC)
	}
	location := environment.location
	if clock.own != nil {
		location = clock.own
	}
	at := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), location)
	return databaseFormulaMoment{at: at.In(environment.location), hasTime: true, own: clock.own, zone: clock.zone}
}
