package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The public API's page Markdown keeps a form of its own, which Notion's MCP
// has moved on from (both read from one page, 2026-11-04): a line break between
// blocks and no blank lines, blocks a tab further in save the lines of code
// and an equation's lines after its first, which stand as written
// (2026-11-10), each list numbered from 1, a color at the end of a
// block's line, <details> for a toggle, **bold** and <span underline="true">,
// <mention-page> and <mention-date>, <table_of_contents/>, <synced_block>,
// a table always as tags, a file's address in src and its closing tag always
// written. A web bookmark and a breadcrumb are unknown there, a link to a page
// its <link-to-page>, an equation's text between $` and `$. Each span's marks stand
// around its text with its edge spaces inside them, as Notion's do. A
// paragraph that would start a heading, a list or a divider is escaped, and
// so is the whitespace a block's text starts with (notionMarkdownVersion1Lead),
// so the Markdown reads back as the same blocks; Notion's escapes neither
// (seen 2026-11-10 and 2026-11-11), which reads back as other text and other
// blocks.

// apiFlow writes blocks one after another, depth tabs in.
func (w *notionMarkdownWriter) apiFlow(blocks []workspaceExportMarkdownBlock, depth int) []string {
	out := []string{}
	number := 0
	previousNumbered := false
	for _, block := range blocks {
		numbered := block.Type == "numbered_list_item" || block.Type == "number"
		if numbered && previousNumbered {
			number++
		} else if numbered {
			number = 1
		}
		previousNumbered = numbered
		out = append(out, w.apiBlock(block, depth, number)...)
	}
	return out
}

// apiUnder writes a block's line and the blocks under it.
func (w *notionMarkdownWriter) apiUnder(block workspaceExportMarkdownBlock, depth int, line string) []string {
	w.rendered = append(w.rendered, block.ID)
	lines := []string{strings.Repeat("\t", depth) + line}
	return append(lines, w.apiFlow(w.children[block.ID], depth+1)...)
}

