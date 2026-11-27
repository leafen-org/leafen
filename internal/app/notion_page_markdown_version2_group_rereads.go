package app

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A <columns> or a <tabs> whose children Notion's "v2" can't all read as its
// columns or tabs is refused, and what it holds is read again as the blocks
// it would be anywhere (seen 2026-11-15, written twice each): Notion reads
// the children one after another — a column's or a tab's opening tag, then
// on to the closing tag of its name that closes it, counting the openings of
// that name it reads on the way — and stops at the first child that is no
// such tag: a line of text, another block, a closing tag closing none, or a
// column's or a tab's opening tag it refuses, whose refusal it says there.
// It then says what <columns> holds, its <column>s (<tabs>' <tab>s),
// and reads the lines between the group's tags as blocks of their own, so
// that each column or tab there is bare and an opening it refuses is said
// again, in the order such a reading says them ("<columns>⏎\t<column>⏎\t\ta⏎
// \t</column>⏎\t<column x:y=\"1\">⏎\t\tb⏎\t</column>⏎</columns>" says the
// malformed attribute, the children, the malformed attribute again and a
// bare <column>; "<columns>⏎\ta⏎\t<column x:y=\"1\">…" says the children and
// then the malformed attribute alone; "<columns>⏎<column>⏎a⏎</column>⏎z⏎
// </columns>" the children and a bare <column>; a <column> or an <li> in a
// column read again is bare where it stands, after the column). A <ul> or an
// <ol> is read so where an <li> among its children has an opening it refuses
// ("<ul>⏎<li>a</li>⏎<li x:y=\"1\">b</li>⏎</ul>" says the malformed attribute,
// what <ul> holds, the malformed attribute twice and a bare <li>); its other children are read as before
// (notionMarkdownVersion2ListChildren). A group nothing closes holds the
// lines to the end, which the tags left open say first
// (notionMarkdownVersion2TrailingSaid) and its
// reading again says nothing of. Notion finds an item's closing tag by its
// name, so one whose opening it refuses closes the item around it: a column
// so read again holding one is read as its lines on their own
// (notionMarkdownVersion2SameNameBlock).

// notionMarkdownVersion2GroupReads are the groups a write's Markdown holds
// whose children Notion refuses, what each says by the line standing for its
// lines in the Markdown read (repair.columnsChild), and how many were said.
type notionMarkdownVersion2GroupReads struct {
	said map[string][]string
	// held is what the blocks whose lines are read on their own say
	// (notionMarkdownVersion2SameNameBlock), by the line standing for
	// those lines.
	held map[string]notionMarkdownVersion2HeldRead
	used int
}

// notionMarkdownVersion2GroupMark starts the line standing for a refused
// group's lines; no text a write sends holds the byte.
const notionMarkdownVersion2GroupMark = "\xfe"

var (
	notionMarkdownVersion2RefusableGroup = regexp.MustCompile(`^<(columns|tabs|ul|ol)(?:[ \t][^<>]*)?>$`)
	notionMarkdownVersion2GroupFence     = regexp.MustCompile("^(?:```|~~~)")
	// The items each group holds.
	notionMarkdownVersion2GroupItems = map[string]string{"columns": "column", "tabs": "tab", "ul": "li", "ol": "li"}
)

