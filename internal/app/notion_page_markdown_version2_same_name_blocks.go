package app

import (
	"regexp"
	"slices"
	"strings"
)

// Notion's "v2" finds a block tag's closing tag by its name, counting the
// openings of that name it reads on the way, and reads what the block holds
// on its own (seen 2026-11-16, written twice each). An opening of that name
// it refuses there counts for none: it says the refusal first of what holds
// the block, and the block ends at the first closing tag of its name, the
// lines between read on their own as Markdown — the refused opening's line
// a line of text left open there — and said where the block is; a closing
// tag of its name after it closes nothing. "<li>z</li>⏎<callout>⏎<callout
// x:y=\"1\">⏎a⏎</callout>⏎</callout>" says the malformed attribute, the bare
// <li>, "Trailing unclosed …: <callout>." and the malformed attribute again,
// as "<callout x:y=\"1\">⏎a" alone does; "<callout>⏎<toggle>⏎<callout x:y=\"1\">
// ⏎a⏎</callout>⏎</toggle>⏎</callout>" the malformed attribute, then "…:
// <toggle>, <callout>." and the attribute again. A column said bare reads
// what it holds as flow tags (notionMarkdownVersion2FramedTrailing): the
// first left open there is never closed (notionMarkdownUnclosedBlock), an
// opening refused there said nothing of
// ("<column>⏎<column x:y=\"1\">⏎a⏎</column>⏎</column>" says the malformed
// attribute, the bare column, then the attribute again); a column in a
// column of columns so read again (notionMarkdownVersion2GroupRead) is too.
// An item's text after its opening tag is the first of what it holds, and
// a closing tag ending the refused opening's line closes it ("<ol>⏎<li>a⏎<li
// x:y=\"1\">b</li>⏎</ol>" says the malformed attribute, "…: <li>." and the
// attribute twice, as "a⏎<li x:y=\"1\">b" does). A block nothing closes so
// says the refusal first, and the rest as its lines read whole say it
// ("<callout>⏎<callout x:y=\"1\">⏎a" says "…: <callout>, <callout>." and the
// malformed attribute twice).

// notionMarkdownVersion2SameNameOpening is the opening tag starting a line
// of a block whose lines Notion reads on their own.
var notionMarkdownVersion2SameNameOpening = regexp.MustCompile(`^<(callout|toggle|column|li)(?:[ \t][^<>]*)?>`)

// notionMarkdownVersion2SameNameBlock is such a block holding an opening of
// its name refused at its own level: its opening tag, the line of the
// closing tag that ends it (len(lines) for none), what is said first of the
// refusals, and the lines it holds.
type notionMarkdownVersion2SameNameBlock struct {
	opening, name string
	close         int
	first, held   []string
	// An item no list holds: whether its closing tag has more before it on
	// its line, is the one Notion puts at the Markdown's end, and makes it
	// read inline (notionMarkdownVersion2BareItemBlockAt).
	closedAfter, autoClosed, inline bool
	// landing is the line after it where a closing tag Notion puts for it
	// lands closing nothing, and landed how many items there read inline.
	landing, landed int
}

