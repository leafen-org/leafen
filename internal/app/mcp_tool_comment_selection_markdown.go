package app

import (
	"context"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A selection is looked for first as Notion's MCP looks for it (seen
// 2026-11-21, worker w2284 and w2294): in the page's Markdown as fetch
// writes it, marks, links and block markers in it, so "합성 <b>기록</b>"
// holds no "합성 기록" and "구역 1 —...합성 기록" names the heading alone
// where the words stand three times. Its runs of spaces are closed up, a
// run holding a line break as one, as the selection's are. One place there
// is taken: in one block's words it marks the words it holds, past them a
// comment on the block it starts in, and on markup alone the block too. The
// Markdown holds no earlier comments' marks, which Notion's does.

// mcpSelectionPage is the page a selection is looked for in, as its reader
// reads its Markdown.
type mcpSelectionPage struct {
	owner, workspaceID string
	reader             notionMarkdownReader
}

// mcpSelectionPageOf is a page in a workspace as an agent reads it.
func mcpSelectionPageOf(authority mcpAuthority, workspace workspaceAuthorization) mcpSelectionPage {
	return mcpSelectionPage{owner: workspace.OwnerID, workspaceID: workspace.WorkspaceID, reader: mcpMarkdownReader(authority)}
}

// mcpSelectionMarkdown is a page's Markdown and where its blocks' words
// stand in it, the blocks in reading order, and each block's type.
type mcpSelectionMarkdown struct {
	text   string
	blocks []mcpSelectionMarkdownBlock
	types  map[string]string
	spans  []notionMarkdownBlockSpan
}

// mcpSelectionMarkdownBlock is a block's text as the Markdown writes it: its
// words (raw), where the text's Markdown stands in the page's ("at" -1 for
// text not found there) and, for each of its bytes, the runes of raw it
// writes, [first, last), -1 for markup.
type mcpSelectionMarkdownBlock struct {
	id string
	// cell is a table row's cell (from 1) the text is, as its own block
	// (mcp_tool_comment_selection_cells.go); 0 for a block's text.
	cell        int
	raw         []rune
	at          int
	first, last []int
	// shown is whether the page's Markdown holds the block at all, which a
	// suggested block's doesn't.
	shown bool
}

func (block mcpSelectionMarkdownBlock) end() int {
	return block.at + len(block.first)
}

// readMCPSelectionMarkdown reads a page's Markdown as fetch writes it, and
// its blocks' text in it, the blocks given in reading order.
func readMCPSelectionMarkdown(
	app core.App, page mcpSelectionPage, pageID string, document *crdt.Doc, ids []string, contents map[string]string,
) (mcpSelectionMarkdown, error) {
	projection, marks := notionPageMarkdownProjection{}, &notionMarkdownBlockMarks{}
	err := runNotionReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		projection, err = projectNotionPageMarkdownMarked(txApp, page.owner, pageID, pageID, page.reader, "", marks)
		return err
	})
	if err != nil {
		return mcpSelectionMarkdown{}, err
	}
	var failed error
	mentions := page.reader.mentions(app, page.workspaceID, &failed)
	mentions.PageID = pageID
	code := map[string]bool{}
	for id, kind := range projection.BlockTypes {
		code[id] = kind == graphBlockTypeCode
	}
	markdown := mcpSelectionMarkdown{types: projection.BlockTypes}
	markdown.text, markdown.spans = mcpSelectionMarkdownSpans(projection.Markdown, marks)
	cursor := 0
	for _, id := range ids {
		block, inline := mcpSelectionTextMarkdown(document.GetText(ygoBlockTextRootPrefix+id), mentions, code[id])
		block.id, block.at = id, -1
		// A block the Markdown doesn't hold, as a suggested one, isn't
		// looked for in it.
		if _, block.shown = projection.BlockTypes[id]; block.shown && inline != "" {
			if at := mcpSelectionTextAt(markdown.text, cursor, inline); at >= 0 {
				block.at, cursor = at, at+len(inline)
			}
		}
		markdown.blocks = append(markdown.blocks, block)
		// A table row's cells stand as blocks of their own (mcp_tool_comment_selection_cells.go).
		markdown.blocks = append(markdown.blocks, markdown.cellBlocks(id, contents[id], mentions, &cursor)...)
	}
	return markdown, failed
}

