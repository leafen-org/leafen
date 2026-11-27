package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// A page's Markdown lays its blocks out as Notion's page Markdown does now
// (seen 2026-10-29 in pages its MCP read back): a blank line between blocks
// and none between the items of one list; a list item's blocks a marker's
// width in under it; a block that has a color or holds others other than a
// list item as a tag around it — <p color="…">, <h2 color>, <quote>,
// <toggle>, <callout> — its text on the line after the opening tag and its
// blocks after a blank line; <columns>, <tabs> and <table> the same way,
// and <p /> for an empty paragraph. Each block reads back as one item, in
// the order written (notion_page_markdown_containers.go).

type notionMarkdownWriter struct {
	children      map[string][]workspaceExportMarkdownBlock
	publicPageID  string
	publicBlockID func(string) string
	references    map[string]notionMarkdownReference
	mentions      notionMarkdownMentions
	sources       bool

	truncated      bool
	included       int
	visited, blank map[string]bool
	rendered       []string
	marks          *notionMarkdownBlockMarks
	unknownIDs     []string
	named          []notionMarkdownReference
	// unknownBlocks are the blocks written as <unknown/> lines, by the ID
	// each line names (notionMarkdownUnknownName); nil notes none.
	unknownBlocks map[string]string
	// underItem are the paragraphs right under a list item, which Notion
	// writes as <p> tags so they don't read as more of the item's text.
	underItem map[string]bool
}

// notionMarkdownListFamily names the list a block's plain line belongs to;
// "" for a block that is no list item, or one written as a tag.
type notionMarkdownListFamily string

// flow writes blocks standing one after another, each line after prefix.
func (w *notionMarkdownWriter) flow(blocks []workspaceExportMarkdownBlock, prefix string) []string {
	out := []string{}
	previous := notionMarkdownListFamily("")
	places := notionMarkdownListPlaces(blocks)
	for index := 0; index < len(blocks); index++ {
		block := blocks[index]
		// A numbered list's items stand without a blank line between them,
		// in their tags or not, as Notion's (seen 2026-11-10: </ol> then "2. b"),
		// save one whose own start breaks the count.
		apart := len(out) > 0 && (index == 0 || !notionMarkdownNumbered(block.Type) ||
			!notionMarkdownNumbered(blocks[index-1].Type) || places[index].restart)
		// Items in letters or roman numerals, or from a start that breaks the
		// count, are a list's tags (notion_page_markdown_list_numbers.go).
		if w.mentions.Version2 || places[index].restart || places[index].format != "" && places[index].format != numberedListFormatNumbers {
			if lines, used := w.version2ListRun(blocks[index:], places[index:], prefix); used > 0 {
				if apart {
					out = append(out, "")
				}
				out, previous, index = append(out, lines...), "", index+used-1
				continue
			}
		}
		lines, family := w.markedBlock(block, prefix, places[index].number)
		if len(lines) == 0 {
			continue
		}
		if apart && (family == "" || family != previous) {
			out = append(out, "")
		}
		out = append(out, lines...)
		previous = family
	}
	return out
}

// held writes a tag around a block: its opening, its text, its blocks after
// a blank line and its closing.
func (w *notionMarkdownWriter) held(block workspaceExportMarkdownBlock, prefix, opening, title, closing string) []string {
	if lines, closed := w.version2ClosedEmpty(block, prefix, opening, title, closing); closed {
		return lines
	}
	w.rendered = append(w.rendered, block.ID)
	w.calloutTextLine(block, title)
	title = w.version2Title(title)
	if title == "" {
		title = "<p />"
	}
	lines := []string{prefix + opening, prefix + title}
	if inner := w.flow(w.children[block.ID], prefix); len(inner) > 0 {
		lines = append(append(lines, ""), inner...)
	}
	return append(lines, prefix+closing)
}

