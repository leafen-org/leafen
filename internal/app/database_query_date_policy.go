package app

import (
	"fmt"
	"strings"
	"time"
)

type databaseQueryRelativeDateValue struct {
	Direction string `json:"direction"`
	Count     int    `json:"count"`
	Unit      string `json:"unit"`
}

func databaseQueryDateFilter(actual, expected any, filter databaseQueryFilter, plan databaseQueryPlan) bool {
	// A date's start, or its end (its start when it has none) when the
	// filter reads "종료일", as Notion's (seen 2026-10-01).
	if filter.DatePart == "end" {
		actual = databaseQueryDateEnd(actual)
	}
	left := databaseQueryDateKey(actual, plan.Location)
	operator := filter.Operator
	if operator == "is_empty" {
		return left == ""
	}
	if operator == "is_not_empty" {
		return left != ""
	}
	if _, ok := databaseQueryRelativeOperators[operator]; ok {
		return databaseQueryRelativeDateMatches(left, operator, expected, plan.ReferenceTime, plan.Location)
	}
	if operator == "is_between" {
		// "범위 내" takes both days; with one, it runs open on the other side.
		start, end := databaseQueryDateRange(expected, plan.Location)
		return left != "" && (start == "" || left >= start) && (end == "" || left <= end)
	}
	if day, relative := databaseQueryRelativeFilterDay(expected, plan.ReferenceTime, plan.Location); relative {
		return left != "" && databaseQueryOrderMatches(operator, strings.Compare(left, day))
	}
	if wanted, timed := databaseQueryFilterMinute(expected); timed {
		at, read := databaseQueryValueMinute(actual, plan.Location)
		return read && databaseQueryOrderMatches(operator, at.Compare(wanted))
	}
	right := databaseQueryDateKey(expected, plan.Location)
	return left != "" && databaseQueryOrderMatches(operator, strings.Compare(left, right))
}

// databaseQueryOrderMatches reports whether a value that compares to the
// filter's as order (-1, 0, 1) meets the filter's condition.
func databaseQueryOrderMatches(operator string, order int) bool {
	switch operator {
	case "equals":
		return order == 0
	case "before":
		return order < 0
	case "after":
		return order > 0
	case "on_or_before":
		return order <= 0
	case "on_or_after":
		return order >= 0
	}
	return false
}

// databaseQueryFilterMinute reads a date filter's value written with a time
// of day ("2020-01-02T10:00:00+09:00", as the API takes one) as its minute,
// or false for a day alone. Such a filter compares moments to the minute, as
// Notion's does (seen 2026-11-11), and a time written with no offset is UTC.
func databaseQueryFilterMinute(value any) (time.Time, bool) {
	text, _ := value.(string)
	if !strings.Contains(text, "T") {
		return time.Time{}, false
	}
	return databaseQueryMinute(strings.TrimSpace(text), time.UTC)
}

// databaseQueryValueMinute reads a row's date as the minute it starts: a day
// alone at its midnight on the reader's clock, a time with no offset on its
// own time zone, or the reader's when it has none.
func databaseQueryValueMinute(value any, location *time.Location) (time.Time, bool) {
	if number, ok := value.(float64); ok {
		return time.UnixMilli(int64(number)).Truncate(time.Minute), true
	}
	if location == nil {
		location = time.UTC
	}
	if object, ok := value.(map[string]any); ok {
		if zone, _ := object["timeZone"].(string); zone != "" {
			if own, err := time.LoadLocation(zone); err == nil {
				location = own
			}
		}
	}
	return databaseQueryMinute(strings.TrimSpace(databaseQueryDateString(value)), location)
}

// databaseQueryMinute reads a day, or a day with a time, as its minute; a
// value with no offset is on the given clock.
func databaseQueryMinute(text string, location *time.Location) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.Truncate(time.Minute), true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, text, location); err == nil {
			return parsed.Truncate(time.Minute), true
		}
	}
	return time.Time{}, false
}

// databaseQueryDateEnd reads a date value by its end: the value with its end
// as its start, or the value itself when it has no end.
func databaseQueryDateEnd(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	end, _ := object["end"].(string)
	if strings.TrimSpace(end) == "" {
		return value
	}
	read := make(map[string]any, len(object))
	for key, item := range object {
		read[key] = item
	}
	read["start"] = end
	delete(read, "end")
	return read
}

// databaseQueryDateRange reads a "범위 내" value's days, either of which may
// be missing.
func databaseQueryDateRange(value any, location *time.Location) (string, string) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}
	day := func(key string) string {
		text, _ := object[key].(string)
		if strings.TrimSpace(text) == "" {
			return ""
		}
		return databaseQueryDateKey(text, location)
	}
	return day("start"), day("end")
}

// databaseQueryDateRangeHasDay reports a "범위 내" value with at least one day.
func databaseQueryDateRangeHasDay(value any) bool {
	start, end := databaseQueryDateRange(value, time.UTC)
	return start != "" || end != ""
}

func databaseQueryDateKey(value any, location *time.Location) string {
	if value == nil {
		return ""
	}
	if number, ok := value.(float64); ok {
		if location == nil {
			location = time.UTC
		}
		return time.UnixMilli(int64(number)).In(location).Format("2006-01-02")
	}
	raw := strings.TrimSpace(databaseQueryDateString(value))
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		if location == nil {
			location = time.UTC
		}
		return parsed.In(location).Format("2006-01-02")
	}
	if len(raw) >= 10 {
		candidate := raw[:10]
		if parsed, err := time.Parse("2006-01-02", candidate); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return raw[:min(10, len(raw))]
}