// notionMarkdownVersion2Input is a "v2" write's Markdown as Leafen reads page
// Markdown, and what it says of it, its refused groups read again as Notion
// reads them: each stands as a line of its own that says so where the group
// stands (repair.columnsChild).
func notionMarkdownVersion2Input(markdown string) (string, []notionMarkdownWarning, error) {
	markdown = strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(markdown, "\n")
	groups := notionMarkdownVersion2RefusedGroups(lines)
	if len(groups) == 0 {
		return notionMarkdownVersion2Read(markdown, nil)
	}
	reads := &notionMarkdownVersion2GroupReads{said: map[string][]string{}, held: map[string]notionMarkdownVersion2HeldRead{}}
	// again are the refusals said first of the blocks nothing closes, said
	// again as their lines read whole say them (notionMarkdownVersion2SameNameBlock).
	written, at, unclosed, marks, again := []string{}, 0, false, 0, []string{}
	// over are the blocks whose closing tags Notion puts before a closing tag around them (notionMarkdownVersion2Landings).
	over := []string{}
	for number, group := range groups {
		mark := notionMarkdownVersion2GroupMark + strconv.Itoa(number)
		lead := lines[group.open][:len(lines[group.open])-len(strings.TrimLeft(lines[group.open], " \t"))]
		switch block := group.block; {
		case block == nil:
			reads.said[mark] = group.said(lines)
			written = append(append(written, lines[at:group.open]...), lead+mark)
			at, unclosed = min(group.close+1, len(lines)), unclosed || group.close == len(lines)
		case group.close == len(lines):
			again = append(again, block.first...)
			continue
		default:
			// Its tags stay, as far in as its opening; the lines it holds stand as one.
			reads.held[mark] = block.heldRead()
			written = append(append(written, lines[at:group.open]...), lead+block.opening, lead+mark, lead+"</"+block.name+">")
			at, unclosed = group.close+1, unclosed || block.autoClosed
			// The items a closing tag closing nothing lands among are said with its refusals.
			if block.landed > 0 {
				lines[block.landing] = notionMarkdownVersion2Unlisted(lines[block.landing])
			}
			if block.landing > 0 && block.landing < len(lines)-1 {
				over = append(over, block.name)
			}
		}
		marks++
	}
	written = append(written, lines[at:]...)
	_, _, err := notionMarkdownVersion2Read(strings.Join(written, "\n"), reads)
	said, ok := notionMarkdownVersion2RefusalLines(err)
	if !ok || reads.used != marks {
		return notionMarkdownVersion2Read(markdown, nil)
	}
	for _, refusal := range again {
		if at := slices.Index(said, refusal); at >= 0 {
			said = slices.Insert(said, at, refusal)
		}
	}
	// The tags a group nothing closes leaves open are said first as they stand.
	if unclosed {
		_, _, err := notionMarkdownVersion2Read(markdown, nil)
		if all, ok := notionMarkdownVersion2RefusalLines(err); ok && len(all) > 0 && strings.HasPrefix(all[0], notionMarkdownVersion2TrailingSaid) {
			if len(said) > 0 && strings.HasPrefix(said[0], notionMarkdownVersion2TrailingSaid) {
				said = said[1:]
			}
			said = append([]string{all[0]}, said...)
		}
	}
	// Those closed before a closing tag around them so too, where nothing else is (seen 2026-11-16, written twice:
	// "<callout>⏎<toggle>⏎<toggle x:y=\"1\">⏎<li>a</li>⏎</toggle>⏎<li>b</li>⏎</callout>" says "…: <toggle>." first).
	if len(over) > 0 && (len(said) == 0 || !strings.HasPrefix(said[0], notionMarkdownVersion2TrailingSaid)) {
		said = append([]string{notionMarkdownVersion2TrailingSaid + "<" + strings.Join(over, ">, <") + ">."}, said...)
	}
	return "", nil, notionMarkdownVersion2Refusal(said)
}

// notionMarkdownVersion2Refusal refuses a write, saying said line by line.
func notionMarkdownVersion2Refusal(said []string) error {
	return errors.New(notionMarkdownRefusedHead + strings.Join(said, "\n"))
}

// notionMarkdownVersion2RefusalLines is what a reading's refusal says, line by line.
func notionMarkdownVersion2RefusalLines(err error) ([]string, bool) {
	if err == nil {
		return nil, false
	}
	words, ok := strings.CutPrefix(err.Error(), notionMarkdownRefusedHead)
	if !ok {
		return nil, false
	}
	return strings.Split(words, "\n"), true
}

// notionMarkdownVersion2RefusedGroup is a group whose children Notion
// refuses: its opening tag's line, the line of its closing tag (or the
// Markdown's end), and the lines whose openings its reading of the children
// refused, said first.
type notionMarkdownVersion2RefusedGroup struct {
	open, close int
	refused     []int
	// block is a block whose lines are read on their own, standing for none
	// (notionMarkdownVersion2SameNameBlock).
	block *notionMarkdownVersion2SameNameBlock
}

