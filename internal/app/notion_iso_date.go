package app

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// notionISODateValid reports whether text is a date Notion's request check
// takes as ISO 8601 (seen 2026-11-10): "2026", "2026-01", "2026-W01",
// "2026-001", "20260101T1000", "2026-01-01 10:00", "2026-01-01T24:00",
// "+002026-01-01", offsets such as "+09", "+0900", "+09:99" and "Z" after a
// time, and two digits alone, an hour, with or without an offset ("20",
// "20Z") are dates; "2026-1-1", "26-01-01", "+0020", "25", "2027-W53",
// "2026-02-30", "T25:00", "T10:60", "T10.5", an offset after a day alone, a
// separator with no time after it, a leading space, a lowercase "t", two
// spaces and "" are not.
func notionISODateValid(text string) bool {
	_, ok := parseNotionISODate(text)
	return ok
}

// notionISODateParts is what a date Notion's request check takes names.
type notionISODateParts struct {
	year, month, day int // the day, a day of the year or of a week read as one
	// hourOfToday is two digits alone: an hour of the day the request is
	// read on (seen 2026-11-10: "20" is today at 20:00, "24" tomorrow's 00:00).
	hourOfToday  bool
	clock        bool
	hour, minute int // to the minute: Notion keeps no seconds
	offsetGiven  bool
	offset       int // minutes east of UTC, as written
}

var (
	notionISOZoneStart = regexp.MustCompile(`[Z+-]`)
	notionISODayForm   = regexp.MustCompile(`^-?(?:(\d{3})|(\d{2})(?:-?(\d{2}))?|W(\d{2})(?:-?(\d))?|)$`)
	notionISOClockForm = regexp.MustCompile(`^(\d{2})(?::?(\d{2})(?::?(\d{2})(?:[.,]\d+)?)?)?$`)
	notionISOZoneForm  = regexp.MustCompile(`^([+-])(\d{2})(?::?(\d{2}))?$`)
	notionISOHourForm  = regexp.MustCompile(`^(\d{2})(Z|[+-]\d{2}(?::?\d{2})?)?$`)
)

func parseNotionISODate(text string) (notionISODateParts, bool) {
	parts := notionISODateParts{}
	if match := notionISOHourForm.FindStringSubmatch(text); match != nil {
		parts.hourOfToday, parts.clock = true, true
		parts.hour, _ = strconv.Atoi(match[1])
		return parts, parts.hour <= 24 && parts.readZone(match[2])
	}
	date, clock, zone, ok := notionISOParts(text)
	if !ok {
		return parts, false
	}
	year, rest, ok := notionISOYear(date)
	if !ok || !parts.readDay(year, rest) {
		return parts, false
	}
	if clock != "" && !parts.readClock(clock) {
		return parts, false
	}
	return parts, parts.readZone(zone)
}

// notionISOParts splits text into its date, its time and its offset: at a
// "T" or a space (one at most) that a time follows, the offset from the
// time's first "Z", "+" or "-". A date holding ":" is a time without one;
// an offset needs a time.
func notionISOParts(text string) (string, string, string, bool) {
	if strings.Count(text, "T")+strings.Count(text, " ") > 1 {
		return "", "", "", false
	}
	date, clock := text, ""
	if at := strings.IndexAny(text, "T "); at >= 0 {
		date, clock = text[:at], text[at+1:]
		if clock == "" {
			return "", "", "", false
		}
	}
	if date == "" || strings.ContainsAny(date, ":Zz") {
		return "", "", "", false
	}
	zone := ""
	if at := notionISOZoneStart.FindStringIndex(clock); at != nil {
		clock, zone = clock[:at[0]], clock[at[0]:]
		if clock == "" {
			return "", "", "", false
		}
	}
	return date, clock, zone, true
}

// notionISOYear reads the year a date starts with: four digits, or six
// with a sign.
func notionISOYear(date string) (int, string, bool) {
	digits := func(text string) bool { return strings.Trim(text, "0123456789") == "" }
	switch {
	case len(date) >= 4 && digits(date[:4]):
		year, _ := strconv.Atoi(date[:4])
		return year, date[4:], true
	case len(date) >= 7 && (date[0] == '+' || date[0] == '-') && digits(date[1:7]):
		year, _ := strconv.Atoi(date[:7])
		return year, date[7:], true
	}
	return 0, "", false
}

