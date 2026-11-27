package app

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// What a refused database automation says to fix (request_refusal.go): the
// field as the request names it (trigger.conditions[1].operator …) and why,
// after "Check the automation details.", and an automation in a state the
// change can't start from says that state instead of "Load the latest
// automation state again." Status and code stay the route's.

// databaseAutomationRefused refuses an automation for the reason given.
func databaseAutomationRefused(format string, args ...any) error {
	return refuse(errDatabaseAutomationInvalid, format, args...)
}

// databaseAutomationConflictMessage is a refused automation change's words.
func databaseAutomationConflictMessage(err error) string {
	if reason := refusalReason(err); reason != "" {
		return refusalMessage("The automation can't be changed this way now.", reason)
	}
	return "Load the latest automation state again."
}

// Refusals more than one automation route says.
var (
	errDatabaseAutomationMutationIDRefused = databaseAutomationRefused(
		"mutationId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice", databaseAutomationMaxIdentifierRunes)
	errDatabaseAutomationDeleted = refuse(errDatabaseAutomationConflict, "the automation is deleted")
)

// databaseAutomationNameAndScopeRefusal is why a trimmed name and scope
// can't be an automation's, or nil. It may be left unnamed, as Notion's, and
// then reads as its maker's.
func databaseAutomationNameAndScopeRefusal(name string, scope databaseAutomationScope) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > databaseAutomationMaxNameRunes {
		return databaseAutomationRefused("name is at most %d characters", databaseAutomationMaxNameRunes)
	}
	switch scope.Type {
	case databaseAutomationScopeDatabase:
		if scope.ViewID != "" {
			return databaseAutomationRefused("scope.viewId goes only with scope.type view")
		}
		return nil
	case databaseAutomationScopeView:
		if !boundedAutomationString(scope.ViewID, databaseAutomationMaxIdentifierRunes) {
			return databaseAutomationRefused("scope.viewId names the view, 1–%d characters", databaseAutomationMaxIdentifierRunes)
		}
		return nil
	}
	return databaseAutomationRefused("scope.type is %s or %s, not %q", databaseAutomationScopeDatabase, databaseAutomationScopeView, scope.Type)
}

// databaseAutomationEventsRefusal is why an events trigger's mode and
// conditions can't run, or nil.
func databaseAutomationEventsRefusal(trigger databaseAutomationTrigger) error {
	switch {
	case trigger.Mode != "any" && trigger.Mode != "all":
		return databaseAutomationRefused("trigger.mode is any or all, not %q", trigger.Mode)
	case len(trigger.Conditions) == 0:
		return databaseAutomationRefused("trigger.conditions is empty: name at least one, such as row_added")
	case trigger.Frequency != "" || trigger.Interval != 0 || trigger.Time != "" || trigger.TimeZone != "" ||
		trigger.StartsOn != "" || trigger.EndsOn != "" || len(trigger.Weekdays) != 0 || trigger.MonthDay != 0 || trigger.MonthWeek != 0:
		return databaseAutomationRefused("trigger's frequency, interval, time, timeZone, startsOn, endsOn, weekdays, monthDay and monthWeek go only with type schedule")
	}
	return nil
}

// databaseAutomationConditionRefusal is why one events condition can't run,
// or nil.
func databaseAutomationConditionRefusal(index int, condition databaseAutomationTriggerCondition) error {
	at := fmt.Sprintf("trigger.conditions[%d].", index)
	switch condition.Type {
	case databaseAutomationTriggerRowAdded:
		if condition.PropertyID != "" || condition.Operator != "" || len(condition.Value) > 0 {
			return databaseAutomationRefused("%spropertyId, operator and value go only with type property_edited", at)
		}
	case databaseAutomationTriggerPropertyEdited:
		switch {
		case condition.PropertyID != "" && !boundedAutomationString(condition.PropertyID, databaseAutomationMaxIdentifierRunes):
			return databaseAutomationRefused("%spropertyId is at most %d characters", at, databaseAutomationMaxIdentifierRunes)
		// Any property's edit names no value; one property's may (Notion's, seen 2026-10-24).
		case condition.PropertyID == "" && (condition.Operator != "" || len(condition.Value) > 0):
			return databaseAutomationRefused("%soperator and value go with a propertyId: any property's edit names no value", at)
		case !databaseAutomationConditionValueValid(condition.Operator, condition.Value):
			return databaseAutomationRefused("%svalue doesn't go with operator %q: is, is_not, contains and does_not_contain take text, a number or a list; "+
				"starts_with and ends_with text; greater_than and the like a number; is_empty and none no value", at, condition.Operator)
		}
	default:
		return databaseAutomationRefused("%stype is %s or %s, not %q", at, databaseAutomationTriggerRowAdded, databaseAutomationTriggerPropertyEdited, condition.Type)
	}
	return nil
}