// said is what the group says: its children's openings refused, each once,
// that its children must be its items, and what its lines say read again —
// the tags they leave open there said last, the first that its closing tag
// is missing and the flow tag closed at the end, the others as left open
// (seen 2026-11-15: "<columns>⏎\tz⏎\t<callout>⏎\t<toggle>⏎</columns>" says
// the children, the <callout> never closed and the <toggle> left open).
func (group notionMarkdownVersion2RefusedGroup) said(lines []string) []string {
	said := []string{}
	for _, at := range group.refused {
		repair := notionMarkdownRepair{}
		trimmed := strings.TrimSpace(lines[at])
		repair.text(trimmed, true, trimmed)
		said = append(said, slices.Compact(slices.Concat(repair.openings, repair.missing))...)
	}
	name := notionMarkdownVersion2RefusableGroup.FindStringSubmatch(strings.TrimSpace(lines[group.open]))[1]
	said = append(said, notionMarkdownHoldsOnly(name, "<"+notionMarkdownVersion2GroupItems[name]+">"))
	// Its lines read again as they stand past the space all of them start with.
	read := notionMarkdownVersion2Dedented(lines[group.open+1 : group.close])
	_, _, err := notionMarkdownVersion2Input(strings.Join(read, "\n"))
	again, _ := notionMarkdownVersion2RefusalLines(err)
	if len(again) == 0 || !strings.HasPrefix(again[0], notionMarkdownVersion2TrailingSaid) {
		return append(said, again...)
	}
	// What a group nothing closes leaves open was said first, and is not
	// again; a list's items its lines leave open are said nothing of (seen
	// 2026-11-16, written twice: "<ul>⏎<li>a⏎<li x:y=\"1\">b</li> x⏎</ul>"
	// says the attribute, the children, then the attribute three times and
	// the <li> inline, not its <li> left open).
	left := again[0]
	open := strings.Split(strings.TrimSuffix(strings.TrimPrefix(left, notionMarkdownVersion2TrailingSaid+"<"), ">."), ">, <")
	if again = again[1:]; group.close == len(lines) || name != "columns" && name != "tabs" && !slices.ContainsFunc(open, func(one string) bool { return one != "li" }) {
		return append(said, again...)
	}
	again = append(again, notionMarkdownUnclosedBlock(open[0]))
	if len(open) > 1 {
		again = append(again, notionMarkdownVersion2TrailingSaid+"<"+strings.Join(open[1:], ">, <")+">.")
	}
	return append(said, again...)
}

// notionMarkdownVersion2RefusedGroups are the groups standing on lines of
// their own whose children Notion refuses, in order, each the first such
// outside one before it; a group or a child Leafen can't be sure of is left
// to the reading as it is (code, a group or an item's tag standing in text,
// a column closed in itself, an item nothing closes).
func notionMarkdownVersion2RefusedGroups(lines []string) []notionMarkdownVersion2RefusedGroup {
	groups := []notionMarkdownVersion2RefusedGroup{}
	fence := ""
	for at := 0; at < len(lines); at++ {
		trimmed := strings.TrimSpace(lines[at])
		if notionMarkdownVersion2GroupSkipped(&fence, trimmed) {
			continue
		}
		match := notionMarkdownVersion2RefusableGroup.FindStringSubmatch(trimmed)
		if match != nil && !notionMarkdownVersion2RefusedOpening(trimmed) {
			if group, ok := notionMarkdownVersion2GroupRead(lines, at, match[1]); ok {
				groups = append(groups, group)
				at = group.close
				continue
			}
		}
		// A block whose lines are read on their own (notionMarkdownVersion2SameNameBlock).
		if block, ok := notionMarkdownVersion2SameNameBlockAt(lines, at); ok {
			groups = append(groups, notionMarkdownVersion2RefusedGroup{open: at, close: block.close, block: &block})
			// What one nothing closes holds is read with the rest.
			if block.close < len(lines) {
				at = block.close
			}
		}
	}
	return groups
}

// notionMarkdownVersion2GroupSkipped tells a line, trimmed, that code or an
// equation holds, or that opens or closes them, fence the one open before it
// and after it.
func notionMarkdownVersion2GroupSkipped(fence *string, trimmed string) bool {
	was := *fence
	*fence = notionMarkdownVersion2GroupFenced(was, trimmed)
	return was != "" || *fence != ""
}

// notionMarkdownVersion2GroupFenced is the fence code is open in after a
// line, trimmed, fence the one open before it; a line of $$ opens and closes
// an equation as a fence.
func notionMarkdownVersion2GroupFenced(fence, trimmed string) string {
	switch {
	case fence == "$$" && trimmed == "$$":
		return ""
	case fence != "" && fence != "$$" && strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "":
		return ""
	case fence != "":
		return fence
	case trimmed == "$$":
		return "$$"
	case notionMarkdownVersion2GroupFence.MatchString(trimmed):
		return trimmed[:3]
	}
	return ""
}

