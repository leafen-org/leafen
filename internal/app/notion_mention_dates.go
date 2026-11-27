package app

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A date mention holds a day or a time. Leafen's editor writes a time as the
// clock of its time zone ("2026-10-01T09:30" in "Asia/Seoul"); the Public API
// may write one with its offset, and a time without a zone or an offset is
// UTC. As Notion does, the Public API shows a time with the offset of its
// zone, to the millisecond, and no time zone name, and a day with no time
// zone; Notion's Markdown writes the day, the clock and the zone apart.

type notionMentionMoment struct {
	day   string
	at    time.Time
	timed bool
}

var (
	notionMentionClockLayouts = []string{
		"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02T15:04:05.999999999",
	}
	notionMentionZonedClock = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
)

// parseNotionMentionMoment reads one end of a date mention in its zone.
func parseNotionMentionMoment(value, zone string) (notionMentionMoment, bool) {
	if parsed, err := time.Parse(time.DateOnly, value); err == nil && parsed.Format(time.DateOnly) == value {
		return notionMentionMoment{day: value}, true
	}
	location := time.UTC
	if zone != "" {
		loaded, err := time.LoadLocation(zone)
		if err != nil || zone == "Local" || len(zone) > 100 {
			return notionMentionMoment{}, false
		}
		location = loaded
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		if zone != "" {
			parsed = parsed.In(location)
		}
		return notionMentionMoment{at: parsed, timed: true}, true
	}
	// A clock in a zone is to the minute, as Leafen's editor and Notion's
	// Markdown write it: Notion reads "9:00" and "09:00:30" there as no time
	// (seen 2026-11-11).
	if zone != "" && !notionMentionZonedClock.MatchString(value) {
		return notionMentionMoment{}, false
	}
	for _, layout := range notionMentionClockLayouts {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return notionMentionMoment{at: parsed, timed: true}, true
		}
	}
	return notionMentionMoment{}, false
}

// notion writes one end of a date mention as the Public API shows it.
func (moment notionMentionMoment) notion() string {
	if !moment.timed {
		return moment.day
	}
	return moment.at.Format("2006-01-02T15:04:05.000-07:00")
}

// parseNotionMentionDateRange reads a date mention's ends; both are days or
// both times, and the end is not before the start.
func parseNotionMentionDateRange(start, end, zone string) (notionMentionMoment, *notionMentionMoment, bool) {
	from, ok := parseNotionMentionMoment(start, zone)
	if !ok {
		return notionMentionMoment{}, nil, false
	}
	if end == "" {
		return from, nil, true
	}
	to, ok := parseNotionMentionMoment(end, zone)
	if !ok || from.timed != to.timed || from.timed && to.at.Before(from.at) || !from.timed && to.day < from.day {
		return notionMentionMoment{}, nil, false
	}
	return from, &to, true
}

func validNotionMentionDateRange(start, end, zone string) bool {
	_, _, ok := parseNotionMentionDateRange(start, end, zone)
	return ok
}

// notionMentionDateObject is a date mention's date as the Public API shows it.
// One Markdown kept as written that isn't a date range (notionMarkdownDateSpan)
// shows each end as Notion's does (seen 2026-11-11): one it reads as a date,
// one that isn't with a time zone as "Invalid DateTime", and one that isn't
// without as written.
func notionMentionDateObject(start, end, zone string) (notionDateObject, bool) {
	from, to, ok := parseNotionMentionDateRange(start, end, zone)
	if !ok {
		shown := func(value string) string {
			if moment, read := parseNotionMentionMoment(value, zone); read {
				return moment.notion()
			}
			if zone != "" {
				return "Invalid DateTime"
			}
			return value
		}
		var shownEnd any
		if end != "" {
			shownEnd = shown(end)
		}
		return notionDateObject{"start": shown(start), "end": shownEnd, "time_zone": nil}, true
	}
	var projectedEnd any
	if to != nil {
		projectedEnd = to.notion()
	}
	return notionDateObject{"start": from.notion(), "end": projectedEnd, "time_zone": nil}, true
}