func (w *notionMarkdownWriter) apiBlock(block workspaceExportMarkdownBlock, depth, number int) []string {
	if !w.admit(block) {
		return nil
	}
	indent := strings.Repeat("\t", depth)
	children := w.children[block.ID]
	suffix := notionMarkdownBlockColorSuffix(block.Content)
	colorName := ""
	if suffix != "" {
		colorName = notionMarkdownColorName(notionBlockColor(block.Content))
	}
	text, written := notionPageMarkdownText(block, w.mentions)
	text = notionMarkdownVersion1Lead(strings.ReplaceAll(text, "\n", "<br>"), block.Type)
	switch block.Type {
	case graphBlockTypeCallout:
		if !written {
			break
		}
		lines := []string{indent + notionPageMarkdownCalloutOpening(block.Content, w.mentions.at(block.ID))}
		w.rendered = append(w.rendered, block.ID)
		w.calloutTextLine(block, text)
		// One holding its lines, its first a block other than a plain
		// paragraph (a heading, a to-do…), writes them as they are, as
		// Notion's screen callout (seen 2026-11-12).
		if text != "" {
			lines = append(lines, notionMarkdownVersion1TagClosed([]string{indent + "\t" + text}, notionPageMarkdownPlainText(block))...)
		}
		lines = append(lines, w.apiFlow(children, depth+1)...)
		return append(lines, indent+notionMarkdownCalloutClose)
	case graphBlockTypeToggle:
		if !written {
			break
		}
		opening := "<details>"
		if colorName != "" {
			opening = `<details color="` + colorName + `">`
		}
		lines := []string{indent + opening}
		w.rendered = append(w.rendered, block.ID)
		// An empty one has no <summary>, its blocks a tab in under its tag,
		// as Notion writes it (seen 2026-11-14: "<details>⏎\tb⏎</details>").
		if text != "" {
			lines = append(lines, indent+"<summary>"+text+"</summary>")
		}
		lines = append(lines, w.apiFlow(children, depth+1)...)
		return append(lines, indent+"</details>")
	case graphBlockTypeToggleHeading1, graphBlockTypeToggleHeading2, graphBlockTypeToggleHeading3, graphBlockTypeToggleHeading4:
		if !written {
			break
		}
		marks := ` {toggle="true"`
		if colorName != "" {
			marks += ` color="` + colorName + `"`
		}
		// An empty one is its marks and then its {…}, one space between, as
		// Notion writes it (seen 2026-11-14: "## {toggle=\"true\"}").
		if text == "" {
			return w.apiUnder(block, depth, notionMarkdownToggleHeadings[block.Type]+marks+"}")
		}
		return w.apiUnder(block, depth, notionMarkdownToggleHeadings[block.Type]+" "+text+marks+"}")
	case graphBlockTypeEquation:
		// Its expression stands as far in as its $$, its lines after the
		// first as written, as Notion's (seen 2026-11-10).
		w.rendered = append(w.rendered, block.ID)
		return []string{indent + "$$", indent + mapString(block.Content, "expression"), indent + "$$" + suffix}
	case graphBlockTypeTable:
		if lines, ok := w.apiTable(block, indent); ok {
			return lines
		}
	case graphBlockTypeColumnList:
		return w.apiColumns(block, depth)
	case graphBlockTypeColumn:
		return w.apiColumn(block, depth)
	case graphBlockTypeTab:
		if lines, ok := w.apiTabs(block, depth); ok {
			return lines
		}
	case graphBlockTypeTableOfContents:
		w.rendered = append(w.rendered, block.ID)
		if colorName != "" {
			return []string{indent + `<table_of_contents color="` + colorName + `"/>`}
		}
		return []string{indent + "<table_of_contents/>"}
	case graphBlockTypeEmbed:
		if lines, ok := w.apiEmbed(block, indent); ok {
			return lines
		}
	case graphBlockTypeSyncedBlock:
		return w.apiSyncedBlock(block, depth)
	case "code":
		// Code without a language is plain text, as the block reads show
		// it; a fence without one reads as JavaScript.
		language, _ := block.Content["language"].(string)
		if strings.TrimSpace(language) == "" {
			language = plainTextCodeLanguage
		}
		// Its lines stand as written, only its fences as far in as the
		// block, as Notion's (seen 2026-11-10), each of three backticks
		// whatever the code holds (seen 2026-11-14: code holding "```" or
		// "````" too).
		w.rendered = append(w.rendered, block.ID)
		return []string{indent + "```" + language, notionPageMarkdownPlainText(block), indent + "```"}
	case "divider":
		return w.apiUnder(block, depth, "---")
	}
	if line, ok := notionMarkdownFileBlockLine(block, w.fileBlockID(block), w.mentions); ok {
		return w.apiUnder(block, depth, line)
	}
	if reference, ok := w.references[block.ID]; ok {
		reference = w.titled(reference)
		w.named = append(w.named, reference)
		return w.apiUnder(block, depth, reference.apiLine())
	}
	if written {
		if line, ok := w.apiTextLine(block, text, suffix, number); ok {
			// A paragraph its template mentions left blank writes no line, and
			// its blocks stand where it stands.
			if line == "" {
				return w.apiFlow(children, depth)
			}
			// A text starting with a block's tag is followed by its closing tag
			// (notion_page_markdown_version1_tag_close_lines.go).
			return notionMarkdownVersion1TagClosed(w.apiUnder(block, depth, line), notionPageMarkdownPlainText(block))
		}
	}
	// A link to a page or a database, or a button, is its own line (ownLine);
	// a block written as an <unknown/> line isn't among the IDs the read
	// couldn't load, as in Notion, a link to a page its "alias" there too
	// (seen 2026-11-10).
	if line, linked := w.ownLine(block); linked {
		return w.apiUnder(block, depth, line)
	}
	id, kind := w.publicBlockID(block.ID), notionMarkdownUnknownAlt(block.Type)
	if id == "" {
		id = block.ID
	}
	w.unknownLine(block, id)
	line := fmt.Sprintf(`<unknown url="%s" alt="%s"/>`, w.unknownAddress(id), kind)
	return w.apiUnder(block, depth, line)
}

// apiTextLine writes a paragraph, a heading, a list item, a to-do or a quote;
// "" for a paragraph that reads back as no block. An empty item, quote or
// heading is its marker alone, as Notion writes it ("-", "1.", "- [ ]", ">",
// "#", seen 2026-11-14).
func (w *notionMarkdownWriter) apiTextLine(block workspaceExportMarkdownBlock, text, suffix string, number int) (string, bool) {
	line, ok := w.apiTextBlockLine(block, text, suffix, number)
	if text == "" && suffix == "" && block.Type != "paragraph" {
		line = strings.TrimRight(line, " ")
	}
	// A colored one's color follows its marker, one space between (seen
	// 2026-11-14: "> {color=\"red\"}", "### {color=\"red\"}").
	if text == "" && suffix != "" && block.Type != "paragraph" && ok {
		line = strings.TrimRight(strings.TrimSuffix(line, suffix), " ") + suffix
	}
	return line, ok
}

