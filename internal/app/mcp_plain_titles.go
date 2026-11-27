package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Every MCP answer that names a page, a database page or a database by its
// title in words says it one of three ways, as Notion's MCP does (seen
// 2026-11-22 on synthetic pages, each tool on three titles: a page mention,
// a date and a page mention, an inline equation and a page mention):
//
//   - as it shows — fetch's head title, a backlink's source.title, and the
//     lists and page-view, which name a page as its backlinks do: each
//     mention "@" and what it shows now in the reader's language, a date in
//     full, a page, database or person by its name, one closed to the reader
//     as its Markdown names it; an inline equation "$x^2$" ("w2322 날짜
//     @2026년 11월 22일 와 @w2322 대상", "w2322 수식 $x^2$ 와 @w2322 대상")
//     (mcpTitleAsShown);
//   - as its search keeps it — search's result title, the path search and
//     fetch give, and the trash tools' lists: each mention, of any kind and
//     whoever reads it, "‣" and an inline equation "⁍", the marks Notion's
//     own records keep them by ("w2322 날짜 ‣ 와 ‣", "w2322 수식 ⁍ 와 ‣")
//     (mcpTitleAsIndexed);
//   - as its search highlights it — a search result's highlight of its
//     title: a page or database mention says nothing, a date its day in
//     full in English, with its time and its zone, whoever reads it, and
//     an inline equation its expression; its spaces as written ("w2331zq
//     제목  와 November 22, 2026 끝", "w2331zq 시각 November 22, 2026 9:30
//     AM (GMT+9) 범위 November 22, 2026 → November 24, 2026 끝", "w2331zq
//     수식 x^2 끝"; seen twice on 2026-11-22 by a reader of Korean)
//     (mcpTitleAsSearched, mcp_search_highlight.go).
//
// A title is spelled by its name's spans (pageNameSpans); a custom emoji in
// one keeps its words either way (not seen in Notion's).

const (
	mcpIndexedMentionMark  = "‣"
	mcpIndexedEquationMark = "⁍"
)

// mcpTitleAsIndexed is a page's title as Notion's search keeps it: each
// mention "‣", each inline equation "⁍"; its words as given for a title
// with neither.
func mcpTitleAsIndexed(app core.App, pageID, title string) string {
	spans, _ := pageNameSpans(app, pageID)
	return mcpSpansAsIndexed(spans, title)
}

// mcpSpansAsIndexed is a name spelled by spans as Notion's search keeps it;
// title, when they hold neither a mention nor an equation.
func mcpSpansAsIndexed(spans []graphTextSpan, title string) string {
	if !mcpSpansMarkTitle(spans) {
		return title
	}
	var said strings.Builder
	for _, span := range spans {
		switch {
		case span.Equation != "":
			said.WriteString(mcpIndexedEquationMark)
		case span.Mention != "" && span.Mention != "custom_emoji":
			said.WriteString(mcpIndexedMentionMark)
		default:
			said.WriteString(span.Text)
		}
	}
	return said.String()
}

// mcpSpansMarkTitle reports spans holding what a plain title says apart
// from its words: a mention other than a custom emoji, or an equation.
func mcpSpansMarkTitle(spans []graphTextSpan) bool {
	for _, span := range spans {
		if span.Equation != "" || span.Mention != "" && span.Mention != "custom_emoji" {
			return true
		}
	}
	return false
}

// mcpDatabaseTitleAsIndexed is a database's title, its first source's name
// as written, as Notion's search keeps it.
func mcpDatabaseTitleAsIndexed(app core.App, sourceID, title string) string {
	return mcpSpansAsIndexed(sourceNameSpansForPage(app, sourceID, title), title)
}

// mcpTitleAsSearched is a page's title as a search's highlight says it.
func mcpTitleAsSearched(app core.App, pageID, title string) string {
	spans, _ := pageNameSpans(app, pageID)
	return mcpSpansAsSearched(spans, title)
}

// mcpSpansAsSearched is a name spelled by spans as a search's highlight
// says it; title, when they hold neither a mention nor an equation. A
// person mention and a custom emoji say what search keeps of them
// (searchSpansText).
func mcpSpansAsSearched(spans []graphTextSpan, title string) string {
	if !mcpSpansMarkTitle(spans) {
		return title
	}
	var said strings.Builder
	for _, span := range spans {
		switch {
		case span.Equation != "":
			said.WriteString(span.Equation)
		case searchMentionHasNoWords(span.Mention), customEmojiSpanNeverHad(span):
		case span.Mention == "date" && mcpDateMentionSearched(span) != "":
			said.WriteString(mcpDateMentionSearched(span))
		default:
			said.WriteString(span.Text)
		}
	}
	return said.String()
}