// under writes a line and the blocks under it a few columns in: under a
// list item at once, its paragraphs as <p> tags (seen 2026-10-29), and under
// another block after a blank line, so they don't read as more of its text.
func (w *notionMarkdownWriter) under(block workspaceExportMarkdownBlock, prefix, line, in string, item bool) []string {
	lines := []string{prefix + line}
	w.rendered = append(w.rendered, block.ID)
	children := w.children[block.ID]
	if item {
		for _, child := range children {
			w.underItem[child.ID] = true
		}
	}
	inner := w.flow(children, prefix+in)
	if len(inner) == 0 {
		return lines
	}
	if !item {
		lines = append(lines, "")
	}
	return append(lines, inner...)
}

// notionMarkdownPlainListFamily tells whether a block is a list item written
// as a plain line, and which list.
func notionMarkdownPlainListFamily(block workspaceExportMarkdownBlock) (string, notionMarkdownListFamily) {
	if notionMarkdownBlockColorSuffix(block.Content) != "" {
		return "", ""
	}
	switch block.Type {
	case "bulleted_list_item", "bullet":
		return "- ", "bullet"
	case "numbered_list_item", "number":
		return "1. ", "number"
	case "to_do", "todo":
		return "- [ ] ", "todo"
	}
	return "", ""
}

// admit says whether a block is written: once, and while the Markdown has room.
func (w *notionMarkdownWriter) admit(block workspaceExportMarkdownBlock) bool {
	if w.visited[block.ID] {
		return false
	}
	w.visited[block.ID] = true
	if w.included >= workspaceExportNotionMarkdownRecordLimit {
		w.truncated = true
		if id := w.publicBlockID(block.ID); len(w.unknownIDs) < workspaceExportNotionMarkdownUnknownIDLimit && id != "" {
			w.unknownIDs = append(w.unknownIDs, id)
		}
		return false
	}
	w.included++
	return true
}

