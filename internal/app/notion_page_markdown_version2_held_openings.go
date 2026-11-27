package app

import "strings"

// A block's tag whose opening Notion's "v2" refuses, first on its line in a
// block's tag of another name left open — a <callout>, a <toggle>, a
// <details>, a <column>, bare or in its <columns> — is said first of what
// that block holds, where the block's reading says that (repair.frames):
// after what is said before the block and of the block itself, before the
// rest it holds; not before all else, as one outside every block's tag is
// (seen 2026-11-16, written twice: "<li>z</li>⏎<toggle>⏎t⏎<callout
// x:y=\"1\">⏎a⏎</callout>⏎</toggle>" says the bare <li>, then the malformed
// attribute, as do the same in a <callout>, a <details>, a <column> or
// <columns> and a <toggle> in a <toggle>; "<li>z</li>⏎<column>⏎<toggle
// x:y=\"1\">…</column>" the bare <li>, the bare <column>, then the malformed
// attribute; "<li>y</li>⏎<toggle>⏎<li>z</li>⏎<callout x:y=\"1\">…</toggle>"
// the bare y, the malformed attribute, then the bare z). One in a block's
// tag of its own name is read with that block's lines on their own, the
// block closed at the refused tag's closing tag
// (notionMarkdownVersion2SameNameBlock).

// heldOpening takes what was said since mark, of a refused opening, to be
// said first of what the block tag left open last holds (repair.frames),
// when the block tags left open hold it and none of them is of its name.
func (repair *notionMarkdownRepair) heldOpening(mark int) bool {
	said := repair.missing[mark:]
	if len(said) == 0 {
		return false
	}
	// A list's tag or an item's is read twice, as its list reads it (repair.malformed).
	name := notionMarkdownVersion2SaidTagName(said[0])
	if name == "" || name == "ul" || name == "ol" || name == "li" {
		return false
	}
	frame := -1
	for _, tag := range notionMarkdownBlockTagsOpen(repair.inline) {
		if tag.over {
			continue
		}
		if tag.name == name {
			return false
		}
		frame = tag.event
	}
	if frame < 0 {
		return false
	}
	if repair.heldOpenings == nil {
		repair.heldOpenings = map[int][]string{}
	}
	repair.heldOpenings[frame] = append(repair.heldOpenings[frame], said...)
	repair.missing = repair.missing[:mark]
	return true
}

// notionMarkdownVersion2SaidTagName is the tag a refusal of a tag's opening
// names (notionMarkdownMalformedTag's, "<callout> isn't written right…"), or "".
func notionMarkdownVersion2SaidTagName(said string) string {
	_, rest, found := strings.Cut(said, "<")
	name, _, closed := strings.Cut(rest, ">")
	if !found || !closed || strings.ContainsAny(name, " /") {
		return ""
	}
	return name
}
