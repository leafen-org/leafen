package app

import (
	"regexp"
	"slices"
	"strings"
)

// The block tags a "v2" write leaves open, as Notion's reading finds them
// (seen 2026-11-14), said in Leafen's words: a closing tag closes the last tag of its kind open, and the
// tags opened in it after it stay open to the end — a <p> a </toggle> or a
// </callout> closes over is refused as left open, said in that block's own
// reading (notionMarkdownVersion2FramedTrailing), and a list's
// item, a table's cell or a toggle's <summary> a closing tag around it
// closes over is never closed (notionMarkdownUnclosedBlock), said after them ("<ul>⏎<li>a⏎</ul>",
// "<details>⏎<summary>a\</summary>⏎b⏎</details>", "<td>a\</td>" in its
// row). A <summary> left open says so whatever holds it
// ("<details>⏎<summary>a\</summary>" says its <details> left open, then
// its <summary>); an item or a cell nothing closed over stands among the
// tags left open ("<ul>⏎<li>a\</li>" is "<ul>, <li>"). An escaped closing
// tag closes nothing (notionMarkdownRepair.text).

// notionMarkdownFlowParents are the tags whose closing tag closing over an
// item, a cell or a table's part Notion says closed it as a flow tag; one
// another block's closing tag closes over stands among the tags left open
// (seen 2026-11-14: "<callout>⏎<tr>⏎</callout>" says "Trailing unclosed …:
// <tr>.", "<td>", "<li>" and "<column>" so).
var notionMarkdownFlowParents = map[string][]string{
	"li": {"ul", "ol"}, "td": {"tr", "table", "thead", "tbody"}, "th": {"tr", "table", "thead", "tbody"},
	"tr": {"table", "thead", "tbody"}, "thead": {"table"}, "tbody": {"table"},
}

// notionMarkdownBlockTagsLeft reads block tags in order: those still open
// that no closing tag closed over (current), the open ones Notion says it
// closed at the end (trailing), and the flow tags it says it closed
// (flow).
func notionMarkdownBlockTagsLeft(tags []notionMarkdownInlineBlockTag) (current, trailing, flow []string) {
	for _, one := range notionMarkdownBlockTagsOpen(tags) {
		if !one.over {
			current = append(current, one.name)
		}
		if one.flow {
			flow = append(flow, one.name)
		}
		if !one.flow || one.first {
			trailing = append(trailing, one.name)
		}
	}
	return current, trailing, flow
}

// notionMarkdownBlockTagOpen is a block tag no closing tag of its own
// closed: where it opened among the tags read (event), whether a closing
// tag closed over it, whether Notion says it closed it as a flow tag, and
// whether it says it among the tags left open too (first).
type notionMarkdownBlockTagOpen struct {
	name              string
	event             int
	over, flow, first bool
	// in is the tag whose closing tag first closed over it, and under the
	// first tag in that one closed over with it that stands in no text and
	// holds it, -1 for none (notionMarkdownVersion2FramedTrailing).
	in, under int
}

