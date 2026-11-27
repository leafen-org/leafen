package app

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text written to a page as Notion's Markdown links the addresses it holds
// bare, as Notion's API reads both its versions (seen 2026-11-10), once its
// marks are read and outside code and links: a web or mail address with its
// scheme (http://, https://, mailto:, any case) after a space, a bracket or
// a quote, to the next space; a domain whose last name is a real top-level
// domain, as a JSON write's link takes one (graphTopLevelDomain: "example.com",
// "www.example.com/x", "readme.md", "a.zip", "예시.한국", not "node.js",
// "a.onion" or "a.xn--p1ai"; seen 2026-11-11), with its port and path, as an
// http:// address; localhost with a port (alone it stays words, which
// Leafen's Markdown could not write unlinked; Notion links it); an
// email address, as a mailto: address; and a person's user:// address with
// their ID, as their mention (notionMarkdownBareUser). An address ends before the
// punctuation that closes its sentence (. , : ; ! ? and quotes) and before a
// closing bracket that closes none it opens (notionMarkdownLinkCloses). An
// address of any other scheme stays text, all of it (no part of it a
// link): an ftp:// or ftps:// one a replace_content or insert_content keeps
// so, as Notion's does, and a create where Notion links it keeps the words
// written, and so does one whose host Notion links alone ("sftp://a.com",
// "file://a.com/x", "notion://a.com"; seen 2026-11-15). In "v2" and the MCP's Markdown its run is the text of one style,
// across the marks that leave it so (linkBare).
//
// An escaped colon or full stop ("\:", "\.") stands between two runs of
// text, as the API's "v1" writes an address in unlinked text so that it
// reads back unlinked (notionMarkdownUnlinkedText).

// notionMarkdownBareLink is an address standing bare in text, from start to
// end, and the link it makes; at is the character that unmakes it when
// escaped.
type notionMarkdownBareLink struct {
	start, end, at int
	target         string
}

// notionMarkdownLinkRune is a letter or digit, what names and words are
// made of.
func notionMarkdownLinkRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r) || unicode.Is(unicode.M, r)
}

// notionMarkdownBareLinks are the addresses standing bare in text, in order.
func notionMarkdownBareLinks(text string) []notionMarkdownBareLink {
	links := []notionMarkdownBareLink{}
	previous := rune(-1)
	for index := 0; index < len(text); {
		r, size := utf8.DecodeRuneInString(text[index:])
		link, ok := notionMarkdownBareDataSource(text, index)
		if !ok {
			link, ok = notionMarkdownBareView(text, index)
		}
		if ok {
			if link.target != "" {
				links = append(links, link)
			}
			previous, _ = utf8.DecodeLastRuneInString(text[:link.end])
			index = link.end
			continue
		}
		if notionMarkdownLinkRune(r) && (previous < 0 || !notionMarkdownLinkRune(previous)) {
			if link, ok := notionMarkdownBareLinkAt(text, index, previous); ok {
				if link.target != "" {
					links = append(links, link)
				}
				previous, _ = utf8.DecodeLastRuneInString(text[:link.end])
				index = link.end
				continue
			}
		}
		previous = r
		index += size
	}
	return links
}

