package app

import (
	"strings"
	"time"
)

// "Only when dates overlap" reads a task's dates to the minute, as Notion's
// does (seen 2026-11-21 in several tabs, both ways along a link): a task
// overlaps the one it follows when it starts no later than that one ends. A
// day alone is its first moment, not the whole day, read on the clock of the
// time it is compared with, so a task that starts at 09:00 on the day its
// predecessor ends with no time does not overlap it, and one that starts on
// that day with no time does. Two times compare as the moments they are,
// and a change of time alone (one added or taken off) moves tasks as a
// change of day does.

// databaseTaskMoment reads one end of a task's dates as the minute it is: a
// time with its offset as written, a clock on its value's time zone, or UTC
// when it has none, as every date reader takes one, shown on that time zone.
// A day alone is no moment.
func databaseTaskMoment(day time.Time, suffix, zone string) (time.Time, bool) {
	if strings.TrimSpace(suffix) == "" {
		return time.Time{}, false
	}
	location := time.UTC
	if zone != "" {
		if loaded, err := time.LoadLocation(zone); err == nil {
			location = loaded
		}
	}
	text := strings.Replace(day.Format(time.DateOnly)+suffix, " ", "T", 1)
	moment, ok := databaseQueryMinute(text, location)
	if !ok {
		return time.Time{}, false
	}
	if zone != "" {
		moment = moment.In(location)
	}
	return moment, true
}

// databaseTaskOverlaps tells whether `successor` starts no later than
// `predecessor` ends.
func databaseTaskOverlaps(predecessor, successor databaseTaskSpan) bool {
	end, endTimed := databaseTaskMoment(predecessor.End, predecessor.EndSuffix, predecessor.EndZone)
	start, startTimed := databaseTaskMoment(successor.Start, successor.StartSuffix, successor.StartZone)
	midnight := func(day time.Time, location *time.Location) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
	}
	switch {
	case !endTimed && !startTimed:
		return !successor.Start.After(predecessor.End)
	case !endTimed:
		end = midnight(predecessor.End, start.Location())
	case !startTimed:
		start = midnight(successor.Start, end.Location())
	}
	return !start.After(end)
}

// startDiffers and endDiffers tell whether an end of a task's dates changed:
// its day, or its time or time zone alone, which can make or end an overlap.
func (span databaseTaskSpan) startDiffers(other databaseTaskSpan) bool {
	return !span.Start.Equal(other.Start) || span.StartSuffix != other.StartSuffix || span.StartZone != other.StartZone
}

func (span databaseTaskSpan) endDiffers(other databaseTaskSpan) bool {
	return !span.End.Equal(other.End) || span.EndSuffix != other.EndSuffix || span.EndZone != other.EndZone
}
