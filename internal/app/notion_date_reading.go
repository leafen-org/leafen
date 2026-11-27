package app

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

// A date in a Notion request is kept as Notion keeps it (seen 2026-11-10):
// a day written "2026-03-05" as that day; any other date the request check
// takes (notionISODateValid) as a time to the minute, its seconds dropped:
// "2026-W02" is 2026-01-05T00:00, "2026-032" 2026-02-01T00:00, "2026" its
// first minute, "2026-03-05T10:20:30" 10:20. A range with one time is two
// times, a day its 00:00. The time is read in the date's zone, or at the
// offset its start, or else its end, is written with when some zone is at
// that offset then ("+09" and "+05:30" are kept, "-05:30" and "+15" are UTC,
// the time as written); both ends take it.

// notionDateEnd is one end of a date as a request writes it.
type notionDateEnd struct {
	text    string
	parts   notionISODateParts
	dayOnly bool
}

var notionDayOnlyForm = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func readNotionDateEnd(text string) (notionDateEnd, bool) {
	parts, ok := parseNotionISODate(text)
	return notionDateEnd{text: text, parts: parts, dayOnly: ok && notionDayOnlyForm.MatchString(text)}, ok
}

// keepable is an end Notion keeps a readable date for: a time outside the
// years 0 to 9999 it keeps as "Invalid DateTime".
func (end notionDateEnd) keepable() bool {
	return end.dayOnly || end.parts.hourOfToday || end.parts.year >= 0 && end.parts.year <= 9999
}

// notionDateZoneName is the zone Leafen reads a request's time zone as: US/Pacific-New,
// which Notion still takes, was Los Angeles' (Go's zone database no longer
// has it).
func notionDateZoneName(zone string) string {
	if zone == "US/Pacific-New" {
		return "America/Los_Angeles"
	}
	return zone
}

var (
	notionZoneLocationsOnce sync.Once
	notionZoneLocations     []*time.Location
)

// notionOffsetHasZone reports whether some zone a date may name is at the
// offset, in minutes east of UTC, at the moment.
func notionOffsetHasZone(offset int, moment time.Time) bool {
	notionZoneLocationsOnce.Do(func() {
		for _, name := range notionTimeZones {
			if location, err := time.LoadLocation(notionDateZoneName(name)); err == nil {
				notionZoneLocations = append(notionZoneLocations, location)
			}
		}
	})
	for _, location := range notionZoneLocations {
		if _, seconds := moment.In(location).Zone(); seconds == offset*60 {
			return true
		}
	}
	return false
}

// notionDateKept is a date's ends as Leafen keeps them: days; times written
// "2006-01-02T15:04" on the clock of the zone a date names; or, naming
// none, times written with the offset they are kept at, UTC's "Z" when
// none is, so that Leafen's own screens show them on the viewer's clock as
// Notion's do.
type notionDateKept struct {
	start, end string
	zone       string
}

// keepNotionDate keeps a date's ends as Notion does; end is nil when there
// is none and zone "" when none is named.
func keepNotionDate(start notionDateEnd, end *notionDateEnd, zone string, now time.Time) notionDateKept {
	if start.dayOnly && (end == nil || end.dayOnly) {
		kept := notionDateKept{start: start.text, zone: notionDateZoneName(zone)}
		if end != nil {
			kept.end = end.text
		}
		return kept
	}
	if zone != "" {
		kept := notionDateKept{start: start.parts.wall(now).Format("2006-01-02T15:04"), zone: notionDateZoneName(zone)}
		if end != nil {
			kept.end = end.parts.wall(now).Format("2006-01-02T15:04")
		}
		return kept
	}
	offset, given := start.parts.offset, start.parts.offsetGiven
	if !given && end != nil {
		offset, given = end.parts.offset, end.parts.offsetGiven
	}
	location := time.UTC
	if given && offset != 0 && notionOffsetHasZone(offset, start.parts.wall(now).Add(-time.Duration(offset)*time.Minute)) {
		location = time.FixedZone("", offset*60)
	}
	write := func(at time.Time) string {
		return time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), at.Minute(), 0, 0, location).Format(time.RFC3339)
	}
	kept := notionDateKept{start: write(start.parts.wall(now))}
	if end != nil {
		kept.end = write(end.parts.wall(now))
	}
	return kept
}

// notionDatePropertyFault is what Notion's API says of a date property's
// value it refuses after its request check (seen 2026-11-10), named by the
// property's raw ID: a zone with a day alone or a non-zero offset, or a
// start after its end as JavaScript reads the two (notionJSDateMillis).
func notionDatePropertyFault(rawID string, start notionDateEnd, end *notionDateEnd, zoned bool) string {
	ends := []notionDateEnd{start}
	if end != nil {
		ends = append(ends, *end)
	}
	if zoned {
		for _, each := range ends {
			if each.dayOnly {
				return rawID + " has a date that doesn't fit: with a time zone given, start and end each need a date and a time."
			}
		}
		for _, each := range ends {
			if each.parts.offset != 0 {
				return rawID + " has a date that doesn't fit: with a time zone given, start and end can't carry an offset from UTC."
			}
		}
	}
	if end != nil {
		from, fromRead := notionJSDateMillis(start.text)
		to, toRead := notionJSDateMillis(end.text)
		if fromRead && toRead && from > to {
			return rawID + " has a date range that ends before it starts."
		}
	}
	return ""
}

var notionJSDateForm = regexp.MustCompile(
	`^(\d{4}|[+-]\d{6})(?:-(\d{2})(?:-(\d{2}))?)?(?:([T ])(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d+))?)?(Z|[+-]\d{2}:\d{2})?)?$`)

// notionJSDateMillis reads text as JavaScript's Date.parse does on a server
// in UTC: the ISO forms, a date alone and a time without an offset as UTC,
// and a full date before a space and a time; any other text it reads as no
// date.
func notionJSDateMillis(text string) (int64, bool) {
	match := notionJSDateForm.FindStringSubmatch(text)
	if match == nil || match[4] == " " && match[3] == "" {
		return 0, false
	}
	number := func(text string, empty int) int {
		if text == "" {
			return empty
		}
		value, _ := strconv.Atoi(text)
		return value
	}
	year, month, day := number(match[1], 0), number(match[2], 1), number(match[3], 1)
	hour, minute, second := number(match[5], 0), number(match[6], 0), number(match[7], 0)
	fraction := (match[8] + "000")[:3]
	if match[8] == "" {
		fraction = "0"
	}
	millis := number(fraction, 0)
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if notionISOLeap(year) {
		days[1] = 29
	}
	if month < 1 || month > 12 || day < 1 || day > days[month-1] || minute > 59 || second > 59 ||
		hour > 24 || hour == 24 && (minute != 0 || second != 0 || millis != 0) {
		return 0, false
	}
	at := time.Date(year, time.Month(month), day, hour, minute, second, millis*int(time.Millisecond), time.UTC)
	if zone := match[9]; zone != "" && zone != "Z" {
		hours, _ := strconv.Atoi(zone[1:3])
		minutes, _ := strconv.Atoi(zone[4:6])
		offset := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
		if zone[0] == '+' {
			offset = -offset
		}
		at = at.Add(offset)
	}
	return at.UnixMilli(), true
}
