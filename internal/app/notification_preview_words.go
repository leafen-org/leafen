package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notificationLineAsItStands is what a line of text says of a notification with
// spans: its date mentions in the reader's language and on the reader's day, as
// the page says them now (the stored words are what the line said when it was
// written, without its time), and a mention closed to the reader "사용 권한
// 없음" (notification_reader_mentions.go).
func notificationLineAsItStands(preview, previewSpans, language string, location *time.Location, now time.Time) string {
	if previewSpans == "" {
		return closedMentionsInLine(preview, language)
	}
	spans := []graphTextSpan{}
	if json.Unmarshal([]byte(previewSpans), &spans) != nil {
		return closedMentionsInLine(preview, language)
	}
	var line strings.Builder
	for _, span := range spans {
		if span.Mention == "date" && span.Date != "" {
			line.WriteString(dateMentionWords(span, language, location, now))
		} else {
			line.WriteString(span.Text)
		}
	}
	return closedMentionsInLine(strings.TrimSpace(line.String()), language)
}

// sourceLineSpans is the line of a mention in a block or of a comment when a
// date is in it, for what shows the line to say that date as it stands. A line
// without one has nothing to go stale, and a read that fails leaves the stored words.
func sourceLineSpans(app core.App, delivery storedCommentNotificationDelivery) string {
	spans := []graphTextSpan{}
	switch delivery.SourceKind {
	case "block":
		spans = blockLineSpans(app, delivery.PageID, delivery.BlockID)
	case "comment":
		body := ""
		if err := app.DB().NewQuery(`SELECT [[body_json]] FROM {{leafen_comments}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": delivery.SourceID}).Row(&body); err != nil {
			return ""
		}
		parsed := struct {
			Rich []graphTextSpan `json:"rich"`
		}{}
		if json.Unmarshal([]byte(body), &parsed) != nil {
			return ""
		}
		spans = parsed.Rich
	}
	// Kept for a date, which reads as it stands when shown, for a comment's
	// person, whom the inbox marks when it is the reader (Notion's, seen
	// 2026-11-06), and for a page or database, which each reader reads as
	// they may open it (notification_reader_mentions.go).
	if !slices.ContainsFunc(spans, func(span graphTextSpan) bool {
		return span.Mention == "date" || (delivery.SourceKind == "comment" && span.Mention == "person") ||
			searchMentionHasNoWords(span.Mention)
	}) {
		return ""
	}
	return encodeNotificationLineSpans(spans)
}

// accountLocation is the place an account's "today" is judged in.
func accountLocation(app core.App, userID string) *time.Location {
	return personLocation(app, userID)
}

// dateMentionWords says a date mention as dateMentionDisplayText does on the page.
func dateMentionWords(span graphTextSpan, language string, location *time.Location, now time.Time) string {
	prefix := ""
	if strings.HasPrefix(strings.TrimSpace(span.Text), "@") {
		prefix = "@"
	}
	words := dateMentionLabelWords(span.Date, span.DateFormat, span.DateTimeFormat, language, location, now)
	if span.DateEnd != "" {
		words += " → " + dateMentionLabelWords(span.DateEnd, span.DateFormat, span.DateTimeFormat, language, location, now)
	}
	return prefix + words
}

func dateMentionLabelWords(value, format, timeFormat, language string, location *time.Location, now time.Time) string {
	value = strings.Replace(strings.TrimSpace(value), " ", "T", 1)
	if len(value) < len("2006-01-02") {
		return value
	}
	day, err := time.Parse("2006-01-02", value[:len("2006-01-02")])
	if err != nil {
		return value
	}
	words := dateMentionDayWords(day, format, language, location, now)
	if !dateMentionHasTime(value) || timeFormat == "hidden" || len(value) < len("2006-01-02T15:04") {
		return words
	}
	clock, err := time.Parse("15:04", value[len("2006-01-02T"):len("2006-01-02T15:04")])
	if err != nil {
		return words
	}
	return words + " " + dateMentionClockWords(clock, timeFormat, language)
}

func dateMentionClockWords(clock time.Time, timeFormat, language string) string {
	switch {
	case timeFormat == "24":
		return fmt.Sprintf("%d:%02d", clock.Hour(), clock.Minute())
	case language == accountLanguageKo:
		period := "오전"
		if clock.Hour() >= 12 {
			period = "오후"
		}
		return period + " " + clock.Format("3:04")
	default:
		return clock.Format("3:04 PM")
	}
}

func dateMentionDayWords(day time.Time, format, language string, location *time.Location, now time.Time) string {
	korean := language == accountLanguageKo
	switch format {
	case "short":
		if korean {
			return fmt.Sprintf("%d월 %d", int(day.Month()), day.Day())
		}
		return day.Format("Jan 2")
	case "month_day_year":
		return day.Format("01/02/2006")
	case "day_month_year":
		return day.Format("02/01/2006")
	case "year_month_day":
		return day.Format("2006/01/02")
	case "", dateMentionFormatRelative:
		if relative := dateMentionRelativeWords(day, korean, location, now); relative != "" {
			return relative
		}
	}
	if korean {
		return day.Format("2006년 1월 2일")
	}
	return day.Format("January 2, 2006")
}

// dateMentionRelativeWords is relativeDayLabel's day: today and its neighbours,
// this week's weekday, next or last week's, and no word further off.
func dateMentionRelativeWords(day time.Time, korean bool, location *time.Location, now time.Time) string {
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	switch int(day.Sub(today).Hours() / 24) {
	case 0:
		return map[bool]string{true: "오늘", false: "Today"}[korean]
	case -1:
		return map[bool]string{true: "어제", false: "Yesterday"}[korean]
	case 1:
		return map[bool]string{true: "내일", false: "Tomorrow"}[korean]
	}
	weekday := day.Weekday().String()
	if korean {
		weekday = []string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"}[day.Weekday()]
	}
	switch int(mondayOfDay(day).Sub(mondayOfDay(today)).Hours() / 24 / 7) {
	case 0:
		return weekday
	case 1:
		if korean {
			return "다음주 " + weekday
		}
		return "Next " + weekday
	case -1:
		if korean {
			return "지난주 " + weekday
		}
		return "Last " + weekday
	}
	return ""
}

func mondayOfDay(day time.Time) time.Time {
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}