// mcpSelectionTextAt is where a block's text's Markdown stands in the page's
// at or after cursor, -1 for nowhere: on its line, after its marker, and
// ending it or before what closes it.
func mcpSelectionTextAt(text string, cursor int, inline string) int {
	for at := cursor; at <= len(text); {
		found := strings.Index(text[at:], inline)
		if found < 0 {
			return -1
		}
		start, end := at+found, at+found+len(inline)
		if (start == 0 || strings.IndexByte("\n\t >\\", text[start-1]) >= 0) &&
			(end == len(text) || strings.IndexByte("\n <{", text[end]) >= 0) {
			return start
		}
		at = start + 1
	}
	return -1
}

// mcpSelectionWrittenBlock is a block's text's Markdown as it is built.
type mcpSelectionWrittenBlock struct {
	mcpSelectionMarkdownBlock
	written strings.Builder
}

func (block *mcpSelectionWrittenBlock) inline() string {
	return block.written.String()
}

// add writes Markdown that writes the runes [first, last) of the block's
// words, -1 for markup.
func (block *mcpSelectionWrittenBlock) add(markdown string, first, last int) {
	block.written.WriteString(markdown)
	for range len(markdown) {
		block.first, block.last = append(block.first, first), append(block.last, last)
	}
}

// mcpSelectionTextMarkdown writes a block's collaborative text as
// mcpSelectionSpansMarkdown writes its spans.
func mcpSelectionTextMarkdown(text *crdt.YText, mentions notionMarkdownMentions, code bool) (mcpSelectionMarkdownBlock, string) {
	return mcpSelectionSpansMarkdown(graphTextSpansFromYText(text), mentions, code)
}

func graphTextSpansText(spans []graphTextSpan) string {
	_, text := graphTextSpansAndText(spans)
	return text
}

// addSpan writes a span as notionMarkdownText does, its words by where they
// stand (runes) where its writing keeps them apart, else the span whole;
// false for a mention Markdown doesn't write.
func (block *mcpSelectionWrittenBlock) addSpan(span graphTextSpan, runes []int, first, last int, mentions notionMarkdownMentions) bool {
	if span.Mention != "" {
		tag, text, written := notionMarkdownMentionTag(span, mentions)
		switch {
		case !written:
			return false
		case text != nil:
			tag = notionMarkdownFlavoredSpanText(*text, mentions)
		case tag != "":
			tag = notionMarkdownVersion2Marked(notionMarkdownDiscussed(tag, mentions.discussionsOf(span)), span)
		}
		block.add(tag, first, last)
		return true
	}
	span = notionMarkdownReaderLink(span, mentions)
	written := notionMarkdownFlavoredSpanText(span, mentions)
	// A "!" a link follows is escaped (notionMarkdownText).
	if before := block.inline(); strings.HasPrefix(written, "[") && strings.HasSuffix(before, "!") && !notionMarkdownEscapedAt(before, len(before)-1) {
		at := len(block.first) - 1
		block.written.Reset()
		block.written.WriteString(before[:at] + `\!`)
		block.first = append(block.first[:at], -1, block.first[at])
		block.last = append(block.last[:at], -1, block.last[at])
	}
	body, words := notionMarkdownVersion2EscapedText(span.Text), span.Text
	if span.Code {
		body = "<code>" + span.Text + "</code>"
	}
	opening := strings.Index(notionMarkdownVersion2Marked(notionMarkdownDiscussed("\x00", mentions.discussionsOf(span)), span), "\x00")
	if target := notionMarkdownLinkTarget(span.Link); target != "" && !graphTextSpanIsEquation(span) {
		opening++
	}
	if graphTextSpanIsEquation(span) || opening < 0 || !strings.HasPrefix(written[min(opening, len(written)):], body) {
		block.add(written, first, last)
		return true
	}
	block.add(written[:opening], -1, -1)
	if span.Code {
		block.add("<code>", -1, -1)
		body = words
	}
	if !block.addWords(body, words, runes, span.Code) {
		return false
	}
	if span.Code {
		block.add("</code>", -1, -1)
		opening += len("<code></code>")
	}
	block.add(written[opening+len(body):], -1, -1)
	return true
}