// notionDatePropertyObject is a date property's value as Notion's API shows
// it (seen 2026-10-29): a day as it is; a time to the millisecond with the
// offset of its zone, a clock with no zone read as UTC; a range with one
// time shows both ends as times, a day as its midnight; and no zone name.
// A value kept from before that reads as no date is shown as it was kept.
func notionDatePropertyObject(start, end, zone string) notionDateObject {
	read := func(text string) (notionMentionMoment, bool) {
		return parseNotionMentionMoment(strings.Replace(strings.TrimSpace(text), " ", "T", 1), zone)
	}
	from, ok := read(start)
	var to notionMentionMoment
	okEnd := true
	if strings.TrimSpace(end) != "" {
		to, okEnd = read(end)
	}
	if !ok || !okEnd {
		object := notionDateObject{"start": start, "end": nil, "time_zone": nil}
		if end != "" {
			object["end"] = end
		}
		if zone != "" {
			object["time_zone"] = zone
		}
		return object
	}
	if strings.TrimSpace(end) != "" && from.timed != to.timed {
		location := time.UTC
		if loaded, err := time.LoadLocation(zone); zone != "" && err == nil {
			location = loaded
		}
		asTime := func(moment notionMentionMoment) notionMentionMoment {
			if moment.timed {
				return moment
			}
			day, _ := time.ParseInLocation(time.DateOnly, moment.day, location)
			return notionMentionMoment{at: day, timed: true}
		}
		from, to = asTime(from), asTime(to)
	}
	object := notionDateObject{"start": from.notion(), "end": nil, "time_zone": nil}
	if strings.TrimSpace(end) != "" {
		object["end"] = to.notion()
	}
	return object
}

// notionMentionDateKept keeps a date mention written through the Public API
// as Notion keeps it (keepNotionDate, seen 2026-11-10: "2026-W02" is
// 2026-01-05T00:00, seconds are dropped, a range with one time is two); ends
// it doesn't read are left as written, for its check to say.
func notionMentionDateKept(start, end, zone string) (string, string, string) {
	from, ok := readNotionDateEnd(start)
	if !ok || !from.keepable() {
		return start, end, zone
	}
	var to *notionDateEnd
	if end != "" {
		read, ok := readNotionDateEnd(end)
		if !ok || !read.keepable() {
			return start, end, zone
		}
		to = &read
	}
	kept := keepNotionDate(from, to, zone, time.Now())
	return kept.start, kept.end, kept.zone
}

// Notion's Markdown names the zone of a time written with an offset alone:
// UTC, Etc/GMT∓hours for whole hours, and a zone of its own for the offsets
// between.
var notionMarkdownOffsetZones = map[int]string{
	5*3600 + 30*60: "Asia/Calcutta",
}

// notionMarkdownDate is a date mention's attributes as Notion's Markdown
// writes them; unset, one whose start was never given (validGraphDateStartUnset).
type notionMarkdownDate struct {
	start, end, startTime, endTime, zone string
	unset                                bool
}

// tag writes them in "v2"'s <date> tag, or "v1"'s <mention-date>, which
// names each end's time after it and writes its values as they are but for
// a quotation mark (seen 2026-11-11: start="<x>", end="2026&"), as a
// callout's icon (notionMarkdownIconEscaped). A start never given is
// start="undefined" in "v1" and none in "v2" (seen 2026-11-11: <date />,
// <date end="…" />).
func (date notionMarkdownDate) tag(version1 bool) string {
	value := notionMarkdownVersion2AttributeEscaped.Replace
	names := []string{"<date", "start", "end", "start-time", "end-time", "time-zone", " />"}
	order := []int{1, 2, 3, 4, 5}
	if version1 {
		value = strings.NewReplacer(`"`, `&quot;`).Replace
		names = []string{"<mention-date", "start", "end", "startTime", "endTime", "timeZone", "/>"}
		order = []int{1, 3, 2, 4, 5}
	}
	values := []string{"", date.start, date.end, date.startTime, date.endTime, date.zone}
	tag := names[0]
	for _, index := range order {
		if index == 1 && date.unset {
			if version1 {
				tag += ` start="undefined"`
			}
			continue
		}
		if values[index] != "" || index == 1 {
			tag += " " + names[index] + `="` + value(values[index]) + `"`
		}
	}
	return tag + names[6]
}

