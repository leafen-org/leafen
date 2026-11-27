package app

import (
	"regexp"
	"strings"
)

// Where a "v2" write's v1 <details> and <summary> read as tags, as Notion's
// "v2" reads them (seen 2026-11-21, its API's page Markdown replace, each
// written twice): only standing first on their line, past the line's quote
// and list marks and the block tags before them — "<details>x</details>",
// "> <details>x</details>", "- <details>x</details>" and
// "<toggle><details>x</details></toggle>" hold a toggle, "<details><summary>s
// </summary>x</details>" its title — and anywhere else as their words, a
// <details> said for nothing: text or an inline tag before them on their
// line keeps them so whatever the line starts with ("<mark>hi</mark> and
// <details>x</details>", "<b>hi</b><details>x</details>", "<span>a</span>
// <details><summary>s</summary>x</details>" and "<callout>a <details>x
// </details></callout>" are text there, the <summary> said as a tag neither
// reads), and so it keeps v1's <synced_block> and <table_of_contents>,
// named as the tags they become and said so ("<b>hi</b> and <synced_block>x
// </synced_block>" is the words "<synced-block>x</synced-block>"). A
// <details> whose text follows its opening tag on its line is converted to
// a <toggle> and read as one, that text its title
// ("<details>x</details>" is the toggle "x", "<details color=\"red\"> x
// </details>" the red toggle " x ", "<details>x⏎y⏎</details>" the toggle
// "x⏎y"); one alone on its line holds its lines as v1's, untitled unless a
// <summary> starts them ("<details>⏎x⏎</details>" holds the paragraph "x").

// notionMarkdownVersion2TitledDetails is a <details> opening a line with its
// text after it there.
var notionMarkdownVersion2TitledDetails = regexp.MustCompile(`^<(details)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*)$`)

// notionMarkdownVersion2TagLeads tells a tag at text[at] that only its
// line's marks and block tags stand before: text is the line's stretch
// read, first whether the line starts with it.
func notionMarkdownVersion2TagLeads(text string, at int, first bool) bool {
	marks := len(notionMarkdownVersion2MarkerRun.FindString(text))
	if !first || at < marks {
		return false
	}
	before := text[marks:at]
	for {
		before = strings.TrimLeft(before, " \t")
		if before == "" {
			return true
		}
		found := notionMarkdownRepairTag.FindStringSubmatchIndex(before)
		if found == nil || found[0] != 0 || !notionMarkdownVersion2HoldingTag(strings.ToLower(before[found[4]:found[5]])) {
			return false
		}
		before = before[found[1]:]
	}
}

// v1Placed tells where the tag at text[found[0]:found[1]] of a line's stretch
// stands, for v1's tags, which read as tags only there: an opening one first
// on its line past its marks and block tags (leads), a closing one where its
// line is a block's tags (structured; block is whether the line starts with
// a tag), closing none left as its words — a closing tag text follows on its
// line is text (seen 2026-11-14: "x⏎</details> y" is the paragraph "x⏎
// </details> y"). An <embed> is made another block where its line starts
// with a tag, as before (repair.tag); other tags take neither.
func (repair *notionMarkdownRepair) v1Placed(name, text string, found []int, first, block, structured bool) (leads, placed bool) {
	closing := found[3] > found[2]
	switch {
	case name == "embed":
		return false, block && (structured || !closing)
	case name != "details" && name != "summary" && notionMarkdownVersion1Tags[name] == "":
		return false, false
	case closing:
		return false, block && structured && !repair.openInText(name)
	}
	leads = notionMarkdownVersion2TagLeads(text, found[0], first)
	return leads, leads
}

// notionMarkdownVersion2HoldingTag tells a block's tag, v1's <details> and
// <summary> among them, from an inline one.
func notionMarkdownVersion2HoldingTag(name string) bool {
	return notionMarkdownVersion2Tags[name] && !notionMarkdownVersion2InlineTags[name] || name == "details" || name == "summary"
}

// notionMarkdownVersion2DetailsTitle tells a <details> opening that rest,
// what follows it on its line, holds its title, then its closing tag
// outside code.
func notionMarkdownVersion2DetailsTitle(rest string) bool {
	return notionMarkdownVersion2DetailsTitleStarts(rest) && notionMarkdownVersion2ClosingOutsideCode(rest, "</details>")
}

// notionMarkdownVersion2DetailsTitleStarts tells what follows a <details>
// opening on its line that starts its title: text or an inline tag, not a
// block's tag.
func notionMarkdownVersion2DetailsTitleStarts(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return false
	}
	found := notionMarkdownRepairTag.FindStringSubmatchIndex(rest)
	return found == nil || found[0] != 0 || !notionMarkdownVersion2HoldingTag(strings.ToLower(rest[found[4]:found[5]]))
}

// olderDetails says, once, that a <details> or a <summary> was read as v1's,
// as its tag is read, after its attributes (seen 2026-11-10).
func (repair *notionMarkdownRepair) olderDetails(name string) {
	words := notionMarkdownOlderTagSaid("summary", "the <toggle>'s title, as it stood inside <details>. "+
		"Next time write the title on the first line of <toggle>, then a blank line.")
	if name == "details" {
		words = notionMarkdownOlderTagSaid("details", notionMarkdownVersion1Tags[name])
	}
	if repair.said == nil {
		repair.said = map[string]bool{}
	}
	if !repair.said["deprecated_tag"+name] {
		repair.said["deprecated_tag"+name] = true
		repair.parsed = append(repair.parsed, notionMarkdownWarning{Code: "deprecated_tag", Message: words})
	}
}

// detailsToggle is the tag at text[found[0]:found[1]] of the stretch of
// line read as it reads: a <details> first on a line of block tags
// (structured) whose title follows it there is the <toggle> it is converted
// to, said so, and so is its closing tag on that line. It gives the tag and
// the name to read on with.
func (repair *notionMarkdownRepair) detailsToggle(text, line string, found []int, first, structured bool) (string, string) {
	tag, name := text[found[0]:found[1]], strings.ToLower(text[found[4]:found[5]])
	if name != "details" || found[9] > found[8] {
		return tag, name
	}
	if found[3] > found[2] {
		if repair.toggledLine != line || repair.toggled == 0 {
			return tag, name
		}
		repair.toggled--
		return "</toggle" + tag[len("</details"):], "toggle"
	}
	// What follows it is the line's, where the stretch is the line's start.
	rest := text[found[1]:]
	if strings.HasPrefix(line, text) {
		rest = line[found[1]:]
	}
	if !structured || !notionMarkdownVersion2TagLeads(text, found[0], first) || !notionMarkdownVersion2DetailsTitle(rest) {
		return tag, name
	}
	repair.olderDetails(name)
	if repair.toggledLine != line {
		repair.toggledLine, repair.toggled = line, 0
	}
	repair.toggled++
	return "<toggle" + tag[len("<details"):], "toggle"
}
