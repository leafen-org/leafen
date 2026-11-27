package app

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A "v2" write reads character references and backslash escapes as Notion's
// API reads them (seen 2026-11-11), as CommonMark's: in text — a block's, a
// link's or an image's, a table cell's, a bare address's — and in a link's
// or an image's address, an HTML5 entity named with its ";" (&amp;, &AMP;,
// &copy;, &ngE;) and a number of 1 to 7 digits or 1 to 6 hex digits (&#65;,
// &#x41;) are their characters, a number no text holds (&#0;, &#xD800;,
// &#128;, &#xFFFE;, &#1114112;) U+FFFD, and a backslash before any ASCII
// punctuation that punctuation ("\&amp;" is "&amp;", "\@" "@"). What they
// read is text, never Markdown: "&#42;b&#42;" is "*b*", "&#35; x" no
// heading, "&#32;x" a space before x; a line break is one, a carriage
// return or a form feed nothing. Code, math, a tag and an address between
// angle brackets are as written, and so are a name without its ";" (&copy),
// a name HTML doesn't hold (&foo;) and a longer number (&#12345678;). A
// tag's attribute reads &amp;, &lt;, &gt;, &quot; and &#39; alone
// (notionMarkdownVersion2AttributeValue). "v2" escapes the & that starts
// what has a reference's shape (notionMarkdownVersion2EscapedText).

// notionMarkdownLeafenEscapes are the characters Leafen's page Markdown
// reads after a backslash as themselves.
const notionMarkdownLeafenEscapes = "\\`*_{}[]()#+-.!|~<>$^:"

// notionMarkdownEntitySpace stands before the <span> a space a reference
// reads stands in, so that a line break after it takes it
// (notionMarkdownVersion2Break), until the line is read. No text a write
// sends holds the byte.
const notionMarkdownEntitySpace = "\xfd"

// notionMarkdownDecodedColon stands for a colon a reference reads until its
// line is read (notionMarkdownVersion2Colons). No text a write sends holds
// the byte.
const notionMarkdownDecodedColon = "\xfe"

var (
	notionMarkdownCharacterReferenceAt = regexp.MustCompile(`^&(?:#[0-9]{1,7};|#[xX][0-9A-Fa-f]{1,6};|[A-Za-z][A-Za-z0-9]{1,31};)`)
	// A tag, or an address between angle brackets, reads as written.
	notionMarkdownVersion2TagAt             = regexp.MustCompile(`^<[A-Za-z/][^<>]*>`)
	notionMarkdownVersion2AttributeEntities = strings.NewReplacer(`&amp;`, `&`, `&lt;`, `<`, `&gt;`, `>`, `&quot;`, `"`, `&#39;`, `'`)
	notionMarkdownAttributeUnescaped        = strings.NewReplacer(`&quot;`, `"`, `&lt;`, `<`, `&gt;`, `>`, `&amp;`, `&`)
	notionMarkdownVersion2HrefEscaped       = strings.NewReplacer(`\`, `\\`, `&`, `\&`)
)

// notionMarkdownVersion2Entities reads a "v2" line's references and escapes
// into Leafen's page Markdown, which reads what they read as text.
func notionMarkdownVersion2Entities(line string) string {
	if !strings.ContainsAny(line, `&\`) {
		return line
	}
	// The line's text starts after its block's marks, which hold no & or \.
	reading := notionMarkdownEntityReading{content: len(notionMarkdownVersion2MarkerRun.FindString(line)), digits: true}
	for index := 0; index < len(line); {
		character, end := line[index], index+1
		switch {
		case character == '\\' && end < len(line) && notionMarkdownASCIIPunctuation(line[end]):
			end++
			if strings.IndexByte(notionMarkdownLeafenEscapes, line[index+1]) < 0 {
				index++
			}
		case character == '`':
			_, end, _ = notionMarkdownCodeSpanAt(line, index)
		case character == '$':
			if _, closed, ok := notionMarkdownVersion2Equation(line, index); ok {
				end = closed
			}
		case strings.HasPrefix(line[index:], "<code>") && strings.Contains(line[index:], "</code>"):
			end = index + strings.Index(line[index:], "</code>") + len("</code>")
		case character == '<':
			end = index + max(1, len(notionMarkdownVersion2TagAt.FindString(line[index:])))
		case strings.HasPrefix(line[index:], "]("):
			if address, used := notionMarkdownVersion2AddressEntities(line[index+2:]); used > 0 {
				reading.write("](" + address)
				index += 2 + used
				continue
			}
		case character == '&':
			if decoded, size, ok := notionMarkdownCharacterReference(line, index); ok {
				reading.text(decoded, line[index+size:])
				index += size
				continue
			}
		}
		reading.write(line[index:end])
		index = end
	}
	return notionMarkdownVersion2Colons(reading.out.String())
}