// notionMarkdownBareLinkAt reads the address a word starting at text[start:]
// begins, after the character previous (-1 for none).
func notionMarkdownBareLinkAt(text string, start int, previous rune) (notionMarkdownBareLink, bool) {
	rest := text[start:]
	lower := strings.ToLower(rest[:min(len(rest), len("https://"))])
	// A scheme is read after any character but a colon (seen 2026-11-15:
	// "x-https://a.com", "x/https://a.com" and "1.https://a.com" link the
	// address whole, "x:https://a.com" "a.com" alone).
	if previous != ':' {
		if link, ok := notionMarkdownBareUser(text, start); ok {
			return link, true
		}
		if strings.HasPrefix(lower, "user://") {
			// Any other user:// address is its text, all of it, as Notion's
			// API keeps it ("user://a.com/x", "user://a@b.com"; seen
			// 2026-11-11).
			return notionMarkdownBareLink{start: start, end: notionMarkdownLinkEnd(text, start+len("user://"), start+len("user://"))}, true
		}
		for _, scheme := range []string{"https://", "http://", "mailto:", "ftps://", "ftp://"} {
			if !strings.HasPrefix(lower, scheme) {
				continue
			}
			end := notionMarkdownLinkEnd(text, start+len(scheme), start+len(scheme))
			if end == start+len(scheme) || scheme == "mailto:" && !strings.Contains(text[start:end], "@") {
				return notionMarkdownBareLink{}, false
			}
			if strings.HasPrefix(scheme, "ftp") {
				// Read as an address, which Leafen's links don't take: its
				// text, all of it.
				return notionMarkdownBareLink{start: start, end: end}, true
			}
			target := text[start:end]
			at := start + strings.IndexByte(scheme, ':')
			// One whose host runs into a character no host holds links the
			// address its host ends before, the rest its path, or else is
			// its words, no part of it a link (notionMarkdownBareHostTail).
			if !validGraphTextLink(target) {
				target = notionMarkdownBareHostTail(target, len(scheme))
			}
			return notionMarkdownBareLink{start: start, end: end, at: at, target: target}, true
		}
	}
	// The host of an address of another scheme is its words to the run's
	// end, as the rest of it: Leafen's links take web, mail and phone
	// addresses only, and no part of one is linked (Notion links
	// "a.com" alone in "sftp://a.com", "file://a.com/x", "notion://a.com",
	// "(ssh://a.com)" and "x:git://a.com"; seen 2026-11-15).
	if scheme := notionMarkdownSchemeBefore(text, start); scheme != "" && scheme != "http" && scheme != "https" {
		return notionMarkdownBareLink{start: start, end: notionMarkdownLinkEnd(text, start, start)}, true
	}
	if previous >= 0 && strings.ContainsRune(".-_@+%", previous) {
		return notionMarkdownBareLink{}, false
	}
	// An email address: its name, @ and a domain.
	name := start
	for name < len(text) {
		r, size := utf8.DecodeRuneInString(text[name:])
		if !notionMarkdownLinkRune(r) && !strings.ContainsRune("._%+-", r) {
			break
		}
		name += size
	}
	if name > start && name < len(text) && text[name] == '@' {
		if end, dot, ok := notionMarkdownDomainEnd(text, name+1); ok {
			target := "mailto:" + text[start:end]
			return notionMarkdownBareLink{start: start, end: end, at: dot, target: target}, validGraphTextLink(target)
		}
		return notionMarkdownBareLink{}, false
	}
	if strings.HasPrefix(strings.ToLower(rest), "localhost:") {
		port := start + len("localhost:")
		for port < len(text) && text[port] >= '0' && text[port] <= '9' {
			port++
		}
		if port > start+len("localhost:") {
			end := notionMarkdownLinkPath(text, port)
			target := "http://" + text[start:end]
			return notionMarkdownBareLink{start: start, end: end, at: start + len("localhost"), target: target}, validGraphTextLink(target)
		}
	}
	end, dot, ok := notionMarkdownDomainEnd(text, start)
	if !ok {
		return notionMarkdownBareLink{}, false
	}
	if end < len(text) && text[end] == ':' {
		port := end + 1
		for port < len(text) && text[port] >= '0' && text[port] <= '9' {
			port++
		}
		if port > end+1 {
			end = port
		}
	}
	end = notionMarkdownLinkPath(text, end)
	target := "http://" + text[start:end]
	return notionMarkdownBareLink{start: start, end: end, at: dot, target: target}, validGraphTextLink(target)
}

// notionMarkdownDomainEnd reads a domain at text[start:]: names of letters,
// digits and hyphens joined by full stops, the last a real top-level domain
// and followed by no letter, digit or name character. It gives where the
// domain ends and its last full stop.
func notionMarkdownDomainEnd(text string, start int) (int, int, bool) {
	end, dot, label := start, -1, start
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		switch {
		case notionMarkdownLinkRune(r) || r == '-' && end > label:
		case r == '.' && end > label && end+1 < len(text):
			// A full stop no name follows ends the domain before it.
			if next, _ := utf8.DecodeRuneInString(text[end+1:]); !notionMarkdownLinkRune(next) {
				return notionMarkdownDomainFound(text, start, end, dot)
			}
			dot, label = end, end+1
		default:
			return notionMarkdownDomainFound(text, start, end, dot)
		}
		end += size
	}
	return notionMarkdownDomainFound(text, start, end, dot)
}