func (w *notionMarkdownWriter) apiTextBlockLine(block workspaceExportMarkdownBlock, text, suffix string, number int) (string, bool) {
	switch block.Type {
	case "paragraph":
		if notionMarkdownBlankText(text, block.Spans) && !holdsTemplateMention(block.Spans) {
			// One of whitespace is its whitespace, its color after it and its
			// blocks a tab in under it, as Notion writes it (seen 2026-11-14:
			// "a⏎  ⏎\tb⏎c", "x⏎   {color=\"red\"}⏎y"); it reads back as no block,
			// and what it holds under the line before — a colored one as an
			// empty block of its color.
			if text != "" {
				w.blank[block.ID] = suffix == ""
				return text + suffix, true
			}
			w.rendered = append(w.rendered, block.ID)
			w.blank[block.ID] = true
			return "", true
		}
		// An empty one is <empty-block/> without its color, as Notion writes
		// it (seen 2026-11-14).
		if strings.TrimSpace(text) == "" {
			return "<empty-block/>", true
		}
		return notionMarkdownParagraph(notionMarkdownColoredMarks(text, suffix)) + suffix, true
	case "heading_1", "heading_2", "heading_3", graphBlockTypeHeading4:
		return strings.Repeat("#", int(block.Type[len(block.Type)-1]-'0')) + " " + text + suffix, true
	case "quote":
		return "> " + text + suffix, true
	case "bulleted_list_item", "bullet":
		return "- " + text + suffix, true
	case "numbered_list_item", "number":
		return strconv.Itoa(max(number, 1)) + ". " + text + suffix, true
	case "to_do", "todo":
		if checked, _ := block.Content["checked"].(bool); checked {
			return "- [x] " + text + suffix, true
		}
		return "- [ ] " + text + suffix, true
	}
	return "", false
}

func (w *notionMarkdownWriter) apiTable(block workspaceExportMarkdownBlock, indent string) ([]string, bool) {
	// One without rows is its tags alone, as Notion writes it whatever its
	// headers and columns (seen 2026-11-14).
	if len(w.children[block.ID]) == 0 {
		w.rendered = append(w.rendered, block.ID)
		return []string{indent + "<table>", indent + "</table>"}, true
	}
	rows := [][]string{}
	for _, row := range w.children[block.ID] {
		// A line break in it is <br>, as in a block's text (seen 2026-11-11).
		cells, written := notionMarkdownTableRowCells(row, w.mentions, "<br>")
		if !written {
			return nil, false
		}
		rows = append(rows, cells)
	}
	opening := "<table"
	if fitted, _ := block.Content["fitPageWidth"].(bool); fitted {
		opening += ` fit-page-width="true"`
	}
	if columnHeader, _ := block.Content["hasColumnHeader"].(bool); columnHeader {
		opening += ` header-row="true"`
	}
	if rowHeader, _ := block.Content["hasRowHeader"].(bool); rowHeader {
		opening += ` header-column="true"`
	}
	lines := append([]string{indent + opening + ">"}, notionMarkdownColumnTags(block.Content, indent, true)...)
	w.rendered = append(w.rendered, block.ID)
	for at, row := range w.children[block.ID] {
		w.visited[row.ID] = true
		w.rendered = append(w.rendered, row.ID)
		color, _ := row.Content["color"].(string)
		cellColors := notionMarkdownStringList(row.Content["cellColors"])
		// Its rows and cells stand at the line's start however deep the table
		// is, its own tags at its depth (seen 2026-11-11, a table in a toggle:
		// "\t<table>", "<tr>", "<td>…</td>", "</tr>", "\t</table>").
		lines = append(lines, "<tr"+notionMarkdownColorAttribute(color)+">")
		for column, cell := range rows[at] {
			cellColor := ""
			if column < len(cellColors) {
				cellColor = cellColors[column]
			}
			lines = append(lines, "<td"+notionMarkdownColorAttribute(cellColor)+">"+cell+"</td>")
		}
		lines = append(lines, "</tr>")
	}
	return append(lines, indent+"</table>"), true
}

