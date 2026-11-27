package app

import (
	"errors"
	"regexp"
	"slices"
	"strings"
)

// Markdown a "v2" write sends reads as Notion's API reads Notion-flavored
// Markdown (seen 2026-11-10), put first the way Leafen's page Markdown reads
// it (notionMarkdownVersion2Input): lines of text one after another are one
// block, joined by line breaks — a paragraph's, a list item's or a quote's —
// until a blank line or a line that starts another block; {color="…"} at a
// line's end is text, spaces after it too (seen 2026-11-15: "x {color=\"red\"} "
// is the paragraph "x {color=\"red\"}", a heading's, an item's, a quote's
// and a to-do's line alike); a fence's language reads as Notion's
// (notionMarkdownFenceLanguage); HTML is put right or left as text, and v1's tags are read, each said
// once in the answer's warnings — a tag named in capitals, <B>, is no tag,
// and a mark closed in itself, <b/>, goes; an image inside a line of text is
// refused. What the end leaves unfinished goes (notionMarkdownVersion2End).

// notionMarkdownWarning is something a "v2" write changed or kept as text,
// which its answer says.
type notionMarkdownWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var errNotionMarkdownImageInText = errors.New(notionMarkdownRefusedHead + "An image stands as a block of its own, " +
	"never with paragraph text or another image on its line: put a blank line between images, as in " +
	"`![first](url)\\n\\n![second](url)`.")

// notionMarkdownVersion2Image is an image, its address in angle brackets or
// holding parentheses in pairs too ("…/(a).png", seen 2026-11-11).
var (
	notionMarkdownVersion2Heading   = regexp.MustCompile(`^#{1,6}(?:[ \t]|$)`)
	notionMarkdownVersion2Bullet    = regexp.MustCompile(`^(?:[-*+](?:[ \t]|$))`)
	notionMarkdownVersion2Number    = regexp.MustCompile(`^(\d{1,9})[.)](?:[ \t]|$)`)
	notionMarkdownVersion2BlockTag  = regexp.MustCompile(`^</?(p|h[1-4]|quote|toggle|callout|ul|ol|li|todo|columns?|tabs?|table|colgroup|col|thead|tbody|tr|td|th|equation|table-of-contents|table_of_contents|image|file|pdf|video|audio|bookmark|embed|page|database|folder|synced-block-reference|synced-block|synced_block_reference|synced_block|details|summary|unknown|empty-block|breadcrumb|link-to-page|button|meeting-notes|custom-block|mail|external_object_instance|div|blockquote|hr)(?:[ \t/>]|$)`)
	notionMarkdownVersion2Image     = regexp.MustCompile(`!\[[^\]]*\]\((?:<[^<>\n]*>[^()]*|(?:[^()]|\([^()]*\))*)\)`)
	notionMarkdownVersion2Attribute = regexp.MustCompile(`([ \t])\{((?:[a-z]+="[^"]*"[ \t]*)+)\}([ \t]*)$`)
)