func (w *notionMarkdownWriter) block(block workspaceExportMarkdownBlock, prefix string, number int) ([]string, notionMarkdownListFamily) {
	if !w.admit(block) {
		return nil, ""
	}
	children := w.children[block.ID]
	color := ""
	if suffix := notionMarkdownBlockColorSuffix(block.Content); suffix != "" {
		color = ` color="` + notionMarkdownColorName(notionBlockColor(block.Content)) + `"`
	}
	text, written := notionPageMarkdownText(block, w.mentions)
	text = strings.ReplaceAll(text, "\n", "<br />")
	switch block.Type {
	case graphBlockTypeCallout:
		if written {
			opening := notionPageMarkdownCalloutOpening(block.Content, w.mentions.at(block.ID))
			// One holding its lines writes them as they are, its first too,
			// whatever block it is, as Notion's screen callout (seen
			// 2026-11-12: a heading first, no <p /> above it).
			if text == "" {
				w.rendered = append(w.rendered, block.ID)
				// One holding nothing closes at once in "v2", as Notion's
				// (seen 2026-11-14: "<callout icon=\"💡\"></callout>").
				if len(children) == 0 && w.mentions.Version2 {
					return []string{prefix + opening + notionMarkdownCalloutClose}, ""
				}
				return append(append([]string{prefix + opening}, w.flow(children, prefix)...), prefix+notionMarkdownCalloutClose), ""
			}
			if lines, ok := w.version2CalloutEdges(block, prefix, opening, text); ok {
				return lines, ""
			}
			return w.held(block, prefix, opening, text, notionMarkdownCalloutClose), ""
		}
	case graphBlockTypeToggle:
		if written {
			// An empty one closes at once, as Notion's reads (seen 2026-11-04).
			if text == "" && len(children) == 0 {
				w.rendered = append(w.rendered, block.ID)
				return []string{prefix + "<toggle" + color + "></toggle>"}, ""
			}
			return w.held(block, prefix, "<toggle"+color+">", text, "</toggle>"), ""
		}
	case graphBlockTypeToggleHeading1, graphBlockTypeToggleHeading2, graphBlockTypeToggleHeading3, graphBlockTypeToggleHeading4:
		if written {
			level := strconv.Itoa(len(notionMarkdownToggleHeadings[block.Type]))
			return w.held(block, prefix, `<toggle heading="h`+level+`"`+color+">", text, "</toggle>"), ""
		}
	case graphBlockTypeEquation:
		w.rendered = append(w.rendered, block.ID)
		// A colored one is a tag with its color, as Notion writes it, closed
		// at once when empty (seen 2026-11-14: <equation color="red"></equation>).
		opening, closing := "$$", "$$"
		expression := notionMarkdownWrittenExpression(mapString(block.Content, "expression"), w.mentions.Version2)
		if color != "" {
			opening, closing = "<equation"+color+">", "</equation>"
			if expression == "" {
				return []string{prefix + opening + closing}, ""
			}
		}
		lines := []string{prefix + opening}
		for _, line := range strings.Split(expression, "\n") {
			lines = append(lines, notionMarkdownIndented(prefix, line))
		}
		return append(lines, prefix+closing), ""
	case graphBlockTypeTable:
		if lines, ok := w.table(block, prefix); ok {
			return lines, ""
		}
	case graphBlockTypeColumnList:
		if lines, ok := w.columns(block, prefix); ok {
			return lines, ""
		}
	case graphBlockTypeTab:
		if lines, ok := w.tabs(block, prefix); ok {
			return lines, ""
		}
	case graphBlockTypeTableOfContents:
		w.rendered = append(w.rendered, block.ID)
		return []string{prefix + "<table-of-contents" + color + " />"}, ""
	case graphBlockTypeBookmark, graphBlockTypeEmbed:
		if lines, ok := w.linkBlock(block, prefix); ok {
			return lines, ""
		}
	case graphBlockTypeBreadcrumb, graphBlockTypeButton, graphBlockTypeLinkToPage, graphBlockTypeSyncedBlock:
		if w.mentions.Version2 && block.Type != graphBlockTypeSyncedBlock {
			break
		}
		if lines, ok := w.ownBlock(block, prefix); ok {
			return lines, ""
		}
	case "code":
		language, _ := block.Content["language"].(string)
		// Notion writes Python's fence as py (seen 2026-10-29).
		if w.mentions.Version2 {
			language = notionMarkdownVersion2Language(language)
		} else if language == "python" {
			language = "py"
		}
		code := notionPageMarkdownPlainText(block)
		fence := "```"
		for strings.Contains(code, fence) {
			fence += "`"
		}
		w.rendered = append(w.rendered, block.ID)
		lines := []string{prefix + fence + language}
		for _, line := range strings.Split(code, "\n") {
			lines = append(lines, notionMarkdownIndented(prefix, line))
		}
		return append(lines, prefix+fence), ""
	case "divider":
		return w.under(block, prefix, "---", "  ", false), ""
	}
	if line, ok := notionMarkdownFileBlockLine(block, w.fileBlockID(block), w.mentions); ok {
		return w.under(block, prefix, strings.ReplaceAll(line, "\n", "\n"+prefix), "  ", false), ""
	}
	if reference, ok := w.references[block.ID]; ok {
		// A database whose sources are all in the trash where they stand
		// isn't in its page's Markdown, as Notion's MCP reads it (seen
		// 2026-11-11: a page holding only such databases is blank); a write
		// leaves the block it doesn't name.
		if reference.Deleted {
			return nil, ""
		}
		reference = w.titled(reference)
		w.named = append(w.named, reference)
		return w.under(block, prefix, strings.ReplaceAll(reference.line(), "\n", "\n"+prefix), "  ", false), ""
	}
	if written {
		if lines, family, ok := w.textBlock(block, prefix, text, color, number); ok {
			return lines, family
		}
	}
	id := w.publicBlockID(block.ID)
	if w.mentions.Version2 {
		if id == "" {
			id = block.ID
		}
		return w.version2Unknown(block, prefix, id), ""
	}
	if id == "" {
		id = block.ID
	}
	w.unknownLine(block, id)
	line := fmt.Sprintf(`<unknown url="%s" alt="%s"/>`, w.unknownAddress(id), block.Type)
	if source := strings.TrimSpace(mapString(block.Content, "url")); w.sources && source != "" {
		line = fmt.Sprintf(`<unknown url="%s" alt="%s" source="%s"/>`, w.unknownAddress(id), block.Type,
			strings.NewReplacer(`&`, `&amp;`, `"`, `&quot;`, `<`, `&lt;`, `>`, `&gt;`).Replace(source))
	}
	return w.under(block, prefix, line, "  ", false), ""
}