// apiColumns writes columns; a block in them that no column holds stands a
// tab in, as Notion writes one "v1" made (seen 2026-11-13:
// "<columns>\n\ta\n</columns>").
func (w *notionMarkdownWriter) apiColumns(block workspaceExportMarkdownBlock, depth int) []string {
	indent := strings.Repeat("\t", depth)
	lines := []string{indent + "<columns>"}
	w.rendered = append(w.rendered, block.ID)
	held := []workspaceExportMarkdownBlock{}
	for _, column := range w.children[block.ID] {
		if column.Type != graphBlockTypeColumn {
			held = append(held, column)
			continue
		}
		lines, held = append(lines, w.apiFlow(held, depth+1)...), nil
		w.visited[column.ID] = true
		w.rendered = append(w.rendered, column.ID)
		opening := "<column>"
		if width, ok := column.Content["width"].(float64); ok && width > 0 && width < 100 {
			opening = `<column ratio="` + strconv.FormatFloat(width, 'f', -1, 64) + `">`
		}
		lines = append(lines, indent+"\t"+opening)
		lines = append(lines, w.apiFlow(w.children[column.ID], depth+2)...)
		lines = append(lines, indent+"\t</column>")
	}
	lines = append(lines, w.apiFlow(held, depth+1)...)
	return append(lines, indent+"</columns>")
}

// apiColumn writes a column out of its columns, as "v1" makes one: its tag
// around its blocks, as Notion writes it (seen 2026-11-13:
// "<column>\n\ta\n</column>", "<column>\n</column>").
func (w *notionMarkdownWriter) apiColumn(block workspaceExportMarkdownBlock, depth int) []string {
	indent := strings.Repeat("\t", depth)
	opening := "<column>"
	if width, ok := block.Content["width"].(float64); ok && width > 0 && width < 100 {
		opening = `<column ratio="` + strconv.FormatFloat(width, 'f', -1, 64) + `">`
	}
	w.rendered = append(w.rendered, block.ID)
	lines := append([]string{indent + opening}, w.apiFlow(w.children[block.ID], depth+1)...)
	return append(lines, indent+"</column>")
}

// apiEmbed writes an embed with its address in src, its closing tag always;
// one without its address has src="" (seen 2026-11-10).
func (w *notionMarkdownWriter) apiEmbed(block workspaceExportMarkdownBlock, indent string) ([]string, bool) {
	address, _, ok := w.embedAddress(block)
	if !ok {
		return nil, false
	}
	caption, written := w.linkCaption(block)
	if !written {
		return nil, false
	}
	w.rendered = append(w.rendered, block.ID)
	// "v1" keeps an address's & as it is; "v2" writes &amp; (seen 2026-11-11).
	escaped := notionMarkdownAttributeEscaped.Replace
	if !w.mentions.Version2 {
		escaped = notionMarkdownVersion1AttributeEscaped.Replace
	}
	return []string{indent + `<embed src="` + escaped(address) + `">` + caption + "</embed>"}, true
}

func (w *notionMarkdownWriter) apiSyncedBlock(block workspaceExportMarkdownBlock, depth int) []string {
	indent, address := strings.Repeat("\t", depth), notionMarkdownAPIAttributeEscaped.Replace
	w.rendered = append(w.rendered, block.ID)
	if original := strings.TrimSpace(mapString(block.Content, "syncedBlockId")); original != "" {
		if page := syncedCopyPage(block.Content); page != "" {
			return w.syncedPageReference(page, block.ID, indent, true) // notion_page_markdown_synced_page.go
		}
		lines := []string{indent + `<synced_block_reference url="` + address(w.syncedAddress(syncedBlockNamed(original, block.ID))) + `">`}
		lines = append(lines, notionMarkdownEachIndented(indent+"\t", w.syncedOriginal(original))...)
		return append(lines, indent+"</synced_block_reference>")
	}
	lines := []string{indent + `<synced_block url="` + address(w.syncedAddress(block.ID)) + `">`}
	lines = append(lines, w.apiFlow(w.children[block.ID], depth+1)...)
	return append(lines, indent+"</synced_block>")
}