// mcpDateMentionSearched is a date mention as a search's highlight says
// it: in full, in English, whoever reads it and whatever its format (seen
// 2026-11-22: a mention made through the API, in full, and one made through
// Notion's MCP, shown "@오늘", "@다음주 월요일" in fetch's title, both
// "November 21, 2026" in the highlight).
func mcpDateMentionSearched(span graphTextSpan) string {
	span.DateFormat = dateMentionFormatFull
	return mcpDateMentionShown(span, accountLanguageEn)
}

// mcpDateMentionShown is a date mention in a language as it shows now
// (mcpDateMentionWords).
func mcpDateMentionShown(span graphTextSpan, language string) string {
	return mcpDateMentionWords(span, language, time.Now())
}

// mcpDateMentionWords is a date mention in a language at a moment, as
// Notion's fetch names it in a title: in its format, one without a format
// relative to that day — "오늘", "어제", "내일", this week's weekday ("수요일"),
// last or next week's ("지난주 토요일", "다음주 일요일"), weeks from Monday,
// and the full date further off — as the editor shows one, and as Notion's
// shows a mention made through its MCP (seen 2026-11-22 at 19:26 and 19:39
// UTC, a Saturday: 2026-11-21 "@오늘", 2026-11-22 "@내일", 2026-11-23 "@다음주
// 월요일", 2026-11-15 "@지난주 일요일"; a range "@오늘 → 내일"). Its day is
// that day where its time is — a day alone in UTC (2026-11-21 "@오늘" when it
// was already 2026-11-22 in Seoul, where the reader is), a time in its zone
// (2026-11-21 23:30 in Asia/Seoul "@어제 오후 11:30 (GMT+9)" at that moment).
// After a time, its zone's offset: its zone's, or the offset other than
// UTC's it was written with, which Notion keeps as a zone of that offset
// (seen 2026-11-22 on two pages: 2026-11-22T09:30:00+09:00 reads
// "@2026년 11월 22일 오전 9:30 (GMT+9)", time-zone="Etc/GMT-9" in its
// Markdown; a time written in UTC, "…Z", "@2026년 11월 22일 오전 12:30",
// seen once). "" for one it can't read.
func mcpDateMentionWords(span graphTextSpan, language string, now time.Time) string {
	from, to, ok := parseNotionMentionDateRange(span.Date, span.DateEnd, span.DateTimeZone)
	if !ok {
		return ""
	}
	format, location := span.DateFormat, time.UTC
	if format == "" {
		format = dateMentionFormatRelative
	}
	if from.timed {
		location = from.at.Location()
	}
	clock := func(moment notionMentionMoment) string {
		if !moment.timed {
			return moment.day
		}
		return moment.at.Format("2006-01-02T15:04")
	}
	words := dateMentionLabelWords(clock(from), format, span.DateTimeFormat, language, location, now)
	if to != nil {
		words += " → " + dateMentionLabelWords(clock(*to), format, span.DateTimeFormat, language, location, now)
	}
	if !from.timed || span.DateTimeZone == "" && !mcpDateWrittenOffAt(span.Date) {
		return words
	}
	_, offset := from.at.Zone()
	zone := "GMT"
	if offset != 0 {
		sign := "+"
		if offset < 0 {
			sign, offset = "-", -offset
		}
		zone += sign + fmt.Sprint(offset/3600)
		if minutes := offset % 3600 / 60; minutes != 0 {
			zone += fmt.Sprintf(":%02d", minutes)
		}
	}
	return words + " (" + zone + ")"
}

// mcpDateWrittenOffAt is whether a date mention's time was written with an
// offset other than UTC's.
func mcpDateWrittenOffAt(value string) bool {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return false
	}
	_, offset := at.Zone()
	return offset != 0
}

// mcpTitleAsShown is a page's title as Notion's fetch names it at its top
// (seen 2026-11-11) and its backlinks name it (seen 2026-11-22): its words,
// each mention "@" and what it shows in the reader's language — a date in
// full ("@2026년 11월 10일", a range with " → ", a time with its zone
// "(GMT+9)"), a page, database or person by its name now, one closed to
// the reader as its Markdown names it, a page or database nothing answers
// to "@찾을 수 없음", a person no one is "@익명", and a data source's kept by
// its address nothing (mcpTitleMentionShowsNothing) — each inline equation
// "$x^2$". A workspace left "" is the page's own.
func mcpTitleAsShown(app core.App, authority mcpAuthority, workspaceID, pageID, title string) string {
	spans, _ := pageNameSpans(app, pageID)
	if !mcpSpansMarkTitle(spans) {
		return title
	}
	if workspaceID == "" {
		_ = app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&workspaceID)
	}
	return mcpSpansAsShown(app, authority, workspaceID, spans, title)
}

