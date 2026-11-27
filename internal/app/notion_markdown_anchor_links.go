package app

import (
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

// Notion's "v2" reads an HTML link, <a href="…">text</a>, as a mark over
// its text, as a <span> marks its own (seen 2026-11-15, create and replace
// alike): the text is read as any text is, one line or over a block's lines,
// and what it holds that links on its own keeps its own address — a
// [text](address) link and an <address> between angle brackets — while the
// rest links the href ("x <a href=a>p [y](b) q</a>" is "p " to a, "y" to b
// and " q" to a); an address standing bare in it is no link of its own. An
// HTML link inside another, or inside a Markdown link's text, links nothing
// of its own: its tags go and its text is the outer link's ("<a href=a>y <a
// href=b>z</a></a>" is "y z" to a, "[<a href=a>y</a>](b)" "y" to b). The
// first href of the tag is the one read, its tabs and line breaks left out
// and its end spaces trimmed, as a browser reads one; a web address's
// scheme and host are put in small letters and its host's other letters in
// punycode ("https://BÜCHER.de/" links https://xn--bcher-kva.de/), its host
// read past the slashes after its scheme, either way ("http:a.com"), and
// the characters an address can't hold bare are percent-encoded ("a b" is
// "a%20b"). An href Notion's links refuse is refused before the text is read
// (notionMarkdownVersion2HrefRefused). One Leafen's links don't take
// (notion://, slack://, "./x", "?q", "x y", a web address no
// browser opens, "https://a b.com") is its Markdown link's words,
// "[text](href)", no part of the address a link.

// notionMarkdownHTMLLinkOpening is an HTML link's opening tag, its
// attributes as the tags of a "v2" line are read.
var notionMarkdownHTMLLinkOpening = regexp.MustCompile(`<a((?:[ \t]+` + notionMarkdownAttributeName + `(?:="[^"]*")?)*)[ \t]*>`)

// notionMarkdownAnchor reads an HTML link opening at value[index:]: its
// first href, as its tag holds it, where its text starts and ends, and where
// its closing tag — past the links opened inside it — ends.
func notionMarkdownAnchor(value string, index int) (string, int, int, int, bool) {
	match := notionMarkdownHTMLLinkOpening.FindStringSubmatchIndex(value[index:])
	if match == nil || match[0] != 0 {
		return "", 0, 0, 0, false
	}
	href, named := notionMarkdownFirstAttribute(value[index+match[2]:index+match[3]], "href")
	if !named {
		return "", 0, 0, 0, false
	}
	start, depth := index+match[1], 1
	for at := start; at < len(value); at++ {
		switch {
		case value[at] == '\\':
			at++
		case value[at] == '`':
			_, end, _ := notionMarkdownCodeSpanAt(value, at)
			at = end - 1
		case strings.HasPrefix(value[at:], "</a>"):
			if depth--; depth == 0 {
				return href, start, at, at + len("</a>"), true
			}
		case value[at] == '<':
			if inner := notionMarkdownHTMLLinkOpening.FindStringIndex(value[at:]); inner != nil && inner[0] == 0 {
				depth++
			}
		}
	}
	return "", 0, 0, 0, false
}

// anchor puts an HTML link's text, with the style around it, linked to its
// href as notionMarkdownAnchor reads them.
func (parser *workspaceImportInlineParser) anchor(href, text string, style workspaceImportInlineStyle, depth int) {
	if style.link != "" {
		parser.parse(text, style, depth+1)
		return
	}
	address := notionMarkdownAnchorAddress(notionMarkdownVersion2AttributeValue(href))
	if target := parser.linkTarget(address); target != "" {
		style.link = target
		parser.parse(text, style, depth+1)
		return
	}
	parser.unlinked("[", style)
	parser.parse(text, style, depth+1)
	parser.unlinked("]("+notionMarkdownVersion2AttributeValue(href)+")", style)
}

// unlinked puts words no address standing in them links.
func (parser *workspaceImportInlineParser) unlinked(words string, style workspaceImportInlineStyle) {
	if parser.barriers == nil {
		parser.barriers = map[int]bool{}
	}
	for at := range len(words) {
		parser.barriers[parser.plain+at] = true
	}
	parser.emit(words, style)
}

// notionMarkdownAnchorAddress is an href as a link keeps it
// (notionMarkdownAnchor's): without tabs and line breaks or end spaces, a
// web or mail address's scheme and a web address's host in small letters,
// the host's other letters in punycode, and percent-encoded but for the
// characters an address holds bare.
func notionMarkdownAnchorAddress(href string) string {
	address := strings.TrimFunc(strings.Map(func(character rune) rune {
		if character == '\t' || character == '\n' || character == '\r' {
			return -1
		}
		return character
	}, href), func(character rune) bool { return character <= ' ' })
	scheme := ""
	if found := notionMarkdownVersion2HrefScheme.FindStringSubmatch(address); found != nil {
		scheme = strings.ToLower(found[1])
		address = scheme + address[len(found[1]):]
	}
	if scheme == "http" || scheme == "https" {
		// Its host starts past the slashes, either way, after its scheme
		// ("http:a.com" and "http:\\a.com" link http://a.com/).
		rest := strings.TrimLeft(address[len(scheme)+1:], `/\`)
		scheme += "://"
		end := strings.IndexAny(rest+"/", "/?#")
		authority := rest[:end]
		at := strings.LastIndexByte(authority, '@') + 1
		host := strings.ToLower(authority[at:])
		port := notionLinkPortPattern.FindString(host)
		if name := host[:len(host)-len(port)]; name != "" && !strings.HasPrefix(name, "[") {
			if ascii, err := idna.Punycode.ToASCII(name); err == nil {
				host = ascii + port
			}
		}
		address = scheme + authority[:at] + host + rest[end:]
	}
	return notionMarkdownEncodedAddress(address, ";/?:@&=+$,-_.!~*'()#[]", true)
}

// notionMarkdownFirstAttribute is the value an attribute is first given in
// a tag's attributes, which a reading keeps of one given twice (seen
// 2026-11-15: <a href="https://…" href="javascript:x"> links the first).
func notionMarkdownFirstAttribute(attributes, name string) (string, bool) {
	for _, match := range notionMarkdownAttributeAt.FindAllStringSubmatch(attributes, -1) {
		if match[1] == name {
			return match[2], true
		}
	}
	return "", false
}
