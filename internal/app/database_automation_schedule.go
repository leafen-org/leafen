package app

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const databaseAutomationScheduleDateLayout = "2006-01-02"

type databaseAutomationScheduleDate struct {
	Year    int
	Month   time.Month
	Day     int
	Ordinal int64
}

func parseDatabaseAutomationScheduleDate(value string) (databaseAutomationScheduleDate, bool) {
	parsed, err := time.Parse(databaseAutomationScheduleDateLayout, value)
	if err != nil || parsed.Format(databaseAutomationScheduleDateLayout) != value {
		return databaseAutomationScheduleDate{}, false
	}
	return databaseAutomationScheduleDate{
		Year: parsed.Year(), Month: parsed.Month(), Day: parsed.Day(),
		Ordinal: parsed.Unix() / int64((24*time.Hour)/time.Second),
	}, true
}

// databaseAutomationScheduleMidnight is Notion's "자정 전후" (seen
// 2026-10-24): the window from midnight to 3am a recurring automation runs
// in once, its first time; it runs at the window's start.
const databaseAutomationScheduleMidnight = "midnight"

func parseDatabaseAutomationScheduleTime(value string) (int, int, bool) {
	if value == databaseAutomationScheduleMidnight {
		return 0, 0, true
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func databaseAutomationScheduleLocalMinute(
	location *time.Location,
	date databaseAutomationScheduleDate,
	hour, minute int,
) (time.Time, bool) {
	naive := time.Date(date.Year, date.Month, date.Day, hour, minute, 0, 0, time.UTC)
	offsets := map[int]struct{}{}
	for _, delta := range []time.Duration{-36 * time.Hour, -12 * time.Hour, 0, 12 * time.Hour, 36 * time.Hour} {
		_, offset := naive.Add(delta).In(location).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make([]time.Time, 0, len(offsets))
	for offset := range offsets {
		candidate := naive.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(location)
		if local.Year() == date.Year && local.Month() == date.Month && local.Day() == date.Day &&
			local.Hour() == hour && local.Minute() == minute {
			candidates = append(candidates, candidate.UTC())
		}
	}
	if len(candidates) == 0 {
		return time.Time{}, false
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].Before(candidates[right]) })
	return candidates[0], true
}

// databaseAutomationScheduleIntervalLimit is the largest repeat interval
// each frequency admits.
func databaseAutomationScheduleIntervalLimit(frequency string) int {
	switch frequency {
	case databaseAutomationScheduleFrequencyDaily:
		return databaseAutomationDailyIntervalDays
	case databaseAutomationScheduleFrequencyWeekly:
		return databaseAutomationWeeklyIntervalWeeks
	case databaseAutomationScheduleFrequencyMonthly:
		return databaseAutomationMonthlyIntervalMonths
	case databaseAutomationScheduleFrequencyYearly:
		return databaseAutomationYearlyIntervalYears
	}
	return 0
}

// normalizeDatabaseAutomationScheduleTrigger orders the chosen weekdays so
// equal schedules store the same trigger.
func normalizeDatabaseAutomationScheduleTrigger(trigger *databaseAutomationTrigger) {
	trigger.Frequency = strings.TrimSpace(trigger.Frequency)
	trigger.Time = strings.TrimSpace(trigger.Time)
	trigger.TimeZone = strings.TrimSpace(trigger.TimeZone)
	trigger.StartsOn = strings.TrimSpace(trigger.StartsOn)
	trigger.EndsOn = strings.TrimSpace(trigger.EndsOn)
	if len(trigger.Weekdays) > 0 {
		weekdays := append([]int(nil), trigger.Weekdays...)
		sort.Ints(weekdays)
		trigger.Weekdays = weekdays
	}
}

func validDatabaseAutomationScheduleWeekdays(weekdays []int, minimum, maximum int) bool {
	if len(weekdays) < minimum || len(weekdays) > maximum {
		return false
	}
	seen := map[int]bool{}
	for index, weekday := range weekdays {
		if weekday < 0 || weekday > 6 || seen[weekday] || index > 0 && weekdays[index-1] > weekday {
			return false
		}
		seen[weekday] = true
	}
	return true
}

func databaseAutomationScheduleDaysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// databaseAutomationScheduleMonthDay is the day a monthly schedule runs in
// one month, or 0 when that month has no such weekday (a fifth weekday).
func databaseAutomationScheduleMonthDay(trigger databaseAutomationTrigger, year int, month time.Month) int {
	days := databaseAutomationScheduleDaysIn(year, month)
	if trigger.MonthDay > 0 {
		return min(trigger.MonthDay, days)
	}
	firstWeekday := int(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).Weekday())
	first := 1 + (trigger.Weekdays[0]-firstWeekday+7)%7
	if trigger.MonthWeek == 5 {
		return first + 7*((days-first)/7)
	}
	day := first + 7*(trigger.MonthWeek-1)
	if day > days {
		return 0
	}
	return day
}