// notionMarkdownBlockTagsOpen are the block tags no closing tag of their
// own closed, in order. A list's closing tag closes any tag over as a flow
// tag (seen 2026-11-15: "<ul>⏎<li>a</li>⏎<callout>⏎x⏎</ul>" says its
// <callout> so, "<ol>⏎<ul>⏎<li>a</li>⏎</ol>" its <ul>), and so does an
// item's ("<li>a\</li>⏎<p>y </li>", "<li>a\</li>⏎<callout>y </li>" and
// "<li>a\</li>⏎<p>⏎y </li>" say the <li> bare, then its <p> or <callout>).
// Where that item is one a list's closing tag standing in text closed over
// first (notionMarkdownVersion2ListClosings), Notion says the tags it
// closes over among those left open too, its first reading closing the item
// with that list (seen 2026-11-15: "<ul>⏎<li>a\</li>⏎</ul> x⏎<p>y </li>⏎z"
// says "Trailing unclosed …: <p>.", a's bare <li>, then its <p> closed as a
// flow tag; "…⏎<p>y </li>⏎<p>z </li>" "…: <p>, <p>.").
func notionMarkdownBlockTagsOpen(tags []notionMarkdownInlineBlockTag) []notionMarkdownBlockTagOpen {
	type opened struct {
		notionMarkdownBlockTagOpen
		// by is the closing tag that first closed over it, and byText
		// whether a list's closing tag standing in text closed over it.
		by     string
		byText bool
	}
	// closed are those an item's closing tag closed over, which no closing
	// tag of their own closes after it ("<li>a\</li>⏎<p>y </li>⏎z</p>" says
	// its <p> too).
	stack, closed := []opened{}, []opened{}
	for event, tag := range tags {
		switch {
		case tag.closedInSelf:
		case !tag.closing:
			stack = append(stack, opened{notionMarkdownBlockTagOpen: notionMarkdownBlockTagOpen{name: tag.name, event: event, in: -1, under: -1}})
		default:
			// One its first reading leaves open, a closing tag of its own
			// after closes there ("…⏎<p>y </li>⏎z</p>" says nothing of it).
			if at := slices.IndexFunc(closed, func(one opened) bool { return one.first && one.name == tag.name }); at >= 0 &&
				!slices.ContainsFunc(stack, func(one opened) bool { return one.name == tag.name }) {
				closed[at].first = false
			}
			for at := len(stack) - 1; at >= 0; at-- {
				if stack[at].name == tag.name {
					under := -1
					for after := at + 1; after < len(stack); after++ {
						if !stack[after].over {
							stack[after].over, stack[after].by, stack[after].in, stack[after].under = true, tag.name, stack[at].event, under
						}
						if under < 0 && !tags[stack[after].event].text {
							under = stack[after].event
						}
						stack[after].byText = stack[after].byText || tag.text && (tag.name == "ul" || tag.name == "ol")
					}
					if tag.name == "li" {
						for after := at + 1; after < len(stack); after++ {
							stack[after].first = stack[at].byText && stack[after].by == "li"
						}
						closed, stack = append(closed, stack[at+1:]...), stack[:at]
					} else {
						stack = slices.Delete(stack, at, at+1)
					}
					break
				}
			}
		}
	}
	stack = append(stack, closed...)
	slices.SortStableFunc(stack, func(left, right opened) int { return left.event - right.event })
	out := make([]notionMarkdownBlockTagOpen, 0, len(stack))
	for _, one := range stack {
		// A list's tag standing in text that a closing tag closes over is
		// said among its text's inline tags alone (notionMarkdownVersion2Unclosed).
		if one.over && tags[one.event].text && tags[one.event].name == "li" {
			continue
		}
		one.flow = one.name == "summary" || one.over && (slices.Contains(notionMarkdownFlowParents[one.name], one.by) ||
			one.by == "ul" || one.by == "ol" || one.by == "li")
		// A bare column's closing tag closes what stands in it as flow tags,
		// one standing in text said nothing of (notionMarkdownVersion2FramedTrailing).
		if one.over && one.in >= 0 && tags[one.in].bare && one.under < 0 {
			if tags[one.event].text {
				continue
			}
			one.flow = true
		}
		// A block's tag standing in an item's text closes with it, said
		// nothing of, but among the tags left open where a list's closing
		// tag in text closed that item over first (seen 2026-11-15:
		// "<li>a\</li>⏎w <p>y </li>⏎<li>c</li>" says both bare alone,
		// "<ul>⏎<li>a\</li>⏎</ul> x⏎w <p>y </li>⏎z" "Trailing …: <p>." and a's bare).
		if one.over && one.by == "li" && tags[one.event].words && tags[one.event].name != "li" {
			if !one.first {
				continue
			}
			one.flow = false
		}
		out = append(out, one.notionMarkdownBlockTagOpen)
	}
	return out
}

