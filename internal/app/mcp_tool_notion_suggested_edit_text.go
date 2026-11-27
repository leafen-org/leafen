package app

import (
	"encoding/json"
	"maps"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/core"
)

// A suggested edit's new_text is Notion Markdown — Notion's tool takes it as
// "Proposed Notion Markdown" (its description, read 2026-11-22; its answers
// weren't seen, suggested edits run on Business only) — read as the page's
// Markdown reads a line where it goes, as notion-update-page reads it: its
// marks, colors, links and mentions are the suggested words' formatting,
// <br> a line break in the block, an escape its character, so "<b>골프채</b>."
// suggests "골프채" in bold. The words also take the formatting the words
// they replace all share, or the word they go beside, as typing there does —
// Markdown put inside "<b>…</b>" is bold — unless old_text was copied with
// the markup at that edge.
// A block marker at new_text's start is one where old_text starts the
// block's line: the block's own ("## " where old_text holds "## ") stays
// as it is, as does new_text written without one; another kind's would turn
// the block into that kind, which a suggestion of words doesn't do, and is
// refused, as is a block's tag other than its own, look and all, as fetch
// writes it ("<p color=\"red\">⏎words⏎</p>" for a red paragraph's).
// Elsewhere in a line a marker or a one-line tag is words, as Markdown reads
// it, and a tag over lines can't start there
// (mcp_tool_notion_suggested_edit_tags.go).
// Its line breaks end the line as Enter in suggesting mode does: its first
// line goes on where old_text is, its other lines are blocks suggested after
// the block — an open <toggle>'s first, as its Markdown holds them — a block
// a line, with a blank line between them or none, a block's tag over lines
// as fetch writes one one line, <br> or a backslash ending a line the break
// inside one (mcp_tool_notion_suggested_edit_lines.go),
// read as the page's Markdown reads a block's line — a callout's words its
// first line inside its tag, as fetch writes them, each block in its kind's
// own look (mcp_tool_notion_suggested_edit_look.go) — and the block's words
// after the edit go on its last line: the block splits there, as Enter
// there splits it, the words moving to a block of the last line's kind. At
// the block's very start the new lines come before it — after the text block
// before it, first in its toggle, or, with neither, before it through a
// break at its start — and the block keeps its words and kind. A code
// block's new_text is its words as they are, lines and all.

// mcpSuggestedPiece is a piece of suggested words with the formatting and
// mention a suggested insert takes.
type mcpSuggestedPiece struct {
	text  string
	marks map[string]any
}

// mcpSuggestedLine is a block new_text suggests: its kind and its words.
type mcpSuggestedLine struct {
	kind   string
	pieces []mcpSuggestedPiece
}

// mcpSuggestedText is new_text as the edit suggests it: the words that go in
// where old_text is (first); with a line break, the blocks its other lines
// make (blocks) and, where the block has words after the edit, its last
// line, which those words go on (last). At the block's start (before) the
// blocks come before it, its first line the first of them, and the last
// line's words go at the block's start.
type mcpSuggestedText struct {
	first  []mcpSuggestedPiece
	blocks []mcpSuggestedLine
	last   *mcpSuggestedLine
	before bool
}

// mcpSuggestedLineMarker is a block marker at a line's start as Notion's
// Markdown writes one: a heading's, a to-do's, a list item's or a quote's.
var mcpSuggestedLineMarker = regexp.MustCompile(`^[ \t]*(#{1,4}[ \t]+|[-*+][ \t]+\[[ xX]\][ \t]+|[-*+][ \t]+|\d+[.)][ \t]+|>[ \t]?)`)

// mcpSuggestedBox is a to-do marker's box at a line's start, " " or "x",
// "" for none.
func mcpSuggestedBox(line string) string {
	if box := mcpSuggestedToDoBox.FindStringSubmatch(line); box != nil {
		return strings.ToLower(box[1])
	}
	return ""
}

var mcpSuggestedToDoBox = regexp.MustCompile(`^[ \t]*[-*+][ \t]+\[([ xX])\]`)