// notionMarkdownVersion1Lead keeps the whitespace a block's text starts with
// before its words: "v1" reads it at a line's start as how far in the line
// stands and after a block's marker as nothing, and a backslash before it as
// itself (seen 2026-11-11: " a" after a paragraph is its child "a", "# \ a"
// a heading " a"), so a backslash goes before it. Where an italic's mark
// opens the text before a space, which starts a list item ("* a*b"), the
// backslash goes after the mark, save in a heading's or a toggle's title,
// where it starts none.
func notionMarkdownVersion1Lead(text, kind string) string {
	switch {
	case strings.TrimSpace(text) == "":
		return text
	case text[0] == ' ' || text[0] == '\t':
		return `\` + text
	case len(text) > 1 && text[0] == '*' && (text[1] == ' ' || text[1] == '\t') &&
		kind != graphBlockTypeToggle && notionMarkdownToggleHeadings[kind] == "" && !strings.HasPrefix(kind, "heading"):
		return `*\` + text[1:]
	}
	return text
}

// notionMarkdownAPISpanText writes a span, in its discussions (notionMarkdownDiscussed), as the API's Markdown does.
func notionMarkdownAPISpanText(span graphTextSpan, discussions string) string {
	var body string
	switch {
	case graphTextSpanIsEquation(span):
		// Without the whitespace at its ends, as Notion's "v1" writes it
		// (seen 2026-11-10: " " is "$``$"), a line break in it one
		// (notionMarkdownEquationLineBreak).
		body = "$`" + strings.ReplaceAll(strings.TrimSpace(span.Equation), "\n", notionMarkdownEquationLineBreak) + "`$"
	case span.Code:
		// A backtick it holds after a backslash, as Notion's "v1" writes it
		// (notion_markdown_version1_code_spans.go).
		body = "`" + strings.ReplaceAll(span.Text, "`", "\\`") + "`"
	case notionMarkdownLinkTarget(span.Link) == "" && span.Mention == "":
		// An address in unlinked text is escaped so that it reads back
		// unlinked, though Notion's "v1" writes it bare.
		body = notionMarkdownUnlinkedText(span.Text, notionMarkdownEscapedText)
	default:
		body = notionMarkdownEscapedText(span.Text)
	}
	if body = notionMarkdownDiscussed(body, discussions); body == "" {
		return ""
	}
	// Marks nest as Notion's do (seen 2026-11-10): strikethrough, italic,
	// bold, then the span, from the inside out, the spaces at the text's
	// edges inside them, as "v1" pairs a mark with the next of its kind
	// whatever stands beside it (seen 2026-11-11: "** a**" is a bold " a").
	if span.Strikethrough {
		body = "~~" + body + "~~"
	}
	if span.Italic {
		body = "*" + body + "*"
	}
	if span.Bold {
		body = "**" + body + "**"
	}
	body = notionMarkdownVersion1Span(body, span)
	if target := notionMarkdownLinkTarget(span.Link); target != "" && span.Mention == "" && !graphTextSpanIsEquation(span) {
		body = "[" + body + "](" + target + ")"
	}
	return body
}

// notionMarkdownVersion1Span puts a span's color and underline around what
// it writes as "v1" does: <span color="…" underline="true">, in that order
// (seen 2026-11-10).
func notionMarkdownVersion1Span(body string, span graphTextSpan) string {
	attributes := ""
	if color := notionMarkdownShownColor(span); color != "" {
		attributes += ` color="` + color + `"`
	}
	if span.Underline {
		attributes += ` underline="true"`
	}
	if attributes == "" {
		return body
	}
	return "<span" + attributes + ">" + body + "</span>"
}

// notionMarkdownAPIMentionText writes a mention's tag as the public API's
// Markdown does; handled is false for what both write alike.
func notionMarkdownAPIMentionText(span graphTextSpan, mentions notionMarkdownMentions) (text string, ok, handled bool) {
	address := notionMarkdownAPIAttributeEscaped.Replace
	switch span.Mention {
	case "page":
		target, readable := mentions.Page(span.PageID)
		// A mention of a block reads by the block's words now, a pair even
		// without them (seen 2026-11-11; page_mention_block_anchor.go).
		if readable && span.BlockID != "" && mentions.Block != nil {
			at, said, there := mentions.Block(target, span.PageID, span.BlockID)
			// One gone from its page reads by the page's address at the
			// block, with the words it was written with, as Notion's reads a
			// mention of a block in the trash (seen 2026-11-11; "v2" names the
			// block alone).
			if !there {
				block := at[strings.LastIndex(at, "/")+1:]
				return `<mention-page url="` + address(target+"#"+block) + `">` +
					notionMarkdownEscaped.Replace(span.Text) + `</mention-page>`, true, true
			}
			return `<mention-page url="` + address(at) + `">` + notionMarkdownEscaped.Replace(said) + `</mention-page>`, true, true
		}
		// One without a title is its pair of tags with nothing between (titled).
		if readable && mentions.titled(span) {
			return `<mention-page url="` + address(target) + `">` + notionMarkdownEscaped.Replace(mentions.PageTitle(span.PageID)) +
				`</mention-page>`, true, true
		}
		if !readable && mentions.Unnamed != nil {
			target, readable = mentions.Unnamed(span.Mention, span.PageID)
		}
		return `<mention-page url="` + address(target) + `"/>`, readable, true
	case "database":
		// One the reader opens reads by its name now, none for a database
		// without one, as Notion's Markdown does (seen 2026-11-10).
		target, readable := mentions.Database(span.DatabaseID)
		if readable && mentions.DatabaseTitle != nil {
			return `<mention-database url="` + address(target) + `">` +
				notionMarkdownEscaped.Replace(mentions.DatabaseTitle(span.DatabaseID)) + `</mention-database>`, true, true
		}
		if !readable && mentions.Unnamed != nil {
			target, readable = mentions.Unnamed(span.Mention, span.DatabaseID)
		}
		return `<mention-page url="` + address(target) + `"/>`, readable, true
	case "person":
		// One the reader may read reads by the person's name now, as
		// Notion's Markdown does (seen 2026-11-11).
		target, name, readable := mentions.Person(span.UserID, span.Text)
		if readable && target == "" {
			return "", true, true
		}
		if readable && name != "" {
			return `<mention-user url="` + address(target) + `">` + notionMarkdownEscaped.Replace(name) + `</mention-user>`, true, true
		}
		return `<mention-user url="` + address(target) + `"/>`, readable, true
	case "date":
		return notionMarkdownDateTag(span, true), true, true
	}
	return "", false, false
}

