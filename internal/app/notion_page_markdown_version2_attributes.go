package app

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A "v2" write reads each tag's attributes as Notion's API does (seen
// 2026-11-10, each tag written with replace_content): a tag takes only its
// own attributes, named in small letters, and an attribute it doesn't take,
// a second of one it has, or a value it can't read is left out and said in
// the answer's warnings — for each tag, the repeated ones first, then the
// ones it doesn't take, then the values, each in the order written. Some it
// refuses instead: an attribute without a quoted value, a list's start that
// isn't a whole number from 1, a file's or link's empty address, both its
// address attributes, and a mention of a kind it doesn't know. A table's
// <colgroup>, <thead> and <tbody> take any attribute, and say only a second
// of one. v1's tags are read as the tag each becomes, a <mention-date> as a
// <date> that also takes its startTime, endTime and timeZone. A tag this
// doesn't name is read as it is.

// notionMarkdownTagAttributes are the attributes each tag takes, as Notion
// names them.
var notionMarkdownTagAttributes = map[string][]string{
	"p": {"color"}, "h1": {"color"}, "h2": {"color"}, "h3": {"color"}, "h4": {"color"}, "quote": {"color"},
	"li": {"color"}, "tr": {"color"}, "td": {"color"}, "th": {"color"}, "equation": {"color"},
	"table-of-contents": {"color"}, "details": {"color"}, "toggle": {"color", "heading"},
	"callout": {"icon", "color"}, "todo": {"checked", "color"}, "ol": {"type", "start"}, "column": {"ratio"},
	"tab": {"icon"}, "table": {"fit-page-width", "header-row", "header-column"}, "col": {"width", "color"},
	"image": {"source", "src", "color"}, "file": {"source", "src", "color"}, "pdf": {"source", "src", "color"},
	"video": {"source", "src", "color"}, "audio": {"source", "src", "color"}, "embed": {"source", "src", "color"},
	"bookmark": {"source"}, "synced-block": {"url"}, "span": {"color", "discussion-urls"}, "a": {"href"},
	"date": {"start", "end", "start-time", "end-time", "time-zone"}, "mention": {"url", "type"},
	"link-preview": {"url", "source"}, "page": {"url", "color"},
	"database": {"url", "data-source-url", "inline", "icon", "color", "wiki"}, "synced-block-reference": {"url", "notice"},
	"unknown": {"url", "alt"}, "link-to-page": {"url", "type"}, "button": {"url"}, "meeting-notes": {"attendees", "view-url", "legacy"}, "ul": {}, "columns": {}, "tabs": {},
	"mail": {"to", "cc", "bcc", "from", "subject", "attachments"}, "empty-block": {}, "b": {}, "i": {}, "s": {}, "u": {},
	"em": {}, "strong": {}, "code": {}, "br": {}, "colgroup": nil, "thead": nil, "tbody": nil,
}

// notionMarkdownLegacyTags are v1's tags, read as the tag each becomes
// (notionMarkdownVersion1Tags), and notionMarkdownLegacyAttributes what
// one takes beside that tag's, unsaid.
var (
	notionMarkdownLegacyTags = map[string]string{
		"mention-page": "mention", "mention-user": "mention", "mention-database": "mention", "mention-data-source": "mention",
		"mention-date":      "date",
		"table_of_contents": "table-of-contents", "synced_block": "synced-block", "synced_block_reference": "synced-block-reference",
	}
	notionMarkdownLegacyAttributes = map[string][]string{"mention-date": {"startTime", "endTime", "timeZone"}}
)

// notionMarkdownLeafenAttributes are attributes only Leafen's own Markdown
// writes, for what Notion's blocks don't hold: an image's link and an
// embed's service. They read as written.
var notionMarkdownLeafenAttributes = map[string]string{"image": "link", "embed": "service"}

var (
	notionMarkdownAttributeToken = regexp.MustCompile(`[ \t]+(` + notionMarkdownAttributeName + `)(?:="([^"]*)")?`)
	// A tag whose attribute has a value in single quotes or none.
	notionMarkdownLooseTag       = regexp.MustCompile(`<([a-z][a-z0-9-]*)((?:[ \t]+[^<>]*?)?)[ \t]*/?>`)
	notionMarkdownLooseAttribute = regexp.MustCompile(`(` + notionMarkdownAttributeName + `)=(?:'|[^"]|$)`)
	notionMarkdownQuotedValue    = regexp.MustCompile(`="[^"]*"`)
	notionMarkdownVersion2Colors = map[string]bool{}
)

func init() {
	for _, color := range []string{"gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"} {
		notionMarkdownVersion2Colors[color], notionMarkdownVersion2Colors[color+"_bg"] = true, true
	}
}

// notionMarkdownAttribute is one attribute as a tag writes it.
type notionMarkdownAttribute struct {
	name, value string
	valued      bool
}

// attributes reads an opening tag's attributes, as written after its name:
// the ones it keeps, written back, and whether Notion refuses the tag.
func (repair *notionMarkdownRepair) attributes(name, written string) (string, bool) {
	// A <details>'s title takes none, unsaid; v1's mentions are read as
	// the <mention> each becomes.
	if name == "summary" {
		return "", true
	}
	legacy := notionMarkdownLegacyAttributes[name]
	if converted, ok := notionMarkdownLegacyTags[name]; ok {
		name = converted
	}
	allowed, checked := notionMarkdownTagAttributes[name]
	if !checked {
		return written, true
	}
	all := []notionMarkdownAttribute{}
	for _, found := range notionMarkdownAttributeToken.FindAllStringSubmatchIndex(written, -1) {
		one := notionMarkdownAttribute{name: written[found[2]:found[3]], valued: found[4] >= 0}
		if one.valued {
			one.value = notionMarkdownVersion2AttributeValue(written[found[4]:found[5]])
		}
		all = append(all, one)
	}
	for _, one := range all {
		if !one.valued {
			repair.malformed(name, one.name, notionMarkdownQuoteValueSaid)
			return written, false
		}
	}
	takes := map[string]bool{notionMarkdownLeafenAttributes[name]: true}
	for _, attribute := range slices.Concat(allowed, legacy) {
		takes[attribute] = true
	}
	kept, seen := []notionMarkdownAttribute{}, map[string]bool{}
	for _, one := range all {
		if seen[one.name] {
			repair.attributeSaid("duplicate_attribute", `<`+name+`> names attribute "`+one.name+`" twice; the first value counts.`)
			continue
		}
		seen[one.name] = true
		kept = append(kept, one)
	}
	named := kept[:0:0]
	for _, one := range kept {
		if allowed != nil && !takes[one.name] {
			repair.attributeSaid("invalid_attribute", `<`+name+`> has no attribute "`+one.name+`", so it was left out; it takes `+
				strings.Join(allowed, ", ")+".")
			continue
		}
		named = append(named, one)
	}
	ok, out := true, strings.Builder{}
	for _, one := range named {
		said, refused := notionMarkdownAttributeValueSaid(name, one, named)
		if refused {
			repair.missing = append(repair.missing, said)
			ok = false
			continue
		}
		if said != "" {
			repair.attributeSaid("invalid_attribute_value", said)
			continue
		}
		// Written so that the page Markdown's reading reads it as it is.
		out.WriteString(" " + one.name + `="` + notionMarkdownAttributeEscaped.Replace(one.value) + `"`)
	}
	return out.String(), ok
}

// notionMarkdownAttributeValueSaid is what Notion says of an attribute's
// value, among the tag's others: words for a value it leaves out, or
// refused when it refuses the tag; none for one it reads.
func notionMarkdownAttributeValueSaid(name string, one notionMarkdownAttribute, all []notionMarkdownAttribute) (string, bool) {
	value := one.value
	switch one.name {
	case "color":
		if !notionMarkdownVersion2Colors[value] {
			return `<` + name + `> has no color "` + value + `"; colors include gray, brown, orange, yellow and blue_bg, purple_bg, pink_bg, red_bg.`, false
		}
	case "checked", "fit-page-width", "header-row", "header-column", "inline", "wiki":
		if value != "true" && value != "false" {
			return `<` + name + `> attribute "` + one.name + `" is true or false, not "` + value + `".`, false
		}
	case "ratio", "width":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return `<` + name + `> attribute "` + one.name + `" takes a number, not "` + value + `".`, false
		}
	case "heading":
		if value != "h1" && value != "h2" && value != "h3" && value != "h4" {
			return `<toggle> attribute "heading" is h1, h2, h3 or h4, not "` + value + `".`, false
		}
	case "type":
		if name == "ol" && value != "1" && value != "a" && value != "A" && value != "i" && value != "I" {
			return `<ol> attribute "type" is 1, a, A, i or I, not "` + value + `".`, false
		}
		if name == "mention" && !map[string]bool{"page": true, "database": true, "data-source": true, "user": true,
			"agent": true, "team": true, "group": true}[value] {
			return `<mention> attribute "type" is page, database, data-source, user, agent, team or group, not "` + value + `".`, true
		}
	case "start":
		if name == "ol" {
			return notionMarkdownListStartSaid(value)
		}
	case "source", "src":
		if name == "link-preview" {
			break
		}
		if value == "" {
			return `<` + name + `> has an empty "` + one.name + `".`, true
		}
		if one.name == "source" {
			for _, other := range all {
				if other.name == "src" {
					return `<` + name + `> takes "source" or "src", not both.`, true
				}
			}
		}
	}
	return "", false
}

