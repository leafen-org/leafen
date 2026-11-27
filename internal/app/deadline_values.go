package app

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// A database date value someone marks as a deadline they must not miss (the
// 2026-10-14 decision, 8) carries "deadline": the days before it its
// notices go, as "7,3,1,0" — 0 is the day itself. A notice goes at 9:00 on
// its day, on the date's clock, and a deadline is its end when it has one:
// a day's deadline lasts until that day ends, a time's until that time.

const (
	deadlineMaxNotices = 8
	deadlineMaxDays    = 365
	deadlineNoticeHour = 9
	// An unacknowledged notice on the deadline's own day comes again this
	// often; on earlier days, at 9:00 the next day.
	deadlineDayRepeat = 3 * time.Hour
)

// canonicalDeadlineDays reads a deadline's days, the most first.
func canonicalDeadlineDays(text string) (string, []int, bool) {
	seen := map[int]bool{}
	days := []int{}
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		day, err := strconv.Atoi(part)
		if err != nil || day < 0 || day > deadlineMaxDays || strconv.Itoa(day) != part {
			return "", nil, false
		}
		if !seen[day] {
			seen[day] = true
			days = append(days, day)
		}
	}
	if len(days) == 0 || len(days) > deadlineMaxNotices {
		return "", nil, false
	}
	sort.Sort(sort.Reverse(sort.IntSlice(days)))
	parts := make([]string, len(days))
	for index, day := range days {
		parts[index] = strconv.Itoa(day)
	}
	return strings.Join(parts, ","), days, true
}

// deadlineText is the part of a date value its deadline is: the end of a
// range, else the start.
func deadlineText(date map[string]any) string {
	if end, _ := date["end"].(string); strings.TrimSpace(end) != "" {
		return strings.TrimSpace(end)
	}
	start, _ := date["start"].(string)
	return strings.TrimSpace(start)
}

// deadlineLocation is the clock a deadline's day is on: the zone it was set
// in, else the server's, as a person's clock is (personZoneLocation).
func deadlineLocation(timeZone string) *time.Location {
	return personZoneLocation(strings.TrimSpace(timeZone))
}

// deadlineMoment is when a deadline passes, and the day it is on.
func deadlineMoment(text string, location *time.Location) (time.Time, time.Time, bool) {
	text = strings.Replace(strings.TrimSpace(text), " ", "T", 1)
	if !dateMentionHasTime(text) {
		day, err := time.ParseInLocation("2006-01-02", text, location)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		return day.AddDate(0, 0, 1), day, true
	}
	moment, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		if moment, err = time.ParseInLocation("2006-01-02T15:04:05.999", text, location); err != nil {
			if moment, err = time.ParseInLocation("2006-01-02T15:04", text, location); err != nil {
				return time.Time{}, time.Time{}, false
			}
		}
	}
	local := moment.In(location)
	return moment, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location), true
}

// deadlineNoticeDue is when the notice some days before a deadline goes:
// 9:00 that day, or an hour before a deadline earlier than that.
func deadlineNoticeDue(deadline, day time.Time, days int) time.Time {
	due := day.AddDate(0, 0, -days).Add(deadlineNoticeHour * time.Hour)
	if latest := deadline.Add(-time.Hour); days == 0 && due.After(latest) {
		due = latest
	}
	return due
}

// deadlineNextRound is when a notice comes again to those who haven't
// acknowledged it, false once the deadline will have passed by then.
func deadlineNextRound(deadline time.Time, location *time.Location, days int, now time.Time) (time.Time, bool) {
	next := now.Add(deadlineDayRepeat)
	if days > 0 {
		local := now.In(location)
		next = time.Date(local.Year(), local.Month(), local.Day()+1, deadlineNoticeHour, 0, 0, 0, location)
	}
	return next, next.Before(deadline)
}
