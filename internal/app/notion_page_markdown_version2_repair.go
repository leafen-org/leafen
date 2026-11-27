package app

import (
	"regexp"
	"slices"
	"strings"
)

// The repair a "v2" write's lines go through as Notion reads their tags
// (notionMarkdownVersion2Input): what it holds, the tags it knows, and how
// it puts a stretch of a line right (text).

// notionMarkdownRepair puts a "v2" line's HTML right as Notion does and
// gathers what it says, once for each tag: v1's tags, which Leafen reads, are
// "deprecated_tag"; HTML it removes or puts as Notion's is
// "html_tag_autofix"; a tag neither reads is "unsupported_tag", kept as text.
// Code, and a tag escaped as \<tag>, are text as they are.
type notionMarkdownRepair struct {
	warnings []notionMarkdownWarning
	said     map[string]bool
	// refused is the first tag Notion's reading refuses standing inline, in
	// its words, and missing what tags lack that its reading needs, in order.
	refused string
	missing []string
	// openings is what it says of the refused openings of blocks' tags
	// first on their lines, said first (openingSaid).
	openings []string
	// inline are the block tags, standing in text or on their lines, in
	// order (trailing).
	inline []notionMarkdownInlineBlockTag
	// parsed is what the reading of the tags says after the warnings above:
	// their attributes and <details>, in order (attributes).
	parsed []notionMarkdownWarning
	// lists are the <ul> and <ol> tags open, each whether it opened;
	// listRefused tells the next to open holds no items.
	lists       []bool
	listRefused bool
	// unmade is what Notion refuses after reading the Markdown whole: a
	// block it reads but can't make.
	unmade string
	// removed are the warnings of HTML taken out with what it held, by
	// their words, which Notion says first (ordered).
	removed map[string]bool
	// sealed is whether the reading met an equation nothing closes standing
	// in block tags left open, past which it reads no block tag (seal).
	sealed bool
	// swallowed is the line the code or equation nothing closes may start
	// at, past which Notion counts a group's items still (bareItems).
	swallowed int
	// unheld are the columns and tabs a column or tab closed in itself
	// broke: the items after it in them are bare (selfClosed).
	unheld map[string]bool
	// groups are the refused groups' lines read again (columnsChild).
	groups *notionMarkdownVersion2GroupReads
	// heldOpenings are what is said of the refused openings each block tag
	// holds, by the tag's place among inline (heldOpening).
	heldOpenings map[int][]string
	// stretchSaid is what was said of each tag of the stretch read, by
	// where it stands, and placed what was said of the tags marked in their
	// texts, said where they stand (notion_page_markdown_version2_inline_said.go).
	stretchSaid map[int][2]int
	placed      []notionMarkdownPlaced
	// toggled is how many <details> of toggledLine read as the <toggle> each
	// is converted to are open there (detailsToggle).
	toggledLine string
	toggled     int
}

// notionMarkdownInlineBlockTag is a block tag a line holds, standing in its
// text (text) or not, and how much of what the reading says (missing) was
// said when it opened, what Notion says of the tag itself with it
// (notion_page_markdown_version2_frames.go).
type notionMarkdownInlineBlockTag struct {
	name                        string
	closing, closedInSelf, text bool
	at                          int
	// words is whether it stands in text after words on its line.
	words bool
	// bare is whether it is a column said bare (notionMarkdownVersion2FramedTrailing).
	bare bool
}

// notionMarkdownListTagMark stands before a list tag standing in text, which
// the line keeps as text, so that the tags it is read among see it
// (notionMarkdownVersion2Unclosed); the Markdown read has a \ there. No text
// a write sends holds the byte.
const notionMarkdownListTagMark = "\xff"