// readMCPSuggestedText reads new_text for an edit at old_text's place.
func readMCPSuggestedText(
	app core.App, authority mcpAuthority, current pageAuthorization, place mcpSuggestionPlace, mode, newText string,
) (mcpSuggestedText, error) {
	newText = strings.ReplaceAll(strings.ReplaceAll(newText, "\r\n", "\n"), "\r", "\n")
	if place.Type == graphBlockTypeCode {
		read := mcpSuggestedText{}
		if newText != "" {
			read.first = []mcpSuggestedPiece{{text: newText, marks: map[string]any{}}}
		}
		return read, nil
	}
	site := mcpValueSitePages(app, authority, current)
	// Its lines, a block's tag over lines one of them (mcpSuggestedTagLines).
	cut := mcpSuggestedTagLines(mcpSuggestedBackslashBreaks(newText))
	line, broken := cut[0], len(cut) > 1
	at := place.Offset
	if mode != "insert_before" {
		at += place.Length
	}
	tail := broken && at < len(utf16.Encode([]rune(place.Text)))
	read := mcpSuggestedText{before: tail && at == 0}
	// A break moves the words after it, which an open suggestion's words
	// among them keep from moving (page_suggestion_splits.go).
	for _, run := range place.Runs {
		if kind, id := run.suggestion(); tail && !read.before && run.End > at && (kind != "" || id != "") {
			return read, mcpInvalid("the block's words after new_text's line break hold a suggestion still open, which a break doesn't move: settle it first, or break the line where no suggested words follow")
		}
	}
	// The lines new_text makes, the first going on where old_text is (none
	// at the block's start, where its lines are all blocks of their own).
	lines, kinds, rest := [][]graphTextSpan{nil}, []string{""}, cut[1:]
	if read.before {
		rest = cut
	} else {
		first, err := mcpSuggestedFirstLine(app, current.Page.Id, place, mode, line, site)
		if err != nil {
			return read, err
		}
		lines[0] = first
	}
	if broken {
		// No line stands inside a block, its last line nor one under the
		// block's own line either (mcpSuggestedUnder).
		if err := mcpSuggestedUnder(place.Type, read.before, rest, site); err != nil {
			return read, err
		}
		// The words after the edit go on new_text's last line, as written,
		// or on a line of their own after a break that ends it.
		middle, last := strings.Join(rest, "\n"), ""
		if tail {
			middle, last = strings.Join(rest[:len(rest)-1], "\n"), rest[len(rest)-1]
		}
		items, err := mcpSuggestedBlockItems(middle, site)
		if err != nil {
			return read, err
		}
		for _, item := range items {
			lines, kinds = append(lines, mcpSuggestedItemSpans(item)), append(kinds, item.Type)
		}
		if tail {
			kind, words, err := mcpSuggestedLastLine(last, site)
			if err != nil {
				return read, err
			}
			lines, kinds = append(lines, words), append(kinds, kind)
		}
	}
	prepared, err := mcpSuggestedMentions(app, authority, current, lines)
	if err != nil {
		return read, err
	}
	for index, spans := range prepared {
		inherited := map[string]any{}
		if index == 0 {
			inherited = mcpSuggestionInherited(place, mode)
		}
		pieces, err := mcpSuggestedPieces(spans, inherited)
		if err != nil {
			return read, err
		}
		switch {
		case index == 0:
			read.first = pieces
		case tail && index == len(prepared)-1:
			read.last = &mcpSuggestedLine{kind: kinds[index], pieces: pieces}
		default:
			read.blocks = append(read.blocks, mcpSuggestedLine{kind: kinds[index], pieces: pieces})
		}
	}
	return read, nil
}