// validateDatabaseAutomationScheduleTrigger admits Notion's recurring
// frequencies: every N days; every N weeks on chosen weekdays; every N
// months on a day of the month (a short month uses its last day) or on the
// first to fourth or last weekday; every N years on the start date. A
// refusal names the field as the schedule names it (interval, weekdays …).
func validateDatabaseAutomationScheduleTrigger(trigger databaseAutomationTrigger) error {
	limit := databaseAutomationScheduleIntervalLimit(trigger.Frequency)
	switch {
	case trigger.Type != databaseAutomationTriggerSchedule:
		return databaseAutomationRefused("type is %s", databaseAutomationTriggerSchedule)
	case limit == 0:
		return databaseAutomationRefused("frequency is daily, weekly, monthly or yearly, not %q", trigger.Frequency)
	case trigger.Interval < 1 || trigger.Interval > limit:
		return databaseAutomationRefused("interval is 1 to %d for %s, not %d", limit, trigger.Frequency, trigger.Interval)
	}
	switch trigger.Frequency {
	case databaseAutomationScheduleFrequencyWeekly:
		if !validDatabaseAutomationScheduleWeekdays(trigger.Weekdays, 1, 7) {
			return databaseAutomationRefused("weekdays names 1 to 7 days, each once from 0 (Sunday) to 6 (Saturday), in order")
		}
		if trigger.MonthDay != 0 || trigger.MonthWeek != 0 {
			return databaseAutomationRefused("monthDay and monthWeek go only with frequency monthly")
		}
	case databaseAutomationScheduleFrequencyMonthly:
		byDay := trigger.MonthDay >= 1 && trigger.MonthDay <= 31 && trigger.MonthWeek == 0 && len(trigger.Weekdays) == 0
		byWeekday := trigger.MonthDay == 0 && trigger.MonthWeek >= 1 && trigger.MonthWeek <= 5 &&
			validDatabaseAutomationScheduleWeekdays(trigger.Weekdays, 1, 1)
		if !byDay && !byWeekday {
			return databaseAutomationRefused("a monthly schedule names monthDay (1 to 31) alone, or monthWeek (1 to 4, 5 for the last) and one weekday")
		}
	default:
		if len(trigger.Weekdays) != 0 || trigger.MonthDay != 0 || trigger.MonthWeek != 0 {
			return databaseAutomationRefused("weekdays, monthDay and monthWeek go only with frequency weekly or monthly")
		}
	}
	if _, _, ok := parseDatabaseAutomationScheduleTime(trigger.Time); !ok {
		return databaseAutomationRefused("time is a time of day as HH:MM, not %q", trigger.Time)
	}
	if !boundedAutomationString(trigger.TimeZone, databaseAutomationMaxIdentifierRunes) {
		return databaseAutomationRefused("timeZone is an IANA time zone such as Asia/Seoul")
	}
	if _, err := time.LoadLocation(trigger.TimeZone); err != nil {
		return databaseAutomationRefused("timeZone is an IANA time zone such as Asia/Seoul, not %q", trigger.TimeZone)
	}
	start, ok := parseDatabaseAutomationScheduleDate(trigger.StartsOn)
	if !ok {
		return databaseAutomationRefused("startsOn is a date as YYYY-MM-DD, not %q", trigger.StartsOn)
	}
	if trigger.EndsOn != "" {
		end, valid := parseDatabaseAutomationScheduleDate(trigger.EndsOn)
		if !valid {
			return databaseAutomationRefused("endsOn is left out, or a date as YYYY-MM-DD, not %q", trigger.EndsOn)
		}
		if end.Ordinal < start.Ordinal {
			return databaseAutomationRefused("endsOn is on or after startsOn")
		}
	}
	// It ends on a date or after so many runs, as Notion's "종료일", not both.
	switch {
	case trigger.EndsAfter < 0 || trigger.EndsAfter > databaseAutomationScheduleMaxRuns:
		return databaseAutomationRefused("endsAfter is left out, or 1 to %d runs", databaseAutomationScheduleMaxRuns)
	case trigger.EndsAfter > 0 && trigger.EndsOn != "":
		return databaseAutomationRefused("endsOn and endsAfter aren't both named: a schedule ends on a date or after so many runs")
	case trigger.Mode != "" || len(trigger.Conditions) != 0:
		return databaseAutomationRefused("mode and conditions go only with type events")
	}
	return nil
}

// databaseAutomationActionRefusal is why an automation's steps can't run
// unattended, or nil: the action document's own reason where it says one.
func databaseAutomationActionRefusal(err error) error {
	if reason := refusalReason(err); reason != "" {
		return databaseAutomationRefused("actionDocument.%s", reason)
	}
	return databaseAutomationRefused("actionDocument is a list of steps the automation runs, each a type it takes with that type's fields")
}
