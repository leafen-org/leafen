package app

import (
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"slices"
	"strings"
)

// A page's Markdown writes each block it can't hold as an <unknown url="…"
// alt="…"/> line whose address ends in the block's ID. Written back, the line
// is that block, as in Notion (seen 2026-11-10, "v1" and "v2"): it stays where
// the line stands, moves when the line moved and goes when the line is left
// out, allow_deleting_content or not; its alt is read as nothing. A line that
// names no block the page writes as unknown, or one named a second time,
// would make a new block, which Notion refuses with its tool's words, naming
// each address once, or the tag as written when it names none.

// A line holding its text between an opening and a closing tag,
// <unknown …>x</unknown>, is one too, which a new page's "v1" Markdown reads
// as text and its "v2" refuses (notionMarkdownCreateUnknownError).
var (
	notionMarkdownUnknownPattern = regexp.MustCompile(`^<unknown((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*(?:/>|>(.*)</unknown>)$`)
	notionMarkdownUnknownOpening = regexp.MustCompile(`^<unknown((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>$`)
)

// notionMarkdownUnknownTag is the tag an <unknown/> line reads as, among the
// <page> and <database> tags a Markdown plan resolves.
const notionMarkdownUnknownTag = "unknown"

// notionMarkdownUnknownLine reads an <unknown/> line as a reference to the
// block it names, for the Markdown's plan to resolve.
func notionMarkdownUnknownLine(line string) (string, json.RawMessage, bool) {
	match := notionMarkdownUnknownPattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return "", nil, false
	}
	address := html.UnescapeString(notionMarkdownAttributes(match[1])["url"])
	content, err := json.Marshal(notionMarkdownTag{Tag: notionMarkdownUnknownTag, URL: address, Line: strings.TrimSpace(line)})
	return notionMarkdownReferenceBlock, content, err == nil
}

// notionMarkdownUnknownKey names the block an <unknown/> line's address ends
// in: its fragment, without case. Notion's addresses name the block's ID
// alone after "#", without its dashes (seen 2026-11-10); a fragment with
// dashes or after "#block-" names no block, so its line would make a new one.
func notionMarkdownUnknownKey(address string) string {
	_, fragment, found := strings.Cut(address, "#")
	if !found {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fragment))
}

// unknownAddress is the address an <unknown/> line names a block by, as
// Notion writes it: the page's and the block's IDs without their dashes.
func (w *notionMarkdownWriter) unknownAddress(id string) string {
	return w.mentions.Origin + "/p/" + strings.ReplaceAll(w.publicPageID, "-", "") + "#" + strings.ReplaceAll(id, "-", "")
}

func notionMarkdownUnknownName(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
}