// mcpSuggestedFirstLine is the words of new_text's first line, which go on
// where old_text is. Where old_text starts the block's line, so does
// new_text: a marker at its start is the block's own, or none, and another
// kind's is refused, as is a block's tag other than its own as fetch writes
// it (mcpSuggestedOwnTag). Elsewhere in the line a marker or a tag on one
// line is words, as Markdown reads it, and a tag going on over lines is
// refused (mcp_tool_notion_suggested_edit_tags.go).
func mcpSuggestedFirstLine(app core.App, pageID string, place mcpSuggestionPlace, mode, line string, site *notionMarkdownSitePages) ([]graphTextSpan, error) {
	startsLine := mode != "insert_after" && (place.LineStart || place.Held != "")
	tagged := mcpSuggestedTagOpening(line) != ""
	switch kind := mcpSuggestedLineKind(line, site); {
	case strings.Contains(line, "\n") && (!startsLine || !tagged):
		return nil, mcpSuggestedTagOverLines(line)
	case !startsLine:
	case tagged:
		return mcpSuggestedOwnTag(app, pageID, place, line, site)
	case place.Held != "" && strings.HasPrefix(line, place.Held):
		line = line[len(place.Held):]
	case kind == "" || kind == graphBlockTypeParagraph:
	case kind == place.Type && mcpSuggestedBox(place.Held) != mcpSuggestedBox(line):
		return nil, mcpInvalid("new_text checks or unchecks the to-do, which a suggested edit of its words doesn't: keep its box as old_text has it")
	case kind == place.Type:
		line = mcpSuggestedLineMarker.ReplaceAllString(line, "")
	default:
		return nil, mcpSuggestedKindChange("marker", kind, place.Type)
	}
	return mcpSuggestedLineSpans(line, site), nil
}

// mcpSuggestedLineKind is the kind of block a line of Notion Markdown makes
// as a line of its own, "" for none.
func mcpSuggestedLineKind(line string, site *notionMarkdownSitePages) string {
	if strings.TrimSpace(line) == "" {
		return ""
	}
	read, _, err := notionMarkdownVersion2Input(line)
	if err != nil {
		return ""
	}
	items, err := parseNotionMarkdown(read, false, site)
	if err != nil || len(items) == 0 {
		return ""
	}
	return items[0].Type
}

// mcpSuggestedLastLine is the kind and words of new_text's last line, which
// the block's words after the edit go on: a paragraph, or what its marker
// or its block's tag makes ("<h2>…</h2>", or over lines as fetch writes one,
// "<p>⏎나⏎</p>": read as the page's Markdown reads the block, its look as a
// suggested block takes it, mcpSuggestedTagLastLine); its words as written,
// spaces and all.
func mcpSuggestedLastLine(line string, site *notionMarkdownSitePages) (string, []graphTextSpan, error) {
	if mcpSuggestedTagOpening(line) != "" || strings.Contains(line, "\n") {
		return mcpSuggestedTagLastLine(line, site)
	}
	kind := mcpSuggestedLineKind(line, site)
	if _, takes := suggestionBlockTypes[kind]; kind == "" || !takes || !mcpSuggestedLineMarker.MatchString(line) {
		return graphBlockTypeParagraph, mcpSuggestedLineSpans(strings.TrimLeft(line, " \t"), site), nil
	}
	if kind == graphBlockTypeToDo && mcpSuggestedBox(line) == "x" {
		return "", nil, mcpInvalid("a suggested to-do starts unchecked: write it \"- [ ] \"")
	}
	return kind, mcpSuggestedLineSpans(mcpSuggestedLineMarker.ReplaceAllString(line, ""), site), nil
}

// mcpSuggestedLineSpans reads words going on a line as an MCP write's inline
// Markdown, a title's, a text property's value or a comment
// (mcpInlineMarkdownSpans): its marks, colors, links and mentions, its
// escapes, a <br> as a line break; none for no words. What the reading says
// goes unsaid, as of the blocks it suggests (mcpSuggestedBlocksAsWritten):
// a suggestion's answer is Notion's.
func mcpSuggestedLineSpans(line string, site *notionMarkdownSitePages) []graphTextSpan {
	if line == "" {
		return nil
	}
	plain, spans, _ := mcpInlineMarkdownSpans(line, site)
	if spans == nil {
		spans = []graphTextSpan{{Text: plain}}
	}
	return spans
}

// mcpSuggestedBlockItems reads new_text's lines after its first as
// notion-update-page reads Markdown, each line a block of its own
// (mcpSuggestedOwnLines): blocks one after another, each a block whose text
// takes suggestions. What the page's Markdown reads of them as written is
// refused as before, a table or code over lines too.
func mcpSuggestedBlockItems(markdown string, site *notionMarkdownSitePages) ([]workspaceImportStagedItem, error) {
	if strings.TrimSpace(markdown) == "" {
		return nil, nil
	}
	if _, err := mcpSuggestedBlockRead(markdown, site); err != nil {
		return nil, err
	}
	return mcpSuggestedBlockRead(mcpSuggestedOwnLines(markdown), site)
}

