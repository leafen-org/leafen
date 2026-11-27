package app

import (
	"regexp"
	"strings"
)

// A block's opening tag first on its line that Notion's "v2" can't read —
// an attribute without a value, or its value in single quotes or none
// ("<columns x>", "<columns x=1>", "<columns x='1'>"), a quote the line ends
// in ("<columns x=\"1>"), a name starting with neither a letter nor "_" or
// an = without one ("<columns 1x=…>", "<columns -x=…>", "<columns = …>"), a
// name a ":" or a "." ends ("<columns x:y=…>") — is refused and said so, and
// its line is a line of text holding the tag (seen 2026-11-15, each written
// twice, create, replace_content and insert_content alike): what stands after
// the tag on its line, and the lines four columns or more past it
// (notionMarkdownVersion2WrittenLines), go on with that text, so the tags in
// them stand in text — a <column> or a <tab> there says nothing, an <li> or
// a <ul> that it cannot be used inline ("<columns x:y=\"1\">⏎\t<column>⏎\t\ta⏎
// \t</column>⏎</columns>" says the malformed attribute alone, a blank line
// between them too); a line less far in starts a block of its own
// ("<columns x:y=\"1\">⏎<column>⏎a⏎</column>⏎</columns>" and "  <column>"
// say a bare <column> too, "<ul x:y=\"1\">⏎<li>a</li>⏎</ul>" a bare <li>);
// and the tag, with those standing in its text, is left open where nothing
// closes it ("<columns x:y=\"1\">⏎\t<column>⏎\t\ta" says <columns> and
// <column> were left open, notionMarkdownVersion2TrailingSaid, first).

// notionMarkdownVersion2RefusedOpening tells a line, trimmed, that starts
// with a block's opening tag so refused.
func notionMarkdownVersion2RefusedOpening(trimmed string) bool {
	match := notionMarkdownVersion2TagName.FindStringSubmatch(trimmed)
	if match == nil || !notionMarkdownVersion2RefusableBlock(match[1]) {
		return false
	}
	if fault, _ := notionMarkdownVersion2OpenerFault(trimmed[len(match[0]):]); fault != "" {
		return true
	}
	found := notionMarkdownRepairTag.FindStringSubmatchIndex(trimmed)
	if found == nil || found[0] != 0 || found[3] > found[2] {
		return false
	}
	if _, checked := notionMarkdownTagAttributes[match[1]]; !checked {
		return false
	}
	written := trimmed[found[6]:found[7]]
	if notionMarkdownLooseAttribute.MatchString(notionMarkdownQuotedValue.ReplaceAllString(written, `=""`)) {
		return true
	}
	for _, one := range notionMarkdownAttributeToken.FindAllStringSubmatchIndex(written, -1) {
		if one[4] < 0 && (one[1] == len(written) || written[one[1]] != '=') {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2RefusableBlock tells a block's tag, by the name it
// is written with, whose opening refused makes its line text: one "v2"
// reads, v1's aside.
func notionMarkdownVersion2RefusableBlock(name string) bool {
	if _, legacy := notionMarkdownLegacyTags[name]; legacy {
		return false
	}
	return (notionMarkdownVersion2Tags[name] || name == "details") && !notionMarkdownVersion2InlineTags[name]
}

// refusedOpening records the tag at text[found[0]], if it is a block's
// opening tag first on its line that its reading refuses, as standing in
// text: open until a closing tag closes it; one no reading holds open
// (notionMarkdownVersion2NeverOpen) stays unsaid ("<unknown url='…'>" says
// its malformed attribute alone).
func (repair *notionMarkdownRepair) refusedOpening(text string, found []int, first bool) {
	name := text[found[4]:found[5]]
	if found[3] > found[2] || !notionMarkdownVersion2RefusableBlock(name) || notionMarkdownVersion2NeverOpen[name] ||
		!notionMarkdownVersion2Leading(text, found[0], first) ||
		!notionMarkdownVersion2RefusedOpening(strings.TrimSpace(text[found[0]:])) {
		return
	}
	repair.standsInText(name, false, found[9] > found[8])
}

// nameless refuses a tag whose attributes' names can't be read; a list's
// tag and an item's are read twice, and said twice (repair.malformed; seen
// 2026-11-15: "<ul 1x=\"1\">", "<ol 1x=\"1\">", "<li 1x=\"1\">b</li>"), where
// its quote its line ends in is said once ("<ul x=\"1>").
func (repair *notionMarkdownRepair) nameless(name string) {
	said := "<" + name + `> has attributes written wrong: quote each value, as in <` + name + ` key="value">.`
	repair.missing = append(repair.missing, said)
	if name == "ul" || name == "ol" || name == "li" {
		repair.missing = append(repair.missing, said)
	}
}

// openingSaid puts what was said since mark, of a block's opening tag first
// on its line that its reading refuses, with what is said first: Notion
// says such openings before all else but the tags left open (seen
// 2026-11-15: "a <b>x⏎⏎<columns x:y=\"1\">⏎</columns>" says the malformed
// attribute before the missing </b>, "<li>z</li>⏎<columns x:y=…>⏎\t<li>a</li>⏎
// </columns>" before the inline <li> and the bare one, "x <span x:y=…>a</span>⏎
// ⏎<columns x:y=…>" before the <span>'s; "Trailing unclosed …" comes first).
// A list's tag or an item's, read twice (repair.malformed), is said the
// second time among its text's inline tags, where it stands at text[at]
// (notion_page_markdown_version2_inline_said.go).
func (repair *notionMarkdownRepair) openingSaid(mark, at int) {
	// In a block's tag of another name it is said where that block is, as
	// its own reading says it (heldOpening).
	if repair.heldOpening(mark) {
		return
	}
	if said := repair.missing[mark:]; len(said) == 2 && said[0] == said[1] {
		repair.openings = append(repair.openings, said[0])
		repair.missing = repair.missing[:mark+1]
		repair.inlineAt(at, mark)
		return
	}
	repair.openings = append(repair.openings, repair.missing[mark:]...)
	repair.missing = repair.missing[:mark]
}

// notionMarkdownVersion2ListOpenings reads a list's opening tag alone on its
// line as its expression does, but for one whose opening is refused.
type notionMarkdownVersion2ListOpenings struct{ *regexp.Regexp }

func (lists notionMarkdownVersion2ListOpenings) MatchString(line string) bool {
	return lists.Regexp.MatchString(line) && !notionMarkdownVersion2RefusedOpening(line)
}

func (lists notionMarkdownVersion2ListOpenings) FindStringSubmatch(line string) []string {
	if notionMarkdownVersion2RefusedOpening(line) {
		return nil
	}
	return lists.Regexp.FindStringSubmatch(line)
}