// notionMarkdownDateTag writes a date mention as Notion's Markdown does; one
// kept as written that isn't a date range, or whose start was never given,
// as written (seen 2026-11-11: a time its zone doesn't read with its day,
// clock and zone apart).
func notionMarkdownDateTag(span graphTextSpan, version1 bool) string {
	start, end, zone := span.Date, span.DateEnd, span.DateTimeZone
	from, to, ok := parseNotionMentionDateRange(start, end, zone)
	if !ok || span.DateStartUnset {
		date := notionMarkdownDate{start: start, end: end, zone: zone, unset: span.DateStartUnset}
		if zone != "" {
			date.start, date.startTime = notionMarkdownDateClock(start)
			date.end, date.endTime = notionMarkdownDateClock(end)
		}
		return date.tag(version1)
	}
	name := zone
	if from.timed && name == "" {
		_, offset := from.at.Zone()
		switch {
		case offset == 0:
			name = "UTC"
		case offset%3600 == 0 && offset >= -12*3600 && offset <= 14*3600:
			hours := -offset / 3600
			name = "Etc/GMT"
			if hours > 0 {
				name += "+"
			}
			name += strconv.Itoa(hours)
		case notionMarkdownOffsetZones[offset] != "":
			name = notionMarkdownOffsetZones[offset]
		default:
			// An offset Notion's zone for isn't known: the same time in UTC.
			name = "UTC"
			from.at = from.at.UTC()
			if to != nil {
				to.at = to.at.UTC()
			}
		}
	}
	day := func(moment notionMentionMoment) string {
		if moment.timed {
			return moment.at.Format(time.DateOnly)
		}
		return moment.day
	}
	// As Notion's page Markdown writes a date (seen 2026-10-29).
	date := notionMarkdownDate{start: day(from)}
	if to != nil {
		date.end = day(*to)
	}
	if from.timed {
		date.startTime, date.zone = from.at.Format("15:04"), name
	}
	if to != nil && to.timed {
		date.endTime = to.at.Format("15:04")
	}
	return date.tag(version1)
}

// notionMarkdownDateText is the words a date mention Markdown wrote keeps:
// its plain text, or "@" for an empty start, which Notion shows so (seen
// 2026-11-11), as a mention's words are never none.
func notionMarkdownDateText(start, end string) string {
	if text := notionMentionDatePlainText(start, end); text != "" {
		return text
	}
	return "@"
}

// notionMarkdownDateClock parts a time Markdown wrote (notionMarkdownDateSpan)
// into its day and its clock, after its last "T".
func notionMarkdownDateClock(value string) (string, string) {
	at := strings.LastIndex(value, "T")
	if at < 0 {
		return value, ""
	}
	return value[:at], value[at+1:]
}

// validGraphDateStartUnset reports whether a span marked as a date mention
// whose start was never given — one "v1" Markdown wrote with attributes but
// no start (notionMarkdownMention) — is one: a date mention holding no day,
// at most the clock its time zone read (notionMarkdownDateSpan).
func validGraphDateStartUnset(span graphTextSpan) bool {
	return !span.DateStartUnset || span.Mention == "date" && (span.Date == "" || strings.HasPrefix(span.Date, "T"))
}

// notionMarkdownDateSpan reads a <mention-date> tag's attributes as Leafen's
// editor writes a date: a day, or with a time zone, the clock there. As in
// Notion, a time without a time zone is left out, and with one, an end
// without its time (seen 2026-11-11). Its values are kept as written,
// whether or not they read as dates, as Notion's Markdown keeps them (seen
// 2026-11-11, "v1" and "v2": "hello", " 2026-01-02 ", an end before its
// start, a clock of "25:00"); a start that is there, even empty, makes it.
func notionMarkdownDateSpan(attributes map[string]string) (string, string, string, bool) {
	start, named := attributes["start"]
	end, zone := attributes["end"], attributes["timeZone"]
	if zone == "" {
		return start, end, "", named
	}
	clock := attributes["startTime"]
	if clock == "" {
		return start, end, "", named
	}
	start += "T" + clock
	if endClock := attributes["endTime"]; end != "" && endClock != "" {
		end += "T" + endClock
	} else {
		end = ""
	}
	return start, end, zone, named
}