// mcpSuggestedBlockRead reads Markdown as blocks suggested
// (mcpSuggestedBlocksAsWritten), refused for a checked to-do or a look a
// suggested block doesn't carry.
func mcpSuggestedBlockRead(markdown string, site *notionMarkdownSitePages) ([]workspaceImportStagedItem, error) {
	items, err := mcpSuggestedBlocksAsWritten(markdown, site)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		content := map[string]any{}
		if json.Unmarshal(item.Content, &content) == nil && content["checked"] == true {
			return nil, mcpInvalid("a suggested to-do starts unchecked: write it \"- [ ] \"")
		}
		if look := mcpSuggestedLook(item, content); look != "" {
			return nil, mcpSuggestedLookRefusal(item.Type, look)
		}
	}
	return items, nil
}

// mcpSuggestedBlocksAsWritten reads Markdown as the page's Markdown reads
// it, a callout's first line its words (mcpSuggestedCalloutWords), refused
// for a block inside a block or a kind a suggestion can't suggest.
func mcpSuggestedBlocksAsWritten(markdown string, site *notionMarkdownSitePages) ([]workspaceImportStagedItem, error) {
	read, _, err := notionMarkdownVersion2Input(markdown)
	if err != nil {
		return nil, mcpInvalid("new_text's lines aren't Notion Markdown Leafen reads: %s", err.Error())
	}
	items, err := parseNotionMarkdown(read, false, site)
	if err != nil {
		return nil, mcpInvalid("new_text's lines aren't Notion Markdown Leafen reads: write them as notion-fetch writes a page's blocks")
	}
	items = mcpSuggestedCalloutWords(items)
	for _, item := range items {
		if item.ParentID != "" {
			return nil, mcpSuggestedInsideRefusal()
		}
		if _, takes := suggestionBlockTypes[item.Type]; !takes || item.Type == graphBlockTypeCode {
			return nil, mcpSuggestedKindRefusal(item.Type)
		}
	}
	return items, nil
}

// mcpSuggestedCalloutWords are blocks read from Markdown with each
// callout's first line its words, as fetch writes a callout's words, on the
// first line inside its tag ("<callout>⏎글⏎</callout>"), where the page's
// Markdown reads a paragraph inside it: a callout suggested with its words,
// as suggesting mode makes one. Its lines after the first stay blocks
// inside it.
func mcpSuggestedCalloutWords(items []workspaceImportStagedItem) []workspaceImportStagedItem {
	out := make([]workspaceImportStagedItem, 0, len(items))
	for index := 0; index < len(items); index++ {
		item := items[index]
		if next := index + 1; item.Type == graphBlockTypeCallout && item.Text == "" && len(item.Spans) == 0 &&
			next < len(items) && items[next].ParentID == item.ID && items[next].Type == graphBlockTypeParagraph {
			item.Text, item.Spans = items[next].Text, items[next].Spans
			index = next
		}
		out = append(out, item)
	}
	return out
}

func mcpSuggestedKindRefusal(kind string) error {
	return mcpInvalid("new_text makes a %s, which a suggested edit can't suggest: a suggested block is a paragraph, heading, list item, to-do, toggle, quote or callout", mcpBlockKindName(kind))
}

// mcpBlockKindName is a block type as a refusal names it.
func mcpBlockKindName(kind string) string {
	if kind == "" {
		return "block"
	}
	return strings.ReplaceAll(kind, "_", " ")
}

// mcpSuggestedMentions names the people, pages and dates new_text's lines
// mention as the page's Markdown writes name them: by the tools' IDs or the
// public API's, and only what the agent reaches.
func mcpSuggestedMentions(app core.App, authority mcpAuthority, current pageAuthorization, lines [][]graphTextSpan) ([][]graphTextSpan, error) {
	updates := make([]graphTextUpdate, len(lines))
	for index, spans := range lines {
		updates[index] = graphTextUpdate{Spans: mcpCommentLocalMentions(app, spans)}
	}
	input, err := narrowedMarkdownMentionInput(authority.PrincipalID, mcpMentionReaches(authority))(
		app, current, nil, graphMutationInput{TextUpdates: updates},
	)
	if err != nil {
		return nil, err
	}
	prepared := make([][]graphTextSpan, len(lines))
	for index := range lines {
		prepared[index] = input.TextUpdates[index].Spans
	}
	return prepared, nil
}