// notionMarkdownVersion2GroupRead reads the children of the group whose
// opening tag stands on lines[open], named name, as Notion does; ok when it
// refuses them.
func notionMarkdownVersion2GroupRead(lines []string, open int, name string) (notionMarkdownVersion2RefusedGroup, bool) {
	group := notionMarkdownVersion2RefusedGroup{open: open, close: len(lines)}
	item := notionMarkdownVersion2GroupItems[name]
	// The group ends at the closing tag of its name its openings leave.
	depth, fence := 0, ""
	for at := open + 1; at < len(lines) && group.close == len(lines); at++ {
		trimmed := strings.TrimSpace(lines[at])
		if notionMarkdownVersion2GroupSkipped(&fence, trimmed) {
			continue
		}
		switch {
		case trimmed == "</"+name+">" && depth == 0:
			group.close = at
		case trimmed == "</"+name+">":
			depth--
		case notionMarkdownVersion2RefusableGroup.MatchString(trimmed) && strings.HasPrefix(trimmed, "<"+name) &&
			!notionMarkdownVersion2RefusedOpening(trimmed):
			depth++
		case strings.Contains(trimmed, "<"+name) || strings.Contains(trimmed, "</"+name):
			return group, false
		}
	}
	opening := regexp.MustCompile(`^<` + item + `(?:[ \t][^<>]*)?>`)
	for at := open + 1; at < group.close; at++ {
		trimmed := strings.TrimSpace(lines[at])
		if trimmed == "" {
			continue
		}
		start := opening.FindString(trimmed)
		switch {
		case start != "" && notionMarkdownVersion2RefusedOpening(trimmed):
			group.refused = append(group.refused, at)
			return group, true
		// A list holding more than items is read as before
		// (notionMarkdownVersion2ListChildren), but where an item's lines
		// held an opening it refuses.
		case start == "":
			return group, item != "li" || len(group.refused) > 0
		case strings.HasSuffix(start, "/>"):
			return group, false
		case strings.HasSuffix(trimmed, "</"+item+">") && !strings.Contains(trimmed[len(start):len(trimmed)-len(item)-3], "<"+item):
			continue
		// An item's text may stand after its opening tag on its line.
		case trimmed != start && (item != "li" || strings.Contains(trimmed[len(start):], "<")):
			return group, false
		}
		end, refused, more := notionMarkdownVersion2GroupItemEnd(lines, at, group.close, item)
		if end < 0 {
			return group, false
		}
		group.refused, at = append(group.refused, refused...), end
		// What follows its closing tag on that line is a child closing none.
		if more {
			return group, true
		}
	}
	return group, false
}

// notionMarkdownVersion2GroupItemEnd is the line of the closing tag of the
// item whose opening tag stands on lines[start], before the group's end, by
// the openings of its name (one refused counts for none, and is said), or
// -1; and whether more follows that closing tag on its line. A refused
// opening's line ending in a closing tag of its name closes the item around
// it there (seen 2026-11-16, written twice: "<ul>⏎<li>a⏎<li x:y=\"1\">b</li>⏎
// </li>⏎</ul>" says the malformed attribute, the list's children, then what
// its lines say read again; "…b</li></li>⏎</ul>" so too).
func notionMarkdownVersion2GroupItemEnd(lines []string, start, end int, item string) (int, []int, bool) {
	depth, fence, refused := 0, "", []int{}
	opening := regexp.MustCompile(`^<` + item + `(?:[ \t][^<>]*)?>`)
	closing := "</" + item + ">"
	for at := start + 1; at < end; at++ {
		trimmed := strings.TrimSpace(lines[at])
		if notionMarkdownVersion2GroupSkipped(&fence, trimmed) {
			continue
		}
		tag := opening.FindString(trimmed)
		switch {
		case trimmed == closing && depth == 0:
			return at, refused, false
		case trimmed == closing:
			depth--
		case tag == trimmed && notionMarkdownVersion2RefusedOpening(trimmed):
			refused = append(refused, at)
		case tag == trimmed && !strings.HasSuffix(trimmed, "/>"):
			depth++
		case item == "li" && tag != "" && depth == 0 && notionMarkdownVersion2RefusedOpening(trimmed) &&
			strings.Contains(trimmed, closing) && !strings.Contains(trimmed[len(tag):strings.Index(trimmed, closing)], "<"+item):
			return at, append(refused, at), strings.TrimSpace(trimmed[strings.Index(trimmed, closing)+len(closing):]) != ""
		case strings.Contains(trimmed, "<"+item+" ") || strings.Contains(trimmed, "<"+item+">") || strings.Contains(trimmed, closing):
			return -1, nil, false
		}
	}
	return -1, nil, false
}

// heldRead is what the block whose lines the line, trimmed, stands for says
// of them, if it stands for one (notionMarkdownVersion2SameNameBlock).
func (reads *notionMarkdownVersion2GroupReads) heldRead(trimmed string) (notionMarkdownVersion2HeldRead, bool) {
	if reads == nil || !strings.HasPrefix(trimmed, notionMarkdownVersion2GroupMark) {
		return notionMarkdownVersion2HeldRead{}, false
	}
	read, ok := reads.held[trimmed]
	if ok {
		reads.used++
	}
	return read, ok
}

// lines is what the group the line, trimmed, stands for says, if it stands
// for one.
func (reads *notionMarkdownVersion2GroupReads) lines(trimmed string) ([]string, bool) {
	if reads == nil || !strings.HasPrefix(trimmed, notionMarkdownVersion2GroupMark) {
		return nil, false
	}
	said, ok := reads.said[trimmed]
	if ok {
		reads.used++
	}
	return said, ok
}