func notionISOLeap(year int) bool {
	return year%400 == 0 || year%4 == 0 && year%100 != 0
}

// notionISOWeeks is how many ISO weeks a year has: 53 when it starts on a
// Thursday, or on a Wednesday in a leap year.
func notionISOWeeks(year int) int {
	first := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).Weekday()
	if first == time.Thursday || first == time.Wednesday && notionISOLeap(year) {
		return 53
	}
	return 52
}

// readDay reads what follows the year: a month and day, a day of the year,
// a week and its day, or nothing; a JavaScript date reaches the year.
func (parts *notionISODateParts) readDay(year int, rest string) bool {
	if year < -271820 || year > 275759 {
		return false
	}
	match := notionISODayForm.FindStringSubmatch(rest)
	if match == nil {
		return false
	}
	unit := func(text string) int {
		if text == "" {
			return 1
		}
		value, _ := strconv.Atoi(text)
		return value
	}
	parts.year = year
	if match[4] != "" {
		week, weekday := unit(match[4]), unit(match[5])
		if week < 1 || week > notionISOWeeks(year) || weekday < 1 || weekday > 7 {
			return false
		}
		january4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
		monday := january4.AddDate(0, 0, -((int(january4.Weekday()) + 6) % 7))
		day := monday.AddDate(0, 0, (week-1)*7+weekday-1)
		parts.year, parts.month, parts.day = day.Year(), int(day.Month()), day.Day()
		return true
	}
	dayOfYear, month, day := unit(match[1]), unit(match[2]), unit(match[3])
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	yearDays := 365
	if notionISOLeap(year) {
		days[1], yearDays = 29, 366
	}
	if month < 1 || month > 12 || day < 1 || day > days[month-1] || dayOfYear < 1 || dayOfYear > yearDays {
		return false
	}
	parts.month, parts.day = month, day
	if match[1] != "" {
		parts.month, parts.day = 1, dayOfYear
	}
	return true
}

// readClock reads a time: hours, minutes and seconds with a fraction on the
// seconds, 24:00 the end of a day.
func (parts *notionISODateParts) readClock(clock string) bool {
	match := notionISOClockForm.FindStringSubmatch(clock)
	if match == nil {
		return false
	}
	unit := func(text string) int {
		value, _ := strconv.Atoi(text)
		return value
	}
	hours, minutes, seconds := unit(match[1]), unit(match[2]), unit(match[3])
	if hours == 24 && (minutes != 0 || seconds != 0) || hours > 24 || minutes > 59 || seconds > 59 {
		return false
	}
	parts.clock, parts.hour, parts.minute = true, hours, minutes
	return true
}

// readZone reads an offset: "Z", or a sign and two digits of hours, with
// two of minutes or none, taken whatever they add up to (seen 2026-11-10:
// "+09:60" is +10:00).
func (parts *notionISODateParts) readZone(zone string) bool {
	if zone == "" {
		return true
	}
	parts.offsetGiven = true
	if zone == "Z" {
		return true
	}
	match := notionISOZoneForm.FindStringSubmatch(zone)
	if match == nil {
		return false
	}
	hours, _ := strconv.Atoi(match[2])
	minutes, _ := strconv.Atoi(match[3])
	parts.offset = hours*60 + minutes
	if match[1] == "-" {
		parts.offset = -parts.offset
	}
	return true
}

// wall is the date and time the parts name, to the minute, as UTC: a date
// alone is its 00:00, an hour of today today's on now's UTC day.
func (parts notionISODateParts) wall(now time.Time) time.Time {
	if parts.hourOfToday {
		now = now.UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), parts.hour, 0, 0, 0, time.UTC)
	}
	return time.Date(parts.year, time.Month(parts.month), parts.day, parts.hour, parts.minute, 0, 0, time.UTC)
}
