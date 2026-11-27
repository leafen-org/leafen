package app

import (
	"net/url"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// workspaceExportTextRun is one formatted stretch of block text.
type workspaceExportTextRun struct {
	Text          string
	Bold          bool
	Italic        bool
	Underline     bool
	Strikethrough bool
	Code          bool
	Color         string
	Background    string
	Link          string
	Mention       string
	PageID        string
	UserID        string
	Equation      string
	// DateTime is a date mention's start, for HTML's <time datetime>.
	DateTime string
}

// workspaceExportRunFromAttributes reads one run; a date mention's words are
// its date in language (workspace_export_date_mentions.go), and a page or
// database mention its exporter may not open, in no-access marks, "사용 권한
// 없음" (workspaceExportDocument.closes).
func workspaceExportRunFromAttributes(text string, attributes map[string]any, language string) workspaceExportTextRun {
	flag := func(key string) bool {
		value, _ := attributes[key].(bool)
		return value
	}
	run := workspaceExportTextRun{
		Text: text, Bold: flag("bold"), Italic: flag("italic"), Underline: flag("underline"),
		Strikethrough: flag("strikethrough"), Code: flag("code"), Color: mapString(attributes, "color"),
		Background: mapString(attributes, "background"), Link: mapString(attributes, "link"), Mention: mapString(attributes, "mention"),
		PageID: mapString(attributes, "pageId"), UserID: mapString(attributes, "userId"),
		Equation: mapString(attributes, "equation"),
	}
	if run.Mention == "date" {
		if words, datetime, ok := workspaceExportDateMentionFromAttributes(attributes).words(language); ok {
			run.Text, run.DateTime = words, datetime
		}
	}
	if searchMentionHasNoWords(run.Mention) && isClosedMentionText(run.Text) {
		run.Text = closedMentionsInLine(run.Text, language)
	}
	return run
}

// workspaceExportRunsFromYText reads a block's text as its exporter reads
// it, by closes.
func workspaceExportRunsFromYText(text *crdt.YText, language string, closes func() searchMentionCloser) []workspaceExportTextRun {
	runs := []workspaceExportTextRun{}
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		content, ok := delta.Insert.(string)
		if delta.Op != crdt.DeltaOpInsert || !ok || content == "" {
			continue
		}
		attributes := map[string]any(delta.Attributes)
		reach := searchMentionReach(closes(), mapString(attributes, "mention"), mapString(attributes, "pageId"), mapString(attributes, "databaseId"))
		if reach != mentionOpens {
			content = unnamedMentionWords(reach, content, true)
		}
		runs = append(runs, workspaceExportRunFromAttributes(content, attributes, language))
	}
	return runs
}

// workspaceExportRunsFromSpans reads stored span lists: captions, table
// cells, row page block text and comment bodies.
func workspaceExportRunsFromSpans(value any, language string) []workspaceExportTextRun {
	spans, _ := value.([]any)
	runs := make([]workspaceExportTextRun, 0, len(spans))
	for _, raw := range spans {
		span, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text := mapString(span, "text")
		if text == "" {
			text = mapString(span, "plainText")
		}
		if text != "" {
			runs = append(runs, workspaceExportRunFromAttributes(text, span, language))
		}
	}
	return runs
}

func workspaceExportRowBlockRuns(content map[string]any, language string) []workspaceExportTextRun {
	if runs := workspaceExportRunsFromSpans(content["rich"], language); len(runs) > 0 {
		return runs
	}
	if text := mapString(content, "text"); text != "" {
		return []workspaceExportTextRun{{Text: text}}
	}
	return nil
}

func workspaceExportPlainText(runs []workspaceExportTextRun) string {
	var builder strings.Builder
	for _, run := range runs {
		builder.WriteString(run.Text)
	}
	return builder.String()
}

