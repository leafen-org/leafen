package app

import (
	"regexp"
	"slices"
	"strings"
)

// A replace_content's or insert_content's Markdown, "v1" and "v2" alike,
// links no web address whose host holds a >, or a port that is no number
// (notionMarkdownHostPortWords) — what stands after its "://"
// up to its path (notionMarkdownAddressHost), past the marks its run of
// words ends in — where its create-page links it (seen 2026-11-14 and 2026-11-15, in a
// paragraph, a quote, an item, a heading, a to-do, a callout, a toggle and
// a <details> title alike): "<unknown url=\"https://example.com\">x",
// "https://a.com>b", "https://a.com\>b", "x(https://a.com>b)" and, in "v2",
// "https://a.com&gt;b" are text as written, "…\">see https://b.com" links
// the second address alone. A > past the host, "https://a.com/x>y" and
// "https://a.com/>b", or ending the run, "https://a.com>", "…\">" and
// "…\"/>", links as in create-page, and so does a mail address, an address
// in a table's cell and one in code or an equation. Its host ends where its
// path, query or fragment starts ("https://a.com?x>y" and
// "https://a.com#x>y" link); the words after it up to the next space are its text,
// addresses and all ("https://a.com>https://c.com" links neither; seen
// 2026-11-15). The tags the Markdown reads are read before them, so the
// address their attributes hold is none of them — in "v2" an HTML link's
// ("<a href=\"https://b.com\">w</a>" links w, "https://a.com>b<a
// href=…>w</a>" w alone), and a mention's ("x <mention-page
// url=\"https://a.com\">y</mention-page>" links y, "v1" too; seen
// 2026-11-15) — and so is an address between angle brackets in "v2"
// ("<https://b.com>y" and "<b><https://b.com></b>" link it; "v1" reads
// none, so they are text there); a tag neither reads is text, its
// addresses so read ("<mention-link url=\"https://a.com\">y</mention-link>"
// is text as written).

// notionMarkdownUpdateUnlinkedAddresses escapes the colon and the dots of
// each web address so read in an update's Markdown, outside code, equations
// and tables; version2 reads an entity of > as one.
func notionMarkdownUpdateUnlinkedAddresses(markdown string, version2 bool) string {
	if !strings.Contains(markdown, "://") {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	fence, equation, table := "", false, false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		cell := strings.TrimLeft(trimmed, " \t>")
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(strings.TrimLeft(trimmed, " \t>"), fence, version2) {
				fence = ""
			}
			continue
		case equation:
			equation = cell != "$$"
			continue
		case strings.HasPrefix(cell, "```"):
			fence = notionMarkdownFenceOf(cell)
			continue
		case cell == "$$":
			equation = true
			continue
		case strings.HasPrefix(cell, "<table"):
			table = true
		}
		if table || strings.HasPrefix(cell, "|") {
			table = table && !strings.Contains(cell, "</table>")
			continue
		}
		lines[index] = notionMarkdownUnlinkedLine(line, version2)
	}
	return strings.Join(lines, "\n")
}

// notionMarkdownUnlinkedLine is a line with each web address whose host
// holds a > escaped so that it is no link (notionMarkdownUpdateUnlinkedAddresses).
func notionMarkdownUnlinkedLine(line string, version2 bool) string {
	codes := notionMarkdownInlineCodeRanges(line)
	tags := notionMarkdownUpdateVersion1Tag.FindAllStringIndex(line, -1)
	if version2 {
		tags = slices.Concat(tags, notionMarkdownUpdateVersion2Tag.FindAllStringIndex(line, -1),
			notionMarkdownHTMLLinkOpening.FindAllStringIndex(line, -1), notionMarkdownUpdateAutolink.FindAllStringIndex(line, -1))
	}
	var out strings.Builder
	at := 0
	for from := 0; ; {
		found := strings.Index(line[from:], "://")
		if found < 0 {
			break
		}
		colon := from + found
		if tag := slices.IndexFunc(tags, func(tag []int) bool { return tag[0] < colon && colon < tag[1] }); tag >= 0 {
			from = tags[tag][1]
			continue
		}
		start := colon
		for start > 0 && (line[start-1] >= 'a' && line[start-1] <= 'z' || line[start-1] >= 'A' && line[start-1] <= 'Z') {
			start--
		}
		end := colon + len("://")
		for end < len(line) && line[end] != ' ' && line[end] != '\t' {
			end++
		}
		from = end
		// Marks and tags its version reads closing around it end its run.
		if closed := notionMarkdownUpdateRunEnd(line[colon+len("://"):end], version2); closed >= 0 {
			end = colon + len("://") + closed
		}
		// The marks ending its run are none of its.
		for end > colon+len("://") && strings.ContainsRune(`">.,;:!?'`, rune(line[end-1])) {
			end--
		}
		host := notionMarkdownAddressHost(line[colon+len("://"):end], version2)
		scheme := strings.ToLower(line[start:colon])
		if scheme != "http" && scheme != "https" || !notionMarkdownHostHoldsGreater(host, version2) && !notionMarkdownHostPortWords(host) ||
			notionMarkdownEscapedAt(line, start) || notionMarkdownVersion2InCode(line, start, false) && len(codes) > 0 {
			continue
		}
		out.WriteString(line[at:colon])
		out.WriteString(`\:`)
		out.WriteString(notionMarkdownUnlinkedRun.Replace(line[colon+1 : end]))
		at = end
	}
	out.WriteString(line[at:])
	return out.String()
}