func notionMarkdownDomainFound(text string, start, end, dot int) (int, int, bool) {
	if dot < 0 || end > start && text[end-1] == '-' {
		return 0, 0, false
	}
	if end < len(text) && strings.ContainsRune("_@", rune(text[end])) {
		return 0, 0, false
	}
	topLevel := strings.ToLower(text[dot+1 : end])
	for _, r := range topLevel {
		if !unicode.IsLetter(r) {
			return 0, 0, false
		}
	}
	if !graphTopLevelDomain(topLevel) {
		return 0, 0, false
	}
	return end, dot, true
}

// notionMarkdownLinkPath is where an address whose host ends at end ends: a
// path, query or fragment runs on to the next space.
func notionMarkdownLinkPath(text string, end int) int {
	if end < len(text) && strings.ContainsRune("/?#", rune(text[end])) {
		return notionMarkdownLinkEnd(text, end, end)
	}
	return end
}

// notionMarkdownLinkEnd is where an address that runs on from from ends:
// before the next space, the punctuation closing a sentence and a closing
// bracket it doesn't open, but never before least.
func notionMarkdownLinkEnd(text string, from, least int) int {
	end := from
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += size
	}
	for end > least {
		last := text[end-1]
		opening := map[byte]byte{')': '(', ']': '[', '}': '{', '>': '<'}[last]
		switch {
		case strings.IndexByte(".,:;!?\"'", last) >= 0:
		case opening != 0 && !notionMarkdownLinkCloses(text[least:end], opening, last):
		default:
			return end
		}
		end--
	}
	return end
}

// notionMarkdownLinkCloses tells whether the bracket an address ends in
// closes one it opens before it, as Notion reads it (seen 2026-11-10:
// "a)(b)" keeps its last ")", "(a))" doesn't, a closing bracket before any
// opening one closing none).
func notionMarkdownLinkCloses(address string, opening, closing byte) bool {
	open := 0
	for index := 0; index < len(address)-1; index++ {
		switch address[index] {
		case opening:
			open++
		case closing:
			open = max(open-1, 0)
		}
	}
	return open > 0
}