// mcpDatabaseTitleAsShown is a database's title, its first source's name
// as written, as Notion's lists name it.
func mcpDatabaseTitleAsShown(app core.App, authority mcpAuthority, workspaceID, sourceID, title string) string {
	return mcpSpansAsShown(app, authority, workspaceID, sourceNameSpansForPage(app, sourceID, title), title)
}

// mcpSpansAsShown is a name spelled by spans as Notion's fetch names a
// title; title, when they hold neither a mention nor an equation.
func mcpSpansAsShown(app core.App, authority mcpAuthority, workspaceID string, spans []graphTextSpan, title string) string {
	if !mcpSpansMarkTitle(spans) {
		return title
	}
	language, err := serverTextLanguage(app, authority.PrincipalID, nil)
	if err != nil {
		language = accountLanguageKo
	}
	closed := mcpReaderCloses(app, authority)
	mentions := mcpPropertyMentions(app, authority, workspaceID)
	var shown strings.Builder
	for _, span := range spans {
		if span.Equation != "" {
			shown.WriteString("$" + span.Equation + "$")
			continue
		}
		name := ""
		switch span.Mention {
		case "date":
			name = mcpDateMentionShown(span, language)
		case "page", "database":
			switch {
			case mcpTitleMentionShowsNothing(span):
				continue
			case mcpTitleMentionMissing(app, span):
				name = missingMentionLabel(language)
			case searchMentionClosed(closed, span.Mention, span.PageID, span.DatabaseID):
				name = searchMentionWords(closed, span.Mention, span.PageID, span.DatabaseID, span.Text)
			case span.Mention == "page":
				if name = mentions.PageTitle(span.PageID); name == "" {
					name = notionUntitledPageMentionTitle
				}
			default:
				name = mentions.DatabaseTitle(span.DatabaseID)
			}
		case "person":
			if name = mentions.UserName(span.UserID); name == "" && notionUnknownPersonMention(span.UserID, span.Text) {
				name = anonymousMentionLabel(language)
			}
		}
		if name == "" {
			shown.WriteString(span.Text)
			continue
		}
		shown.WriteString("@" + strings.TrimPrefix(name, "@"))
	}
	return shown.String()
}

// mcpAncestorTitleAsIndexed is a page's or database's title above another,
// as a path says it (mcpAncestorPath): a page by its name's spans; a
// database in a page by its source's name, a full-page one by its page's.
func mcpAncestorTitleAsIndexed(app core.App, ancestor mcpFetchAncestor) string {
	switch {
	case ancestor.Kind == mcpFetchAncestorPage && ancestor.Page != nil:
		return mcpTitleAsIndexed(app, ancestor.Page.Id, ancestor.Title)
	case ancestor.Kind != mcpFetchAncestorDatabase:
		return ancestor.Title
	}
	if spans := sourceNameSpansForPage(app, ancestor.Source.ID, ancestor.Title); len(spans) > 0 {
		return mcpSpansAsIndexed(spans, ancestor.Title)
	}
	if ancestor.Page != nil && !ancestor.Inline {
		return mcpTitleAsIndexed(app, ancestor.Page.Id, ancestor.Title)
	}
	return ancestor.Title
}

// mcpDatabaseNameAsShown is a database's name as Notion's fetch names it at
// its top (seen 2026-11-22: "w2322 이름 DB @w2322 대상"): by the spans it
// was read with, else its first source's, else — a full-page one's — its
// page's; its words as given while none spell them.
func mcpDatabaseNameAsShown(
	app core.App, authority mcpAuthority, workspaceID, sourceID, fullPageID string, spans []graphTextSpan, title string,
) string {
	switch {
	case len(spans) > 0 && spelledName(spans) == title:
	case sourceID != "" && len(sourceNameSpansForPage(app, sourceID, title)) > 0:
		spans = sourceNameSpansForPage(app, sourceID, title)
	case fullPageID != "":
		return mcpTitleAsShown(app, authority, workspaceID, fullPageID, title)
	default:
		return title
	}
	return mcpSpansAsShown(app, authority, workspaceID, spans, title)
}

// mcpDataSourceAsShown is a data source with its name as fetch heads it
// (mcpDatabaseTitleAsShown).
func mcpDataSourceAsShown(app core.App, authority mcpAuthority, location mcpDataSourceLocation) mcpDataSourceLocation {
	location.Title = mcpDatabaseTitleAsShown(app, authority, location.WorkspaceID, location.ID, location.Title)
	return location
}