// notionMarkdownVersion2Read is a "v2" write's Markdown as Leafen reads page
// Markdown, and what it says of it; reads are its refused groups' lines read
// again (notionMarkdownVersion2Input).
func notionMarkdownVersion2Read(markdown string, reads *notionMarkdownVersion2GroupReads) (string, []notionMarkdownWarning, error) {
	markdown = strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	// Its tags' attributes read with spaces around their = and over lines (notionMarkdownVersion2SpacedAttributes).
	markdown, spaced, overRefused := notionMarkdownVersion2SpacedAttributes(markdown)
	if overRefused != "" {
		return "", nil, errors.New(notionMarkdownRefusedHead + overRefused)
	}
	written := strings.Split(markdown, "\n")
	// Fences and tags four columns past the text before them are its words
	// (notionMarkdownVersion2WrittenLines), and code a text block's tag
	// opens on its line stands on lines of its own (notionMarkdownVersion2TagFences).
	markdown, dropped, err := notionMarkdownVersion2End(strings.Join(notionMarkdownVersion2TagFences(
		notionMarkdownVersion2WrittenLines(strings.Split(markdown, "\n"))), "\n"))
	if err != nil {
		return "", nil, err
	}
	// A flow tag's lines that are its text first stand on its line
	// (notionMarkdownVersion2FlowLines), a table's columns closed in
	// themselves (notionMarkdownVersion2Colgroups).
	// Lists it refuses are said, each, and read no further (listsSaid).
	// A table's rows and cells on one line stand on lines of their own first
	// (notionMarkdownTableRowLines), and so do a <colgroup>'s tags
	// (notionMarkdownVersion2ColgroupLines) and an equation's tags over lines
	// (notionMarkdownVersion2EquationTagRuns).
	// Text in a table beside its tags stands apart (notionMarkdownVersion2TableTextLines); a list's closing tag where a closing tag Notion puts closing nothing lands has more after it (notionMarkdownVersion2LandedListClosings).
	rows := notionMarkdownVersion2LandedListClosings(notionMarkdownTableRowLines(notionMarkdownVersion2ColgroupLines(
		notionMarkdownVersion2TableTextLines(notionMarkdownVersion2EquationTagRuns(strings.Split(markdown, "\n"))))))
	// A list's tags in code pair first (notionMarkdownVersion2CodePairsOf).
	rows, listPairsRefused := notionMarkdownVersion2CodePairsOf(rows, notionMarkdownVersion2ListPairNames)
	if listPairsRefused != "" {
		return "", nil, errors.New(notionMarkdownRefusedHead + listPairsRefused)
	}
	// A list's closing tag after text stands on a line of its own, and one
	// with more after it makes no list (notionMarkdownVersion2ListClosings).
	// An item alone on its line no list holds may be its words (notionMarkdownVersion2LoneTextItems), one ending it in a block left open text (notionMarkdownVersion2LastLineItems).
	rows = notionMarkdownVersion2LastLineItems(notionMarkdownVersion2LoneTextItems(notionMarkdownVersion2ListClosings(rows)), rows[len(rows)-1])
	flowing, flowRefused, listsSaid, listsAt, ending := notionMarkdownVersion2FlowLines(rows)
	if flowRefused == "" {
		// An item whose closing tag has more on its line (notionMarkdownVersion2BrokenItems), and
		// the tags the Markdown leaves open (notionMarkdownVersion2LeftOpen), its refused openings after them.
		said, span := notionMarkdownVersion2BrokenItems(rows)
		if flowRefused = said; flowRefused != "" {
			flowRefused, listsSaid = notionMarkdownVersion2BrokenItemOpenings(rows, notionMarkdownVersion2LeftOpen(rows, flowRefused, span), span), span.listsBefore(listsSaid, listsAt)
		}
	}
	if flowRefused == "" {
		flowing, flowRefused = notionMarkdownVersion2Colgroups(flowing)
	}
	// What an HTML table holds as no child of its own (notionMarkdownVersion2TableChildren).
	tableSaid := notionMarkdownVersion2TableChildren(rows)
	if flowRefused != "" {
		return "", nil, errors.New(notionMarkdownRefusedHead + strings.Join(slices.Concat(tableSaid, listsSaid, []string{flowRefused}), "\n"))
	}
	tilded, left := notionMarkdownVersion2TildeFences(flowing)
	// The closing tags that tags in code take are text (notionMarkdownVersion2CodePairs).
	paired, pairsRefused := notionMarkdownVersion2CodePairs(notionMarkdownVersion2CodeLines(notionMarkdownVersion2TitleLines(
		notionMarkdownVersion2Indents(notionMarkdownVersion2ItemLines(notionMarkdownFoldedCells(tilded))))))
	if pairsRefused != "" {
		return "", nil, errors.New(notionMarkdownRefusedHead + pairsRefused)
	}
	lines, repair := notionMarkdownVersion2Tagged(paired, reads)
	// What each list holds besides its items (notionMarkdownVersion2ListChildren):
	// an item read as words is so.
	lists := notionMarkdownVersion2ListChildren(lines)
	notionMarkdownVersion2OwnLists(lines, lists)
	for at := range lists.words {
		if opened := strings.Index(lines[at], "<li"); opened >= 0 {
			lines[at] = lines[at][:opened] + `\` + lines[at][opened:]
		}
	}
	if notionMarkdownVersion2Blockquotes(lines) {
		repair.say("html_tag_autofix", "blockquote", notionMarkdownHTMLSaid("blockquote"))
	}
	out, texts := make([]string, 0, len(lines)), map[int]bool{}
	// inline is each block's text as written, its lines one after another,
	// by its line in out (notionMarkdownVersion2Unclosed).
	inline := map[int]string{}
	fence, equation, open, item, columns := "", false, false, false, 0
	place := notionMarkdownVersion2Place{item: -1}
	// quote is the quote text a next line may go on with, and joined the
	// lines that go on with one (notionMarkdownVersion2QuoteTexts).
	quote, joined := notionMarkdownVersion2QuoteText{}, map[int]notionMarkdownVersion2QuoteText{}
	// A caption's lines stand apart, as its tag reads them
	// (notionMarkdownCaptionLines): caption is the tag closing it.
	caption := ""
	// Code and equations stand in the quotes and items a line starts with:
	// quotes is how many > their lines start with.
	quotes := 0
	// details is the line in out of the <details> whose title a <summary>
	// line may be next, or -1 (repair.summary).
	details := -1
	expressions := notionMarkdownVersion2EquationTagLines(lines)
	// places are the block tags met by each block's first line, by its line
	// in out (repair.frames).
	places := map[int]notionMarkdownVersion2BlockTags{}
	for index, line := range lines {
		from := len(repair.inline)
		// Said where they stand among what the reading says of tags.
		if said, ok := lists.opening[index]; ok {
			repair.missing = append(repair.missing, said)
		}
		repair.missing, repair.listRefused = append(repair.missing, lists.closing[index]...), lists.refused[index]
		trimmed := strings.TrimSpace(line)
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		titled := details
		if trimmed != "" || fence != "" || equation {
			details = -1
		}
		if fence == "" && !equation {
			out, line = repair.summary(out, titled, line, trimmed)
		}
		switch {
		case fence != "":
			out = append(out, line)
			if inside := notionMarkdownVersion2Unquoted(line, quotes); strings.HasPrefix(inside, fence) && strings.Trim(inside, "`") == "" {
				fence = ""
			}
			continue
		case equation:
			// Its blank lines at its start and end go (seen 2026-11-10), and
			// a line of spaces stays (seen 2026-11-13: "$$⏎  a⏎   ⏎$$" is "a⏎ ").
			if (line == "" || quotes > 0 && notionMarkdownVersion2Unquoted(line, quotes) == "") &&
				notionMarkdownVersion2EquationEdge(lines, index, quotes) {
				continue
			}
			out, equation = append(out, line), notionMarkdownVersion2Unquoted(line, quotes) != "$$"
			continue
		case notionMarkdownVersion2Fence(body):
			// Code standing in columns or tabs is refused as any line there.
			repair.columnsChild(trimmed)
			fence, quotes = notionMarkdownFenceOf(body), strings.Count(prefix, ">")
			if !repair.sealed {
				repair.swallowed = index
			}
			line = line[:len(prefix)] + fence + notionMarkdownFenceLanguage(body[len(fence):])
			out, open, columns = append(out, line), false, 0
			continue
		case body == "$$" && notionMarkdownVersion2EquationCloses(lines[index+1:], strings.Count(prefix, ">")):
			repair.columnsChild(trimmed)
			out, equation, open, columns, quotes = append(out, line), true, false, 0, strings.Count(prefix, ">")
			continue
		}
		if trimmed == "$$" {
			repair.seal(index)
		}
		repair.selfClosed(body)
		repair.selfClosing(lines, index, body)
		if said, refused := notionMarkdownVersion2Captions(lines, index); refused {
			repair.missing = append(repair.missing, said)
		}
		// A block's closing tag that no tag before it opened is text: it
		// goes on with the text before it as another line, or stands as a
		// paragraph (seen 2026-11-11: "1. one\n</ol>" is the item
		// "one\n</ol>", "> q\n</p>" the quote "q\n</p>").
		// In quotes it stands after their > (seen 2026-11-14: "> q⏎>
		// </details>" is the quote "q⏎</details>").
		quoted := ""
		if strings.Contains(prefix, ">") && strings.Trim(prefix, " \t>") == "" {
			quoted = prefix
		}
		// It stands as far in as written, in the list item it may stand in,
		// its tabs as the spaces they stand for, as any line's
		// (notionMarkdownVersion2Columns; seen 2026-11-15: "<columns>⏎<column>⏎
		// x⏎⏎\t</ul>⏎</column>…" is "x" and "</ul>", as at the top).
		if stray, ok := repair.stray(strings.TrimSpace(line[len(quoted):]), lines[:index]); ok {
			line = quoted + notionMarkdownVersion2Lead(notionMarkdownVersion2Columns(line[len(quoted):])) + stray
		} else {
			// A caption's tabs are its text (seen 2026-11-10), and an
			// equation's tag's lines' its expression's.
			if caption == "" && !expressions[index] {
				line = notionMarkdownVersion2Columns(line)
			}
			line = notionMarkdownVersion2Attribute.ReplaceAllString(repair.line(line), `$1\{$2\}$3`)
			line = notionMarkdownVersion2Links(notionMarkdownVersion2Entities(line))
		}
		trimmed = strings.TrimSpace(line)
		// A line of whitespace in an equation's tag is its expression's
		// (notionMarkdownEquationTagText).
		if trimmed == "" && line != "" && (notionMarkdownVersion2TagTitleLine(lines, index) || expressions[index]) {
			out, open = append(out, line), false
			continue
		}
		if trimmed == "" && notionMarkdownVersion2TagFirstTabs(lines, index) {
			out, open = append(out, strings.TrimRight(lines[index], " ")), false
			continue
		}
		if trimmed == "" {
			out, open, columns = append(out, ""), false, 0
			continue
		}
		// An equation's tag's lines are its expression's, each as written,
		// none going on with the one before it (notionMarkdownEquationTagText).
		if expressions[index] {
			out, open = append(out, line), false
			continue
		}
		repair.columnsChild(trimmed)
		next := ""
		if index+1 < len(lines) {
			next = strings.TrimSpace(lines[index+1])
		}
		// A table's rows go on to a blank line or another block, a line of
		// text among them a row of its own.
		if strings.HasPrefix(trimmed, "|") && workspaceImportTableSeparatorPattern.MatchString(next) && columns == 0 {
			columns = len(workspaceImportTableCells(trimmed))
		} else if columns > 0 && !strings.HasPrefix(trimmed, "|") {
			if notionMarkdownVersion2Starts(trimmed, next, false) {
				columns = 0
			} else {
				line = "| " + trimmed + " |" + strings.Repeat("  |", columns-1)
				out = append(out, line)
				inline[len(out)-1], places[len(out)-1] = line, notionMarkdownVersion2BlockTags{from, len(repair.inline)}
				continue
			}
		}
		// $$ that no later $$ closes starts text of its own (notionMarkdownFlow);
		// a numbered item after text, as it stands (notionMarkdownVersion2Place).
		numbered := item
		if notionMarkdownVersion2Number.MatchString(trimmed) {
			numbered = place.elsewhere(line, lines[index])
		}
		goesOn := open && notionMarkdownVersion2QuoteGoesOn(quote, prefix, body, next, lines[index])
		// A list's or an item's opening tag its list's closing tag made text
		// starts a paragraph (notionMarkdownVersion2ListClosings).
		if open && body != "$$" && !notionMarkdownVersion2TextListStart(lines[index]) && !notionMarkdownVersion2Starts(trimmed, next, numbered) {
			last := len(out) - 1
			out[last] = notionMarkdownVersion2Break(out[last]) + "<br>" + strings.TrimLeft(line, " \t")
			inline[last] += "\n" + strings.TrimLeft(line, " \t")
			continue
		}
		// In a quote whose text it doesn't go on with, it is a paragraph the
		// quote holds (seen 2026-11-15: "> <ul>⏎> <li>a\</li>⏎> </ul> x⏎> `y`"), out of it one after it ("> x⏎<p>a</p> b").
		if open && quote.quotes > 0 && strings.Count(prefix, ">") <= quote.quotes && notionMarkdownVersion2TextListStart(lines[index]) {
			out = append(out, strings.TrimRight(prefix, " \t"))
		}
		written := place.written(line)
		if !written {
			line = notionMarkdownVersion2NumberedTodo(notionMarkdownVersion2Numbered(line, open && !numbered))
		}
		line = notionMarkdownVersion2LongNumbered(line)
		out = append(out, line)
		inline[len(out)-1], places[len(out)-1] = line, notionMarkdownVersion2BlockTags{from, len(repair.inline)}
		if goesOn {
			joined[len(out)-1] = quote
		} else {
			quote = notionMarkdownVersion2QuoteText{len(out) - 1, notionMarkdownVersion2QuoteTextAt(prefix, body, next)}
		}
		// A quote's <details> is titled as any (seen 2026-11-14: "> <details>⏎> <summary>a</summary>").
		if notionMarkdownDetailsPattern.MatchString(strings.TrimLeft(trimmed, " \t>")) {
			details = len(out) - 1
		}
		item = place.read(line, written)
		// An empty item's text is the line in under it, its own (notionMarkdownFlow).
		// A heading four columns or more in, no list or quote before it, is
		// text (seen 2026-11-14: "\t## h⏎x" is "## h⏎x").
		heading := notionMarkdownVersion2Heading.MatchString(trimmed) && (strings.Trim(prefix, " \t") != "" ||
			notionMarkdownVersion2Indent(line, 0) < 4 || notionMarkdownVersion2HeldBefore(lines[:index]))
		// A line starting with a pipe is text a next line goes on from, but
		// for a table's row (seen 2026-11-14: "|a|⏎|b|c|" is the paragraph
		// "|a|⏎|b|c|", "|a|⏎|b|⏎|---|" "|a|" and a table).
		open = !heading && !notionMarkdownVersion2BlockTag.MatchString(trimmed) &&
			(written || !notionMarkdownFlowThematic.MatchString(trimmed) && !(strings.HasPrefix(trimmed, "|") && columns > 0) &&
				!notionMarkdownVersion2EmptyItem.MatchString(trimmed))
		texts[len(out)-1] = open
		if match := notionMarkdownVersion2CaptionTag.FindStringSubmatch(trimmed); match != nil {
			caption = "</" + match[1] + ">"
		} else if trimmed == caption {
			caption = ""
		}
		open = open && caption == ""
	}
	// What a list left open to the end holds besides items (notionMarkdownVersion2ListChildren).
	repair.missing = append(repair.missing, lists.closing[len(lines)]...)
	// What blocks in block tags say is said with those tags (repair.frames).
	// A quote's lines are one text (notionMarkdownVersion2QuoteTexts).
	blocks, blockPlaces, ends := notionMarkdownVersion2QuoteTexts(inline, places, joined)
	said, flow := repair.frames(blocks, blockPlaces)
	if dropped != "" {
		repair.tag("<"+dropped+">", dropped, false, nil, false, false)
	}
	// A group a tag in text leaves open holds what its last line stands in
	// (notion_page_markdown_version2_open_text_groups.go).
	repair.lastLineHeld(lines, rows)
	// The block tags left open come first, then the lists refused, then a
	// tag that can't stand inline, a tag's missing attribute last.
	// A list ending the Markdown in an item left open says its items first
	// and its bare ones with the items said bare (notionMarkdownVersion2OpenItemList).
	refused := slices.Concat(ending.first, listsSaid)
	if trailing := repair.trailing(); trailing != "" {
		// The items left bare in a list left open are said as such, else those after code hid a closing tag.
		bare := repair.bareItems(fence != "", lines)
		if len(bare) == 0 {
			bare = notionMarkdownVersion2LeftItems(tilded, left)
		}
		// A table code swallowed (notionMarkdownVersion2SwallowedTable).
		if len(bare) == 0 && (fence != "" || equation || repair.sealed) {
			bare = notionMarkdownVersion2SwallowedTable(written, trailing)
		}
		refused = slices.Concat([]string{trailing}, ending.first, listsSaid, bare)
	}
	refused = append(append(refused, tableSaid...), flow...)
	if repair.refused != "" {
		refused = append(refused, repair.refused)
	}
	// A tag a block's text ends in before its > is said before all else, in
	// a tag's blocks after the block tags left open; a block's tag whose
	// opening is refused after those (repair.openingSaid).
	free, held := notionMarkdownVersion2TrailingOpeners(out, blocks, ends)
	refused = slices.Concat(free, refused, repair.openings, held, said, ending.bare, repair.missing)
	if len(refused) > 0 {
		return "", nil, errors.New(notionMarkdownRefusedHead + strings.Join(refused, "\n"))
	}
	// A backslash ending a block's text is its words, which no next line goes
	// on from (seen 2026-11-14: "a\⏎- b" is "a\" and an item, "## a\⏎b" the
	// heading "a\" and "b", "a\\t" "a\"); a quote's lines go on in the quote
	// (notionMarkdownFlowQuote).
	for index := range inline {
		if text := strings.TrimRight(out[index], " \t"); !strings.HasPrefix(strings.TrimLeft(text, " \t"), ">") {
			out[index] = notionMarkdownVersion2TextBackslash(text) + out[index][len(text):]
		}
	}
	for index, line := range out {
		if !texts[index] {
			continue
		}
		// An escaped ! makes no image, its [text](address) a link (seen
		// 2026-11-11: "\![a](…)", and "&#33;[a](…)" read so).
		images := slices.DeleteFunc(notionMarkdownVersion2Image.FindAllStringIndex(line, -1), func(image []int) bool {
			return notionMarkdownEscapedAt(line, image[0])
		})
		text, at := "", 0
		for _, image := range images {
			text, at = text+line[at:image[0]], image[1]
		}
		if text = strings.TrimSpace(text + line[at:]); len(images) > 1 || len(images) == 1 && text != "" {
			return "", nil, errNotionMarkdownImageInText
		}
	}
	if repair.unmade != "" {
		return "", nil, errors.New(repair.unmade)
	}
	read := strings.NewReplacer(notionMarkdownListTagMark+`\`, `\`, notionMarkdownListTagMark, `\`, notionMarkdownEntitySpace, "")
	out = notionMarkdownVersion2BacktickLines(out, inline)
	return notionMarkdownVersion2SpacedWords(read.Replace(strings.Join(out, "\n")), spaced), repair.ordered(), nil
}

// notionMarkdownVersion2TagTitleLine reports whether lines[index], a line of
// spaces, is the title of a sub-page's or a database's tag on lines of its
// own, between its opening and closing tags, which names it so as Notion's
// "v2" reads it (seen 2026-11-13: "   " names a page "   " and leaves a
// database unnamed); any other line of spaces is a blank line.
func notionMarkdownVersion2TagTitleLine(lines []string, index int) bool {
	if index == 0 || index+1 >= len(lines) {
		return false
	}
	match := notionMarkdownContainerOpen.FindStringSubmatch(strings.TrimSpace(lines[index-1]))
	if match == nil || match[1] != "page" && match[1] != "database" || strings.TrimSpace(match[3]) != "" {
		return false
	}
	return strings.TrimSpace(lines[index+1]) == "</"+match[1]+">"
}

// ordered is what the reading says, in Notion's order (seen 2026-11-11, its API's
// "v2" and its MCP alike): the HTML taken out with what it held, then the
// tags put right or read as their "v2" ones, then the tags kept as text,
// each in the order they stand; then what reading the tags said, a
// duplicated attribute first.
func (repair *notionMarkdownRepair) ordered() []notionMarkdownWarning {
	rank := func(warning notionMarkdownWarning) int {
		switch {
		case repair.removed[warning.Message]:
			return 0
		case warning.Code == "unsupported_tag":
			return 2
		}
		return 1
	}
	warnings := slices.DeleteFunc(slices.Clone(repair.warnings), repair.unsaid)
	slices.SortStableFunc(warnings, func(left, right notionMarkdownWarning) int { return rank(left) - rank(right) })
	parsed := slices.Clone(repair.parsed)
	slices.SortStableFunc(parsed, func(left, right notionMarkdownWarning) int {
		return notionMarkdownBoolRank(right.Code == "duplicate_attribute") - notionMarkdownBoolRank(left.Code == "duplicate_attribute")
	})
	return append(warnings, parsed...)
}

// notionMarkdownBoolRank is 1 for true, 0 for false.
func notionMarkdownBoolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

// notionMarkdownVersion2Starts tells a line that starts a block of its own
// after a line of text, rather than going on with it: a heading, a quote, a
// list item (a numbered one after text only from 1), a divider, a table, a
// block's tag, and an image under a list item.
func notionMarkdownVersion2Starts(trimmed, next string, afterItem bool) bool {
	if match := notionMarkdownVersion2Number.FindStringSubmatch(trimmed); match != nil {
		return afterItem || notionMarkdownVersion2FromOne(match[1])
	}
	return notionMarkdownVersion2Heading.MatchString(trimmed) || strings.HasPrefix(trimmed, ">") ||
		notionMarkdownVersion2Bullet.MatchString(trimmed) || notionMarkdownFlowThematic.MatchString(trimmed) ||
		notionMarkdownVersion2BlockTag.MatchString(trimmed) ||
		strings.HasPrefix(trimmed, "|") && workspaceImportTableSeparatorPattern.MatchString(next) ||
		afterItem && notionMarkdownVersion2Image.FindString(trimmed) == trimmed
}

// notionMarkdownVersion2HeldBefore tells lines holding a list's item or a
// quote, which a line further in may stand in.
func notionMarkdownVersion2HeldBefore(lines []string) bool {
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, ">") ||
			notionMarkdownVersion2Bullet.MatchString(trimmed) || notionMarkdownVersion2Number.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2Break is a line a next one goes on from, as Notion's
// "v2" reads its end (seen 2026-11-14): without the backslash that ends it
// in a break ("a \⏎b" is "a ⏎b"), or else without the spaces ending it,
// its tabs kept — a backslash they stand after is its words ("a\ ⏎b" is
// "a\⏎b", "a\\t⏎b" "a\\t⏎b", "a\t ⏎b" "a\t⏎b") — and without the spaces
// references read at its end (seen 2026-11-11: "x&#32;&#32;\ny" is "x", a
// break, "y"); an equation it would open goes on in no next line
// (notionMarkdownEquationsEnded).
func notionMarkdownVersion2Break(line string) string {
	if at := notionMarkdownEntitySpaceAt(line); at >= 0 {
		line = strings.TrimRight(line[:at], " \t")
	}
	if trailing := len(line) - len(strings.TrimRight(line, `\`)); trailing%2 == 1 {
		line = line[:len(line)-1]
	} else {
		line = notionMarkdownVersion2TextBackslash(strings.TrimRight(line, " "))
	}
	return notionMarkdownEquationsEnded(line)
}

// notionMarkdownVersion2TextBackslash is a line whose last backslash, ending
// it, is its words: one more escapes it, so that no line goes on from it
// (notionMarkdownFlow) and it escapes no <br> or tag after it.
func notionMarkdownVersion2TextBackslash(line string) string {
	if trailing := len(line) - len(strings.TrimRight(line, `\`)); trailing%2 == 1 {
		return line + `\`
	}
	return line
}
