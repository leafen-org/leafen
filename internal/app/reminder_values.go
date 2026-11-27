package app

import (
	"strconv"
	"strings"
	"time"
)

// A reminder on a date mention is the number of minutes before the date it
// fires. A date without a time is reminded from 9:00 that day, so it only
// takes whole days before.
var (
	timedReminderMinutes  = []int{0, 5, 10, 15, 30, 60, 120, 1440, 2880}
	allDayReminderMinutes = []int{0, 1440, 2880, 10080}
)

const allDayReminderHour = 9

func dateMentionHasTime(date string) bool {
	return strings.ContainsAny(strings.TrimSpace(date), "T ")
}

func validDateMentionReminder(date, reminder string) bool {
	if reminder == "" {
		return true
	}
	minutes, err := strconv.Atoi(reminder)
	if err != nil || strconv.Itoa(minutes) != reminder {
		return false
	}
	allowed := allDayReminderMinutes
	if dateMentionHasTime(date) {
		allowed = timedReminderMinutes
	}
	for _, candidate := range allowed {
		if candidate == minutes {
			return true
		}
	}
	return false
}

// dateMentionReminderDue is when a reminder fires. A date with an offset is
// absolute; otherwise the time zone it is given places it — the mention's,
// else its setter's — and the server's stands in where neither has one, as a
// person's clock does (personZoneLocation).
func dateMentionReminderDue(date, timeZone, reminder string) (time.Time, bool) {
	minutes, err := strconv.Atoi(reminder)
	if err != nil || !validDateMentionReminder(date, reminder) {
		return time.Time{}, false
	}
	location := personZoneLocation(strings.TrimSpace(timeZone))
	date = strings.Replace(strings.TrimSpace(date), " ", "T", 1)
	var anchor time.Time
	if !dateMentionHasTime(date) {
		day, err := time.ParseInLocation("2006-01-02", date, location)
		if err != nil {
			return time.Time{}, false
		}
		anchor = day.Add(allDayReminderHour * time.Hour)
	} else if parsed, err := time.Parse(time.RFC3339Nano, date); err == nil {
		anchor = parsed
	} else {
		parsed, err := time.ParseInLocation("2006-01-02T15:04:05.999", date, location)
		if err != nil {
			if parsed, err = time.ParseInLocation("2006-01-02T15:04", date, location); err != nil {
				return time.Time{}, false
			}
		}
		anchor = parsed
	}
	// A timed date's reminders a day or more ahead ring at 9:00 on their day,
	// as Notion's says "(오전 9:00) 1일 전" of a 16:00 date too (seen
	// 2026-11-05); the web keeps the same rule (dateReminders.ts).
	if dateMentionHasTime(date) && minutes >= 1440 {
		anchor = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), allDayReminderHour, 0, 0, 0, anchor.Location())
	}
	return anchor.Add(-time.Duration(minutes) * time.Minute), true
}
