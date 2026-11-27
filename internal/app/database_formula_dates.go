package app

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Formula dates are read in the viewer's time zone: a day without a time
// is that calendar day, a time without a zone is a wall-clock time there,
// and an instant shows its hour, day and week as it falls there. A time
// with its own offset, or a date whose time zone is named, keeps that clock:
// adding to it counts on it, and it is written at its own offset. Results
// keep the input's precision: a day stays a day, and a time on the viewer's
// clock is written as its UTC instant. As in Notion, parseDate() and a day
// given hours read on the viewer's clock.

type databaseFormulaMoment struct {
	at      time.Time
	hasTime bool
	// own is the clock the time keeps, nil for the viewer's; zone names it
	// when it is a named time zone.
	own  *time.Location
	zone string
}

const databaseFormulaMaxMilliseconds = 8.64e15

var databaseFormulaISODate = regexp.MustCompile(
	`^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?(?:\.(\d+))?(Z|[+-]\d{2}:?\d{2})?)?$`,
)

// databaseFormulaZones holds the time zones dates have named, so each zone's
// rules are read once. A case-insensitive file system also loads a name's
// case variants, so it stops taking zones past the few hundred that exist.
var (
	databaseFormulaZones     sync.Map
	databaseFormulaZoneCount atomic.Int32
)

const databaseFormulaMaxZones = 1024

// databaseFormulaLocation loads a named time zone. "Local", the server's own
// zone, is none a date can name.
func databaseFormulaLocation(zone string) (*time.Location, bool) {
	if zone == "" || zone == "Local" {
		return nil, false
	}
	if cached, ok := databaseFormulaZones.Load(zone); ok {
		return cached.(*time.Location), true
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, false
	}
	if databaseFormulaZoneCount.Load() < databaseFormulaMaxZones && databaseFormulaZoneCount.Add(1) <= databaseFormulaMaxZones {
		databaseFormulaZones.Store(zone, location)
	}
	return location, true
}

func (environment *databaseFormulaEnvironment) parseDate(text string) (databaseFormulaMoment, bool) {
	return environment.parseZonedDate(text, "")
}

// momentOf reads a value as a date: a date with the time zone it names,
// other values from their text.
func (environment *databaseFormulaEnvironment) momentOf(value any) (databaseFormulaMoment, bool) {
	if date, ok := value.(databaseFormulaDate); ok {
		return environment.parseZonedDate(date.Text, date.Zone)
	}
	return environment.parseDate(databaseFormulaText(value))
}

// parseZonedDate reads a date's first end; a named time zone keeps its time
// on that zone's clock, a wall-clock time among them.
func (environment *databaseFormulaEnvironment) parseZonedDate(text, zone string) (databaseFormulaMoment, bool) {
	raw, _, _ := strings.Cut(text, "/")
	match := databaseFormulaISODate.FindStringSubmatch(databaseFormulaTrim(raw))
	if match == nil {
		return databaseFormulaMoment{}, false
	}
	number := func(index int) int {
		value, _ := strconv.Atoi(match[index])
		return value
	}
	year, month, day := number(1), number(2), number(3)
	hour, minute, second := number(4), number(5), number(6)
	if month < 1 || month > 12 || day < 1 || day > databaseFormulaDaysIn(year, time.Month(month)) ||
		hour > 23 || minute > 59 || second > 59 {
		return databaseFormulaMoment{}, false
	}
	if match[4] == "" {
		return databaseFormulaMoment{at: time.Date(year, time.Month(month), day, 0, 0, 0, 0, environment.location)}, true
	}
	moment := databaseFormulaMoment{hasTime: true}
	location := environment.location
	if offsetText := match[8]; offsetText != "" {
		offset := 0
		if offsetText != "Z" {
			digits := strings.ReplaceAll(offsetText[1:], ":", "")
			hours, _ := strconv.Atoi(digits[:2])
			minutes, _ := strconv.Atoi(digits[2:])
			offset = hours*3600 + minutes*60
			if offsetText[0] == '-' {
				offset = -offset
			}
			moment.own = time.FixedZone("", offset)
		}
		location = time.FixedZone("", offset)
	}
	if named, ok := databaseFormulaLocation(zone); ok {
		if match[8] == "" {
			location = named
		}
		moment.own, moment.zone = named, zone
	}
	// A time keeps whole minutes (databaseFormulaMinute).
	at := time.Date(year, time.Month(month), day, hour, minute, 0, 0, location)
	moment.at = at.In(environment.location)
	return moment, true
}

func databaseFormulaDaysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// A result is a date while a time's UTC year is 0 to 9999 and a day is one
// JavaScript counts (databaseFormulaInvalidDate).
func (moment databaseFormulaMoment) valid() bool {
	if moment.hasTime {
		year := moment.at.UTC().Year()
		return year >= 0 && year <= 9999
	}
	return math.Abs(float64(moment.at.Unix())) <= databaseFormulaMaxMilliseconds/1000
}

func (moment databaseFormulaMoment) text() string {
	if !moment.hasTime {
		return databaseFormulaDayText(moment.at.Year(), int(moment.at.Month()), moment.at.Day())
	}
	if moment.own != nil {
		return moment.at.In(moment.own).Format("2006-01-02T15:04:05-07:00")
	}
	return moment.at.UTC().Format("2006-01-02T15:04:05Z")
}

func (moment databaseFormulaMoment) value() any {
	if !moment.valid() {
		return databaseFormulaDate{Text: databaseFormulaInvalidDate}
	}
	return databaseFormulaDate{Text: moment.text(), Zone: moment.zone}
}

// clock is the time on the clock it keeps.
func (moment databaseFormulaMoment) clock() time.Time {
	if moment.own != nil {
		return moment.at.In(moment.own)
	}
	return moment.at
}

// databaseFormulaDateUnit is a duration unit as Notion's date functions read
// it: a year, quarter, month, week, day, hour, minute, second or
// millisecond, one or more, in any case and with nothing around it; any
// other fails the formula (seen 2026-11-14: " days" fails, "Days" is days),
// as the editor's dateUnit (formulaDates.ts).
func databaseFormulaDateUnit(value any) (string, error) {
	if value == nil {
		return "days", nil
	}
	unit := strings.ToLower(databaseFormulaText(value))
	if !strings.HasSuffix(unit, "s") {
		unit += "s"
	}
	switch unit {
	case "years", "quarters", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds":
		return unit, nil
	}
	return "", errDatabaseFormulaInvalid
}

// databaseFormulaClockUnits are the milliseconds of a unit dateAdd() and
// dateBetween() count on the clock.
var databaseFormulaClockUnits = map[string]float64{"hours": 36e5, "minutes": 6e4, "seconds": 1e3, "milliseconds": 1}

// databaseFormulaAddMonths keeps the day of the month, or the month's last
// day when it is shorter, and the time of day.
func databaseFormulaAddMonths(at time.Time, months int) time.Time {
	year, month, day := at.Date()
	first := time.Date(year, month+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	day = min(day, databaseFormulaDaysIn(first.Year(), first.Month()))
	return time.Date(first.Year(), first.Month(), day, at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), at.Location())
}

// databaseFormulaCasualDays is how many days the fraction of a unit counts,
// as Notion's dateAdd does: whole units go on the calendar, the rest as time
// with a month of 30 days, a quarter of 91 and a year of 365 (seen
// 2026-10-10: 1.5 months from January 1 is February 16).
var databaseFormulaCasualDays = map[string]float64{"years": 365, "quarters": 91, "months": 30, "weeks": 7, "days": 1}

