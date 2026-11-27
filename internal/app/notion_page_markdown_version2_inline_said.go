package app

import (
	"strconv"
	"strings"
)

// What Notion's "v2" says of an inline tag whose opening it can't read — an
// attribute it can't read (notionMarkdownRepair.malformed) — it says
// as it reads the texts' inline tags, in the order they stand in the text,
// among what it says of the tags those texts leave open or hold that can't
// stand inline (notionMarkdownVersion2Said), and before what it says of the
// block tags (seen 2026-11-15, written twice each: "x <span x:y=\"1\">a</span>
// <li>b</li>" says the <span> before the <li>, "x <li>b</li> <span x:y=…>"
// the <li> first, "y <span x:y=…>a</span>⏎⏎x <b>a" the <span> before the
// missing </b>, "<li>z</li>⏎⏎x <span x:y=…>a</span>" the <span> before the
// bare <li>, "x <span x:y=…>a</span> <page>p</page>" before the <page>). A
// list's tag or an item's, first on its line, whose opening it refuses is
// read twice: once with the openings refused, said first
// (repair.openingSaid), once among its text's inline tags ("x <span
// x:y=…>a</span>⏎⏎<ul x:y=\"1\">⏎<li>b</li>⏎</ul>" says the <ul>, the <span>,
// the <ul> again, then the bare <li>). Leafen marks where such a tag stands
// in its text (inlineMark) and says what was said of it there (frames).

// notionMarkdownVersion2InlineSaidMark stands around the number of what was
// said of a tag, before the tag in its text; no text a write sends holds the
// byte, and a write so marked is refused.
const notionMarkdownVersion2InlineSaidMark = "\xfd"

// notionMarkdownPlaced is what was said of a tag marked in its text: lines
// of what the reading says (from, to), or the words of the first tag that
// can't stand inline (repair.refused).
type notionMarkdownPlaced struct {
	from, to int
	words    string
}

// inlineAt records what was said since mark, of the tag at text[at] of the
// stretch read (repair.text), as said where the tag stands.
func (repair *notionMarkdownRepair) inlineAt(at, mark int) {
	if len(repair.missing) <= mark {
		return
	}
	if repair.stretchSaid == nil {
		repair.stretchSaid = map[int][2]int{}
	}
	said := repair.stretchSaid[at]
	if said[1] == 0 {
		said[0] = mark
	}
	said[1] = len(repair.missing)
	repair.stretchSaid[at] = said
}

// inlineMark is the mark of what was said of the tag at text[at] of the
// stretch read, which stands before it; none when nothing was.
func (repair *notionMarkdownRepair) inlineMark(at int) string {
	said, ok := repair.stretchSaid[at]
	if !ok {
		return ""
	}
	delete(repair.stretchSaid, at)
	return repair.place(notionMarkdownPlaced{from: said[0], to: said[1]})
}

// place records what was said of a tag, and gives the mark standing before
// it in its text.
func (repair *notionMarkdownRepair) place(placed notionMarkdownPlaced) string {
	repair.placed = append(repair.placed, placed)
	return notionMarkdownVersion2InlineSaidMark + strconv.Itoa(len(repair.placed)-1) + notionMarkdownVersion2InlineSaidMark
}

// notionMarkdownVersion2InlineSaidAt is where each mark stands in a block's
// text, and the mark.
func notionMarkdownVersion2InlineSaidAt(text string) [][2]int {
	found := [][2]int{}
	for at := 0; ; {
		start := strings.Index(text[at:], notionMarkdownVersion2InlineSaidMark)
		if start < 0 {
			return found
		}
		start += at
		end := strings.Index(text[start+1:], notionMarkdownVersion2InlineSaidMark)
		if end < 0 {
			return found
		}
		end += start + 2
		found, at = append(found, [2]int{start, end}), end
	}
}

// placedSaid puts, for each mark among words, what was said of its tag,
// once, and gives the lines of what the reading says it took.
func (repair *notionMarkdownRepair) placedSaid(words []string, taken map[int]bool) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		number, ok := strings.CutPrefix(word, notionMarkdownVersion2InlineSaidMark)
		if !ok {
			out = append(out, word)
			continue
		}
		index, err := strconv.Atoi(strings.TrimSuffix(number, notionMarkdownVersion2InlineSaidMark))
		if err != nil || index >= len(repair.placed) {
			continue
		}
		if placed := repair.placed[index]; placed.words != "" {
			if repair.refused == placed.words {
				out, repair.refused = append(out, placed.words), ""
			}
			continue
		}
		for at := repair.placed[index].from; at < repair.placed[index].to && at < len(repair.missing); at++ {
			if !taken[at] {
				out, taken[at] = append(out, repair.missing[at]), true
			}
		}
	}
	return out
}
