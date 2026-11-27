package app

// The words Leafen's reading of page Markdown says about the tags it closed,
// moved or refused, in Leafen's own words, kept here so every reading says
// them alike. Each names the tags it is about as written; a refusal puts
// several together, a line each, after notionMarkdownRefusedHead.
const (
	// notionMarkdownRefusedHead heads a refused Markdown's faults.
	notionMarkdownRefusedHead = "The Markdown couldn't be read:\n"
	// notionMarkdownVersion2TrailingSaid heads the tags a block left open
	// at its end, which were closed there; the tags follow as "<a>, <b>.".
	notionMarkdownVersion2TrailingSaid = "Tags still open at the end were closed there: "
	// notionMarkdownPartialChildSaid follows what a table's part holds when
	// an unfinished part came after.
	notionMarkdownPartialChildSaid = " The unfinished part after them was skipped."
)

// notionMarkdownListTagInline says a list's tag or an item's stood inside
// text, where it can't.
func notionMarkdownListTagInline(name string) string {
	return "<" + name + "> is a list tag, so it goes on a line of its own, not inside text."
}

// notionMarkdownUnclosedBlock says a block's tag no closing tag closed was
// closed at the end of the input.
func notionMarkdownUnclosedBlock(name string) string {
	return "<" + name + "> is never closed with </" + name + ">, so the block runs to the end of the input."
}

// notionMarkdownUnclosedInline says a mark's tag no closing tag closed was
// closed at the end of the input.
func notionMarkdownUnclosedInline(name string) string {
	return "<" + name + "> is never closed with </" + name + ">, so the marked text runs to the end of the input."
}

// notionMarkdownTablePartAlone says a table's part stood outside a <table>.
func notionMarkdownTablePartAlone(name string) string {
	return "<" + name + "> only goes inside a <table>."
}

// notionMarkdownHoldsOnly says what a tag may hold, children as written
// ("<li>", "<td>, <th>").
func notionMarkdownHoldsOnly(name, children string) string {
	return "<" + name + "> holds only " + children + "."
}

// notionMarkdownNotListContent says a list held something other than its
// items, kept outside the list.
func notionMarkdownNotListContent(name string) string {
	return "<" + name + "> held something that isn't a list item; it was kept, outside the list."
}

// notionMarkdownSelfClosingOnly says a tag that holds nothing was written
// with content or a closing tag.
func notionMarkdownSelfClosingOnly(name string) string {
	return "<" + name + "> closes itself: write it as <" + name + " />."
}

// notionMarkdownNotSelfClosing says a block that holds text was written
// closing itself.
func notionMarkdownNotSelfClosing(name string) string {
	return "<" + name + "> needs content between its tags; an empty paragraph is <p />."
}

// notionMarkdownMalformedTag says a tag's opening isn't name="value"
// attributes ended by ">".
func notionMarkdownMalformedTag(name string) string {
	return "<" + name + `> isn't written right: quote its attribute values and end the opening tag with ">".`
}

// notionMarkdownTrailingOpener says an opening tag the text ended in
// before its ">" was dropped.
func notionMarkdownTrailingOpener(tag string) string {
	return notionMarkdownTrailingOpenerSaid + tag + "> at the end was dropped."
}

const (
	notionMarkdownTrailingOpenerSaid = "The unfinished opening <"
	// The words for an attribute value in single quotes or none.
	notionMarkdownQuoteValueSaid       = "put its value in quotes."
	notionMarkdownDoubleQuoteValueSaid = "put its value in double quotes."
)

// notionMarkdownOlderTagSaid says a tag of the older Markdown ("v1") was
// read as the tag of now it names (notionMarkdownVersion1Tags).
func notionMarkdownOlderTagSaid(name, read string) string {
	return "<" + name + "> is an older Markdown tag; it was read as " + read
}

// notionMarkdownHTMLTagSaid says HTML was read as Markdown or taken out.
func notionMarkdownHTMLTagSaid(name, read string) string {
	return "<" + name + "> is HTML, which this Markdown doesn't take; it was " + read
}
