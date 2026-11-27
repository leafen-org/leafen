package app

import "strings"

// Where Notion's "v2" says the block tags a block's closing tag closes over
// (seen 2026-11-16, written twice each): it reads what a toggle or a callout
// holds on its own, so the tags it leaves open there are said first of what
// that block holds, after what is said before the block — "<li>z</li>⏎
// <toggle>⏎<callout>⏎a⏎</toggle>" says the bare <li>, then "Trailing
// unclosed …: <callout>."; "<toggle>⏎<callout>⏎a⏎</toggle>⏎<quote>⏎b" says
// "…: <quote>." first, then "…: <callout>."; "<li>z</li>⏎<callout>⏎<toggle>⏎
// <p>⏎a⏎</callout>" the bare <li>, then "…: <toggle>, <p>.". A column said
// bare holds what stands in it as flow tags: one its closing tag closes
// over is never closed (notionMarkdownUnclosedBlock), said after the column
// is said bare, and one standing in
// text there, an opening refused, is said nothing of; the tags in a block
// standing in it are said as that block's own reading says them
// ("<column>⏎<callout>⏎a⏎</column>" says the bare column, then the callout
// closed as a flow tag; "<column>⏎x <callout>⏎a⏎</column>" and
// "<column>⏎<callout x:y=\"1\">⏎a⏎</column>" the bare column and what the
// opening says; "<li>z</li>⏎<column>⏎<callout>⏎<toggle x:y=\"1\">⏎a⏎
// </column>" the bare <li>, the bare column, the callout closed as a flow
// tag, then "…: <toggle>." and the malformed attribute). A column in its
// columns is no flow tag ("<columns>⏎<column>⏎<callout>⏎a⏎</column>⏎
// </columns>" says "…: <callout>."). Tags a block left open to the end holds
// are said with the tags left open.

// notionMarkdownVersion2FramedTrailing is what the block tags whose own
// reading says the tags their closing tag closed over say first, by their
// place among tags, and the tags so said.
func notionMarkdownVersion2FramedTrailing(tags []notionMarkdownInlineBlockTag) (map[int][]string, map[int]bool) {
	open, parents := notionMarkdownBlockTagsOpen(tags), notionMarkdownVersion2TagFrames(tags)
	left := map[int]bool{}
	for _, one := range open {
		left[one.event] = !one.over
	}
	names, moved := map[int][]string{}, map[int]bool{}
	for _, one := range open {
		if !one.over || one.flow || one.in < 0 || tags[one.in].text {
			continue
		}
		frame := -1
		switch closer := tags[one.in]; {
		case closer.bare:
			frame = one.under
		case closer.name == "toggle" || closer.name == "callout":
			frame = one.in
		}
		for at := frame; at >= 0; at = parents[at] {
			if left[at] {
				frame = -1
				break
			}
		}
		if frame < 0 {
			continue
		}
		names[frame] = append(names[frame], "<"+one.name+">")
		moved[one.event] = true
	}
	said := map[int][]string{}
	for frame, open := range names {
		said[frame] = []string{notionMarkdownVersion2TrailingSaid + strings.Join(open, ", ") + "."}
	}
	return said, moved
}