var (
	notionMarkdownAPIContents    = regexp.MustCompile(`^<table_of_contents((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*/>$`)
	notionMarkdownAPIToggleColor = regexp.MustCompile(`[ \t]+\{toggle="true" color="([a-z_]+)"\}$`)
	notionMarkdownAPITagNames    = strings.NewReplacer(
		"<synced_block_reference", "<synced-block-reference", "</synced_block_reference>", "</synced-block-reference>",
		"<synced_block", "<synced-block", "</synced_block>", "</synced-block>",
	)
)

// notionMarkdownAPILines puts the lines only the public API's form writes the
// way the other form does, so what it reads can be written back.
func notionMarkdownAPILines(lines []string) []string {
	fence, equation := "", false
	tabNames := map[int]bool{}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		if equation {
			// Its lines stand as written to the $$ closing it; a color after
			// that $$, as the public API's form writes one, is one of them
			// (notionMarkdownVersion1EquationCloses).
			equation = trimmed != "$$"
			continue
		}
		switch {
		case trimmed == "$$":
			equation = true
		case strings.HasPrefix(trimmed, "<table_of_contents"):
			if match := notionMarkdownAPIContents.FindStringSubmatch(trimmed); match != nil {
				lines[index] = lead + "<table-of-contents" + match[1] + " />"
			}
		case strings.HasPrefix(trimmed, "<synced_block"), strings.HasPrefix(trimmed, "</synced_block"):
			lines[index] = lead + notionMarkdownAPITagNames.Replace(trimmed)
		case strings.HasPrefix(trimmed, "#"):
			lines[index] = notionMarkdownAPIToggleColor.ReplaceAllString(line, ` {toggle="true"} {color="$1"}`)
		case trimmed == "<tab>", strings.HasPrefix(trimmed, "<tab "):
			if index+1 < len(lines) && notionMarkdownIndent(lines[index+1]) > notionMarkdownIndent(line) {
				tabNames[index+1] = true
			}
		}
	}
	if len(tabNames) == 0 {
		return lines
	}
	// A tab's name stands right above its blocks there, and apart from them here.
	spaced := make([]string, 0, len(lines)+len(tabNames))
	for index, line := range lines {
		spaced = append(spaced, line)
		if tabNames[index] {
			spaced = append(spaced, "")
		}
	}
	return spaced
}