// textBlock writes a paragraph, a heading, a list item, a to-do or a quote.
func (w *notionMarkdownWriter) textBlock(block workspaceExportMarkdownBlock, prefix, text, color string, number int) ([]string, notionMarkdownListFamily, bool) {
	held := color != "" || len(w.children[block.ID]) > 0 ||
		block.Type == "paragraph" && w.underItem[block.ID] && strings.TrimSpace(text) != "" ||
		w.mentions.Version2 && notionMarkdownEdgeSpace(text)
	paragraph := notionMarkdownParagraph
	if w.mentions.Version2 {
		paragraph = notionMarkdownVersion2Paragraph
	}
	switch block.Type {
	case "paragraph":
		if lines, ok := w.version2SpacesParagraph(block, prefix, text, color); ok {
			return lines, "", true
		}
		// "v2" writes one of spaces alone as its tag on one line, which
		// reads back as an empty block.
		if w.mentions.Version2 && strings.TrimSpace(text) == "" && text != "" && !holdsTemplateMention(block.Spans) &&
			len(w.children[block.ID]) == 0 && color == "" {
			w.rendered = append(w.rendered, block.ID)
			return []string{prefix + "<p>" + text + "</p>"}, "", true
		}
		// A paragraph left with only spaces by its template mentions, which
		// Markdown doesn't write, is an empty block, so that a change to
		// another line keeps it (an approved difference); one of
		// spaces alone reads back as no block, as in Notion.
		if notionMarkdownBlankText(text, block.Spans) && !holdsTemplateMention(block.Spans) {
			w.rendered = append(w.rendered, block.ID)
			w.blank[block.ID] = true
			return nil, "", true
		}
		if strings.TrimSpace(text) == "" {
			text = ""
		}
		// An empty one holding blocks writes them alone in its tag when the
		// first is no paragraph, which would read back as its text (seen
		// 2026-11-14: "<p>\n- b\n</p>", "<p>\n## b\n</p>", "<p>\n$$\nx\n$$\n</p>",
		// and "<p>\n<p />\n\nb\n</p>" before a paragraph).
		if children := w.children[block.ID]; w.mentions.Version2 && text == "" && len(children) > 0 &&
			children[0].Type != graphBlockTypeParagraph {
			w.rendered = append(w.rendered, block.ID)
			return append(append([]string{prefix + "<p" + color + ">"}, w.flow(children, prefix)...), prefix+"</p>"), "", true
		}
		if held {
			return w.held(block, prefix, "<p"+color+">", paragraph(text), "</p>"), "", true
		}
		if text == "" {
			w.rendered = append(w.rendered, block.ID)
			return []string{prefix + "<p />"}, "", true
		}
		w.rendered = append(w.rendered, block.ID)
		return []string{prefix + paragraph(text)}, "", true
	case "heading_1", "heading_2", "heading_3", graphBlockTypeHeading4:
		level := int(block.Type[len(block.Type)-1] - '0')
		// One whose code holds a line break is its tag too (seen 2026-11-14).
		if held || strings.Contains(text, notionMarkdownCodeBreak) {
			tag := "h" + strconv.Itoa(level)
			return w.held(block, prefix, "<"+tag+color+">", text, "</"+tag+">"), "", true
		}
		w.rendered = append(w.rendered, block.ID)
		return []string{prefix + strings.Repeat("#", level) + " " + text}, "", true
	case "quote":
		if held {
			return w.held(block, prefix, "<quote"+color+">", text, "</quote>"), "", true
		}
		w.rendered = append(w.rendered, block.ID)
		// An empty one is its > alone, as Notion writes it.
		if text == "" {
			return []string{prefix + ">"}, "", true
		}
		return []string{prefix + "> " + notionMarkdownVersion2Paragraph(text)}, "", true
	case "bulleted_list_item", "bullet":
		if color != "" {
			return w.listTag(block, prefix, "<ul>", "</ul>", "<li"+color+">", text, "</li>"), "", true
		}
		return w.under(block, prefix, "- "+notionMarkdownVersion2Paragraph(text), "  ", true), "bullet", true
	case "numbered_list_item", "number":
		if color != "" {
			opening := notionMarkdownOrderedOpening(notionMarkdownListPlace{number: number})
			return w.listTag(block, prefix, opening, "</ol>", "<li"+color+">", text, "</li>"), "", true
		}
		marker := strconv.Itoa(max(number, 1)) + ". "
		return w.under(block, prefix, marker+notionMarkdownVersion2Paragraph(text), strings.Repeat(" ", len(marker)), true), "number", true
	case "to_do", "todo":
		checked, _ := block.Content["checked"].(bool)
		if color != "" {
			return w.held(block, prefix, `<todo checked="`+strconv.FormatBool(checked)+`"`+color+">", text, "</todo>"), "", true
		}
		if w.mentions.Version2 && notionMarkdownEdgeSpace(text) {
			return w.held(block, prefix, `<todo checked="`+strconv.FormatBool(checked)+`">`, text, "</todo>"), "", true
		}
		marker := " "
		if checked {
			marker = "x"
		}
		return w.under(block, prefix, "- ["+marker+"] "+notionMarkdownVersion2Paragraph(text), "  ", true), "todo", true
	}
	return nil, "", false
}