// notionMarkdownListStartSaid is what Notion says of an <ol>'s start: none
// for a whole number from 1 it can hold exactly, a refusal else.
func notionMarkdownListStartSaid(value string) (string, bool) {
	said := `A numbered list can't start at "` + value + `": `
	if value == "" || strings.Trim(value, "0123456789") != "" {
		return said + "it starts at a whole number from 1.", true
	}
	number, err := strconv.ParseUint(value, 10, 64)
	switch {
	case err != nil || number > 1<<53-1:
		return said + "that number is too large to keep exactly.", true
	case number == 0:
		return said + "it starts at 1 or more.", true
	}
	return "", false
}

// malformed refuses a tag whose attribute isn't written name="value"; a
// list's tag and an item's are read twice, and said twice (seen 2026-11-15:
// "<li x:y=\"1\">b</li>", "<li x>a</li>"; an item a list's reading of its
// children refuses is said once there, notionMarkdownVersion2Input).
func (repair *notionMarkdownRepair) malformed(name, attribute, words string) {
	said := `<` + name + `> has its attribute "` + attribute + `" written wrong: ` + words
	repair.missing = append(repair.missing, said)
	if name == "ul" || name == "ol" || name == "li" {
		repair.missing = append(repair.missing, said)
	}
}

// loose refuses the tags in a stretch of a line (first: it starts the line)
// whose attribute's value stands in single quotes or none, which the tag
// reading doesn't take: the tags it reads attributes of, and a <details>'s
// <summary> (seen 2026-11-10); a block's tag after words is words (seen
// 2026-11-15: "a <p color='red'>x</p>"), and one whose opening its reading
// can't read is said so alone (words, repair.openers). A table's part says
// too that the table's reading stopped there.
func (repair *notionMarkdownRepair) loose(text string, first bool, words map[int]bool) {
	for _, found := range notionMarkdownLooseTag.FindAllStringSubmatchIndex(text, -1) {
		name := text[found[2]:found[3]]
		if _, checked := notionMarkdownTagAttributes[name]; !checked && name != "summary" || notionMarkdownEscapedAt(text, found[0]) ||
			words[found[0]] || notionMarkdownVersion2Tags[name] && !notionMarkdownVersion2InlineTags[name] &&
			!notionMarkdownVersion2Leading(text, found[0], first) {
			continue
		}
		written := notionMarkdownQuotedValue.ReplaceAllString(text[found[4]:found[5]], `=""`)
		mark := len(repair.missing)
		if attribute := notionMarkdownLooseAttribute.FindStringSubmatch(written); attribute != nil {
			repair.malformed(name, attribute[1], notionMarkdownDoubleQuoteValueSaid)
			if notionMarkdownTagAttributes[name] == nil && name != "summary" {
				repair.missing = append(repair.missing, notionMarkdownHoldsOnly("table", "<colgroup>, <thead>, <tbody>, <tr>")+
					notionMarkdownPartialChildSaid)
			}
		}
		if notionMarkdownVersion2RefusableBlock(name) {
			repair.openingSaid(mark, found[0])
		} else if notionMarkdownVersion2InlineTags[name] {
			repair.inlineAt(found[0], mark)
		}
	}
}

