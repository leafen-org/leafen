package app

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// A "v2" write reads a [text](address) link as Notion's API reads it (seen
// 2026-11-10): the address runs to the ) that closes it, past ones it opens,
// or between angle brackets; one with a space inside, or a javascript:, vbscript:,
// file: or data: one, makes no link, its Markdown text as written. The page
// keeps the address with an http, https or mailto host's other letters in
// punycode and the characters an address can't hold bare percent-encoded,
// the percent-encoding already there kept. "v2" writes a link's address
// percent-encoded too (notionMarkdownVersion2Destination); the page's
// other links stay as written.

var (
	notionMarkdownVersion2UnsafeLink  = regexp.MustCompile(`(?i)^(?:vbscript|javascript|file|data):`)
	notionMarkdownVersion2ImageData   = regexp.MustCompile(`(?i)^data:image/(?:gif|png|jpeg|webp);`)
	notionMarkdownVersion2LinkTitle   = regexp.MustCompile(`^[ \t]+(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\((?:[^()\\]|\\.)*\))[ \t]*\)`)
	notionMarkdownVersion2InlineTagAt = regexp.MustCompile(`^<(/?)(b|i|s|u|em|strong|code|span|a|mention|date|link-preview)((?:[ \t][^<>]*)?)>`)
)

// notionMarkdownVersion2Links writes each link's address in a "v2" line as
// the page keeps it, between angle brackets when it holds a parenthesis, and
// escapes the ( of one that makes no link; code is as written. An inline
// tag stands whole, what it holds up to its closing tag, as Notion reads it
// (notionMarkdownLinkTextEnd): a "](" inside one opened in a link's text is
// no link's end, and an address its closing tag stands in is no link's.
func notionMarkdownVersion2Links(line string) string {
	if !strings.Contains(line, "](") {
		return line
	}
	var out strings.Builder
	// tags are the inline tags open, by name, and brackets how many were
	// open at each [ not yet closed.
	tags, brackets := []string{}, []int{}
	for index := 0; index < len(line); {
		switch {
		case line[index] == '\\' && index+1 < len(line):
			out.WriteString(line[index : index+2])
			index += 2
			continue
		case line[index] == '`':
			// Code, or a run of backticks nothing closes, is as written.
			_, end, _ := notionMarkdownCodeSpanAt(line, index)
			out.WriteString(line[index:end])
			index = end
			continue
		case line[index] == '$':
			// So is an equation (notionMarkdownVersion2Equation).
			if _, end, ok := notionMarkdownVersion2Equation(line, index); ok {
				out.WriteString(line[index:end])
				index = end
				continue
			}
		case strings.HasPrefix(line[index:], "<code>"):
			if end := strings.Index(line[index:], "</code>"); end > 0 {
				out.WriteString(line[index : index+end+len("</code>")])
				index += end + len("</code>")
				continue
			}
		case line[index] == '<':
			if found := notionMarkdownVersion2InlineTagAt.FindStringSubmatch(line[index:]); found != nil &&
				!strings.HasSuffix(found[3], "/") {
				if found[1] == "" {
					tags = append(tags, found[2])
				}
				for at := len(tags) - 1; found[1] != "" && at >= 0; at-- {
					if tags[at] == found[2] {
						tags = tags[:at]
						break
					}
				}
			}
		case line[index] == '[':
			brackets = append(brackets, len(tags))
		case line[index] == ']' && !strings.HasPrefix(line[index:], "]("):
			if len(brackets) > 0 {
				brackets = brackets[:len(brackets)-1]
			}
		case strings.HasPrefix(line[index:], "]("):
			address, used, linked := notionMarkdownVersion2Address(line[index+2:])
			inside := len(brackets) > 0 && len(tags) > brackets[len(brackets)-1]
			if len(brackets) > 0 && !inside {
				brackets = brackets[:len(brackets)-1]
			}
			for _, name := range tags {
				inside = inside || used > 0 && strings.Contains(line[index+2:index+2+used], "</"+name+">")
			}
			switch {
			case used == 0 || inside:
			case !linked:
				out.WriteString(`]\(`)
				index += 2
				continue
			case notionMarkdownVersion2TextOnlyHost(address):
				// Its text stands without the link (seen 2026-11-10).
				written := out.String()
				if open := strings.LastIndexByte(written, '['); open >= 0 {
					out.Reset()
					out.WriteString(written[:open] + written[open+1:])
					index += 2 + used
					continue
				}
				out.WriteString("](" + notionMarkdownVersion2Destination(notionMarkdownVersion2LinkAddress(address)) + ")")
				index += 2 + used
				continue
			default:
				out.WriteString("](" + notionMarkdownVersion2Destination(notionMarkdownVersion2LinkAddress(address)) + ")")
				index += 2 + used
				continue
			}
		}
		out.WriteByte(line[index])
		index++
	}
	return out.String()
}