// notionMarkdownPairedUnknown reads the public API's <unknown> lines holding
// text between their tags, <unknown url="…">x</unknown>, that name blocks
// the page writes as unknown, once every <unknown> line names one (else the
// plan refuses the Markdown as making new ones): "v2" refuses each, unread,
// and "v1" reads it as text (seen 2026-11-10).
func notionMarkdownPairedUnknown(markdown string, unknown map[string]string, version2 bool) (string, error) {
	lines := strings.Split(markdown, "\n")
	paired, fence := []int{}, ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" || strings.HasPrefix(trimmed, "```") {
			if fence == "" {
				fence = notionMarkdownFenceOf(trimmed)
			} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		match := notionMarkdownUnknownPattern.FindStringSubmatch(trimmed)
		if opening := notionMarkdownUnknownOpening.FindStringSubmatch(trimmed); match == nil && opening != nil && version2 &&
			unknown[notionMarkdownUnknownKey(notionMarkdownUpdateUnknownAddress(opening[1]))] != "" &&
			slices.ContainsFunc(lines[index+1:], func(line string) bool { return strings.TrimSpace(line) == "</unknown>" }) {
			// Its text on the lines to its closing tag is the same (seen 2026-11-10).
			paired = append(paired, index)
		}
		if match == nil {
			continue
		}
		if unknown[notionMarkdownUnknownKey(notionMarkdownUpdateUnknownAddress(match[1]))] == "" {
			return markdown, nil
		}
		if !strings.HasSuffix(trimmed, "/>") {
			paired = append(paired, index)
		}
	}
	if len(paired) > 0 && version2 {
		return "", errors.New(notionMarkdownRefusedHead +
			strings.TrimSuffix(strings.Repeat(notionMarkdownCreateUnknownPaired+"\n", len(paired)), "\n"))
	}
	for _, index := range paired {
		line := lines[index]
		lines[index] = line[:len(line)-len(strings.TrimLeft(line, " \t"))] + `\` + strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n"), nil
}

// notionMarkdownUpdateUnknownLine is a line an update reads as an <unknown>
// block's tag before it reads its Markdown: <unknown and a space opening it
// past its indent, whatever closes it (seen 2026-11-14: "<unknown\turl=…>x"
// is read as the Markdown reads it, "<unknown  url=…>x" refused).
var notionMarkdownUpdateUnknownLine = regexp.MustCompile(`^<unknown [^>]*(?:>|$)`)

// notionMarkdownUpdateMarkedUnknown is an <unknown> closed in itself, alone
// as the text of a quote or a list's item.
var notionMarkdownUpdateMarkedUnknown = regexp.MustCompile(`^<unknown(?:[ \t][^<>]*)?/>$`)

// notionMarkdownVersion1ItemRun is what starts a "v1" list's item before
// its text: its marks, and a to-do's box.
var notionMarkdownVersion1ItemRun = regexp.MustCompile(`^(?:(?:[-*+]|\d{1,9}\.)[ \t]+)+(?:\[[ xX]\][ \t]+)?`)

// notionMarkdownVersion1ItemMarks is what starts a "v1" line's item before
// its text (notionMarkdownVersion1ItemRun): a box is a to-do's after "- "
// only, and the text of an item another mark opens (seen 2026-11-15: "* [ ]
// <unknown url=\"…\"/>" and "1. [ ] …" are items holding those words).
func notionMarkdownVersion1ItemMarks(trimmed string) string {
	marks := notionMarkdownVersion1ItemRun.FindString(trimmed)
	if box := strings.LastIndexByte(marks, '['); box >= 0 && !notionMarkdownVersion1ToDoBox.MatchString(marks) {
		return marks[:box]
	}
	return marks
}

// notionMarkdownVersion1ToDoBox is a "v1" to-do's box after its "- " mark.
var notionMarkdownVersion1ToDoBox = regexp.MustCompile(`(?:^|[ \t])-[ \t]+\[[ xX]\][ \t]+$`)

// notionMarkdownUpdateNewUnknown refuses an update whose <unknown> tags name
// no block the page writes as unknown before its Markdown is read, as
// Notion's update-page does, "v1" and "v2" alike (seen 2026-11-14,
// replace_content and insert_content): one standing first on its line past
// its indent, in a callout, a toggle or under an item too, closed in itself,
// left open, paired on its line or over lines, its text between them or
// none, and before a tag the Markdown leaves open ("<unknown url=\"…\">⏎
// <callout>⏎a"), each named by its address, a double-quoted url's, once,
// or by its line as written without one ("<unknown alt=\"a\">x", a url in
// single quotes); one after other text or in code is text. Only where none
// is so, one closed in itself as the text of a list's item names no page it
// can make ("- <unknown url=\"…\"/>", in a callout too): "v2"'s after the
// marks of the items and quotes it stands in, a to-do's being text ("> <unknown
// url=\"…\"/>" is refused, "- [ ] <unknown url=\"…\"/>" a to-do), "v1"'s
// after an item's marks and a to-do's box, a quote's being text
// (notionMarkdownMarkedUnknownRefusal), "v1" reading its values in single
// quotes or none too, "v2" refusing them (notionMarkdownMarkedUnknownURL).
// One without a url, or with an empty one, "v2" refuses as it reads the
// Markdown, and "v1" reads as an empty item
// (notionMarkdownVersion1EmptiedUnknownItems; seen 2026-11-15).
func notionMarkdownUpdateNewUnknown(markdown string, unknown map[string]string, version2 bool) error {
	if version2 {
		markdown = notionMarkdownVersion2OverLinesUnknown(markdown)
	}
	fresh, lines, named := []string{}, []string{}, map[string]bool{}
	marked, fence, faults, equation := "", "", []string{}, false
	top := notionMarkdownVersion2TopWritten{}
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		}
		if notionMarkdownVersion2Fence(trimmed) {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		marks := notionMarkdownVersion2MarkerRun.FindString(trimmed)
		if !version2 {
			marks = notionMarkdownVersion1ItemMarks(trimmed)
		}
		// An item's in an equation, or "v2"'s four columns in at the top, is text.
		asText := version2 && top.written(line)
		asText = asText || equation || trimmed == "$$"
		equation = equation != (trimmed == "$$")
		if strings.TrimSpace(marks) != "" {
			if match := notionMarkdownUpdateMarkedUnknown.FindString(trimmed[len(marks):]); match != "" && !asText {
				address, addressed, loose := notionMarkdownMarkedUnknownURL(match, version2)
				switch {
				case loose != "":
					faults = append(faults, loose)
				case version2 && !addressed:
					faults = append(faults, notionMarkdownCreateUnknownMissingURL)
				case version2 && address == "":
					faults = append(faults, notionMarkdownCreateUnknownEmptyURL)
				case address != "" && marked == "" && unknown[notionMarkdownUnknownKey(address)] == "":
					marked = address
				}
			}
			continue
		}
		tag := notionMarkdownUpdateUnknownLine.FindString(trimmed)
		if read := notionMarkdownUpdateReadUnknownLine.FindString(trimmed); tag == "" && read != "" && !asText {
			// One its Markdown reads (notionMarkdownUpdateReadUnknown).
			if fault, refused := notionMarkdownUpdateReadUnknown(read, unknown, version2); fault != "" {
				faults = append(faults, fault)
			} else if refused != "" && marked == "" {
				marked = refused
			}
			continue
		}
		if tag == "" {
			continue
		}
		name := notionMarkdownUpdateUnknownAddress(tag)
		if unknown[notionMarkdownUnknownKey(name)] != "" {
			continue
		}
		// A line names itself as written (notionMarkdownLinkedAddress).
		written := notionMarkdownLinkedAddress(trimmed)
		if strings.TrimSpace(name) == "" {
			name = written
		}
		if !named[name] {
			fresh, named[name] = append(fresh, name), true
		}
		lines = append(lines, written)
	}
	switch {
	case len(fresh) > 0:
		return notionMarkdownNewUnknownError{addresses: fresh, lines: lines}
	case len(faults) > 0:
		return errors.New(notionMarkdownRefusedHead + strings.Join(faults, "\n"))
	case marked != "":
		return errors.New(notionMarkdownMarkedUnknownRefusal(marked))
	}
	return nil
}

// notionMarkdownUpdateUnknownAddress is the address an update's <unknown>
// tag names (notionMarkdownUpdateUnknownURL).
func notionMarkdownUpdateUnknownAddress(tag string) string {
	address, _ := notionMarkdownUpdateUnknownURL(tag)
	return address
}

// notionMarkdownNewUnknownError refuses an update's Markdown whose <unknown/>
// lines would make new blocks, naming their addresses, as Notion's refuses
// them (seen 2026-11-10), in Leafen's words, with the way to copy or move
// the block itself where Notion's says to ask the person
// (notionMarkdownUnknownCopyWay); a new page's says why by its first such line instead
// (notionMarkdownCreateUnknownError, mcpCreatePageUnknownRefusal). lines are
// those lines as written, in order.
type notionMarkdownNewUnknownError struct {
	addresses []string
	lines     []string
}

func (e notionMarkdownNewUnknownError) Error() string {
	quoted := make([]string, len(e.addresses))
	for index, address := range e.addresses {
		quoted[index] = `"` + address + `"`
	}
	return "This page update can't make new `<unknown .../>` blocks: `<unknown .../>` stands for a block or " +
		"content the tool can't fully see, so one already there can only stay, move or be taken out when the page " +
		"is edited." +
		" Take out the new `<unknown .../>` if it got in by mistake, or use a database template if there is one; " +
		notionMarkdownUnknownCopyWay + " New unknown blocks: " + strings.Join(quoted, ", ")
}

// A callout's own text is the first line inside it, which reads back as a
// paragraph inside it: a callout holds its lines, as the screen's does. The
// writer counts that line as a block of its own, the callout's ID and
// notionMarkdownCalloutTextLine, so every line read back has its block.
// Written back unchanged it leaves the callout as it is; changed in place the
// callout takes the new text; left out the callout's text goes; anywhere else
// it is a new paragraph.
const notionMarkdownCalloutTextLine = "#text"

// notionMarkdownCalloutTextOf is the callout whose own text a written line is.
func notionMarkdownCalloutTextOf(blockID string) (string, bool) {
	return strings.CutSuffix(blockID, notionMarkdownCalloutTextLine)
}

// calloutTextLine counts a callout's own text as the line after it; text of
// spaces alone reads back as no line.
func (w *notionMarkdownWriter) calloutTextLine(block workspaceExportMarkdownBlock, text string) {
	if block.Type == graphBlockTypeCallout && strings.TrimSpace(text) != "" {
		w.rendered = append(w.rendered, block.ID+notionMarkdownCalloutTextLine)
	}
}

// unknownLine notes a block written as an <unknown/> line by the ID it names.
func (w *notionMarkdownWriter) unknownLine(block workspaceExportMarkdownBlock, id string) {
	if w.unknownBlocks != nil {
		w.unknownBlocks[notionMarkdownUnknownName(id)] = block.ID
	}
}

// notionMarkdownCalloutTextPlace gives the lines written as callouts' own
// text their callouts as parents, read as paragraphs.
func notionMarkdownCalloutTextPlace(projection *notionPageMarkdownProjection) {
	for _, blockID := range projection.RenderedBlockIDs {
		if callout, ok := notionMarkdownCalloutTextOf(blockID); ok {
			projection.BlockParents[blockID], projection.BlockTypes[blockID] = callout, graphBlockTypeParagraph
		}
	}
}

// notionMarkdownCalloutTexts pairs the lines of new Markdown with callouts'
// own text: a callout that stays keeps its first line, a plain paragraph,
// as its text, changed or not; a line paired with a callout's text anywhere
// else is a new block.
func notionMarkdownCalloutTexts(
	old []workspaceImportStagedItem, oldIDs []string, items []workspaceImportStagedItem,
	matched, edited []int, moved []bool,
) {
	position := make(map[string]int, len(items))
	for index, item := range items {
		position[item.ID] = index
	}
	keeps := func(index int) string {
		if at := max(matched[index], edited[index]); at >= 0 {
			return oldIDs[at]
		}
		return ""
	}
	firstLine := func(item workspaceImportStagedItem, callout string, at int) bool {
		parent, inside := position[item.ParentID]
		return inside && keeps(parent) == callout && item.Type == graphBlockTypeParagraph && item.AfterID == "" &&
			string(item.Content) == string(old[at].Content)
	}
	own, taken := map[string]int{}, map[int]bool{}
	for at, blockID := range oldIDs {
		if callout, ok := notionMarkdownCalloutTextOf(blockID); ok {
			own[callout] = at
		}
	}
	for index, item := range items {
		at := max(matched[index], edited[index])
		if at < 0 {
			continue
		}
		if callout, ok := notionMarkdownCalloutTextOf(oldIDs[at]); ok {
			if moved[index] || !firstLine(item, callout, at) {
				matched[index], edited[index], moved[index] = -1, -1, false
				continue
			}
			taken[at] = true
		}
	}
	for index, item := range items {
		if matched[index] >= 0 || edited[index] >= 0 || item.ParentID == "" {
			continue
		}
		parent, inside := position[item.ParentID]
		if !inside {
			continue
		}
		if at, ok := own[keeps(parent)]; ok && !taken[at] && firstLine(item, keeps(parent), at) {
			edited[index], taken[at] = at, true
		}
	}
}

// notionMarkdownCalloutTextChanges are the callouts whose own text the new
// Markdown changes: the text their first line now holds, or none when the
// line went.
func notionMarkdownCalloutTextChanges(
	old []workspaceImportStagedItem, oldIDs []string, items []workspaceImportStagedItem,
	ids map[string]string, edited []int, used map[string]bool,
) map[string][]graphTextSpan {
	changes := map[string][]graphTextSpan{}
	for index, item := range items {
		callout, own := notionMarkdownCalloutTextOf(ids[item.ID])
		if !own || edited[index] < 0 {
			continue
		}
		if before := old[edited[index]]; before.Text != item.Text || !sameGraphTextSpans(before.Spans, item.Spans) {
			changes[callout] = notionMarkdownItemSpans(item)
		}
	}
	for _, blockID := range oldIDs {
		if callout, own := notionMarkdownCalloutTextOf(blockID); own && !used[blockID] && used[callout] {
			changes[callout] = []graphTextSpan{}
		}
	}
	return changes
}

// notionMarkdownItemSpans is a line's text as spans.
func notionMarkdownItemSpans(item workspaceImportStagedItem) []graphTextSpan {
	if len(item.Spans) == 0 && item.Text != "" {
		return []graphTextSpan{{Text: item.Text}}
	}
	return item.Spans
}