var (
	// A tag's attributes are name="value" or a name alone, named as
	// notionMarkdownAttributeName says ("<callout _x=\"1\">x</callout>" is a
	// callout, the attribute dropped); HTML's, and a tag neither reads, may
	// be written otherwise (notionMarkdownRepair.text).
	notionMarkdownRepairTag        = regexp.MustCompile(`<(/?)([A-Za-z][A-Za-z0-9_-]*)((?:[ \t]+` + notionMarkdownAttributeName + `(?:="[^"]*")?)*|[ \t][^<>]*?)[ \t]*(/?)>`)
	notionMarkdownRepairAttributes = regexp.MustCompile(`^(?:[ \t]+` + notionMarkdownAttributeName + `(?:="[^"]*")?)*$`)
	// The tags Notion's "v2" and Leafen's page Markdown read.
	notionMarkdownVersion2Tags = map[string]bool{
		"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "quote": true, "toggle": true, "callout": true,
		"ul": true, "ol": true, "li": true, "todo": true, "columns": true, "column": true, "tabs": true, "tab": true,
		"table": true, "colgroup": true, "col": true, "thead": true, "tbody": true, "tr": true, "td": true, "th": true,
		"equation": true, "table-of-contents": true, "image": true, "file": true, "pdf": true, "video": true,
		"audio": true, "bookmark": true, "embed": true, "page": true, "database": true, "folder": true,
		"synced-block": true, "synced-block-reference": true, "unknown": true, "empty-block": true, "mention": true,
		"date": true, "link-preview": true, "b": true, "i": true, "s": true, "u": true, "code": true, "span": true,
		"br": true, "strong": true, "em": true, "a": true, "meeting-notes": true, "custom-block": true, "mail": true,
		"external_object_instance": true, "unknown_mention": true,
	}
	// The tags that stand in text; the others are blocks, read only as a
	// line's tags, and text where text stands with them on their line.
	notionMarkdownVersion2InlineTags = map[string]bool{
		"b": true, "i": true, "s": true, "u": true, "code": true, "span": true, "br": true, "strong": true, "em": true,
		"a": true, "mention": true, "date": true, "link-preview": true, "unknown_mention": true,
	}
	// v1's tags, which Leafen reads as their "v2" ones, and what each was read as (notionMarkdownOlderTagSaid).
	notionMarkdownVersion1Tags = map[string]string{
		"details":                "<toggle>. Write <toggle> next time.",
		"summary":                "",
		"mention-page":           "<mention>. Write <mention url=\"...\" /> next time.",
		"mention-user":           "<mention>. Write <mention url=\"user://...\" /> next time.",
		"mention-database":       "<mention>. Write <mention url=\"pageOrCollectionViewBlock://...\" type=\"database\" /> next time (leave type out for a collection:// URL).",
		"mention-data-source":    "<mention>. Write <mention url=\"collection://...\" /> next time.",
		"mention-date":           "<date>. Write <date start=\"YYYY-MM-DD\" end=\"YYYY-MM-DD\" start-time=\"HH:mm\" end-time=\"HH:mm\" time-zone=\"IANA_TZ\" /> next time.",
		"table_of_contents":      "<table-of-contents>. Write <table-of-contents /> next time.",
		"synced_block":           "<synced-block>. Write <synced-block>...</synced-block> next time.",
		"synced_block_reference": "<synced-block-reference>. Write <synced-block-reference url=\"...\">...</synced-block-reference> next time.",
	}
	// The marks a tag around text gives it.
	notionMarkdownVersion2Marks = map[string]bool{
		"b": true, "i": true, "s": true, "u": true, "code": true, "span": true, "strong": true, "em": true,
	}
	// HTML Notion takes out, keeping what it holds, or with it.
	notionMarkdownHTMLKept    = map[string]bool{"div": true, "sup": true, "sub": true, "small": true, "aside": true, "html": true, "body": true}
	notionMarkdownHTMLSpaced  = map[string]bool{"div": true, "aside": true, "html": true, "body": true}
	notionMarkdownHTMLDropped = map[string]*regexp.Regexp{
		"script": regexp.MustCompile(`<script(?:[ \t][^>]*)?>.*?</script>`),
		"style":  regexp.MustCompile(`<style(?:[ \t][^>]*)?>.*?</style>`),
		"head":   regexp.MustCompile(`<head(?:[ \t][^>]*)?>.*?</head>`),
	}
	notionMarkdownHTMLQuote = regexp.MustCompile(`^<blockquote(?:[ \t][^<>]*)?>`)
	// An HTML link is Markdown's, as Notion puts it.
	notionMarkdownHTMLLink  = regexp.MustCompile(`<a((?:[ \t]+` + notionMarkdownAttributeName + `(?:="[^"]*")?)*)[ \t]*>(.*?)</a>`)
	notionMarkdownHTMLWords = map[string]string{
		"img":        "made ![](url) where it stood on a line of its own with an http(s) src, and its alt text anywhere else. Write ![caption](url) on a line of its own; the caption shows under the image.",
		"mark":       "made <span color=\"yellow_bg\">. Write <span color=\"yellow_bg\"> next time.",
		"kbd":        "made <code>. Write <code> next time.",
		"hr":         "made --- where it stood on a line of its own, and a space anywhere else. Write --- on a line of its own next time.",
		"blockquote": "made <quote> where it started a line and its closing tag ended one; anywhere else it was taken out and what it held kept. Write <quote> at the start of a line next time.",
	}
	notionMarkdownCodeSpan = regexp.MustCompile("`+[^`]*`+|<code>.*?</code>")
)