func (environment *databaseFormulaEnvironment) dateAdd(value, amountValue, unitValue any, sign float64) (any, error) {
	unit, err := databaseFormulaDateUnit(unitValue)
	if err != nil {
		return nil, err
	}
	moment, ok := environment.momentOf(value)
	if !ok && environment.unreadableDate(value) {
		return databaseFormulaDate{Text: databaseFormulaInvalidDate}, nil
	}
	// An empty amount leaves the date empty, as Notion's (seen 2026-10-10).
	if !ok || amountValue == nil || amountValue == "" {
		return "", nil
	}
	amount := sign * databaseFormulaFiniteNumber(amountValue)
	if !databaseFormulaCountable(amount, unit) {
		return databaseFormulaDate{Text: databaseFormulaInvalidDate}, nil
	}
	wholeAmount := math.Trunc(amount)
	whole := int(wholeAmount)
	// A time counts on the clock it keeps.
	at := moment.clock()
	switch unit {
	case "years":
		at = databaseFormulaAddMonths(at, whole*12)
	case "quarters":
		at = databaseFormulaAddMonths(at, whole*3)
	case "months":
		at = databaseFormulaAddMonths(at, whole)
	case "weeks":
		at = at.AddDate(0, 0, whole*7)
	case "hours", "minutes", "seconds", "milliseconds":
		// Times keep whole minutes, as Notion's (2.5 minutes adds two).
		at = databaseFormulaMinute(time.UnixMilli(at.UnixMilli() + int64(math.Round(amount*databaseFormulaClockUnits[unit]))))
		moment.hasTime = true
	default:
		at = at.AddDate(0, 0, whole)
	}
	if rest := amount - wholeAmount; rest != 0 && databaseFormulaClockUnits[unit] == 0 {
		at = at.Add(time.Duration(math.Round(rest*databaseFormulaCasualDays[unit]*float64(24*time.Hour/time.Millisecond))) * time.Millisecond)
		// A date without a time stays at the day's start (1.5 days from
		// January 1 is January 2, 00:00).
		if !moment.hasTime {
			at = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
		} else {
			at = databaseFormulaMinute(at)
		}
	}
	moment.at = at.In(environment.location)
	return moment.value(), nil
}

// workday moves a date to a working day of a calendar
// (business_calendar.go) — Leafen's, beside Notion's functions: the next one
// for nextWorkday, a count of them on for addWorkdays — on the clock the
// date keeps, its time of day as it was. A calendar Leafen hasn't, or a count
// of more than ten years, gives no date.
func (environment *databaseFormulaEnvironment) workday(value, countValue, calendarValue any, adding bool) any {
	moment, ok := environment.momentOf(value)
	if !ok {
		return ""
	}
	code := ""
	if calendarValue != nil {
		code = databaseFormulaText(calendarValue)
	}
	calendar, found := businessCalendarNamed(code)
	if !found {
		return ""
	}
	at := moment.clock()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	moved := day
	if adding {
		if countValue == nil || countValue == "" {
			return ""
		}
		count := math.Trunc(databaseFormulaFiniteNumber(countValue))
		if math.Abs(count) > 3660 {
			return ""
		}
		moved = calendar.addWorkdays(day, int(count))
	} else {
		moved = calendar.nextWorkday(day)
	}
	moment.at = at.AddDate(0, 0, int(moved.Sub(day)/(24*time.Hour))).In(environment.location)
	return moment.value()
}

// databaseFormulaWallMilliseconds is a time's wall clock read as UTC, so
// day differences follow the calendar across daylight saving changes.
func databaseFormulaWallMilliseconds(at time.Time) float64 {
	year, month, day := at.Date()
	return float64(time.Date(year, month, day, at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), time.UTC).UnixMilli())
}

// databaseFormulaMonthDiff is how many months, with a fraction, left is
// after right.
func databaseFormulaMonthDiff(left, right time.Time) float64 {
	if left.Day() < right.Day() {
		return -databaseFormulaMonthDiff(right, left)
	}
	whole := (right.Year()-left.Year())*12 + int(right.Month()-left.Month())
	anchor := databaseFormulaAddMonths(left, whole)
	target := databaseFormulaWallMilliseconds(right)
	anchorAt := databaseFormulaWallMilliseconds(anchor)
	var adjust float64
	if target-anchorAt < 0 {
		previous := databaseFormulaWallMilliseconds(databaseFormulaAddMonths(left, whole-1))
		adjust = (target - anchorAt) / (anchorAt - previous)
	} else {
		next := databaseFormulaWallMilliseconds(databaseFormulaAddMonths(left, whole+1))
		adjust = (target - anchorAt) / (next - anchorAt)
	}
	return -(float64(whole) + adjust)
}

