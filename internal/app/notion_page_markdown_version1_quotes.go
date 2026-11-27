package app

import "strings"

// A public API write's "v1" Markdown reads a tag's attribute values in
// single quotes, or in none, as it reads them in double quotes (seen
// 2026-11-11, each written with replace_content and read back as blocks):
// <mention-date start=2026-01-01/> is a date, <span color='red'> red text,
// <callout icon=💡 color=blue_bg> a callout, <td color=blue> a cell, and so
// for every tag "v1" reads. Spaces around = are nothing, an attribute without
// a value is left out (<span color=red underline> is red), and a / right
// before the tag's > closes the tag, not the value ("timeZone=Asia/Seoul/>"
// is Asia/Seoul). Each name reads from its first letter, one that "v1" takes
// nothing from left out (notionMarkdownVersion1AttributeName: "<callout
// _color=\"red_bg\">" is red, "<callout x.y=\"1\">" a callout). Such a tag
// is put with its values in double quotes and its names so before
// the rest of the reading takes it. A tag "v1" keeps as text is as written:
// one it doesn't know (<foo a=b>), a date mention that isn't closed in
// itself, a file's or a link's tag that isn't its whole line with its caption
// between its tags, a <link-preview> without an address, one whose /
// stands apart from its > ("/ >"), and an <unknown> that isn't closed in
// itself, which "v1" keeps as its words (seen 2026-11-15: "<unknown
// url='https://…'>x" is the paragraph of those words, its address linked
// from where it starts to the line's end). An update names each new
// <unknown> by the url in double quotes it holds before any of this
// (notionMarkdownUpdateNewUnknown).

// notionMarkdownVersion1BlockTags are the tags "v1" reads at the start of a
// line, as a block or a piece of one; notionMarkdownVersion1CaptionTags those
// it reads only as a whole line with their caption between their tags.
var (
	notionMarkdownVersion1BlockTags = map[string]bool{
		"callout": true, "details": true, "summary": true, "table": true, "tr": true, "td": true, "th": true,
		"col": true, "colgroup": true, "thead": true, "tbody": true, "table_of_contents": true,
		"synced_block": true, "synced_block_reference": true, "page": true, "database": true,
		"columns": true, "column": true, "tabs": true, "tab": true, "unknown": true, "empty-block": true,
	}
	notionMarkdownVersion1CaptionTags = map[string]bool{
		"image": true, "file": true, "pdf": true, "video": true, "audio": true, "embed": true, "bookmark": true,
	}
)

// notionMarkdownVersion1QuotedTag is a tag opening a text as "v1" reads it:
// its name, the tag with its values in double quotes, how long it stood,
// whether it closes in itself and whether it stood so already.
type notionMarkdownVersion1QuotedTag struct {
	name, written string
	size          int
	closed, same  bool
}

// notionMarkdownVersion1ReadTag reads the tag text starts with; false for
// text that doesn't start with one.
func notionMarkdownVersion1ReadTag(text string) (notionMarkdownVersion1QuotedTag, bool) {
	tag := notionMarkdownVersion1QuotedTag{same: true}
	nameChar := func(character byte, first bool) bool {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		return letter || !first && (character >= '0' && character <= '9' || character == '-' || character == '_')
	}
	at := 1
	for at < len(text) && nameChar(text[at], at == 1) {
		at++
	}
	if len(text) < 2 || text[0] != '<' || at == 1 {
		return tag, false
	}
	tag.name = text[1:at]
	var written strings.Builder
	written.WriteString(text[:at])
	space := func() int {
		start := at
		for at < len(text) && (text[at] == ' ' || text[at] == '\t') {
			at++
		}
		return at - start
	}
	for {
		spaced := space()
		switch {
		case at >= len(text):
			return tag, false
		case text[at] == '>':
			tag.written, tag.size = written.String()+">", at+1
			return tag, true
		case strings.HasPrefix(text[at:], "/>"):
			tag.written, tag.size, tag.closed = written.String()+"/>", at+2, true
			return tag, true
		case spaced == 0:
			return tag, false
		}
		// A name is what stands up to its =, read as "v1" reads it
		// (notionMarkdownVersion1AttributeName).
		start := at
		for at < len(text) && !strings.ContainsRune(" \t=\"'<>/`", rune(text[at])) {
			at++
		}
		if at == start {
			return tag, false
		}
		key, kept := notionMarkdownVersion1AttributeName(tag.name, text[start:at])
		if !kept || key != text[start:at] {
			tag.same = false
		}
		before := at
		spaced = space()
		if at >= len(text) || text[at] != '=' {
			// An attribute without a value is left out.
			at, tag.same = before, false
			continue
		}
		at++
		if spaced+space() > 0 {
			tag.same = false
		}
		if at >= len(text) {
			return tag, false
		}
		value := ""
		switch quote := text[at]; quote {
		case '"', '\'':
			end := strings.IndexByte(text[at+1:], quote)
			if end < 0 {
				return tag, false
			}
			value = text[at+1 : at+1+end]
			at += end + 2
			if quote == '\'' {
				value, tag.same = strings.ReplaceAll(value, `"`, "&quot;"), false
			}
		default:
			begin := at
			for at < len(text) && !strings.ContainsRune(" \t\"'=<>`", rune(text[at])) {
				at++
			}
			value, tag.same = text[begin:at], false
			// A / right before > closes the tag.
			if at < len(text) && text[at] == '>' && strings.HasSuffix(value, "/") {
				value, at = value[:len(value)-1], at-1
			}
			if value == "" {
				return tag, false
			}
		}
		if kept {
			written.WriteString(" " + key + `="` + value + `"`)
		}
	}
}