func (repair *notionMarkdownRepair) say(code, name, words string) {
	if repair.said == nil {
		repair.said = map[string]bool{}
	}
	if repair.said[code+name] {
		return
	}
	repair.said[code+name] = true
	repair.warnings = append(repair.warnings, notionMarkdownWarning{Code: code, Message: words})
}

// text puts a stretch of a line right; whole is whether it is all the line.
func (repair *notionMarkdownRepair) text(text string, whole bool, line string) string {
	trimmed, first := strings.TrimSpace(text), strings.HasPrefix(line, text)
	// Its attributes go with it (seen 2026-11-10).
	if opening := notionMarkdownHTMLQuote.FindString(trimmed); whole && opening != "" && strings.HasSuffix(trimmed, "</blockquote>") {
		repair.say("html_tag_autofix", "blockquote", notionMarkdownHTMLSaid("blockquote"))
		text = strings.Replace(strings.Replace(text, opening, "<quote>", 1), "</blockquote>", "</quote>", 1)
	}
	text = repair.dropped(text)
	// A quote's lines start after their >.
	block := strings.HasPrefix(strings.TrimLeft(line, " \t>"), "<")
	// Block tags are a line's only when its text stands inside one: a line
	// that a mark's tag starts, "<b>x <ul> y</b>", holds them in its text;
	// v1's contents are as Notion puts them (repair.contents).
	put := notionMarkdownVersion1ContentsLine(line)
	// One whose opening tag is refused is a line of text (notionMarkdownVersion2RefusedOpening).
	structured := notionMarkdownVersion2BlockTag.MatchString(strings.TrimLeft(put, " \t>")) &&
		strings.HasSuffix(strings.TrimSpace(put), ">") && !notionMarkdownVersion2ClosedBeforeText(put) &&
		!notionMarkdownVersion2RefusedOpening(strings.TrimLeft(put, " \t>"))
	// Tags whose opening can't be read are said or words (repair.openers).
	repair.stretchSaid = nil
	words := repair.openers(text, first)
	repair.loose(text, first, words)
	var out strings.Builder
	at := 0
	for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(text[at:found[0]] + repair.inlineMark(found[0]))
		tag := text[found[0]:found[1]]
		at = found[1]
		if notionMarkdownEscapedAt(text, found[0]) {
			out.WriteString(tag)
			continue
		}
		if words[found[0]] {
			repair.refusedOpening(text, found, first)
			out.WriteString(`\` + tag)
			continue
		}
		written := text[found[4]:found[5]]
		closing, name := found[3] > found[2], strings.ToLower(written)
		// A list's tag its closing tag left text is so already
		// (notionMarkdownVersion2ListClosings).
		if found[0] > 0 && text[found[0]-1] == notionMarkdownListTagMark[0] {
			repair.standsInText(name, closing, false)
			out.WriteString(tag)
			continue
		}
		// A tag Notion reads whose attributes aren't written as its reading
		// takes them is text; HTML's is put right all the same (seen
		// 2026-11-10: <div zz=1>).
		if !notionMarkdownRepairAttributes.MatchString(text[found[6]:found[7]]) && !notionMarkdownHTMLTag(name) &&
			(notionMarkdownVersion2Tags[name] || notionMarkdownVersion1Tags[name] != "" || name == "details" || name == "summary") {
			repair.refusedOpening(text, found, first)
			out.WriteString(tag)
			continue
		}
		// Tags are named in small letters: <B> is no tag (seen 2026-11-10).
		if written != name {
			repair.unsupported(written, text[found[6]:found[7]])
			out.WriteString(`\` + tag)
			continue
		}
		// One neither reads holding an attribute without a value is said for
		// nothing (repair.unsupported).
		if !notionMarkdownVersion2Tags[name] && notionMarkdownVersion1Tags[name] == "" && name != "details" && name != "summary" &&
			!notionMarkdownHTMLTag(name) && !notionMarkdownValuedAttributes.MatchString(text[found[6]:found[7]]) {
			out.WriteString(`\` + tag)
			continue
		}
		// v1's <details> holding its title on its line reads as the <toggle>
		// it is converted to (notion_page_markdown_version2_details_toggles.go).
		tag, name = repair.detailsToggle(text, line, found, first, structured)
		written = name
		// A mark closed in itself, <b/>, marks nothing and goes.
		if found[9] > found[8] && notionMarkdownVersion2Marks[name] {
			continue
		}
		if repair.summaryWords(name, closing, line) {
			out.WriteString(`\` + tag)
			continue
		}
		if name == "table_of_contents" {
			out.WriteString(repair.contents(text[found[6]:found[7]], closing, structured))
			continue
		}
		// A contents' tag not closed in itself is its words (seen 2026-11-14:
		// "<table-of-contents>"; its pair is refused, selfClosing).
		if name == "table-of-contents" && found[9] == found[8] {
			out.WriteString(`\` + tag)
			continue
		}
		alone := whole && strings.TrimSpace(text) == tag
		if notionMarkdownVersion2Tags[name] && !notionMarkdownVersion2InlineTags[name] && !structured {
			// One first on its line whose attribute has no value is refused (repair.valueless).
			valueless := false
			if !closing && notionMarkdownVersion2Leading(text, found[0], first) {
				mark := len(repair.missing)
				valueless = repair.valueless(name, text[found[6]:found[7]])
				repair.openingSaid(mark, found[0])
			}
			// An <unknown> in text is its words (seen 2026-11-14: "a<unknown>b").
			if name != "unknown" {
				repair.standsInText(name, closing, found[9] > found[8])
				if last := len(repair.inline) - 1; last >= 0 && repair.inline[last].text && repair.inline[last].name == name {
					repair.inline[last].words = !notionMarkdownVersion2Leading(text, found[0], first)
				}
			}
			switch name {
			case "ul", "ol", "li", "todo":
				// A <todo> opening its line is a block's tag left open, as a
				// <p> is (seen 2026-11-14: "<todo>`a</todo>`" says only that).
				if name == "todo" && !closing && whole && found[0] == len(text)-len(strings.TrimLeft(text, " \t>")) {
					out.WriteString(`\` + tag)
					continue
				}
				// Said among the inline tags it stands with; an item's opening
				// so refused is said for that alone (seen 2026-11-15: "<li
				// x>a</li>" as "<li x:y=\"1\">a</li>").
				if valueless && name == "li" {
					out.WriteString(`\` + tag)
					continue
				}
				out.WriteString(notionMarkdownListTagMark + tag)
				continue
			case "page":
				// A to-do's text Notion reads twice, so says it twice (seen
				// 2026-11-14: "- [ ] <page />", "- [ ] x <page></page>").
				said := "<page> is a block tag, so it goes on a line of its own; to point to a page inside text, use <mention>."
				if rest := line[len(notionMarkdownVersion2MarkerRun.FindString(line)):]; notionMarkdownFlowTodo.MatchString(rest) &&
					strings.Trim(line[:len(line)-len(rest)], " \t>") != "" {
					said += "\n" + said
				}
				// The first is said where it stands among the text's inline tags
				// (notion_page_markdown_version2_inline_said.go).
				if !closing && repair.refused == "" {
					repair.refuse(said)
					out.WriteString(repair.place(notionMarkdownPlaced{words: said}))
				}
			}
			out.WriteString(`\` + tag)
			continue
		}
		opens := true
		if !closing {
			// Its attributes as Notion reads them: those it leaves out go.
			kept, mark := "", len(repair.missing)
			if kept, opens = repair.attributes(name, text[found[6]:found[7]]); kept != text[found[6]:found[7]] {
				tag = "<" + written + kept + text[found[7]:found[1]]
			}
			if notionMarkdownVersion2InlineTags[name] {
				repair.inlineAt(found[0], mark)
				out.WriteString(repair.inlineMark(found[0]))
			}
			// An <a> whose attribute is malformed is said for that alone.
			if opens || name != "a" {
				repair.check(name, tag, notionMarkdownVersion2AttributeValues(kept))
			}
			repair.dateBody(name, tag, text[found[1]:])
		}
		// A block's closing tag that closes none open is its words where it
		// stands (repair.closesNone).
		if closing && repair.closesNone(name) {
			out.WriteString(`\` + tag)
			continue
		}
		// Where v1's tags read as tags (repair.v1Placed).
		leads, placed := repair.v1Placed(name, text, found, first, block, structured)
		// An <unknown> left open is text (seen 2026-11-10).
		// A <summary> starting a line is a toggle's title only in a <details>
		// (seen 2026-11-14: "<summary>a\</summary>" alone is its words,
		// "<details>⏎<summary>a⏎b⏎</details>" leaves it open).
		if (notionMarkdownVersion2Tags[name] && !notionMarkdownVersion2InlineTags[name] || structured && placed && name == "details" ||
			(leads || closing && block) && name == "summary" && slices.Contains(repair.openTags(), "details")) && name != "unknown" {
			repair.block(name, closing, found[9] > found[8], opens)
		}
		out.WriteString(repair.tag(tag, name, closing, notionMarkdownAttributes(text[found[6]:found[7]]), alone, placed))
	}
	out.WriteString(text[at:])
	return out.String()
}