// listTag writes a colored list item in its own list's tags, a numbered
// one's going on with its list's numbers.
func (w *notionMarkdownWriter) listTag(block workspaceExportMarkdownBlock, prefix, listOpening, listClosing, opening, text, closing string) []string {
	return append(append([]string{prefix + listOpening}, w.held(block, prefix, opening, text, closing)...), prefix+listClosing)
}

// notionMarkdownCellPipes escapes the pipes in a pipe table cell's text that
// its text doesn't already, as code's, so each reads as text, \| as Notion
// writes it (seen 2026-11-10).
func notionMarkdownCellPipes(cell string) string {
	var out strings.Builder
	backslashes := 0
	for index := 0; index < len(cell); index++ {
		if cell[index] == '|' && backslashes%2 == 0 {
			out.WriteByte('\\')
		}
		if cell[index] == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
		out.WriteByte(cell[index])
	}
	return out.String()
}

// notionMarkdownColumnTags writes a table's <colgroup>, each of its columns on
// a line once one has a width or a color (seen 2026-11-05): the width first
// and closed in itself, or in the public API's form the color first and left
// open. A table with neither has none.
func notionMarkdownColumnTags(content map[string]any, prefix string, apiForm bool) []string {
	colors := notionMarkdownStringList(content["columnColors"])
	widths, _ := content["columnWidths"].([]any)
	count, _ := content["tableWidth"].(float64)
	if strings.Join(colors, "") == "" && len(widths) == 0 {
		return nil
	}
	lines := []string{prefix + "<colgroup>"}
	for column := range max(int(count), len(colors), len(widths)) {
		color, width := "", ""
		if column < len(colors) {
			color = notionMarkdownColorAttribute(colors[column])
		}
		if column < len(widths) {
			if value, _ := widths[column].(float64); value > 0 {
				width = ` width="` + strconv.FormatFloat(value, 'f', -1, 64) + `"`
			}
		}
		if apiForm {
			lines = append(lines, prefix+"<col"+color+width+">")
		} else {
			lines = append(lines, prefix+"<col"+width+color+" />")
		}
	}
	return append(lines, prefix+"</colgroup>")
}

// notionMarkdownStringList is a content field's list of words; none when
// it holds no list.
func notionMarkdownStringList(value any) []string {
	items, _ := value.([]any)
	list := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		list = append(list, text)
	}
	return list
}

