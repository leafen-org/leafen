package app

import (
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// workspaceExportDateMention is a date mention as an export writes it. The
// editor shows a mention relative to today ("@오늘", "@어제") and keeps the
// words of the day it was written, which are wrong a day later. Notion's
// export (seen 2026-11-06) writes the date in the language of the person
// exporting and in the mention's own date and time formats, a relative one as
// the full date: "2026년 11월 7일 오후 3:31", "11/20/2026", "15:31", a range
// "2026년 11월 6일 → 2026년 11월 8일", and no time zone. Its Markdown writes the
// words alone; its HTML writes "@" and them in <time datetime="…">.
type workspaceExportDateMention struct {
	Start, End, TimeZone, Format, TimeFormat string
}

func workspaceExportDateMentionFromAttributes(attributes map[string]any) workspaceExportDateMention {
	return workspaceExportDateMention{
		Start: mapString(attributes, "date"), End: mapString(attributes, "dateEnd"),
		TimeZone: mapString(attributes, "dateTimeZone"), Format: mapString(attributes, "dateFormat"),
		TimeFormat: mapString(attributes, "dateTimeFormat"),
	}
}

// words are the mention's date in language, and its start as a datetime; ok
// is false for a mention without a date it can read, which keeps its words.
func (mention workspaceExportDateMention) words(language string) (words, datetime string, ok bool) {
	from, to, ok := parseNotionMentionDateRange(mention.Start, mention.End, mention.TimeZone)
	if !ok {
		return "", "", false
	}
	format := mention.Format
	if format == "" || format == dateMentionFormatRelative {
		format = "full"
	}
	clock := func(moment notionMentionMoment) string {
		if !moment.timed {
			return moment.day
		}
		return moment.at.Format("2006-01-02T15:04")
	}
	words = dateMentionLabelWords(clock(from), format, mention.TimeFormat, language, time.UTC, time.Time{})
	if to != nil {
		words += " → " + dateMentionLabelWords(clock(*to), format, mention.TimeFormat, language, time.UTC, time.Time{})
	}
	return words, clock(from), true
}

// workspaceExportLanguage is the language an export writes its dates in: the
// one its person asked in, or else their account's.
func workspaceExportLanguage(app core.App, job storedJob, payload workspaceExportJobPayload) string {
	if payload.Language != "" {
		return payload.Language
	}
	if language, err := serverTextLanguage(app, job.Owner, nil); err == nil {
		return language
	}
	return accountLanguageKo
}

// workspaceExportServerOrigin is the address a link out of an export takes:
// the one this server knows is its own, as a mail's link (mailServerOrigin),
// with no request to go by.
func workspaceExportServerOrigin(app core.App) string {
	return mailServerOrigin(app, &http.Request{})
}