// mcpSuggestionInherited is the formatting new words take from where they
// go: replacing words, what those all share; before or after them, the
// first or last word's — none at an edge old_text holds markup at.
func mcpSuggestionInherited(place mcpSuggestionPlace, mode string) map[string]any {
	from, to := place.Offset, place.Offset+place.Length
	switch {
	case mode == "insert_before" && place.Opens, mode == "insert_after" && place.Closes,
		mode != "insert_before" && mode != "insert_after" && (place.Opens || place.Closes):
		return map[string]any{}
	case mode == "insert_before":
		to = from + 1
	case mode == "insert_after":
		from = to - 1
	}
	var shared map[string]any
	for _, run := range place.Runs {
		if run.End <= from || run.Start >= to {
			continue
		}
		attributes, stands := textAttributesAsTheyStand(run.Attributes, false)
		if !stands {
			continue
		}
		marks := map[string]any{}
		for _, mark := range textSuggestionMarks {
			if value := attributes[mark]; value != nil && value != false && value != "" {
				marks[mark] = value
			}
		}
		if shared == nil {
			shared = marks
			continue
		}
		for mark, value := range shared {
			if marks[mark] != value {
				delete(shared, mark)
			}
		}
	}
	if shared == nil {
		return map[string]any{}
	}
	return shared
}

// mcpSuggestedPieces are words as suggested inserts take them, each with its
// own formatting over what it inherits; refused for a piece whose kind a
// suggestion's words don't carry.
func mcpSuggestedPieces(spans []graphTextSpan, inherited map[string]any) ([]mcpSuggestedPiece, error) {
	pieces := []mcpSuggestedPiece{}
	for _, span := range spans {
		if span.Text == "" {
			continue
		}
		what := ""
		switch {
		case span.Equation != "":
			what = "an equation"
		case span.Mention == "database" || span.DatabaseID != "":
			what = "a database's mention"
		case span.BlockID != "":
			what = "a block's mention"
		case span.CustomEmojiID != "":
			what = "a custom emoji"
		case span.IconURL != "" || span.Provider != "" || span.ImageURL != "" || span.Description != "":
			what = "a link preview"
		case span.Template != "" || span.DateStartUnset:
			what = "a template's mention"
		case span.Mention != "" && span.Mention != "person" && span.Mention != "page" && span.Mention != "date":
			what = "a " + span.Mention + " mention"
		}
		if what != "" {
			return nil, mcpInvalid("new_text holds %s, which a suggested edit's words can't carry: they take marks, colors, links and mentions of people, pages and dates", what)
		}
		marks := maps.Clone(inherited)
		for mark, on := range map[string]bool{
			"bold": span.Bold, "italic": span.Italic, "underline": span.Underline, "strikethrough": span.Strikethrough, "code": span.Code,
		} {
			if on {
				marks[mark] = true
			}
		}
		for mark, value := range map[string]string{"color": span.Color, "background": span.Background, "link": span.Link} {
			if value != "" {
				marks[mark] = value
			}
		}
		if span.Mention != "" {
			for key, value := range map[string]string{
				"mention": span.Mention, "userId": span.UserID, "pageId": span.PageID, "date": span.Date, "dateEnd": span.DateEnd,
				"dateTimeZone": span.DateTimeZone, "dateReminder": span.DateReminder, "dateFormat": span.DateFormat,
				"dateTimeFormat": span.DateTimeFormat,
			} {
				if value != "" {
					marks[key] = value
				}
			}
		}
		if last := len(pieces) - 1; last >= 0 && span.Mention == "" && pieces[last].marks["mention"] == nil && maps.Equal(pieces[last].marks, marks) {
			pieces[last].text += span.Text
			continue
		}
		pieces = append(pieces, mcpSuggestedPiece{text: span.Text, marks: marks})
	}
	return pieces, nil
}