// notionMarkdownVersion1BlockQuotes puts the values of the tag each line
// starts with in double quotes, as "v1" reads them, outside code and
// equations, whose lines stand as written.
func notionMarkdownVersion1BlockQuotes(lines []string) []string {
	fence, equation := "", false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		_, rest := notionMarkdownVersion1Items(strings.TrimLeft(line, " \t"))
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, false) {
				fence = ""
			}
		case equation:
			equation = !notionMarkdownVersion1EquationCloses(trimmed)
		case strings.HasPrefix(rest, "```"):
			fence = notionMarkdownFenceOf(rest)
		case strings.TrimSpace(rest) == "$$":
			equation = true
		case strings.HasPrefix(trimmed, "<"):
			tag, read := notionMarkdownVersion1ReadTag(trimmed)
			whole := notionMarkdownVersion1CaptionTags[tag.name] && !tag.closed &&
				strings.HasSuffix(trimmed, "</"+tag.name+">")
			// A tag holding blocks whose value holds a > is text as written
			// (notionMarkdownVersion1OpeningTag).
			text := read && notionMarkdownVersion1ContainerTags[tag.name] && strings.Contains(strings.TrimSuffix(tag.written, ">"), ">")
			text = text || tag.name == "unknown" && !tag.closed
			if read && !tag.same && !text && (notionMarkdownVersion1BlockTags[tag.name] || whole) {
				lines[index] = line[:len(line)-len(strings.TrimLeft(line, " \t"))] + tag.written + trimmed[tag.size:]
			}
		}
	}
	return lines
}

// notionMarkdownVersion1Quotes is Markdown with the tags its lines start
// with put so (notionMarkdownVersion1BlockQuotes).
func notionMarkdownVersion1Quotes(markdown string) string {
	return strings.Join(notionMarkdownVersion1BlockQuotes(strings.Split(markdown, "\n")), "\n")
}

// notionMarkdownVersion1InlineQuotes is a mention's or a span's tag opening
// text with its values in double quotes, and how long it stood; none for a
// tag "v1" reads as written or keeps as text.
func notionMarkdownVersion1InlineQuotes(text string) (string, int) {
	tag, read := notionMarkdownVersion1ReadTag(text)
	if !read || tag.same {
		return "", 0
	}
	switch tag.name {
	case "mention-page", "mention-database", "mention-data-source", "mention-user":
		if !tag.closed && !strings.Contains(text[tag.size:], "</"+tag.name+">") {
			return "", 0
		}
	case "mention-date":
		if !tag.closed {
			return "", 0
		}
	case "link-preview":
		if !tag.closed || notionMarkdownAttributes(tag.written)["url"] == "" {
			return "", 0
		}
	case "span":
		if tag.closed {
			return "", 0
		}
	default:
		return "", 0
	}
	return tag.written, tag.size
}

// notionMarkdownVersion1InlineQuoted reports whether text opens a tag
// notionMarkdownVersion1InlineQuotes puts otherwise.
func notionMarkdownVersion1InlineQuoted(text string) bool {
	_, size := notionMarkdownVersion1InlineQuotes(text)
	return size > 0
}