func (w *notionMarkdownWriter) columns(block workspaceExportMarkdownBlock, prefix string) ([]string, bool) {
	for _, column := range w.children[block.ID] {
		if column.Type != graphBlockTypeColumn {
			return nil, false
		}
	}
	lines := []string{prefix + "<columns>"}
	w.rendered = append(w.rendered, block.ID)
	for _, column := range w.children[block.ID] {
		w.visited[column.ID] = true
		w.rendered = append(w.rendered, column.ID)
		// Its share of the row, a percentage, as Notion's ratio.
		opening := "<column>"
		if width, ok := column.Content["width"].(float64); ok && width > 0 && width < 100 {
			opening = `<column ratio="` + strconv.FormatFloat(width, 'f', -1, 64) + `">`
		}
		held := append(append([]string{prefix + opening}, w.flow(w.children[column.ID], prefix)...), prefix+"</column>")
		lines = append(lines, w.marked(column.ID, column.Type, prefix, held)...)
	}
	return append(lines, prefix+"</columns>"), true
}

// embedAddress is the address a bookmark or embed names: its web address, or
// an uploaded embed's file as a file block's is named. ok is false for an
// address no block keeps.
func (w *notionMarkdownWriter) embedAddress(block workspaceExportMarkdownBlock) (address string, stored, ok bool) {
	address = mapString(block.Content, "url")
	if fileID := mapString(block.Content, "fileId"); fileID != "" {
		address = workspaceImportFileReference + fileID
		if w.mentions.FileAddress != nil {
			address = w.mentions.FileAddress(block.ID, fileID, mapString(block.Content, "name"))
		}
		return address, true, true
	}
	// Any address the block keeps is said as kept, spaces too, as Notion's
	// reads it.
	return address, false, address == "" || validGraphStoredFileAddress(address)
}

// linkCaption writes a bookmark's or embed's caption.
func (w *notionMarkdownWriter) linkCaption(block workspaceExportMarkdownBlock) (string, bool) {
	raw, present := block.Content["caption"]
	if !present {
		return "", true
	}
	encoded, err := json.Marshal(raw)
	spans := []graphTextSpan{}
	if err != nil || json.Unmarshal(encoded, &spans) != nil {
		return "", false
	}
	text, written := notionMarkdownText(spans, w.mentions.at(block.ID))
	if !written {
		return "", false
	}
	if w.mentions.APIForm {
		return strings.ReplaceAll(text, "\n", "<br>"), true
	}
	return strings.ReplaceAll(text, "\n", "<br />"), true
}

// notionMarkdownIndented is a line of code or of an equation as far in as its
// block stands: an empty one stands without the indent, as Notion's writes it
// (seen 2026-11-10, in a list item and a to-do).
func notionMarkdownIndented(prefix, line string) string {
	if line == "" {
		return ""
	}
	return prefix + line
}

// linkBlock writes a web bookmark or embed, its caption as the tag's text.
func (w *notionMarkdownWriter) linkBlock(block workspaceExportMarkdownBlock, prefix string) ([]string, bool) {
	service := strings.TrimSpace(mapString(block.Content, "service"))
	tag := "bookmark"
	if block.Type == graphBlockTypeEmbed {
		tag = "embed"
	}
	attributes := ""
	if service != "" {
		attributes = ` service="` + notionMarkdownAttributeEscaped.Replace(service) + `"`
	}
	address, _, ok := w.embedAddress(block)
	if !ok {
		return nil, false
	}
	caption, written := w.linkCaption(block)
	if !written {
		return nil, false
	}
	w.rendered = append(w.rendered, block.ID)
	// One without its address yet waits for it, as Notion's <bookmark /> does,
	// its caption between its tags (seen 2026-11-10); a bookmark's address
	// other than a web one (notionMarkdownWebAddress) is left out so too
	// (seen 2026-11-11).
	opening := "<" + tag + attributes
	if address != "" && (block.Type != graphBlockTypeBookmark || notionMarkdownWebAddress(address)) {
		opening = "<" + tag + ` source="` + notionMarkdownAttributeEscaped.Replace(address) + `"`
	}
	// Without a caption it closes in itself, as Notion's reads (seen 2026-11-04).
	if caption == "" {
		return []string{prefix + opening + " />"}, true
	}
	return []string{prefix + opening + ">", prefix + caption, prefix + "</" + tag + ">"}, true
}