// notionMarkdownEntityReading is a line read so far: content is where its
// text starts, digits whether its text so far is digits alone, and last the
// character it ends with.
type notionMarkdownEntityReading struct {
	out     strings.Builder
	content int
	digits  bool
	last    rune
}

func (reading *notionMarkdownEntityReading) write(written string) {
	if written == "" {
		return
	}
	if reading.out.Len() >= reading.content {
		reading.digits = reading.digits && strings.Trim(written, "0123456789") == ""
	}
	reading.out.WriteString(written)
	reading.last, _ = utf8.DecodeLastRuneInString(written)
}

// text writes the characters a reference reads, rest the line after it, as
// Leafen's page Markdown reads them as text: punctuation escaped; a space
// between two letters or digits as it is, and elsewhere in a <span>, which
// keeps it at the text's edge (notionMarkdownTitleEdges) and leaves the
// marks beside it as they read (seen 2026-11-11: "*a&#32;*" an italic "a ");
// a digit that starts the text in one, and a "." after the digits it starts
// with escaped, so that no list's number is read; a line break a <br>.
func (reading *notionMarkdownEntityReading) text(decoded, rest string) {
	for at, character := range decoded {
		after := decoded[at+utf8.RuneLen(character):]
		if after == "" {
			after = rest
		}
		next, _ := utf8.DecodeRuneInString(after)
		started := reading.out.Len() > reading.content
		switch {
		case character == '\n':
			reading.write("<br>")
		case character == '\r' || character == '\f':
		case unicode.IsSpace(character):
			if started && notionMarkdownWordRune(reading.last) && after != "" && notionMarkdownWordRune(next) {
				reading.write(string(character))
			} else {
				reading.write(notionMarkdownEntitySpace + "<span>" + string(character) + "</span>")
			}
		case character >= '0' && character <= '9' && reading.digits:
			reading.write("<span>" + string(character) + "</span>")
		case character == '.' && !(started && reading.digits):
			reading.write(".")
		case character == ':':
			reading.write(notionMarkdownDecodedColon)
		case character < utf8.RuneSelf && strings.ContainsRune(notionMarkdownLeafenEscapes, character):
			reading.write(`\` + string(character))
		default:
			reading.write(string(character))
		}
	}
}

// notionMarkdownEntitySpaceAt is where the spaces references read end a
// line, after the spaces written there, or -1.
func notionMarkdownEntitySpaceAt(line string) int {
	end := -1
	for {
		line = strings.TrimRight(line, " \t")
		at := strings.LastIndex(line, notionMarkdownEntitySpace+"<span>")
		inside, closed := strings.CutSuffix(line[max(at, 0):], "</span>")
		if at < 0 || !closed || strings.TrimSpace(inside[len(notionMarkdownEntitySpace+"<span>"):]) != "" {
			return end
		}
		line, end = line[:at], at
	}
}

// notionMarkdownVersion2Colons writes the colons references read: escaped
// at the edge of a custom emoji's :name:, which a reference's colon doesn't
// make (seen 2026-11-11: "&#58;smile&#58;" is text), and bare elsewhere,
// where an address goes on across them ("https&#58;//example.com/" links).
func notionMarkdownVersion2Colons(line string) string {
	if !strings.Contains(line, notionMarkdownDecodedColon) {
		return line
	}
	colons := []int{}
	for index := 0; index < len(line); index++ {
		if line[index] == notionMarkdownDecodedColon[0] || line[index] == ':' && !notionMarkdownEscapedAt(line, index) {
			colons = append(colons, index)
		}
	}
	var out strings.Builder
	at := 0
	for order, colon := range colons {
		if line[colon] != notionMarkdownDecodedColon[0] {
			continue
		}
		out.WriteString(line[at:colon])
		if order > 0 && notionMarkdownEmojiName(line[colons[order-1]+1:colon]) ||
			order+1 < len(colons) && notionMarkdownEmojiName(line[colon+1:colons[order+1]]) {
			out.WriteString(`\:`)
		} else {
			out.WriteString(":")
		}
		at = colon + 1
	}
	out.WriteString(line[at:])
	return out.String()
}

// notionMarkdownVersion2AddressEntities reads the references in a link's or
// an image's address after its "(" (seen 2026-11-11: "?a=1&amp;b=2" is
// "?a=1&b=2", "&#41;" a ")" inside it, "&#32;" %20) as its reading
// (notionMarkdownVersion2Address) takes them, and says how much of rest the
// address and what follows it to its ")" take, a title as written; 0 when
// it is no link's.
func notionMarkdownVersion2AddressEntities(rest string) (string, int) {
	_, used, _ := notionMarkdownVersion2Address(rest)
	if used == 0 {
		return "", 0
	}
	lead := len(rest) - len(strings.TrimLeft(rest, " \t"))
	angled, end := strings.HasPrefix(rest[lead:], "<"), used
	if space := strings.IndexAny(rest[lead:used], " \t"); !angled && space >= 0 {
		end = lead + space
	} else if closing := strings.IndexByte(rest[lead:used], '>'); angled && closing >= 0 {
		end = lead + closing
	}
	var out strings.Builder
	out.WriteString(rest[:lead])
	for index := lead; index < end; {
		if rest[index] == '\\' && index+1 < end && notionMarkdownASCIIPunctuation(rest[index+1]) {
			out.WriteString(rest[index : index+2])
			index += 2
			continue
		}
		if decoded, size, ok := notionMarkdownCharacterReference(rest, index); ok && index+size <= end {
			for _, character := range decoded {
				out.WriteString(notionMarkdownAddressCharacter(character, angled))
			}
			index += size
			continue
		}
		out.WriteByte(rest[index])
		index++
	}
	out.WriteString(rest[end:used])
	return out.String(), used
}

// notionMarkdownAddressCharacter is a character a reference reads in an
// address, written so that its reading keeps it: punctuation escaped,
// between angle brackets as it is, and space, a control character or an
// angle bracket that would close it percent-encoded.
func notionMarkdownAddressCharacter(character rune, angled bool) string {
	switch {
	case character <= ' ' || unicode.IsSpace(character) || angled && (character == '<' || character == '>'):
		return notionMarkdownEncodedAddress(string(character), "", false)
	case !angled && character < utf8.RuneSelf && notionMarkdownASCIIPunctuation(byte(character)):
		return `\` + string(character)
	}
	return string(character)
}

// notionMarkdownCharacterReference reads the reference at value[index:]:
// its characters, how long it is written, and whether it is one.
func notionMarkdownCharacterReference(value string, index int) (string, int, bool) {
	written := notionMarkdownCharacterReferenceAt.FindString(value[index:])
	if written == "" {
		return "", 0, false
	}
	if written[1] != '#' {
		// A name HTML doesn't hold reads as written, and so does one with a
		// name HTML holds without its ";" before more letters: three or more
		// characters.
		decoded := html.UnescapeString(written)
		if utf8.RuneCountInString(decoded) > 2 {
			return "", 0, false
		}
		return decoded, len(written), true
	}
	digits, base := written[2:len(written)-1], 10
	if digits[0] == 'x' || digits[0] == 'X' {
		digits, base = digits[1:], 16
	}
	code, _ := strconv.ParseUint(digits, base, 32)
	return string(notionMarkdownReferenceRune(code)), len(written), true
}

// notionMarkdownReferenceRune is the character a number reads: U+FFFD for
// one past Unicode, a surrogate, a noncharacter or a control character but
// a tab, a line break, a form feed and a carriage return.
func notionMarkdownReferenceRune(code uint64) rune {
	switch {
	case code > unicode.MaxRune, code >= 0xD800 && code <= 0xDFFF, code >= 0xFDD0 && code <= 0xFDEF,
		code&0xFFFF >= 0xFFFE, code <= 8, code == 0x0B, code >= 0x0E && code <= 0x1F, code >= 0x7F && code <= 0x9F:
		return utf8.RuneError
	}
	return rune(code)
}

// notionMarkdownVersion2AttributeValue is a tag's attribute value as "v2"
// reads it (seen 2026-11-11): &amp;, &lt;, &gt;, &quot; and &#39; are their
// characters, and every other reference is as written ("&#46;", "&apos;",
// "&copy;").
func notionMarkdownVersion2AttributeValue(written string) string {
	return notionMarkdownVersion2AttributeEntities.Replace(written)
}

// notionMarkdownVersion2AttributeValues are a tag's attributes, as kept,
// by name, each value as it reads.
func notionMarkdownVersion2AttributeValues(kept string) map[string]string {
	attributes := notionMarkdownAttributes(kept)
	for name, value := range attributes {
		attributes[name] = notionMarkdownAttributeUnescaped.Replace(value)
	}
	return attributes
}

// notionMarkdownVersion2HrefWritten is an HTML link's address, its attribute
// read, written as a Markdown link's that reads back as it (seen
// 2026-11-11: href="…?a=1&#38;b=2&amp;c" links "…?a=1&#38;b=2&c").
func notionMarkdownVersion2HrefWritten(href string) string {
	return notionMarkdownVersion2HrefEscaped.Replace(notionMarkdownVersion2AttributeValue(href))
}