func (environment *databaseFormulaEnvironment) dateBetween(leftValue, rightValue, unitValue any) (any, error) {
	unit, err := databaseFormulaDateUnit(unitValue)
	if err != nil {
		return nil, err
	}
	left, leftOK := environment.momentOf(leftValue)
	right, rightOK := environment.momentOf(rightValue)
	if environment.unreadableDate(leftValue) || environment.unreadableDate(rightValue) {
		return math.NaN(), nil
	}
	if !leftOK || !rightOK {
		return float64(0), nil
	}
	var difference float64
	switch unit {
	case "years":
		difference = databaseFormulaMonthDiff(left.at, right.at) / 12
	case "quarters":
		difference = databaseFormulaMonthDiff(left.at, right.at) / 3
	case "months":
		difference = databaseFormulaMonthDiff(left.at, right.at)
	case "weeks":
		difference = (databaseFormulaWallMilliseconds(left.at) - databaseFormulaWallMilliseconds(right.at)) / 6048e5
	case "hours", "minutes", "seconds", "milliseconds":
		difference = float64(left.at.UnixMilli()-right.at.UnixMilli()) / databaseFormulaClockUnits[unit]
	default:
		difference = (databaseFormulaWallMilliseconds(left.at) - databaseFormulaWallMilliseconds(right.at)) / 864e5
	}
	return math.Trunc(difference) + 0, nil
}

// databaseFormulaDatesOnly are Notion's date functions by the places of the
// values they take as a date or a number: an empty one given there fails the
// formula, as Notion's (seen 2026-11-14 through its API: dateAdd(prop("d"), 1,
// "days"), dateAdd(now(), prop("n"), "days"), dateBetween(), dateRange(),
// dateStart(), dateEnd(), timestamp(), fromTimestamp(), formatDate() and
// year() … minute() of an empty property are no value, empty() of them too,
// so another formula reading one fails as well), as numbers do
// (databaseFormulaNumbersOnly) and as the editor's (formulaDates.ts
// DATES_ONLY).
// databaseFormulaReadsDate are the functions that give a number that is no
// number of a date they can't read (databaseFormulaInvalidDate).
var databaseFormulaReadsDate = map[string]bool{
	"timestamp": true, "year": true, "month": true, "date": true, "day": true, "week": true, "hour": true, "minute": true,
}

var databaseFormulaDatesOnly = map[string][]int{
	"dateAdd": {0, 1}, "dateSubtract": {0, 1}, "dateBetween": {0, 1}, "dateRange": {0, 1},
	"dateStart": {0}, "dateEnd": {0}, "timestamp": {0}, "fromTimestamp": {0}, "formatDate": {0},
	"year": {0}, "month": {0}, "date": {0}, "day": {0}, "week": {0}, "hour": {0}, "minute": {0},
}