// databaseQueryRelativeFilterDays are the days a day condition's value may
// name from today instead of a date, as Notion's "사용자 지정 날짜 ⌄" choices
// (seen 2026-11-11: "1주 전" reads back through its API as before
// "one_week_ago"), each today shifted; a month clamps to its last day
// (the web's queryRelativeDate.mjs relativeFilterDay).
var databaseQueryRelativeFilterDays = map[string]struct {
	count int
	unit  string
}{
	"today": {0, "day"}, "tomorrow": {1, "day"}, "yesterday": {-1, "day"},
	"one_week_ago": {-1, "week"}, "one_week_from_now": {1, "week"},
	"one_month_ago": {-1, "month"}, "one_month_from_now": {1, "month"},
}

// databaseQueryRelativeFilterDay is the day such a value names on the
// reader's clock, or false for any other value.
func databaseQueryRelativeFilterDay(value any, reference time.Time, location *time.Location) (string, bool) {
	word, _ := value.(string)
	shift, ok := databaseQueryRelativeFilterDays[word]
	if !ok {
		return "", false
	}
	if location == nil {
		location = time.UTC
	}
	reference = reference.In(location)
	today := time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, location)
	return shiftDatabaseDateClamped(today, shift.count, shift.unit).Format("2006-01-02"), true
}

// databaseQueryFixedRelativeDates names the relative value each fixed
// operator ("this_week", "past_month") stands for, so both read alike.
var databaseQueryFixedRelativeDates = map[string]databaseQueryRelativeDateValue{
	"this_week":  {Direction: "this", Count: 1, Unit: "week"},
	"past_week":  {Direction: "past", Count: 1, Unit: "week"},
	"past_month": {Direction: "past", Count: 1, Unit: "month"},
	"past_year":  {Direction: "past", Count: 1, Unit: "year"},
	"next_week":  {Direction: "next", Count: 1, Unit: "week"},
	"next_month": {Direction: "next", Count: 1, Unit: "month"},
	"next_year":  {Direction: "next", Count: 1, Unit: "year"},
}

// databaseQueryRelativeDateMatches matches a day against a relative date, as
// Notion's (seen 2026-10-01): "이번" is the calendar day, week (from Sunday),
// month or year holding today; "지난 N" runs from N units before today to
// today and "다음 N" from today to N units after, both ends included.
func databaseQueryRelativeDateMatches(
	actualDay, operator string,
	expected any,
	reference time.Time,
	location *time.Location,
) bool {
	if location == nil {
		location = time.UTC
	}
	actual, err := time.ParseInLocation("2006-01-02", actualDay, location)
	if err != nil {
		return false
	}
	value, fixed := databaseQueryFixedRelativeDates[operator]
	if !fixed {
		if operator != "relative_to_today" {
			return false
		}
		var ok bool
		if value, ok = databaseQueryRelativeDateInput(expected); !ok {
			return false
		}
	}
	reference = reference.In(location)
	today := time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, location)
	var start, end time.Time
	switch value.Direction {
	case "this":
		start = databaseRelativeUnitStart(today, value.Unit)
		end = shiftDatabaseDateClamped(start, 1, value.Unit)
	case "past":
		start, end = shiftDatabaseDateClamped(today, -value.Count, value.Unit), today.AddDate(0, 0, 1)
	default:
		start, end = today, shiftDatabaseDateClamped(today, value.Count, value.Unit).AddDate(0, 0, 1)
	}
	return !actual.Before(start) && actual.Before(end)
}

func databaseQueryRelativeDateInput(value any) (databaseQueryRelativeDateValue, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return databaseQueryRelativeDateValue{}, false
	}
	result := databaseQueryRelativeDateValue{
		Direction: fmt.Sprint(object["direction"]),
		Unit:      fmt.Sprint(object["unit"]),
	}
	if count, ok := databaseQueryStrictNumber(object["count"]); ok {
		result.Count = int(count)
	}
	if result.Direction == "this" {
		result.Count = 1
	}
	validUnit := result.Unit == "day" || result.Unit == "week" || result.Unit == "month" || result.Unit == "year"
	validDirection := result.Direction == "this" || result.Direction == "past" || result.Direction == "next"
	return result, validUnit && validDirection && result.Count > 0
}

func databaseRelativeUnitStart(value time.Time, unit string) time.Time {
	switch unit {
	case "week":
		return value.AddDate(0, 0, -int(value.Weekday()))
	case "month":
		return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, value.Location())
	case "year":
		return time.Date(value.Year(), time.January, 1, 0, 0, 0, 0, value.Location())
	default:
		return value
	}
}

func shiftDatabaseDateClamped(value time.Time, count int, unit string) time.Time {
	if unit == "day" || unit == "week" {
		multiplier := 1
		if unit == "week" {
			multiplier = 7
		}
		return value.AddDate(0, 0, count*multiplier)
	}
	year, month := value.Year(), int(value.Month())
	if unit == "month" {
		month += count
		for month < 1 {
			year--
			month += 12
		}
		for month > 12 {
			year++
			month -= 12
		}
	} else {
		year += count
	}
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, value.Location()).Day()
	day := min(value.Day(), lastDay)
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, value.Location())
}
