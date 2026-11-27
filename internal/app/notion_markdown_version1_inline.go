package app

import (
	"strings"
	"unicode"
)

// The public API's "v1" Markdown pairs emphasis its own way, not as
// CommonMark (seen 2026-11-10, each line written with replace_content and
// read back as blocks). A mark is read where it stands, left to right, as
// code, an equation, a link and an escape are, and pairs with the first mark
// of its kind after it, whatever stands between — an escaped mark, code, a
// link's text or address, or a mark that would open: "*a \*b*" is an italic
// "a \" and "b*", "*a [l*](…)" an italic "a [l", and "*a *b* c*" an italic
// "a ", a plain "b" and an italic " c". What it holds is read the same way
// again, its marks added to the ones around it.
//
//   - *** pairs with the next ***, ** with the next **, both around some
//     text, and * with the next *, around nothing too: "**a* b" is "a* b",
//     the ** an italic of nothing. A * pairs across spaces: "a * b * c" is
//     an italic " b ".
//   - _, __ and ___ open where no letter or digit stands before them, and
//     close where something other than a space stands before them and no
//     letter or digit after them: "_a_b_" is an italic "a_b", "a_b_c" text.
//     A lone _ opens only before something other than a space or another _.
//     Two of a run that pair with nothing are text, and the rest of the run
//     is read after them: "__a_ b" is text, "___a_ b" a "__" and an italic
//     "a".
//   - ~~ pairs with the next ~~, around nothing too; a lone ~ is text.

// notionMarkdownVersion1Emphasis reads the emphasis a run of *, _ or ~
// opening at value[index:] starts, as "v1" pairs it: the style inside it,
// where the text it holds starts and ends, and where reading goes on. When
// it pairs with nothing, next is where the text it is ends.
func notionMarkdownVersion1Emphasis(value string, index int, style workspaceImportInlineStyle) (
	inner workspaceImportInlineStyle, textStart, textEnd, next int, ok bool,
) {
	mark := value[index]
	run := len(value[index:]) - len(strings.TrimLeft(value[index:], string(mark)))
	text := index + min(run, 2)
	switch {
	case mark == '~':
		text = index + run
	case mark == '_' && workspaceImportWordByte(value, index, true):
		return style, 0, 0, index + run, false
	}
	// pair finds the first delimiter at or after from that may close.
	pair := func(delimiter string, from int) int {
		for at := from; at <= len(value)-len(delimiter); at++ {
			if !strings.HasPrefix(value[at:], delimiter) {
				continue
			}
			if mark != '_' || !workspaceImportSpaceAt(value, at-1) && !workspaceImportWordByte(value, at+len(delimiter), false) {
				return at
			}
		}
		return -1
	}
	inner = style
	switch mark {
	case '~':
		if run < 2 {
			return style, 0, 0, text, false
		}
		if end := pair("~~", index+2); end >= 0 {
			inner.strikethrough = true
			return inner, index + 2, end, end + 2, true
		}
		return style, 0, 0, text, false
	case '*', '_':
		if run >= 3 {
			if end := pair(strings.Repeat(string(mark), 3), index+4); end >= 0 {
				inner.bold, inner.italic = true, true
				return inner, index + 3, end, end + 3, true
			}
		}
		if run >= 2 {
			if end := pair(strings.Repeat(string(mark), 2), index+3); end >= 0 {
				inner.bold = true
				return inner, index + 2, end, end + 2, true
			}
		}
		if mark == '_' && (run > 1 || workspaceImportSpaceAt(value, index+1)) {
			return style, 0, 0, text, false
		}
		from := index + 1
		if mark == '_' {
			from++
		}
		if end := pair(string(mark), from); end >= 0 {
			inner.italic = true
			return inner, index + 1, end, end + 1, true
		}
	}
	return style, 0, 0, text, false
}

// notionMarkdownVersion1LinkClose is where a "v1" link's address in rest
// ends: at the ) past the ones it opens, but at the first ) when that would
// hold a space (seen 2026-11-10:
// "[a](https://example.com/(b) c) d" links https://example.com/(b, and
// "[a](https://example.com (t)) b" is "a) b").
func notionMarkdownVersion1LinkClose(rest string) int {
	end := workspaceImportLinkClose(rest, true, false)
	if end > 0 && strings.ContainsAny(rest[:end], " \t") {
		return strings.IndexByte(rest, ')')
	}
	return end
}

// version1Link is the link a "v1" link's address makes, as Notion's does
// (seen 2026-11-11): none for one it doesn't link (graphWrittenTextLink),
// but in a table cell a web address whose host a space parts, as written
// (graphWrittenCellTextLink). One written without whitespace links as
// Notion's Markdown links it when it is a web, mail or phone address, a page
// of the workspace, a host under a top-level domain as an https address
// ("example.com", not "host.example"), or any host a browser opens after //
// (two slashes, not backslashes) as an https address ("//a" links
// https://a); any other is the
// address as written, whitespace and angle brackets and all ("a b.com",
// " example.com", "<a b.com>"), which Notion reads back so.
func (parser *workspaceImportInlineParser) version1Link(address string) string {
	if notionMarkdownUserLink(address) {
		return "/"
	}
	// A data source's or a view's address by Leafen's own ID is as written,
	// for the write to point here (notion_address_own_sources_here.go).
	if target, source := parser.sitePages.sourceLinkTarget(address); source {
		return target
	}
	// One of Notion's web addresses naming a page is kept as a write keeps
	// it, whitespace about it or not (seen 2026-11-11:
	// "[a]( https://www.notion.so/ID )"; notionMarkdownWrittenAddress).
	if trimmed := strings.TrimSpace(address); !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		if kept, named := notionMarkdownWrittenAddress(trimmed); named {
			return kept
		}
		// The app's own address of a page here (leafen://page/ID) is its
		// path in the app, as "v2" reads it (notionMarkdownPageLinkTarget).
		if target := notionMarkdownPageLinkTarget(trimmed, false); target != "" {
			return target
		}
	}
	if !graphWrittenTextLink(address) {
		if parser.cell && graphWrittenCellTextLink(address) {
			return address
		}
		return ""
	}
	// A page's address with whitespace after it, none before, names the
	// page (seen 2026-11-11: "[a](/ID )", "[a](/x/ID )"; "[a]( /ID)" is as
	// written).
	if trimmed := strings.TrimRightFunc(address, unicode.IsSpace); trimmed != address &&
		!strings.ContainsFunc(trimmed, unicode.IsSpace) {
		if target := notionMarkdownPageLinkTarget(trimmed, false); target != "" {
			return target
		}
	}
	if !strings.ContainsFunc(address, unicode.IsSpace) {
		if target := workspaceImportLinkTarget(address); target != "" {
			return target
		}
		if target := notionMarkdownPageLinkTarget(address, false); target != "" {
			return target
		}
		if strings.HasPrefix(address, "//") {
			return "https:" + address
		}
		if graphSchemelessWebLink(address) {
			if target := notionMarkdownSchemelessLinkTarget(address); strings.HasPrefix(target, "https://") {
				return target
			}
		}
	}
	return address
}