func databaseAutomationScheduleOccursOn(
	trigger databaseAutomationTrigger,
	start, date databaseAutomationScheduleDate,
) bool {
	if date.Ordinal < start.Ordinal {
		return false
	}
	interval := trigger.Interval
	switch trigger.Frequency {
	case databaseAutomationScheduleFrequencyDaily:
		return (date.Ordinal-start.Ordinal)%int64(interval) == 0
	case databaseAutomationScheduleFrequencyWeekly:
		weekday := int(time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).Weekday())
		startWeekday := int(time.Date(start.Year, start.Month, start.Day, 0, 0, 0, 0, time.UTC).Weekday())
		chosen := false
		for _, candidate := range trigger.Weekdays {
			chosen = chosen || candidate == weekday
		}
		weeks := (date.Ordinal - int64(weekday) - (start.Ordinal - int64(startWeekday))) / 7
		return chosen && weeks%int64(interval) == 0
	case databaseAutomationScheduleFrequencyMonthly:
		months := (date.Year-start.Year)*12 + int(date.Month-start.Month)
		return months%interval == 0 && date.Day == databaseAutomationScheduleMonthDay(trigger, date.Year, date.Month)
	case databaseAutomationScheduleFrequencyYearly:
		years := date.Year - start.Year
		return years%interval == 0 && date.Month == start.Month &&
			date.Day == min(start.Day, databaseAutomationScheduleDaysIn(date.Year, start.Month))
	}
	return false
}

// databaseAutomationScheduleSearchDays bounds the search for the next run:
// the longest gap is ten years between yearly runs.
const databaseAutomationScheduleSearchDays = 11 * 366

func nextDatabaseAutomationScheduleRun(
	trigger databaseAutomationTrigger,
	after time.Time,
) (time.Time, bool, error) {
	if err := validateDatabaseAutomationScheduleTrigger(trigger); err != nil {
		return time.Time{}, false, err
	}
	if after.IsZero() {
		return time.Time{}, false, errors.New("database automation schedule requires a current instant")
	}
	location, _ := time.LoadLocation(trigger.TimeZone)
	start, _ := parseDatabaseAutomationScheduleDate(trigger.StartsOn)
	var end databaseAutomationScheduleDate
	hasEnd := false
	if trigger.EndsOn != "" {
		end, hasEnd = parseDatabaseAutomationScheduleDate(trigger.EndsOn)
	}
	afterLocal := after.In(location)
	afterDate, _ := parseDatabaseAutomationScheduleDate(afterLocal.Format(databaseAutomationScheduleDateLayout))
	hour, minute, _ := parseDatabaseAutomationScheduleTime(trigger.Time)
	ordinal := max(start.Ordinal, afterDate.Ordinal)
	for limit := ordinal + databaseAutomationScheduleSearchDays; ordinal <= limit; ordinal++ {
		if hasEnd && ordinal > end.Ordinal {
			return time.Time{}, false, nil
		}
		dateAt := time.Unix(ordinal*int64((24*time.Hour)/time.Second), 0).UTC()
		date := databaseAutomationScheduleDate{
			Year: dateAt.Year(), Month: dateAt.Month(), Day: dateAt.Day(), Ordinal: ordinal,
		}
		if !databaseAutomationScheduleOccursOn(trigger, start, date) {
			continue
		}
		candidate, exists := databaseAutomationScheduleLocalMinute(location, date, hour, minute)
		if exists && candidate.After(after) {
			return candidate, true, nil
		}
	}
	return time.Time{}, false, nil
}

// databaseAutomationScheduleRunsLeft says whether a recurring automation
// that ends after so many runs ("실행 횟수", Notion's) has runs left: each
// run it was admitted for, through any edit, counts.
func databaseAutomationScheduleRunsLeft(app core.App, definitionID string, trigger databaseAutomationTrigger, admitting int) (bool, error) {
	if trigger.EndsAfter <= 0 || definitionID == "" {
		return true, nil
	}
	runs := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_automation_schedule_occurrences}}
		WHERE [[definition_id]] = {:id}`).Bind(dbx.Params{"id": definitionID}).Row(&runs); err != nil {
		return false, err
	}
	return runs+admitting < trigger.EndsAfter, nil
}