// addWords writes words as written (body) — escaped, or as they are in code
// — each by the rune it is (runes).
func (block *mcpSelectionWrittenBlock) addWords(body, words string, runes []int, code bool) bool {
	at := 0
	for index, character := range []rune(words) {
		letter := string(character)
		if !code && body[at:] != "" && body[at] == '\\' && (character == '\\' || !strings.HasPrefix(body[at:], letter)) {
			if !strings.HasPrefix(body[at+1:], letter) {
				return false
			}
			block.add(`\`, -1, -1)
			at++
		}
		if !strings.HasPrefix(body[at:], letter) {
			return false
		}
		block.add(letter, runes[index], runes[index]+1)
		at += len(letter)
	}
	return at == len(body)
}

// lineBreaks writes the text's line breaks as <br />, as the page's
// Markdown does.
func (block *mcpSelectionWrittenBlock) lineBreaks() {
	text := block.inline()
	if !strings.Contains(text, "\n") {
		return
	}
	block.written.Reset()
	first, last := block.first, block.last
	block.first, block.last = nil, nil
	for index := range len(text) {
		piece := text[index : index+1]
		if piece == "\n" {
			piece = "<br />"
		}
		block.add(piece, first[index], last[index])
	}
}

// mcpSelectionClosedText is a text with its runs of spaces closed up — a run
// holding a line break as one — and, for each of its bytes, where it stands
// in the text.
func mcpSelectionClosedText(text string) (string, []int) {
	var closed strings.Builder
	places := make([]int, 0, len(text))
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character != ' ' && character != '\t' && character != '\n' && character != '\r' {
			closed.WriteByte(character)
			places = append(places, index)
			continue
		}
		run, breaks := index, false
		for ; run < len(text) && strings.IndexByte(" \t\n\r", text[run]) >= 0; run++ {
			breaks = breaks || text[run] == '\n'
		}
		if breaks {
			closed.WriteByte('\n')
		} else {
			closed.WriteByte(' ')
		}
		places = append(places, index)
		index = run - 1
	}
	return closed.String(), places
}

// readMCPMarkdownSelection reads a selection as it is looked for in the
// page's Markdown: its spaces closed up as the Markdown's are.
func readMCPMarkdownSelection(selection string) (mcpCommentSelection, bool) {
	closed, _ := mcpSelectionClosedText(strings.TrimSpace(selection))
	query, ok := readMCPCommentSelectionWords(closed)
	query.spaceBefore = strings.TrimLeft(selection, " \t\n\r") != selection
	query.spaceAfter = strings.TrimRight(selection, " \t\n\r") != selection
	return query, ok
}

// mcpSelectionMarkdownPlace is where a selection stands in a page's
// Markdown, how many places it names there, and — for one — the block it
// starts in, with the words it marks there: none for one running past them
// (past) or on markup alone. One on a block whose text the Markdown doesn't
// show apart (unplaced) is that block's only when the page's words name no
// one place.
type mcpSelectionMarkdownPlace struct {
	found, cell int
	blockID     string
	quote       string
	marked      *mcpCommentQuoteRange
	unplaced    bool
	past        bool
	edges       mcpSelectionEdges
}

// mcpSelectionEdges are what text found in a block's line in the Markdown
// holds at its edges: the part of the line's marker before the block's
// words ("## ", "- ") it holds, whether it starts where the line does — at
// the marker, or at the words of a line without one — and whether its
// first or last byte among the block's words is markup.
type mcpSelectionEdges struct {
	inMarkdown    bool
	held          string
	lineStart     bool
	opens, closes bool
}

// marker is what stands before a block's words on its line in the Markdown,
// "" for a block it doesn't place.
func (markdown mcpSelectionMarkdown) marker(blockID string) string {
	for _, block := range markdown.blocks {
		if block.id == blockID && block.at >= 0 {
			return markdown.text[strings.LastIndexByte(markdown.text[:block.at], '\n')+1 : block.at]
		}
	}
	return ""
}

// place finds a selection in the Markdown, a stored file's address in it
// the page's own (notion_page_markdown_file_matches.go).
func (markdown mcpSelectionMarkdown) place(selection string) mcpSelectionMarkdownPlace {
	return markdown.placeQuery(readMCPMarkdownSelection(notionMarkdownFileAddressesRebased(markdown.text, selection)))
}

// placeText finds exact text in the Markdown, as a suggested edit's old_text.
func (markdown mcpSelectionMarkdown) placeText(text string) mcpSelectionMarkdownPlace {
	return markdown.placeQuery(readMCPExactText(notionMarkdownFileAddressesRebased(markdown.text, text)))
}

func (markdown mcpSelectionMarkdown) placeQuery(query mcpCommentSelection, ok bool) mcpSelectionMarkdownPlace {
	if !ok || len(markdown.blocks) == 0 {
		return mcpSelectionMarkdownPlace{}
	}
	closed, places := mcpSelectionClosedText(markdown.text)
	start, end, found := query.place(closed)
	if found != 1 || end <= start {
		return mcpSelectionMarkdownPlace{found: found}
	}
	start, end = places[start], places[end-1]+1
	block := markdown.blockAtPlace(start, end)
	result := mcpSelectionMarkdownPlace{found: 1, blockID: block.id, cell: block.cell, unplaced: block.at < 0}
	if block.at < 0 {
		return result
	}
	if result.past = end > block.end(); result.past {
		return result
	}
	first, last := -1, -1
	for index := max(start, block.at) - block.at; index < end-block.at; index++ {
		if block.first[index] < 0 {
			continue
		}
		if first < 0 {
			first = block.first[index]
		}
		last = max(last, block.last[index])
	}
	if first < 0 {
		return result
	}
	marker := markdown.marker(block.id)
	result.edges = mcpSelectionEdges{inMarkdown: true, lineStart: start <= block.at-len(strings.TrimLeft(marker, " \t")),
		opens: block.first[max(start, block.at)-block.at] < 0, closes: block.first[end-block.at-1] < 0}
	if start < block.at {
		result.edges.held = markdown.text[start:block.at]
	}
	result.quote = normalizeMCPCommentSelectionText(string(block.raw[first:last]))
	result.marked = &mcpCommentQuoteRange{
		start: len(utf16.Encode(block.raw[:first])), end: len(utf16.Encode(block.raw[:last])),
	}
	return result
}

// blockAt is the block a place in the Markdown starts in: the one whose text
// holds it or, on markup that starts a block's line ("## ", "> "), whose
// text that line goes on to; else the first the Markdown holds after the
// last block whose text ends before it, or the last it holds.
func (markdown mcpSelectionMarkdown) blockAt(at int) mcpSelectionMarkdownBlock {
	next, last := -1, len(markdown.blocks)-1
	for index, block := range markdown.blocks {
		if !block.shown {
			continue
		}
		last = index
		switch {
		case block.at >= 0 && block.at <= at && at < block.end():
			return block
		case block.at > at && !strings.Contains(markdown.text[at:block.at], "\n"):
			return block
		case block.at >= 0 && block.end() <= at:
			next = -1
		case next < 0:
			next = index
		}
	}
	if next < 0 {
		next = last
	}
	return markdown.blocks[next]
}

// mcpSelectionTextPlace is where a selection, or a suggested edit's
// old_text, stands in a page: how many places it names in the text they
// were counted in (target), and for one, the block it starts in with the
// words it marks there — none when it runs past them (past), or stands on
// markup alone or on a block whose text the Markdown doesn't show apart.
type mcpSelectionTextPlace struct {
	found   int
	cell    int
	target  string
	blockID string
	quote   string
	marked  *mcpCommentQuoteRange
	past    bool
	edges   mcpSelectionEdges
}

// find is where text stands in a page, as both tools look for it: one place
// in the Markdown on a block's text (inMarkdown), else one among the words
// (query), else the Markdown's one place on a block it doesn't show apart;
// none otherwise, counted in the Markdown unless only the words held more.
func (texts mcpSelectionTexts) find(inMarkdown mcpSelectionMarkdownPlace, query mcpCommentSelection) mcpSelectionTextPlace {
	if inMarkdown.found == 1 && !inMarkdown.unplaced {
		return mcpSelectionTextPlace{found: 1, cell: inMarkdown.cell, blockID: inMarkdown.blockID, quote: inMarkdown.quote, marked: inMarkdown.marked,
			past: inMarkdown.past, edges: inMarkdown.edges}
	}
	start, end, found := query.place(texts.words)
	switch {
	case found == 1:
		for _, place := range texts.places {
			if start < place.start || start >= place.end {
				continue
			}
			if end > place.end {
				return mcpSelectionTextPlace{found: 1, blockID: place.id, past: true}
			}
			quote := texts.words[start:end]
			return mcpSelectionTextPlace{found: 1, blockID: place.id, quote: quote, marked: mcpCommentQuoteOffsets(place.text, quote)}
		}
		return mcpSelectionTextPlace{found: 1}
	case inMarkdown.found == 1:
		return mcpSelectionTextPlace{found: 1, blockID: inMarkdown.blockID}
	case inMarkdown.found > 1:
		return mcpSelectionTextPlace{found: inMarkdown.found, target: texts.markdown.text}
	case found > 1:
		return mcpSelectionTextPlace{found: found, target: texts.words}
	}
	return mcpSelectionTextPlace{target: texts.markdown.text}
}