// notionMarkdownVersion2Address reads a link's address after its "(", as
// CommonMark's: the address, how much of rest it and its ) take, and whether
// it makes a link. A backslash before punctuation escapes it, which the
// address holds as itself (seen 2026-11-10: "a\)b" is "a)b", "a\\b" one
// backslash). A title after it, "…", '…' or (…), is left out (seen
// 2026-11-10: [a](https://example.com "t") links the address).
func notionMarkdownVersion2Address(rest string) (string, int, bool) {
	// Spaces before it are no part of it.
	lead := len(rest) - len(strings.TrimLeft(rest, " \t"))
	address, used, linked := notionMarkdownVersion2AddressAt(rest[lead:])
	if used > 0 {
		used += lead
	}
	return address, used, linked
}

func notionMarkdownVersion2AddressAt(rest string) (string, int, bool) {
	if strings.HasPrefix(rest, "<") {
		if end := strings.IndexAny(rest[1:], "<>\n"); end >= 0 && rest[1+end] == '>' {
			address := rest[1 : 1+end]
			if strings.HasPrefix(rest[2+end:], ")") {
				return address, end + 3, notionMarkdownVersion2Linkable(address)
			}
			if title := notionMarkdownVersion2LinkTitle.FindString(rest[2+end:]); title != "" {
				return address, end + 2 + len(title), notionMarkdownVersion2Linkable(address)
			}
		}
		return "", 1, false
	}
	depth, escaped := 0, false
	unescaped := func(address string) string {
		if !escaped {
			return address
		}
		return notionMarkdownVersion2Unescaped(address)
	}
	for index := 0; index < len(rest); index++ {
		switch character := rest[index]; {
		case character == '\\' && index+1 < len(rest) && notionMarkdownASCIIPunctuation(rest[index+1]):
			escaped = true
			index++
		case character == '(':
			depth++
		case character == ')' && depth > 0:
			depth--
		case character == ')':
			address := unescaped(rest[:index])
			return address, index + 1, notionMarkdownVersion2Linkable(address)
		case character == ' ' || character == '\t' || character == '\n':
			if after := strings.TrimLeft(rest[index:], " \t"); strings.HasPrefix(after, ")") {
				address := unescaped(rest[:index])
				return address, len(rest) - len(after) + 1, notionMarkdownVersion2Linkable(address)
			}
			if title := notionMarkdownVersion2LinkTitle.FindString(rest[index:]); title != "" {
				address := unescaped(rest[:index])
				return address, index + len(title), notionMarkdownVersion2Linkable(address)
			}
			return "", 1, false
		case character < ' ':
			return "", 1, false
		}
	}
	return "", 0, true
}