// notionMarkdownVersion2SummaryOpening is a <summary> a line's text starts
// with, and notionMarkdownVersion2DetailsSummary one a <details> before it
// on its line holds.
var (
	notionMarkdownVersion2SummaryOpening = regexp.MustCompile(`^<summary(?:[ \t/>]|$)`)
	notionMarkdownVersion2DetailsSummary = regexp.MustCompile(`^<details(?:[ \t][^<>]*)?>[ \t]*<summary(?:[ \t/>]|$)`)
)

// notionMarkdownVersion2SummaryStarts tells a line a <summary> starts, past
// the > of its quotes, or one its <details> holds.
func notionMarkdownVersion2SummaryStarts(line string) bool {
	text := strings.TrimLeft(line, " \t>")
	return notionMarkdownVersion2SummaryOpening.MatchString(text) || notionMarkdownVersion2DetailsSummary.MatchString(text)
}

// summary is a line as Notion's "v2" reads a <summary> starting it, and out
// with the <details> before it as Leafen reads a toggle; titled is the line
// in out of the <details> a <summary> may be the title of, or -1. A
// <summary> starting a line is the title of the <details> whose first line
// it is, blank lines between them aside; anywhere else it is its words,
// going on with the text around it, and says so (seen 2026-11-14:
// "<summary>a</summary>⏎b" and "x⏎<summary>a</summary>" are one paragraph,
// "> q⏎<summary>a</summary>" one quote, "<details>⏎<summary>s</summary>⏎
// <summary>a</summary>⏎b⏎</details>" the toggle "s" holding the paragraph
// "<summary>a</summary>⏎b", "<details>⏎⏎<summary>a</summary>" the toggle
// "a"). A <details> whose first line is no <summary> is untitled and holds
// its lines ("<details>⏎x⏎<summary>a</summary>⏎</details>" holds the
// paragraph "x⏎<summary>a</summary>"), which Leafen's reading takes for a
// toggle's only after its title (notionMarkdownToggleBlocks).
func (repair *notionMarkdownRepair) summary(out []string, titled int, line, trimmed string) ([]string, string) {
	if trimmed == "" {
		return out, line
	}
	text := strings.TrimLeft(line, " \t>")
	opens := notionMarkdownVersion2SummaryOpening.MatchString(text)
	switch {
	case titled >= 0 && opens:
		// Its title stands next to its tag, as Leafen reads one.
		return out[:titled+1], line
	case titled >= 0:
		lead := out[titled][:len(out[titled])-len(strings.TrimLeft(out[titled], " \t"))]
		out[titled] += "\n" + lead + "<summary></summary>"
	}
	if opens {
		repair.say("unsupported_tag", "summary", notionMarkdownUnsupportedSaid("summary"))
		at := len(line) - len(text)
		line = line[:at] + `\` + line[at:]
	}
	return out, line
}

// unsaid tells a warning Notion leaves out: a <summary> read as its words
// says nothing once one is read as a toggle's title, before it or after it
// (seen 2026-11-14: "<summary>a</summary>⏎<details>⏎<summary>s</summary>…"
// says only that its <details> and <summary> were converted).
func (repair *notionMarkdownRepair) unsaid(warning notionMarkdownWarning) bool {
	return repair.said["deprecated_tagsummary"] && warning.Code == "unsupported_tag" &&
		warning.Message == notionMarkdownUnsupportedSaid("summary")
}

// summaryWords tells a <summary> standing in a line's text that is its words,
// and says so: one not starting its line, or a </summary> closing none
// (repair.summary; seen 2026-11-14: "<p>x⏎<summary>a</summary></p>" is
// "x⏎<summary>a</summary>", "x⏎</summary>" "x⏎</summary>").
func (repair *notionMarkdownRepair) summaryWords(name string, closing bool, line string) bool {
	if name != "summary" || notionMarkdownVersion2SummaryStarts(line) || closing && slices.Contains(repair.openTags(), "summary") {
		return false
	}
	repair.say("unsupported_tag", name, notionMarkdownUnsupportedSaid(name))
	return true
}