// notionMarkdownUnlinkedText writes text no link holds with a form's escapes,
// escaping a colon or full stop in each address it holds bare so that it
// reads back unlinked.
func notionMarkdownUnlinkedText(text string, escape func(string) string) string {
	barriers := map[int]bool{}
	// An address escaped may leave another standing ("https\://example.com"
	// leaves "example.com"), so its runs are read again until none does.
	for added := true; added; {
		added = false
		for _, piece := range notionMarkdownPieces(text, barriers) {
			for _, link := range notionMarkdownBareLinks(text[piece[0]:piece[1]]) {
				barriers[piece[0]+link.at], added = true, true
			}
		}
	}
	if len(barriers) == 0 {
		return escape(text)
	}
	var out strings.Builder
	for _, piece := range notionMarkdownPieces(text, barriers) {
		if piece[0] > 0 && barriers[piece[0]-1] {
			out.WriteString(`\` + text[piece[0]-1:piece[0]])
		}
		out.WriteString(escape(text[piece[0]:piece[1]]))
	}
	return out.String()
}

// notionMarkdownPieces are the runs of text between barriers, as start and
// end.
func notionMarkdownPieces(text string, barriers map[int]bool) [][2]int {
	pieces, start := [][2]int{}, 0
	for index := 0; index < len(text); index++ {
		if barriers[index] {
			pieces = append(pieces, [2]int{start, index})
			start = index + 1
		}
	}
	return append(pieces, [2]int{start, len(text)})
}

// notionMarkdownBareUser reads a person's address standing bare at
// text[start:] — user:// and an ID Notion takes for a person's
// (notionMarkdownPersonUUID) to a word's end, or to the end of an email
// after it ("/a@b.com", to the next "/") — which is their mention, as
// Notion's API reads one in both versions (seen 2026-11-11; any other
// user:// address, "user://ID/" and "user://ID/x" among them, stays text);
// its target is user:// and the ID (linkBare).
func notionMarkdownBareUser(text string, start int) (notionMarkdownBareLink, bool) {
	const scheme = "user://"
	if len(text)-start < len(scheme)+36 || text[start:start+len(scheme)] != scheme {
		return notionMarkdownBareLink{}, false
	}
	end := start + len(scheme) + 36
	id := text[start+len(scheme) : end]
	if !notionMarkdownPersonUUID(id) {
		return notionMarkdownBareLink{}, false
	}
	if end < len(text) && text[end] == '/' {
		stop := end + 1
		for stop < len(text) {
			r, size := utf8.DecodeRuneInString(text[stop:])
			if unicode.IsSpace(r) || r == '/' {
				break
			}
			stop += size
		}
		stop = notionMarkdownLinkEnd(text[:stop], end+1, end+1)
		if !strings.Contains(text[end+1:stop], "@") {
			return notionMarkdownBareLink{}, false
		}
		end = stop
	} else if next, _ := utf8.DecodeRuneInString(text[end:]); end < len(text) && (notionMarkdownLinkRune(next) || next == '-') {
		return notionMarkdownBareLink{}, false
	}
	// The ID as written, in either case (notionKeptUUID).
	return notionMarkdownBareLink{start: start, end: end, at: start + len("user"), target: scheme + id}, true
}

// notionMarkdownBareDataSource reads a data source's address standing bare
// at text[start:], wherever it starts — collection:// and an ID, its 32
// hexadecimal digits with or without dashes, in either case — to a word's
// end, which is the data source's mention, as Notion's API reads one in
// "v1", "v2" and its MCP (seen 2026-11-11; "xcollection://ID" too); its
// target is collection:// and the ID as written (linkBare). One with a
// path after it ("collection://ID/x") is its text, all of it, and one of
// no ID ("collection://abc", "COLLECTION://ID") is words.
func notionMarkdownBareDataSource(text string, start int) (notionMarkdownBareLink, bool) {
	if !strings.HasPrefix(text[start:], notionMarkdownDataSourceAddress) {
		return notionMarkdownBareLink{}, false
	}
	from := start + len(notionMarkdownDataSourceAddress)
	end := from
	for end < len(text) && (notionMarkdownLinkRune(rune(text[end])) && text[end] < utf8.RuneSelf || text[end] == '-') {
		end++
	}
	if _, id := normalizeNotionUUIDRequest(text[from:end]); !id || strings.TrimSpace(text[from:end]) != text[from:end] {
		return notionMarkdownBareLink{}, false
	}
	if next, _ := utf8.DecodeRuneInString(text[end:]); end < len(text) && (notionMarkdownLinkRune(next) || next == '/') {
		return notionMarkdownBareLink{start: start, end: notionMarkdownLinkEnd(text, end, end)}, true
	}
	return notionMarkdownBareLink{start: start, end: end, at: start + len("collection"), target: text[start:end]}, true
}

// notionMarkdownViewAddress begins a view's address, view:// and its ID,
// which Notion's page Markdown links at its app's address of the ID, the
// words of one standing bare or in a tag without words that address (seen
// 2026-11-13, "v1" and "v2": "[a](view://ID)", "view://ID" bare with or
// without dashes, "xview://ID", and "v1"'s <mention-page> and
// <mention-database> of one; "VIEW://ID" is words; and 2026-11-22 an ID of
// no UUID's form too, notionMarkdownViewLink) — no page's, so a link that
// opens nothing there, as here. "v2"'s <mention> of one is refused
// (notionMarkdownRepair.check).
const notionMarkdownViewAddress = "view://"

// notionMarkdownBareView reads a view's address standing bare at
// text[start:] as notionMarkdownBareDataSource reads a data source's, its
// target view:// and the ID as written; a path after it is left off, as
// Notion's reading drops it (seen 2026-11-13: "view://ID/x" reads the
// address alone).
func notionMarkdownBareView(text string, start int) (notionMarkdownBareLink, bool) {
	if !strings.HasPrefix(text[start:], notionMarkdownViewAddress) {
		return notionMarkdownBareLink{}, false
	}
	from := start + len(notionMarkdownViewAddress)
	end := from
	for end < len(text) && (notionMarkdownLinkRune(rune(text[end])) && text[end] < utf8.RuneSelf || text[end] == '-' || text[end] == '_') {
		end++
	}
	if !notionMarkdownViewID(text[from:end]) {
		return notionMarkdownBareLink{}, false
	}
	target := text[start:end]
	if next, _ := utf8.DecodeRuneInString(text[end:]); end < len(text) && next == '/' {
		end = notionMarkdownLinkEnd(text, end, end)
	} else if end < len(text) && notionMarkdownLinkRune(next) {
		return notionMarkdownBareLink{start: start, end: notionMarkdownLinkEnd(text, end, end)}, true
	}
	return notionMarkdownBareLink{start: start, end: end, at: start + len("view"), target: target}, true
}

// viewAddress is the link a view's address makes (notionMarkdownViewLink)
// and the words it reads as without its own: the app's address of its ID on
// this server; false for any other address.
func (parser *workspaceImportInlineParser) viewAddress(address string) (words, link string, ok bool) {
	trimmed := strings.TrimSpace(address)
	shown, link, ok := notionMarkdownViewLink(trimmed, parser.sitePages != nil)
	if !ok {
		return "", "", false
	}
	words = trimmed
	if parser.sitePages != nil {
		words = strings.TrimRight(parser.sitePages.Origin, "/") + shown
	}
	return words, link, true
}

// notionMarkdownBareHostTail is a bare web address whose host runs into a
// character no host holds, as Notion links it (seen 2026-11-15, "v1" and
// "v2" create): its host ends before a <, a >, a quote or a colon that no
// port number follows, and what follows is its path, percent-encoded
// ("https://a.com:80>b" links https://a.com:80/%3Eb, "http://a.com:8o/x"
// http://a.com/:8o/x). One that still makes no link Leafen's links take
// (a port past 65535) is "".
func notionMarkdownBareHostTail(address string, scheme int) string {
	rest := address[scheme:]
	authority := rest[:strings.IndexAny(rest+"/", "/?#")]
	userinfo := strings.LastIndexByte(authority, '@') + 1
	stray := func(at int) bool { return at < len(authority) && strings.IndexByte(`<>"'`+"`", authority[at]) >= 0 }
	end := userinfo
	for end < len(authority) && !stray(end) && authority[end] != ':' {
		end++
	}
	// A port's number stays the host's when a character no host holds, or
	// nothing, follows it.
	if end < len(authority) && authority[end] == ':' {
		digits := end + 1
		for digits < len(authority) && authority[digits] >= '0' && authority[digits] <= '9' {
			digits++
		}
		if digits > end+1 && (digits == len(authority) || stray(digits)) {
			end = digits
		}
	}
	if end == len(authority) || end == userinfo {
		return ""
	}
	tail := authority[end:] + rest[len(authority):]
	linked := address[:scheme+end] + "/" + notionMarkdownEncodedAddress(tail, ";/?:@&=+$,-_.!~*'()#", true)
	if !validGraphTextLink(linked) {
		return ""
	}
	return linked
}

// notionMarkdownSchemeBefore is the scheme, in small letters, of the address
// whose host starts at text[start:] past its "://" (or more slashes), or "".
func notionMarkdownSchemeBefore(text string, start int) string {
	at := start
	for at > 0 && text[at-1] == '/' {
		at--
	}
	if start-at < 2 || at == 0 || text[at-1] != ':' {
		return ""
	}
	end := at - 1
	begin := end
	for begin > 0 && (unicode.IsLetter(rune(text[begin-1])) || unicode.IsDigit(rune(text[begin-1])) ||
		strings.IndexByte("+.-", text[begin-1]) >= 0) && text[begin-1] < utf8.RuneSelf {
		begin--
	}
	for begin < end && !(text[begin] >= 'a' && text[begin] <= 'z' || text[begin] >= 'A' && text[begin] <= 'Z') {
		begin++
	}
	return strings.ToLower(text[begin:end])
}