var (
	// notionMarkdownUpdateVersion1Tag is a mention's tag, as "v1" names
	// them, and notionMarkdownUpdateVersion2Tag one "v2" alone reads, which
	// an update reads before the addresses standing bare in its text ("v1"
	// keeps "<mention url=…>y</mention>" as text, unlinked).
	notionMarkdownUpdateVersion1Tag = regexp.MustCompile(`<mention-(?:page|user|database|data-source|date)(?:[ \t][^<>]*)?>`)
	notionMarkdownUpdateVersion2Tag = regexp.MustCompile(`<(?:mention|link-preview)(?:[ \t][^<>]*)?>`)
	// notionMarkdownUpdateAutolink is an address between angle brackets, as
	// CommonMark reads one: a scheme, then no space, < or >; "v2" reads it.
	notionMarkdownUpdateAutolink = regexp.MustCompile(`<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\x00-\x20]*>`)
)

// notionMarkdownUpdateRunEnd is where marks or a tag closing around an
// address end the words after its "://" (rest), as Notion's update reads
// them, its create alike (seen 2026-11-15, "v1" and "v2"): bold's, italic's
// and strikethrough's marks ("x **<https://b.com>**", "x
// *https://b.com>*", "x __<https://b.com>__" link https://b.com, "x
// **<https://b.com>y**" is text), and the closing tag of a <span> — in
// "v2" of any tag it reads in text — ("x <span color=\"red\"><https://b.com>
// </span>" links it; "v1"'s "x <u><https://b.com></u>" is text, "v1"
// reading no <u>); -1 for none.
func notionMarkdownUpdateRunEnd(rest string, version2 bool) int {
	end := -1
	for _, mark := range []string{"*", "~~", "__"} {
		if at := strings.Index(rest, mark); at >= 0 && (end < 0 || at < end) {
			end = at
		}
	}
	for _, found := range notionMarkdownUpdateClosingTag.FindAllStringSubmatchIndex(rest, -1) {
		if name := rest[found[2]:found[3]]; (name == "span" || version2 && notionMarkdownVersion2InlineTags[name]) && (end < 0 || found[0] < end) {
			end = found[0]
			break
		}
	}
	return end
}

// notionMarkdownUpdateClosingTag is a tag's closing tag.
var notionMarkdownUpdateClosingTag = regexp.MustCompile(`</([a-z][a-z0-9-]*)>`)

// notionMarkdownAddressHost is the host an address's words after its "://"
// name, up to its path, query or fragment; "v2" reads a backslash before
// punctuation as escaping it, and another as ending the host (seen
// 2026-11-15: "https://a.com\>b" is text, "https://a.com\x>y" links).
func notionMarkdownAddressHost(rest string, version2 bool) string {
	var host strings.Builder
	for index := 0; index < len(rest); index++ {
		character := rest[index]
		if version2 && character == '\\' {
			if index+1 >= len(rest) || !notionMarkdownASCIIPunctuation(rest[index+1]) {
				break
			}
			index++
			character = rest[index]
		}
		if strings.IndexByte("/?#", character) >= 0 {
			break
		}
		host.WriteByte(character)
	}
	return host.String()
}

// notionMarkdownUnlinkedRun escapes the dots and the addresses' colons of
// the words an unlinked address runs to.
var notionMarkdownUnlinkedRun = strings.NewReplacer(".", `\.`, "://", `\://`)

// notionMarkdownHostHoldsGreater tells an address's host that holds a >, in
// "v2" an entity of one too.
func notionMarkdownHostHoldsGreater(host string, version2 bool) bool {
	if strings.Contains(host, ">") {
		return true
	}
	lower := strings.ToLower(host)
	return version2 && (strings.Contains(lower, "&gt;") || strings.Contains(lower, "&#62;") || strings.Contains(lower, "&#x3e;"))
}

// notionMarkdownHostPortWords tells a host whose port is no number
// ("a.com:8o"), which an update leaves unlinked as one holding a > (seen
// 2026-11-15: "http://a.com:8o/x" is text where its create links it).
func notionMarkdownHostPortWords(host string) bool {
	host = host[strings.LastIndexByte(host, '@')+1:]
	if close := strings.LastIndexByte(host, ']'); close >= 0 {
		host = host[close+1:]
	}
	_, port, ported := strings.Cut(host, ":")
	return ported && strings.Trim(port, "0123456789") != ""
}

// notionMarkdownLinkedAddress is an address as written before
// notionMarkdownUpdateUnlinkedAddresses escaped it.
func notionMarkdownLinkedAddress(address string) string {
	return strings.NewReplacer(`\:`, ":", `\.`, ".").Replace(address)
}