// notionMarkdownVersion2SameNameBlockAt is the block whose opening tag
// starts lines[open], if an opening of its name refused at its own level
// stands in it; a tag of its name standing elsewhere on a line leaves it to
// the reading as it is.
func notionMarkdownVersion2SameNameBlockAt(lines []string, open int) (notionMarkdownVersion2SameNameBlock, bool) {
	trimmed := strings.TrimSpace(lines[open])
	match := notionMarkdownVersion2SameNameOpening.FindStringSubmatch(trimmed)
	if match == nil || strings.HasSuffix(match[0], "/>") || notionMarkdownVersion2RefusedOpening(trimmed) {
		return notionMarkdownVersion2SameNameBlock{}, false
	}
	name, head := match[1], strings.TrimSpace(trimmed[len(match[0]):])
	named := regexp.MustCompile(`</?` + name + `(?:[ \t/>]|$)`)
	if head != "" && name != "li" || named.MatchString(head) {
		return notionMarkdownVersion2SameNameBlock{}, false
	}
	// An item no list holds finds its closing tag otherwise.
	if name == "li" && !notionMarkdownVersion2OpensBefore(lines[:open], "ul", "ol") {
		return notionMarkdownVersion2BareItemBlockAt(lines, open, match[0], head)
	}
	block := notionMarkdownVersion2SameNameBlock{opening: match[0], name: name, close: len(lines)}
	closing, opening := "</"+name+">", regexp.MustCompile(`^<`+name+`(?:[ \t][^<>]*)?>`)
	depth, fence, refused := 0, "", 0
	for at := open + 1; at < len(lines) && block.close == len(lines); at++ {
		line := strings.TrimSpace(lines[at])
		if notionMarkdownVersion2GroupSkipped(&fence, line) {
			continue
		}
		start := opening.FindString(line)
		// What stands after its tag on a line is read as it is.
		after := line
		if start != "" {
			after = line[len(start):]
		}
		ends := strings.HasSuffix(after, closing) && !named.MatchString(strings.TrimSuffix(after, closing))
		switch {
		case start != "" && notionMarkdownVersion2RefusedOpening(line) && (ends || !named.MatchString(after)):
			if depth == 0 {
				refused++
				block.first = append(block.first, notionMarkdownVersion2OpeningRefusal(line)...)
			}
			if ends && depth == 0 {
				block.close = at
			} else if ends {
				depth--
			}
		case start != "" && !strings.HasSuffix(start, "/>") && (ends || !named.MatchString(after)):
			if !ends {
				depth++
			}
		case line == closing && depth == 0:
			block.close = at
		case line == closing:
			depth--
		case named.MatchString(line):
			return notionMarkdownVersion2SameNameBlock{}, false
		}
	}
	if refused == 0 {
		return notionMarkdownVersion2SameNameBlock{}, false
	}
	// A block the refused openings leave open, counted as openings, is one
	// nothing closes where the closing tag Notion puts for it lands on the
	// line of the closing tag ending it (notionMarkdownVersion2Landings;
	// seen 2026-11-16: "<toggle>⏎<toggle x:y=\"1\">⏎<li>a</li>⏎</toggle>"
	// says "Trailing …: <toggle>.", the malformed attribute twice and a bare
	// <li>, as "<toggle>⏎<toggle x:y=\"1\">a</toggle>" says the trailing tag
	// and the attribute twice; with one more </toggle> it is the block it
	// is). Where it lands after that line, the block ends there and the
	// closing tag landing closes nothing ("…⏎</toggle>⏎<li>b</li>").
	if name != "li" && !notionMarkdownVersion2SameNameClosed(lines[open+1:], name) {
		if landing := notionMarkdownVersion2LandingOf(lines, open); landing <= block.close {
			block.close = len(lines)
		} else {
			block.land(lines, landing)
		}
	}
	// An item nothing closes is left to the reading as it is.
	if block.close == len(lines) {
		return block, name != "li"
	}
	held := slices.Clone(lines[open+1 : block.close])
	if last := strings.TrimSpace(lines[block.close]); last != closing {
		held = append(held, strings.TrimSuffix(strings.TrimRight(lines[block.close], " \t"), closing))
	}
	if head != "" {
		held = append([]string{head}, held...)
	}
	block.held = notionMarkdownVersion2Dedented(held)
	return block, true
}

// notionMarkdownVersion2OpeningRefusal is what Notion says of a block's
// opening tag it refuses starting a line, trimmed, read once.
func notionMarkdownVersion2OpeningRefusal(trimmed string) []string {
	repair := notionMarkdownRepair{}
	repair.text(trimmed, true, trimmed)
	return slices.Compact(slices.Concat(repair.openings, repair.missing))
}

// notionMarkdownVersion2Dedented is lines as they stand past the space all
// of them but blank ones start with.
func notionMarkdownVersion2Dedented(lines []string) []string {
	lead, first := "", true
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if first {
			lead, first = indent, false
		}
		for !strings.HasPrefix(indent, lead) {
			lead = lead[:len(lead)-1]
		}
	}
	read := make([]string, len(lines))
	for index, line := range lines {
		read[index] = strings.TrimPrefix(line, lead)
	}
	return read
}