// dateBody says, of a date's tag that isn't closed in itself, that the words
// it holds before its closing tag in rest go, each time; spaces alone say
// nothing (seen 2026-11-11, <mention-date> too).
func (repair *notionMarkdownRepair) dateBody(name, tag, rest string) {
	if name != "date" && name != "mention-date" || strings.HasSuffix(tag, "/>") {
		return
	}
	// A <mention-date>'s closing tag is a </date> too, as it becomes one
	// (seen 2026-11-14: "<mention-date start=…>b</date>" says so).
	end := strings.Index(rest, "</"+name+">")
	if dated := strings.Index(rest, "</date>"); name == "mention-date" && dated >= 0 && (end < 0 || dated < end) {
		end = dated
	}
	if end > 0 && strings.TrimSpace(rest[:end]) != "" {
		repair.attributeSaid("ignored_date_mention_body", "A date mention has no label, so the text inside it was left out; "+
			"write the date as <date ... />.")
	}
}

// notionMarkdownNamed reports whether a tag's attributes name one, even empty.
func notionMarkdownNamed(attributes map[string]string, name string) bool {
	_, named := attributes[name]
	return named
}

// attributeSaid notes what the reading of a tag's attributes says, each time.
func (repair *notionMarkdownRepair) attributeSaid(code, words string) {
	repair.parsed = append(repair.parsed, notionMarkdownWarning{Code: code, Message: words})
}