// workspaceExportSafeLink keeps web, mail and phone links, a web address
// written without its scheme as the https one the app opens
// (graphSchemelessWebLink); any other is dropped from the exported document.
func workspaceExportSafeLink(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err == nil && value != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "mailto", "tel":
			return value
		}
	}
	inApp := strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
	if !inApp && graphSchemelessWebLink(value) {
		return "https://" + strings.TrimLeft(value, `/\`)
	}
	return ""
}

// workspaceExportTextLinkHref is where a text link goes in the exported
// document: one to a page of the workspace (/p/ID), or to a block on one
// (/p/ID#block-ID, #block on the page being written, page), where the
// page's mention goes (link). An HTML document, whose blocks are its
// anchors, links at the block (its ID, as its element is named; a page on
// this server at #block-ID); a Markdown file has no anchors,
// so a block link there is its page's file, as Notion's Markdown export
// writes one (seen 2026-11-11). A link to this server's page keeps the
// block; any other as workspaceExportSafeLink keeps it.
func workspaceExportTextLinkHref(value string, link workspaceExportLinker, anchors bool, page string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	id := notionMarkdownPageLinkID(value)
	if err != nil || link == nil || id == "" && !strings.HasPrefix(value, "#") {
		return workspaceExportSafeLink(value)
	}
	anchor, block := "", mcpLinkedBlockID(parsed.Fragment)
	// A block that is the page the link opens is that page (block_address_anchor.go).
	if target := id; target != "" && sameAddressID(block, target) || id == "" && sameAddressID(block, page) {
		block = ""
	}
	if block != "" {
		anchor = "#" + block
	}
	switch {
	case id != "":
	case anchors || anchor == "":
		return anchor
	case page == "":
		return ""
	default:
		id = page
	}
	href := link(id)
	switch {
	case href == "" || anchor == "":
		return href
	case strings.HasPrefix(href, "#"):
		return anchor
	case strings.Contains(href, "://"):
		// The page on this server opens at the block its address names.
		return href + "#block-" + block
	case anchors:
		return href + anchor
	}
	return href
}

var workspaceExportColors = map[string]bool{
	"gray": true, "brown": true, "orange": true, "yellow": true, "green": true,
	"blue": true, "purple": true, "pink": true, "red": true,
}

// workspaceExportColorClass is a text color's class, by Notion's export's
// name for it: green is teal there (seen 2026-11-14: block-color-teal,
// highlight-teal_background, data-notion-highlight="teal").
func workspaceExportColorClass(color string) string {
	base := strings.TrimSuffix(color, "_background")
	if !workspaceExportColors[base] {
		return ""
	}
	name := base
	if base == "green" {
		name = "teal"
	}
	if base != color {
		return "highlight-" + name + "_background"
	}
	return "highlight-" + name
}

// workspaceExportEscape escapes words for an HTML export as Notion's export
// does (seen 2026-11-14: ' as &#x27; and " as &quot;).
var workspaceExportEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;").Replace

// workspaceExportLinker resolves a link to another exported page or row page
// relative to the page being written; it returns "" when the target is not
// part of the export.
type workspaceExportLinker func(targetID string) string

// workspaceExportRunsHTML writes runs as HTML: a page's mention links its
// page with the page's icon (icon) before its words, as Notion's export
// does (seen 2026-11-14).
func workspaceExportRunsHTML(runs []workspaceExportTextRun, link workspaceExportLinker, icon func(pageID string) string,
	people map[string]string) string {
	var builder strings.Builder
	for _, run := range runs {
		text := workspaceExportEscape(run.Text)
		text = strings.ReplaceAll(text, "\n", "<br/>")
		if run.Mention == "person" && run.UserID != "" && people[run.UserID] != "" &&
			!strings.Contains(run.Text, people[run.UserID]) {
			text = workspaceExportEscape("@" + people[run.UserID])
		}
		// A person mention is a "user" span, a connection's bot a "bot" one,
		// as Notion's HTML export writes them (seen 2026-11-12: <span
		// class="bot">@〈연결〉</span> and <span class="user">@〈사람〉</span>, in a
		// page's text and a database's text cell alike).
		if run.Mention == "person" {
			class := "user"
			if notionBotUserIDShape(run.UserID) {
				class = "bot"
			}
			text = `<span class="` + class + `">` + text + "</span>"
		}
		if run.Equation != "" {
			text = workspaceExportInlineEquationHTML(run.Equation)
		}
		if run.DateTime != "" {
			text = `<time datetime="` + workspaceExportEscape(run.DateTime) + `">@` + text + "</time>"
		}
		if run.Code {
			text = "<code>" + text + "</code>"
		}
		if run.Bold {
			text = "<strong>" + text + "</strong>"
		}
		if run.Italic {
			text = "<em>" + text + "</em>"
		}
		if run.Strikethrough {
			text = "<del>" + text + "</del>"
		}
		if run.Underline {
			text = `<span class="underline">` + text + "</span>"
		}
		// A run with both colors keeps both, its background outside, each
		// named in data-notion-highlight, as Notion's export marks them.
		color, background := textSpanColors(run.Color, run.Background)
		for _, shade := range []string{color, background} {
			if class := workspaceExportColorClass(shade); class != "" {
				text = `<mark class="` + class + `" data-notion-highlight="` + strings.TrimPrefix(class, "highlight-") + `">` + text + "</mark>"
			}
		}
		href, page := "", ""
		if run.Mention == "page" && run.PageID != "" && link != nil {
			href, page = link(run.PageID), run.PageID
		} else if run.Link != "" {
			href = workspaceExportTextLinkHref(run.Link, link, true, "")
		}
		if href != "" {
			// A page's mention names its page, as Notion's export does.
			if page != "" && icon != nil {
				text = icon(page) + text
			}
			text = `<a href="` + workspaceExportEscape(href) + `"` + workspaceExportPageIDAttr(page) + `>` + text + "</a>"
		}
		builder.WriteString(text)
	}
	return builder.String()
}

// workspaceExportRunsMarkdown writes runs as Markdown in the file of page.
// Their text is written as it is, Markdown's marks in it too, as Notion's
// export writes it (seen 2026-11-12: "가*_\<>[]`#|~나", "<safely>", "# 제목
// 아님" at a line's start, a code run "co`de*_" between single backticks); so
// are the titles and names its links show (workspaceExportRenderer).
func workspaceExportRunsMarkdown(runs []workspaceExportTextRun, link workspaceExportLinker, page string) string {
	var builder strings.Builder
	for _, run := range runs {
		text := run.Text
		if run.Equation != "" {
			builder.WriteString("$" + run.Equation + "$")
			continue
		}
		if run.Code {
			text = "`" + text + "`"
		}
		if strings.TrimSpace(run.Text) != "" && (run.Bold || run.Italic || run.Strikethrough) {
			// A mark's spaces at its ends go outside it, as Markdown reads
			// "** and**" as no mark (2026-10-14).
			core := strings.TrimSpace(text)
			before, after := text[:strings.Index(text, core)], text[strings.Index(text, core)+len(core):]
			if run.Bold {
				core = "**" + core + "**"
			}
			if run.Italic {
				core = "*" + core + "*"
			}
			if run.Strikethrough {
				core = "~~" + core + "~~"
			}
			text = before + core + after
		}
		href := ""
		if run.Mention == "page" && run.PageID != "" && link != nil {
			href = link(run.PageID)
		} else if run.Link != "" {
			href = workspaceExportTextLinkHref(run.Link, link, false, page)
		}
		if href != "" {
			text = "[" + text + "](" + workspaceExportMarkdownHref(href) + ")"
		}
		builder.WriteString(text)
	}
	return builder.String()
}

func workspaceExportMarkdownHref(href string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(href)
}