// notionMarkdownVersion2HeldRead is what such a block's lines say read on
// their own, as a document's are and as a bare column's flow tags are, and
// what is said first of its refusals.
type notionMarkdownVersion2HeldRead struct {
	first, document, flow []string
	// inline is whether the item no list holds that the lines stand in reads inline.
	inline bool
}

// heldRead reads what the block holds on its own.
func (block notionMarkdownVersion2SameNameBlock) heldRead() notionMarkdownVersion2HeldRead {
	_, _, err := notionMarkdownVersion2Input(strings.Join(block.held, "\n"))
	said, _ := notionMarkdownVersion2RefusalLines(err)
	read := notionMarkdownVersion2HeldRead{first: block.first, document: said, flow: said, inline: block.inline}
	if len(said) == 0 || !strings.HasPrefix(said[0], notionMarkdownVersion2TrailingSaid) {
		return read
	}
	// As flow tags: those standing in text, an opening refused, go unsaid.
	open := strings.Split(strings.TrimSuffix(strings.TrimPrefix(said[0], notionMarkdownVersion2TrailingSaid+"<"), ">."), ">, <")
	for _, line := range block.held {
		trimmed := strings.TrimSpace(line)
		if match := notionMarkdownVersion2TagName.FindStringSubmatch(trimmed); match != nil && notionMarkdownVersion2RefusedOpening(trimmed) {
			if at := slices.Index(open, match[1]); at >= 0 {
				open = slices.Delete(open, at, at+1)
			}
		}
	}
	read.flow = said[1:]
	if len(open) > 0 {
		left := []string{notionMarkdownUnclosedBlock(open[0])}
		if len(open) > 1 {
			left = append(left, notionMarkdownVersion2TrailingSaid+"<"+strings.Join(open[1:], ">, <")+">.")
		}
		read.flow = slices.Concat(left, read.flow)
	}
	return read
}

// heldLines says what the block whose lines a line of the Markdown read
// stands for holds, where it stands: what its refusals say first of what
// holds the block, the block tag left open before it, or before all else;
// what its lines say as the block reads them, a bare column as flow tags.
func (repair *notionMarkdownRepair) heldLines(read notionMarkdownVersion2HeldRead) {
	if repair.bareItemHeld(read) {
		return
	}
	frames := slices.DeleteFunc(notionMarkdownBlockTagsOpen(repair.inline), func(one notionMarkdownBlockTagOpen) bool {
		return one.over || repair.inline[one.event].text
	})
	said := read.document
	if len(frames) > 0 && repair.inline[frames[len(frames)-1].event].bare {
		said = read.flow
	}
	if len(frames) > 1 {
		if repair.heldOpenings == nil {
			repair.heldOpenings = map[int][]string{}
		}
		frame := frames[len(frames)-2].event
		repair.heldOpenings[frame] = append(repair.heldOpenings[frame], read.first...)
	} else {
		repair.openings = append(repair.openings, read.first...)
	}
	repair.missing = append(repair.missing, said...)
}

// notionMarkdownVersion2SameNameClosed tells lines closing a block of the
// name opened before them, every opening of its name they hold counted,
// refused or not; code and equations hold none.
func notionMarkdownVersion2SameNameClosed(lines []string, name string) bool {
	tag := regexp.MustCompile(`<(/?)` + name + `(?:[ \t][^<>]*)?(/?)>`)
	depth, fence := 1, ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if notionMarkdownVersion2GroupSkipped(&fence, trimmed) {
			continue
		}
		for _, found := range tag.FindAllStringSubmatchIndex(trimmed, -1) {
			switch {
			case notionMarkdownEscapedAt(trimmed, found[0]) || found[5] > found[4]:
			case found[3] > found[2]:
				if depth--; depth == 0 {
					return true
				}
			default:
				depth++
			}
		}
	}
	return false
}
