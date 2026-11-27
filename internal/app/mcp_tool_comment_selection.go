package app

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A selection names text as Notion's MCP finds it (seen 2026-11-11 and
// 2026-11-22), in the page's Markdown as fetch writes it first
// (mcp_tool_comment_selection_markdown.go) and then, where that names no one
// place, in the page's words: its blocks' words in reading order
// with a blank line between blocks, spaces closed up. Text on both sides of
// its first ellipsis ("..." or "…", notionMarkdownCutEllipsis) is
// "start...end", a start and an end found at or after it, the end able to
// stand within the start — each such pair counts, so an end found twice
// after the start is two places; words alone, or with a side of that
// ellipsis left empty, are the words as both ("zulu...." is "zulu" and ".",
// "zulu..." the words "zulu..."). Its sides are found as written first and,
// where that finds no pair, by their words, as a page's Markdown selection's
// are (notionMarkdownWordStretches: letters and digits in any case, whatever
// stands between them — "xray3....yankee zulu" finds "xray3" and "yankee
// zulu", "Delta. Echo" "delta. echo"), each found within its line. One side
// left empty runs the other's words on to the edge of their word on that
// side, one space beyond it and the marks beyond that, within the block
// ("fox..." is "foxtrot " in "foxtrot alpha", "kilo..." "kilo ." in "kilo
// .lima"); spaces given before or after the words take the space beside
// them, where there is one. One place within one block marks those words in
// it; one running past its block is a comment on the block it starts in. No
// place, or more than one, is refused as Notion's is, with the text the
// places were counted in: the Markdown's, unless only the words held more
// than one. A suggested edit's old_text is looked for the same way, as
// exact text (readMCPExactText).

const mcpCommentSelectionBlockBreak = "\n\n"

type mcpCommentSelectionBlock struct {
	ID      string `db:"id"`
	Content string `db:"content"`
}

// mcpCommentSelectionText is a block's place in the page's text.
type mcpCommentSelectionText struct {
	id         string
	text       string
	start, end int
}

// findMCPCommentSelectionBlockForTarget finds the block a selection names,
// and the words it marks there: none for a selection running past its block.
// A block holding a mention closed to the agent (closed, nil for none) reads
// to it as an unknown block, so it names none of its text
// (mcp_reader_mentions.go); page is how the agent reads the page's Markdown.
func findMCPCommentSelectionBlockForTarget(
	app core.App,
	target nativeCommentTarget,
	selection string,
	closed searchMentionCloser,
	page mcpSelectionPage,
) (string, string, *mcpCommentQuoteRange, error) {
	query, ok := readMCPCommentSelection(selection)
	if !ok {
		return "", "", nil, mcpInvalid("selection_with_ellipsis is the start and end of the text, as \"First words...last words\"")
	}
	document, _, err := loadYgoDoc(app, target.ID)
	if err != nil {
		return "", "", nil, failedBecause(errMCPToolFailed, err)
	}
	texts, err := readMCPSelectionTexts(app, target.ID, document, closed, page)
	if err != nil {
		return "", "", nil, err
	}
	found := texts.find(texts.markdown.place(selection), query)
	switch {
	case found.found == 1 && found.blockID != "":
		// Words in a table row's cell are the row's, in that cell.
		if found.cell > 0 && found.marked != nil {
			found.marked.field = commentQuoteCellField(found.cell)
		}
		return found.blockID, found.quote, found.marked, nil
	case found.found == 1:
		return "", "", nil, errMCPToolFailed
	case found.found > 1:
		return "", "", nil, mcpInvalid("More than one place matches: %d places for <pattern>%s</pattern> in the page as it is now: <target>%s</target>.",
			found.found, query.pattern, found.target)
	}
	return "", "", nil, mcpInvalid("No match for <pattern>%s</pattern> in the page as it is now: <target>%s</target>.",
		query.pattern, found.target)
}

