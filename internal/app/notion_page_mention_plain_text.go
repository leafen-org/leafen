package app

import (
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// notionMentionedPagePlainText is a page mention's plain_text as Notion's API
// gives it (seen 2026-11-10, Notion-Version 2026-03-11): the page's title now,
// each mention in it said as "@" and what it names — a page by its own title
// said the same way ("g @x @cee" for a title "g " and a mention of a page
// titled "x " and a mention of "cee"), a page already being said by nothing
// (a page titled "me " and a mention of itself reads "me @me "), a date in
// its long form ("@November 10, 2026"), a person by name, a database by its
// name, a custom emoji by nothing. `visiting` holds
// the pages being said around this one; the first call holds none, so a
// page's mention of itself says its words once.
func notionMentionedPagePlainText(
	app core.App,
	identity notionOAuthBearerIdentity,
	ownerID, workspaceID string,
	target notionPageMentionTarget,
	visiting map[string]bool,
) string {
	spans := notionMentionedPageTitleSpans(app, target)
	if !notionSpansHaveMention(spans) {
		return target.Title
	}
	var said strings.Builder
	for _, span := range spans {
		said.WriteString(notionSaidMention(app, identity, ownerID, workspaceID, span, visiting))
	}
	return said.String()
}

// notionSaidMention is a span of a mentioned title, or of a mentioned
// block's words (page_mention_block_anchor.go), as Notion's API says it:
// a mention as "@" and what it names, anything else as its text.
func notionSaidMention(
	app core.App,
	identity notionOAuthBearerIdentity,
	ownerID, workspaceID string,
	span graphTextSpan,
	visiting map[string]bool,
) string {
	switch span.Mention {
	case "page":
		// A mention of a page already being said says nothing.
		if visiting[span.PageID] {
			return ""
		}
		inner, allowed, err := authorizeNotionPageMentionTarget(app, identity, ownerID, workspaceID, span.PageID)
		if err != nil || !allowed {
			return "@" + notionUnreachableMentionTitle
		}
		next := make(map[string]bool, len(visiting)+1)
		for id := range visiting {
			next[id] = true
		}
		next[span.PageID] = true
		return "@" + notionMentionedPagePlainText(app, identity, ownerID, workspaceID, inner, next)
	case "database":
		title, found, err := databaseContainerTitle(app, span.DatabaseID)
		if err != nil || !found {
			return "@" + notionUnreachableMentionTitle
		}
		return "@" + notionDatabaseShownTitle(title)
	case "date":
		return "@" + notionMentionedDateText(span)
	case "person":
		user, found, err := loadNotionMentionUser(app, workspaceID, span.UserID)
		if err != nil || !found {
			return span.Text
		}
		return "@" + user.Name
	case "custom_emoji":
		// A custom emoji says nothing, one the workspace has as one it
		// hasn't (seen 2026-11-11: a title "emo ", that emoji and " end"
		// reads "emo  end" either way; the title's own rich text and page
		// Markdown say ":name:").
		return ""
	}
	return span.Text
}

// notionUnreachableMentionTitle is how a mention the reader can't reach is
// said inside a mentioned title, as its own mention reads.
const notionUnreachableMentionTitle = "Untitled"

// notionMentionedPageTitleSpans is a mentioned page's title spans, while they
// spell its title: a page's own, or a database page's title cell's.
func notionMentionedPageTitleSpans(app core.App, target notionPageMentionTarget) []graphTextSpan {
	if target.RowPage {
		spans, _ := rowTitleSpans(app, target.InternalID)
		return spans
	}
	page, err := app.FindRecordById(pagesCollection, target.InternalID)
	if err != nil {
		return nil
	}
	if page.GetString("data_source_id") != "" {
		spans, _ := rowTitleSpans(app, target.InternalID)
		return spans
	}
	return ordinaryPageTitleSpans(page)
}

func notionSpansHaveMention(spans []graphTextSpan) bool {
	for _, span := range spans {
		if span.Mention == "page" || span.Mention == "database" || span.Mention == "date" || span.Mention == "person" ||
			span.Mention == "custom_emoji" {
			return true
		}
	}
	return false
}

// notionMentionedDateText is a date mention said inside a mentioned title:
// its long form, with its time when it has one and its end after an arrow.
func notionMentionedDateText(span graphTextSpan) string {
	say := func(value string) string {
		for _, layout := range []string{"2006-01-02T15:04:05.000Z07:00", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if at, err := time.Parse(layout, value); err == nil {
				return at.Format("January 2, 2006 3:04 PM")
			}
		}
		if at, err := time.Parse("2006-01-02", value); err == nil {
			return at.Format("January 2, 2006")
		}
		return value
	}
	text := say(strings.TrimSpace(span.Date))
	if end := strings.TrimSpace(span.DateEnd); end != "" {
		text += " → " + say(end)
	}
	return text
}