func (environment *databaseFormulaEnvironment) dateFunction(name string, values []any) (any, bool, error) {
	argument := func(index int) any { return databaseFormulaArgument(values, index) }
	for _, index := range databaseFormulaDatesOnly[name] {
		if index < len(values) && values[index] == nil {
			return nil, true, errDatabaseFormulaInvalid
		}
	}
	parsed := func() (databaseFormulaMoment, bool) {
		return environment.momentOf(argument(0))
	}
	invalid := databaseFormulaDate{Text: databaseFormulaInvalidDate}
	if databaseFormulaReadsDate[name] && environment.unreadableDate(argument(0)) {
		return math.NaN(), true, nil
	}
	part := func(read func(time.Time) int) any {
		moment, ok := parsed()
		if !ok {
			return float64(0)
		}
		return float64(read(moment.at))
	}
	switch name {
	case "now":
		return databaseFormulaMoment{at: databaseFormulaMinute(environment.now).In(environment.location), hasTime: true}.value(), true, nil
	case "today":
		year, month, day := environment.now.In(environment.location).Date()
		return databaseFormulaMoment{at: time.Date(year, month, day, 0, 0, 0, 0, environment.location)}.value(), true, nil
	case "dateAdd":
		result, err := environment.dateAdd(argument(0), argument(1), argument(2), 1)
		return result, true, err
	case "dateSubtract":
		result, err := environment.dateAdd(argument(0), argument(1), argument(2), -1)
		return result, true, err
	case "dateBetween":
		result, err := environment.dateBetween(argument(0), argument(1), argument(2))
		return result, true, err
	case "nextWorkday":
		return environment.workday(argument(0), nil, argument(1), false), true, nil
	case "addWorkdays":
		return environment.workday(argument(0), argument(1), argument(2), true), true, nil
	case "dateRange":
		return environment.dateRange(argument(0), argument(1)), true, nil
	case "dateStart":
		if moment, ok := parsed(); ok {
			return moment.value(), true, nil
		}
		if environment.unreadableDate(argument(0)) {
			return invalid, true, nil
		}
		return "", true, nil
	case "parseDate":
		// Text is read on the viewer's clock, as Notion reads it; text that
		// is no date gives an empty date, as Notion's (seen 2026-11-14:
		// parseDate("abc") is {"type":"date","date":null} and empty).
		if moment, ok := parsed(); ok {
			moment.own, moment.zone = nil, ""
			return moment.value(), true, nil
		}
		return nil, true, nil
	case "dateEnd":
		date, isDate := argument(0).(databaseFormulaDate)
		if !isDate {
			date = databaseFormulaDate{Text: databaseFormulaText(argument(0))}
		}
		if _, end, ranged := strings.Cut(date.Text, "/"); ranged && databaseFormulaTrim(end) != "" {
			date.Text = end
		}
		if moment, ok := environment.momentOf(date); ok {
			return moment.value(), true, nil
		}
		if isDate {
			return invalid, true, nil
		}
		return "", true, nil
	case "timestamp":
		if moment, ok := parsed(); ok {
			return float64(moment.at.UnixMilli()), true, nil
		}
		return float64(0), true, nil
	case "fromTimestamp":
		milliseconds := math.Trunc(databaseFormulaFiniteNumber(argument(0)))
		if math.Abs(milliseconds) > databaseFormulaMaxMilliseconds {
			return invalid, true, nil
		}
		at := databaseFormulaMinute(time.UnixMilli(int64(milliseconds))).In(environment.location)
		return databaseFormulaMoment{at: at, hasTime: true}.value(), true, nil
	case "formatDate":
		moment, ok := parsed()
		if !ok {
			return "", true, nil
		}
		layout := databaseFormulaText(argument(1))
		if layout == "" {
			layout = "YYYY-MM-DD"
		}
		if len(values) > 2 {
			// A zone it doesn't know fails it, as Notion's (seen 2026-11-14:
			// empty(formatDate(now(), "YYYY", "Nowhere/Zone")) is no value).
			location, known := databaseFormulaNamedZone(databaseFormulaText(argument(2)))
			if !known {
				return nil, true, errDatabaseFormulaInvalid
			}
			moment.at = moment.at.In(location)
		}
		return databaseFormulaFormatDate(moment, layout, environment.locale()), true, nil
	case "year":
		return part(func(at time.Time) int { return at.Year() }), true, nil
	case "month":
		return part(func(at time.Time) int { return int(at.Month()) }), true, nil
	case "date":
		return part(func(at time.Time) int { return at.Day() }), true, nil
	case "day":
		return part(func(at time.Time) int { return (int(at.Weekday())+6)%7 + 1 }), true, nil
	case "week":
		return part(func(at time.Time) int { _, week := at.ISOWeek(); return week }), true, nil
	case "hour":
		return part(func(at time.Time) int { return at.Hour() }), true, nil
	case "minute":
		return part(func(at time.Time) int { return at.Minute() }), true, nil
	}
	return nil, false, nil
}

var databaseFormulaFixedZone = regexp.MustCompile(`^UTC([+-])(\d{1,2})(?::(\d{2}))?$`)

// databaseFormulaNamedZone reads the zone formatDate() is given, "UTC+1" or
// "Asia/Seoul"; a name it doesn't know fails formatDate(), which then has no
// value, as Notion's (seen 2026-11-04, its API 2026-11-14).
func databaseFormulaNamedZone(zone string) (*time.Location, bool) {
	if match := databaseFormulaFixedZone.FindStringSubmatch(zone); match != nil {
		hours, _ := strconv.Atoi(match[2])
		minutes, _ := strconv.Atoi(match[3])
		seconds := (hours*60 + minutes) * 60
		if match[1] == "-" {
			seconds = -seconds
		}
		return time.FixedZone(zone, seconds), true
	}
	if zone == "" {
		return nil, false
	}
	location, err := time.LoadLocation(zone)
	return location, err == nil
}