// mcpSelectionTexts is a page's text as a selection, or a suggested edit's
// old_text, is looked for in it: its Markdown as fetch writes it, and its
// blocks' words in reading order with a blank line between blocks, spaces
// closed up, blocks holding a mention closed to the agent left out.
type mcpSelectionTexts struct {
	markdown mcpSelectionMarkdown
	words    string
	places   []mcpCommentSelectionText
}

// readMCPSelectionTexts reads a page's texts — a database page's under the
// row's ID — from its text document.
func readMCPSelectionTexts(app core.App, targetID string, document *crdt.Doc, closed searchMentionCloser, page mcpSelectionPage) (mcpSelectionTexts, error) {
	// Every block, however many: a selection is looked for in the whole
	// page, as fetch writes it whole.
	blocks := []mcpCommentSelectionBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:targetId} ORDER BY [[id]]
	`).Bind(dbx.Params{"targetId": targetID}).All(&blocks)
	if err != nil {
		return mcpSelectionTexts{}, failedBecause(errMCPToolFailed, err)
	}
	contents := make(map[string]string, len(blocks))
	ids := make([]string, 0, len(blocks))
	for _, block := range blocks {
		contents[block.ID] = block.Content
		ids = append(ids, block.ID)
	}
	var written strings.Builder
	texts, open := mcpSelectionTexts{}, []string{}
	for _, id := range mcpBlocksInReadingOrder(app, targetID, ids) {
		ytext := document.GetText(ygoBlockTextRootPrefix + id)
		if mcpTextHoldsClosed(ytext.ToDelta(), contents[id], closed) {
			continue
		}
		open = append(open, id)
		text := ytext.ToString()
		if text == "" {
			text = mcpCommentLegacyBlockText(contents[id])
		}
		words := normalizeMCPCommentSelectionText(text)
		if words == "" {
			continue
		}
		if written.Len() > 0 {
			written.WriteString(mcpCommentSelectionBlockBreak)
		}
		texts.places = append(texts.places, mcpCommentSelectionText{id: id, text: text, start: written.Len(), end: written.Len() + len(words)})
		written.WriteString(words)
	}
	texts.words = written.String()
	if texts.markdown, err = readMCPSelectionMarkdown(app, page, targetID, document, open, contents); err != nil {
		return mcpSelectionTexts{}, failedBecause(errMCPToolFailed, err)
	}
	return texts, nil
}

// mcpCommentSelection is a selection as it is looked for: its start and end,
// its spaces closed up — the same words for words alone or a side left
// empty, the ellipsis kept in them, which run on (oneSided) — and how a
// refusal names it, as Notion's names it: "start...end", the words as both
// ("words...words"), or the selection as given as both
// ("words......words...").
type mcpCommentSelection struct {
	start, end string
	oneSided   bool
	// spaceBefore and spaceAfter are a selection's spaces before and after
	// its words, which take the space beside them where there is one (seen
	// 2026-11-11: "  delta  " is " delta" at its block's end).
	spaceBefore, spaceAfter bool
	// exact is text looked for as it is, as a suggested edit's old_text:
	// "..." in it is its own words, and each place it stands counts once.
	exact   bool
	pattern string
}

// readMCPExactText reads text looked for as it is: its runs of spaces closed
// up as the page's are, those before and after its words kept; false for one
// with no words.
func readMCPExactText(text string) (mcpCommentSelection, bool) {
	closed, _ := mcpSelectionClosedText(text)
	return mcpCommentSelection{start: closed, end: closed, exact: true, pattern: text}, strings.TrimSpace(closed) != ""
}

// readMCPCommentSelection reads a selection; false for one with no words.
func readMCPCommentSelection(selection string) (mcpCommentSelection, bool) {
	query, ok := readMCPCommentSelectionWords(normalizeMCPCommentSelectionText(selection))
	query.spaceBefore = strings.TrimLeftFunc(selection, unicode.IsSpace) != selection
	query.spaceAfter = strings.TrimRightFunc(selection, unicode.IsSpace) != selection
	return query, ok
}

func readMCPCommentSelectionWords(normalized string) (mcpCommentSelection, bool) {
	before, after, ranged := notionMarkdownCutEllipsis(normalized)
	before, after = strings.TrimSpace(before), strings.TrimSpace(after)
	if !ranged || before == "" || after == "" {
		words := strings.TrimSpace(strings.NewReplacer("...", "", "…", "").Replace(normalized))
		return mcpCommentSelection{
			start: normalized, end: normalized, oneSided: ranged, pattern: normalized + "..." + normalized,
		}, words != ""
	}
	return mcpCommentSelection{start: before, end: after, pattern: before + "..." + after}, true
}

// place finds where a selection stands in the page's text and how many
// places it names: each start with each end found at or after it, as
// written or else by their words (mcpCommentSelectionStretches), a side
// left empty run on (mcpCommentSelectionRunOn).
func (query mcpCommentSelection) place(text string) (int, int, int) {
	start, end, found := query.placeWords(text)
	if query.spaceBefore && start > 0 && text[start-1] == ' ' {
		start--
	}
	if query.spaceAfter && end < len(text) && text[end] == ' ' {
		end++
	}
	return start, end, found
}

func (query mcpCommentSelection) placeWords(text string) (int, int, int) {
	if query.exact {
		starts := mcpCommentSelectionOccurrences(text, query.start)
		if len(starts) == 0 {
			return 0, 0, 0
		}
		return starts[0], starts[0] + len(query.start), len(starts)
	}
	if query.oneSided {
		// Its words as written are what the ellipsis leaves of them, run on.
		words, runsBefore, runsAfter := mcpCommentSelectionRunning(query.start)
		if starts := mcpCommentSelectionOccurrences(text, words); len(starts) > 0 {
			start, end := mcpCommentSelectionRunOn(text, starts[0], starts[0]+len(words), runsBefore, runsAfter)
			return start, end, len(starts) * (len(starts) + 1) / 2
		}
	} else if pairs, found := notionMarkdownPairs(text, query.start, query.end, notionMarkdownWrittenStretches); pairs > 0 {
		return found.start, found.end, pairs
	}
	pairs, found := notionMarkdownPairs(text, query.start, query.end, mcpCommentSelectionStretches)
	return found.start, found.end, pairs
}

// mcpCommentSelectionRunning is a side-left-empty selection's words without
// its ellipses, and which sides they run on.
func mcpCommentSelectionRunning(selection string) (string, bool, bool) {
	words, runsBefore, runsAfter := selection, false, false
	for _, ellipsis := range []string{"...", "…"} {
		if rest, cut := strings.CutPrefix(words, ellipsis); cut && !runsBefore {
			words, runsBefore = strings.TrimSpace(rest), true
		}
	}
	for _, ellipsis := range []string{"...", "…"} {
		if rest, cut := strings.CutSuffix(words, ellipsis); cut && !runsAfter {
			words, runsAfter = strings.TrimSpace(rest), true
		}
	}
	return words, runsBefore, runsAfter
}

// mcpCommentSelectionStretches are where a side's words stand in a page's
// text, as a page's Markdown selection finds them by its words
// (notionMarkdownWordStretches), each kept within its line: a comment marks
// a block's words, and what a side holds around them takes nothing past a
// line break.
func mcpCommentSelectionStretches(text, side string) []notionMarkdownStretch {
	found := notionMarkdownWordStretches(text, side)
	for index, stretch := range found {
		words := strings.TrimLeftFunc(text[stretch.start:stretch.end], func(r rune) bool { return !notionMarkdownWordRune(r) })
		if lead := text[stretch.start : stretch.end-len(words)]; strings.Contains(lead, "\n") {
			stretch.start += strings.LastIndexByte(lead, '\n') + 1
		}
		trimmed := strings.TrimRightFunc(text[stretch.start:stretch.end], func(r rune) bool { return !notionMarkdownWordRune(r) })
		if tail := text[stretch.start+len(trimmed) : stretch.end]; strings.Contains(tail, "\n") {
			stretch.end = stretch.start + len(trimmed) + strings.IndexByte(tail, '\n')
		}
		found[index] = stretch
	}
	return found
}

// mcpCommentSelectionRunOn runs words standing from start to end on, on the
// sides given, to the edge of their word, one space beyond it and the marks
// standing right beyond that, within their block (seen 2026-11-22: "kilo..." is "kilo ." in
// "kilo .lima", "pap..." "papa --" in "papa -- quebec", "...tango" ",
// tango" in "sierra, tango").
func mcpCommentSelectionRunOn(text string, start, end int, runsBefore, runsAfter bool) (int, int) {
	// A mark is a character of one byte that is no space, letter or digit.
	mark := func(r byte) bool {
		return r < utf8.RuneSelf && r != ' ' && r != '\n' && !notionMarkdownWordRune(rune(r))
	}
	if runsBefore {
		for start > 0 && text[start-1] != ' ' && text[start-1] != '\n' {
			start--
		}
		if start > 0 && text[start-1] == ' ' {
			start--
			for start > 0 && mark(text[start-1]) {
				start--
			}
		}
	}
	if runsAfter {
		for end < len(text) && text[end] != ' ' && text[end] != '\n' {
			end++
		}
		if end < len(text) && text[end] == ' ' {
			end++
			for end < len(text) && mark(text[end]) {
				end++
			}
		}
	}
	return start, end
}

// mcpCommentSelectionOccurrences are where words stand in the page's text,
// each place they start, in order.
func mcpCommentSelectionOccurrences(text, words string) []int {
	places := []int{}
	for at := 0; at < len(text); {
		relative := strings.Index(text[at:], words)
		if relative < 0 {
			break
		}
		places = append(places, at+relative)
		at += relative + 1
	}
	return places
}

// mcpBlocksInReadingOrder puts blocks in the order the page shows them; its
// structural document holds that order, as search reads it.
func mcpBlocksInReadingOrder(app core.App, room string, ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	if document, found, done, err := borrowYgoStreamDocument(app, ygoStructuralStream, room); err == nil {
		if found {
			ids = searchBlocksInReadingOrder(document, ids)
		}
		done()
	}
	return ids
}

// mcpCommentQuoteRange is where a thread's text stands in its block, in the
// units the page's text counts.
type mcpCommentQuoteRange struct {
	start, end int
	// field is the quote's field: "" for a block's text, "cell:<n>" for a
	// table row's cell (comment_table_cell_anchors.go).
	field string
}

// mcpCommentQuoteOffsets finds a quote, whose spaces are closed up, in its
// block's text; nil when it isn't there as a whole.
func mcpCommentQuoteOffsets(text, quote string) *mcpCommentQuoteRange {
	runes := []rune(text)
	// Each character the closed-up text keeps, by where it stands.
	kept := make([]rune, 0, len(runes))
	places := make([]int, 0, len(runes))
	for index, character := range runes {
		if unicode.IsSpace(character) {
			if len(kept) == 0 || unicode.IsSpace(kept[len(kept)-1]) {
				continue
			}
			character = ' '
		}
		kept = append(kept, character)
		places = append(places, index)
	}
	at := strings.Index(string(kept), quote)
	if at < 0 {
		return nil
	}
	first := utf8.RuneCountInString(string(kept)[:at])
	last := first + utf8.RuneCountInString(quote) - 1
	if last >= len(places) {
		return nil
	}
	return &mcpCommentQuoteRange{
		start: len(utf16.Encode(runes[:places[first]])),
		end:   len(utf16.Encode(runes[:places[last]+1])),
	}
}

func mcpCommentLegacyBlockText(content string) string {
	var value struct {
		Text string `json:"text"`
		Rich []struct {
			Text string `json:"text"`
		} `json:"rich"`
	}
	if json.Unmarshal([]byte(content), &value) != nil {
		return ""
	}
	if value.Text != "" {
		return value.Text
	}
	var builder strings.Builder
	for _, span := range value.Rich {
		builder.WriteString(span.Text)
	}
	return builder.String()
}

func normalizeMCPCommentSelectionText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
