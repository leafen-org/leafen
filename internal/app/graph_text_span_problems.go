package app

import (
	"fmt"
	"slices"
	"strings"
)

// What is wrong with a span of rich text kept outside a block's own text — a
// title, a table cell, a caption, a database's name — said where it is
// checked, so a refusal names it: a page's title written with a mention of
// kind "user" (Notion's API's word; Leafen's spans, as the screen writes
// them, say "person") or a person mention without its userId was told
// "titleSpans' texts must spell title" though they did (2026-11-26, w2720).

// graphMentionKinds are the mentions a span takes, each with the field it
// needs and the ones it takes: the date's three, an outside page's icon.
var graphMentionKinds = map[string]struct {
	needs string
	takes []string
}{
	"":             {},
	"page":         {needs: "pageId", takes: []string{"pageId"}},
	"database":     {needs: "databaseId", takes: []string{"databaseId"}},
	"person":       {needs: "userId", takes: []string{"userId"}},
	"custom_emoji": {needs: "customEmojiId", takes: []string{"customEmojiId"}},
	"date":         {takes: []string{"date", "dateEnd", "dateTimeZone"}},
	"external":     {needs: "link", takes: []string{"iconUrl"}},
}

// graphMentionKindsSaid lists the mentions a span takes, as a refusal says them.
const graphMentionKindsSaid = `page (with pageId), database (databaseId), person (userId), date, custom_emoji (customEmojiId) or external (link)`

// graphSpanHeld is a field a span may name something by, and whether it does.
type graphSpanHeld struct {
	name string
	set  bool
}

// graphContentTextSpanProblem is what is wrong with one span of rich text
// kept outside a block's own text, as a refusal says it after the span's
// place ("titleSpans[1] has …"), or "" for a span it takes. Whether it takes
// one is validGraphContentTextSpan's to say (content_graph_table.go); this
// says why it doesn't, and a span it refuses for a reason not told here is
// told so plainly.
func graphContentTextSpanProblem(span graphTextSpan) string {
	if validGraphContentTextSpan(span) {
		return ""
	}
	if problem := graphContentTextSpanReason(span); problem != "" {
		return problem
	}
	return "isn't a span it takes"
}

// graphContentTextSpanReason is why validGraphContentTextSpan refuses a span,
// in the same order it checks.
func graphContentTextSpanReason(span graphTextSpan) string {
	// Suggested edits live on block text only; captions and cells carry none.
	switch {
	case span.Color != "" && !validNotionBlockColor(span.Color):
		return fmt.Sprintf("has color %q, which isn't a color it takes", span.Color)
	case span.Background != "" && !validNotionSpanBackground(span.Background):
		return fmt.Sprintf("has background %q, which isn't a background it takes", span.Background)
	case span.Link != "" && !validGraphHeldTextLink(span.Link):
		return "has a link that isn't an address it keeps"
	case span.CommentID != "":
		return "has commentId, which it doesn't keep"
	case span.DateReminder != "" && (span.Mention != "date" || !validDateMentionReminder(span.Date, span.DateReminder)):
		return "has a dateReminder, which goes with a date mention's date"
	case span.Suggestion != "" || span.SuggestionID != "" || span.SuggestionBefore != "":
		return "has a suggestion, which only a block's own text keeps"
	case span.DateFormat != "" && (span.Mention != "date" || !validDateMentionFormat(span.DateFormat)),
		span.DateTimeFormat != "" && (span.Mention != "date" || !validDateMentionTimeFormat(span.DateTimeFormat)):
		return "has a dateFormat or dateTimeFormat, which goes with a date mention, in a format it takes"
	case span.BlockID != "" && span.Mention != "page":
		return "has blockId, which goes with a page mention"
	case !validGraphLinkMentionPreview(span):
		return "has provider, description or imageUrl, which go with an external mention"
	case !validGraphDateStartUnset(span):
		return "has dateStartUnset, which goes with a date mention whose date is a time alone"
	}
	held := []graphSpanHeld{
		{"pageId", span.PageID != ""}, {"databaseId", span.DatabaseID != ""}, {"customEmojiId", span.CustomEmojiID != ""},
		{"userId", span.UserID != ""}, {"date", span.Date != ""}, {"dateEnd", span.DateEnd != ""},
		{"dateTimeZone", span.DateTimeZone != ""}, {"iconUrl", span.IconURL != ""},
	}
	if span.Equation != "" {
		if span.Text != span.Equation || len([]rune(span.Equation)) > maxNotionInlineEquationRunes ||
			span.Link != "" || span.Mention != "" || slices.ContainsFunc(held, func(field graphSpanHeld) bool { return field.set }) {
			return fmt.Sprintf("is an equation, whose text is its equation (up to %d characters) with no link or mention", maxNotionInlineEquationRunes)
		}
		return ""
	}
	kind, known := graphMentionKinds[span.Mention]
	if !known {
		return fmt.Sprintf("has mention %q: a mention is %s", span.Mention, graphMentionKindsSaid)
	}
	what := "a " + strings.ReplaceAll(span.Mention, "_", " ") + " mention"
	if span.Mention == "" {
		what = "a span without a mention"
	}
	needed := map[string]bool{"pageId": span.PageID != "", "databaseId": span.DatabaseID != "", "userId": span.UserID != "",
		"customEmojiId": span.CustomEmojiID != "", "link": span.Link != ""}
	if kind.needs != "" && !needed[kind.needs] {
		return fmt.Sprintf("is %s without its %s", what, kind.needs)
	}
	extra := []string{}
	for _, field := range held {
		if field.set && !slices.Contains(kind.takes, field.name) {
			extra = append(extra, field.name)
		}
	}
	switch {
	case len(extra) > 0 && span.Mention == "":
		return fmt.Sprintf("has %s without a mention: a mention is %s", strings.Join(extra, " and "), graphMentionKindsSaid)
	case len(extra) > 0:
		takes := kind.takes
		if kind.needs != "" && !slices.Contains(takes, kind.needs) {
			takes = append([]string{kind.needs}, takes...)
		}
		return fmt.Sprintf("is %s with %s, which it doesn't take; it takes %s", what, strings.Join(extra, " and "), strings.Join(takes, ", "))
	}
	if span.Mention == "external" && span.IconURL != "" && !validGraphExternalFileURL(span.IconURL) {
		return "has an iconUrl that isn't a picture's address"
	}
	return ""
}

// graphTitleSpansProblem says what is wrong with a title's spans as sent in
// field: the first span refused and why, more than a title holds (once its
// alike pieces are joined, where joined: a page's title), or texts that don't
// spell the title.
func graphTitleSpansProblem(field string, spans []graphTextSpan, joined bool) string {
	for index, span := range spans {
		// A thread left on a title's words is kept apart (admitGraphRichTextSpans).
		if validCommentIdentifier(span.CommentID) {
			span.CommentID = ""
		}
		if problem := graphContentTextSpanProblem(span); problem != "" {
			return fmt.Sprintf("%s[%d] %s", field, index, problem)
		}
	}
	kept := spans
	if joined {
		kept = joinedGraphTextSpans(spans)
	}
	if !graphRichTextFits(kept, maxGraphTitleRunes) {
		return fmt.Sprintf("%s hold more than a title does: at most 100 pieces, and %d characters of words", field, maxGraphTitleRunes)
	}
	return field + "' texts must spell title"
}