// notionMarkdownVersion2TextOnlyHost reports a web address whose host holds
// a capital letter beside letters outside ASCII (https://BÜCHER.de/): "v2"
// keeps such a link's text and drops its address, where "v1" links it in
// punycode (seen 2026-11-10).
func notionMarkdownVersion2TextOnlyHost(address string) bool {
	scheme, rest, found := strings.Cut(strings.TrimSpace(address), ":")
	if !found || !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") || !strings.HasPrefix(rest, "//") {
		return false
	}
	host := rest[2:]
	if end := strings.IndexAny(host, "/?#"); end >= 0 {
		host = host[:end]
	}
	host = host[strings.LastIndexByte(host, '@')+1:]
	capital, wide := false, false
	for _, character := range host {
		capital = capital || unicode.IsUpper(character)
		wide = wide || character >= utf8.RuneSelf
	}
	return capital && wide
}

// notionMarkdownASCIIPunctuation reports a character a backslash escapes.
func notionMarkdownASCIIPunctuation(character byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", character) >= 0
}

// notionMarkdownVersion2Unescaped is an address without the backslashes
// that escape its punctuation.
func notionMarkdownVersion2Unescaped(address string) string {
	var out strings.Builder
	for index := 0; index < len(address); index++ {
		if address[index] == '\\' && index+1 < len(address) && notionMarkdownASCIIPunctuation(address[index+1]) {
			index++
		}
		out.WriteByte(address[index])
	}
	return out.String()
}

func notionMarkdownVersion2Linkable(address string) bool {
	address = strings.TrimSpace(address)
	return !notionMarkdownVersion2UnsafeLink.MatchString(address) || notionMarkdownVersion2ImageData.MatchString(address)
}

// notionMarkdownVersion2LinkAddress is a "v2" link's address as the page
// keeps it.
func notionMarkdownVersion2LinkAddress(address string) string {
	address = strings.TrimSpace(address)
	scheme, rest, found := strings.Cut(address, ":")
	if found && (scheme == "http" || scheme == "https") && strings.HasPrefix(rest, "//") || found && scheme == "mailto" {
		start := len(scheme) + 1
		if scheme != "mailto" {
			start += 2
		}
		end := start + strings.IndexAny(address[start:]+"/", "/?#")
		authority := address[start:end]
		at := strings.LastIndexByte(authority, '@') + 1
		host := authority[at:]
		port := notionLinkPortPattern.FindString(host)
		if name := host[:len(host)-len(port)]; name != "" && !strings.HasPrefix(name, "[") {
			if ascii, err := idna.Punycode.ToASCII(name); err == nil {
				address = address[:start+at] + ascii + port + address[end:]
			}
		}
	}
	return notionMarkdownEncodedAddress(address, ";/?:@&=+$,-_.!~*'()#", true)
}

// notionMarkdownVersion2Destination is a link's address as "v2" writes it:
// percent-encoded but for the characters an address holds bare, a % that a
// digit follows kept, and between angle brackets when it holds a
// parenthesis.
func notionMarkdownVersion2Destination(address string) string {
	address = notionMarkdownEncodedAddress(address, ";/?:@&=+$,-_.!~*'()#[]", false)
	if strings.ContainsAny(address, "()") {
		return "<" + address + ">"
	}
	return address
}

// notionMarkdownEncodedAddress percent-encodes an address's characters but
// letters, digits, -._~ and those kept, as UTF-8; a % already encoding a
// byte stays: before two hex digits when pair, else before one.
func notionMarkdownEncodedAddress(address, kept string, pair bool) string {
	hex := func(character byte) bool {
		return character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F'
	}
	var out strings.Builder
	for index := 0; index < len(address); {
		character := address[index]
		switch {
		case character == '%' && index+1 < len(address) && hex(address[index+1]) &&
			(!pair || index+2 < len(address) && hex(address[index+2])):
			out.WriteByte(character)
		case character < utf8.RuneSelf && (notionLinkUnreserved(character) || strings.IndexByte(kept, character) >= 0):
			out.WriteByte(character)
		default:
			_, size := utf8.DecodeRuneInString(address[index:])
			for _, encoded := range []byte(address[index : index+size]) {
				out.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[encoded>>4])+string("0123456789abcdef"[encoded&15])))
			}
			index += size
			continue
		}
		index++
	}
	return out.String()
}
