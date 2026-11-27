package app

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/publicsuffix"
)

// A block's address as Notion's API takes it after its request check (seen
// 2026-11-10, Notion-Version 2026-03-11). A new bookmark or embed keeps any
// text as given, "" and words too. A bookmark's or an embed's address
// changed is checked (notionLinkTaken), "" leaving the one it has, and one
// not taken is "Invalid URL " and the text as given. A new picture, video,
// PDF, sound or file is checked the same, one not taken a fault in its
// content (notionMediaLinkRequestFault).

// notionLinkTaken tells whether Notion's API takes text as an address where
// it checks one: trimmed, an address of the web, mail, a telephone or Notion
// itself ("https://e", "http:example.com", "mailto:", "tel:1",
// "notion://x"), or text holding a domain whose last name is a real
// top-level domain ("a b.com", "例え.jp", "x_y.com", "e.com:80", "irc://e.com")
// or localhost, outside an ftp:// or file:// address. Not "x.y", "x.local",
// "1.2.3.4", "/path/only", "javascript:…", "data:…", "about:blank",
// "ftp://example.com/x".
func notionLinkTaken(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if parsed, err := url.Parse(text); err == nil {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			if parsed.Host != "" || parsed.Opaque != "" {
				return true
			}
		case "mailto", "tel", "notion":
			return true
		}
	}
	return notionTextHoldsDomain(text)
}

// notionTextHoldsDomain tells whether text holds a run of names joined by
// full stops whose last is a real top-level domain, or whose first is
// localhost, that no ftp://, ftps:// or file:// address holds.
func notionTextHoldsDomain(text string) bool {
	name := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r) || r == '-' || r == '_' || r == '.'
	}
	for start := 0; start < len(text); {
		r, size := utf8.DecodeRuneInString(text[start:])
		if !name(r) {
			start += size
			continue
		}
		end := start
		for end < len(text) {
			r, size := utf8.DecodeRuneInString(text[end:])
			if !name(r) {
				break
			}
			end += size
		}
		if notionDomainRun(text[start:end]) && !notionUntakenSchemeBefore(text[:start]) {
			return true
		}
		start = end
	}
	return false
}

// notionDomainRun tells whether a run of names is a domain: after its last
// empty name ("x..com" holds only "com"), localhost first, or two names or
// more, the last a real top-level domain.
func notionDomainRun(run string) bool {
	names := strings.Split(strings.TrimRight(run, "."), ".")
	for index := len(names) - 1; index >= 0; index-- {
		if names[index] == "" {
			names = names[index+1:]
			break
		}
	}
	if len(names) > 0 && strings.EqualFold(names[0], "localhost") {
		return true
	}
	if len(names) < 2 {
		return false
	}
	topLevel := strings.ToLower(names[len(names)-1])
	for _, r := range topLevel {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	suffix, icann := publicsuffix.PublicSuffix(topLevel)
	return icann && suffix == topLevel
}

// notionUntakenSchemeBefore tells whether before ends in the scheme of an
// address Notion reads as one and doesn't take: ftp://, ftps:// or file://.
func notionUntakenSchemeBefore(before string) bool {
	lower := strings.ToLower(before)
	for _, scheme := range []string{"ftp://", "ftps://", "file://"} {
		if strings.HasSuffix(lower, scheme) {
			return true
		}
	}
	return false
}

// notionLinkRefusedOnUpdate is Notion's refusal of a bookmark's or an
// embed's changed address it doesn't take, the text as given, spaces too.
func notionLinkRefusedOnUpdate(text string) string {
	return "Invalid URL " + text
}
