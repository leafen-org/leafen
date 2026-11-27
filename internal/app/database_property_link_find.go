package app

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// firstTextLink finds the first web or mail link in text as linkifyjs 4 does
// — the finder the web's propertyLinks.ts uses and Notion's URL values follow
// (seen 2026-11-12 on 64 values) — and gives where it leads: "http://" before
// one without a scheme, "mailto:" before a bare email address. It is "" when
// the text holds none ("plain words", "tel:0101234", "127.0.0.1:3000/p").
// The port keeps linkifyjs's states; a top-level domain is one the public
// suffix list's ICANN part names, as linkifyjs's own list of them.
func firstTextLink(text string) string {
	tokens := scanTextLinkTokens(text)
	for start := range tokens {
		state, end, accepted := linkStart, -1, linkStart
		for at := start; at < len(tokens); at++ {
			state = textLinkNext(state, tokens[at])
			if state == linkNone {
				break
			}
			if state.accepts() {
				end, accepted = at, state
			}
		}
		if end < 0 {
			continue
		}
		value := text[tokens[start].from:tokens[end].to]
		if accepted == linkEmail {
			return "mailto:" + value
		}
		if end > start && (tokens[start].kind == tokenScheme || tokens[start].kind == tokenSlashScheme) && tokens[start+1].text == ":" {
			return value
		}
		return "http://" + value
	}
	return ""
}

type textLinkTokenKind uint8

const (
	tokenSymbol textLinkTokenKind = iota
	tokenWord
	tokenTLD
	tokenNumber
	tokenScheme
	tokenSlashScheme
	tokenLocalhost
	tokenSpace
)

type textLinkToken struct {
	kind     textLinkTokenKind
	text     string
	from, to int
}

func (token textLinkToken) domain() bool {
	return token.kind != tokenSymbol && token.kind != tokenSpace
}

// scanTextLinkTokens splits text as linkifyjs's scanner: runs of ASCII
// letters and digits (a top-level domain, scheme or "localhost" when the run
// is exactly one), runs of other letters and digits, runs of space, and each
// other character on its own.
func scanTextLinkTokens(text string) []textLinkToken {
	tokens := []textLinkToken{}
	runes := []rune(text)
	asciiWord := func(r rune) bool { return r < 0x80 && (unicode.IsLetter(r) || textLinkDigit(r)) }
	letterWord := func(r rune) bool { return r >= 0x80 && unicode.IsLetter(r) || textLinkDigit(r) }
	for at, from := 0, 0; at < len(runes); {
		start, startByte := at, from
		take := func(in func(rune) bool) {
			for at < len(runes) && in(runes[at]) {
				from += utf8.RuneLen(runes[at])
				at++
			}
		}
		kind := tokenSymbol
		switch r := runes[at]; {
		case unicode.IsSpace(r):
			take(unicode.IsSpace)
			kind = tokenSpace
		case asciiWord(r):
			take(asciiWord)
			kind = textLinkWordKind(string(runes[start:at]))
		case letterWord(r):
			take(letterWord)
			kind = textLinkWordKind(string(runes[start:at]))
		default:
			from += utf8.RuneLen(r)
			at++
		}
		tokens = append(tokens, textLinkToken{kind: kind, text: text[startByte:from], from: startByte, to: from})
	}
	return tokens
}

func textLinkDigit(r rune) bool { return r >= '0' && r <= '9' }

func textLinkWordKind(word string) textLinkTokenKind {
	lower := strings.ToLower(word)
	switch lower {
	case "file", "mailto":
		return tokenScheme
	case "http", "https", "ftp", "ftps":
		return tokenSlashScheme
	case "localhost":
		return tokenLocalhost
	}
	if strings.IndexFunc(lower, unicode.IsLetter) < 0 {
		return tokenNumber
	}
	if strings.IndexFunc(lower, textLinkDigit) >= 0 {
		return tokenWord
	}
	label, err := idna.Lookup.ToASCII(lower)
	if err != nil || label == "" {
		return tokenWord
	}
	if suffix, icann := publicsuffix.PublicSuffix("x." + label); icann && suffix == label {
		return tokenTLD
	}
	return tokenWord
}

type textLinkState uint8

const (
	linkNone textLinkState = iota
	linkStart
	linkDomain
	linkScheme
	linkSlashScheme
	linkLocalpart
	linkLocalpartDot
	linkLocalpartAt
	linkEmailDomain
	linkEmailDomainDot
	linkEmailDomainHyphen
	linkEmail
	linkDomainHyphen
	linkDomainDot
	linkDomainDotTLD
	linkDomainDotTLDColon
	linkDomainDotTLDColonPort
	linkSchemeColon
	linkSlashSchemeColon
	linkSlashSchemeColonSlash
	linkURIPrefix
	linkURL
	linkURLNonaccept
	// A link after an opening bracket: linkURLOpen, then linkURLOpenQ (ending
	// there) or linkURLOpenSyms (not), each once per pair in textLinkBrackets.
	linkURLOpen
)

const (
	linkURLOpenQ    = linkURLOpen + textLinkState(len(textLinkBrackets))
	linkURLOpenSyms = linkURLOpenQ + textLinkState(len(textLinkBrackets))
)

// textLinkBrackets are the pairs a link keeps when it closes them.
var textLinkBrackets = [...][2]string{{"{", "}"}, {"[", "]"}, {"(", ")"}, {"<", ">"}, {"（", "）"}, {"「", "」"}, {"『", "』"}, {"＜", "＞"}}

func (state textLinkState) accepts() bool {
	switch {
	case state == linkEmail, state == linkDomainDotTLD, state == linkDomainDotTLDColonPort, state == linkURL:
		return true
	}
	return state >= linkURLOpenQ && state < linkURLOpenSyms
}

const (
	textLinkLocalpartSymbols = "&'*\\`^$=-{}%|+#?/~_"
	textLinkURLSymbols       = "&*@\\`^$=-|+#/~_"
	textLinkURLTrailSymbols  = "':,.!%?\";<>{}[]()（）「」『』＜＞"
	textLinkKnownSymbols     = "()<>（）「」『』＜＞&*@`^:,$.=!-%|+#?\"/;~_\\・{}[]'"
)

// textLinkSymbol says whether a symbol token is among symbols, an unlisted
// character standing for linkifyjs's SYM.
func textLinkSymbol(token textLinkToken, symbols string, unlisted bool) bool {
	if token.kind != tokenSymbol {
		return false
	}
	if !strings.Contains(textLinkKnownSymbols, token.text) {
		return unlisted
	}
	return strings.Contains(symbols, token.text)
}

// textLinkNext is linkifyjs's parser transition from state on token.
func textLinkNext(state textLinkState, token textLinkToken) textLinkState {
	symbol := func(text string) bool { return token.kind == tokenSymbol && token.text == text }
	localpart := token.domain() || textLinkSymbol(token, textLinkLocalpartSymbols, true)
	switch state {
	case linkStart:
		switch {
		case token.kind == tokenScheme:
			return linkScheme
		case token.kind == tokenSlashScheme:
			return linkSlashScheme
		case token.kind == tokenLocalhost:
			return linkDomainDotTLD
		case token.domain():
			return linkDomain
		case symbol("~"):
			return linkLocalpart
		}
	case linkDomain, linkScheme, linkSlashScheme:
		switch {
		case symbol("@"):
			return linkLocalpartAt
		case symbol("-"):
			return linkDomainHyphen
		case symbol("."):
			return linkDomainDot
		case symbol(":") && state == linkScheme:
			return linkSchemeColon
		case symbol(":") && state == linkSlashScheme:
			return linkSlashSchemeColon
		case token.domain():
			return linkDomain
		case localpart && state == linkDomain:
			return linkLocalpart
		}
	case linkLocalpart:
		switch {
		case symbol("@"):
			return linkLocalpartAt
		case symbol("."):
			return linkLocalpartDot
		case localpart:
			return linkLocalpart
		}
	case linkLocalpartDot:
		if localpart {
			return linkLocalpart
		}
	case linkLocalpartAt:
		switch {
		case token.kind == tokenLocalhost:
			return linkEmail
		case token.domain():
			return linkEmailDomain
		}
	case linkEmailDomain, linkEmail:
		switch {
		case symbol("."):
			return linkEmailDomainDot
		case symbol("-"):
			return linkEmailDomainHyphen
		case token.domain():
			return linkEmailDomain
		}
	case linkEmailDomainDot:
		switch {
		case token.kind == tokenTLD:
			return linkEmail
		case token.domain():
			return linkEmailDomain
		}
	case linkEmailDomainHyphen:
		switch {
		case symbol("-"):
			return linkEmailDomainHyphen
		case token.domain():
			return linkEmailDomain
		}
	case linkDomainHyphen:
		switch {
		case symbol("-"):
			return linkDomainHyphen
		case token.domain():
			return linkDomain
		}
	case linkDomainDot:
		switch {
		case token.kind == tokenTLD:
			return linkDomainDotTLD
		case token.domain() || localpart:
			if token.domain() {
				return linkDomain
			}
			return linkLocalpart
		}
	case linkDomainDotTLD:
		switch {
		case symbol("/"):
			return linkURL
		case symbol("."):
			return linkDomainDot
		case symbol("-"):
			return linkDomainHyphen
		case symbol("@"):
			return linkLocalpartAt
		case symbol(":"):
			return linkDomainDotTLDColon
		case token.domain():
			return linkDomain
		case localpart:
			return linkLocalpart
		}
	case linkDomainDotTLDColon:
		if token.kind == tokenNumber {
			return linkDomainDotTLDColonPort
		}
	case linkDomainDotTLDColonPort:
		if symbol("/") {
			return linkURL
		}
	case linkSchemeColon:
		if token.domain() || symbol("/") || symbol("?") {
			return linkURL
		}
	case linkSlashSchemeColon:
		if symbol("/") {
			return linkSlashSchemeColonSlash
		}
	case linkSlashSchemeColonSlash:
		if symbol("/") {
			return linkURIPrefix
		}
	case linkURIPrefix:
		if token.domain() || textLinkSymbol(token, textLinkURLSymbols+"%", true) {
			return linkURL
		}
	case linkURL, linkURLNonaccept:
		for pair, brackets := range textLinkBrackets {
			if symbol(brackets[0]) {
				return linkURLOpen + textLinkState(pair)
			}
		}
		return textLinkQuery(token, linkURL, linkURLNonaccept)
	}
	if state >= linkURLOpen && state < linkURLOpenSyms+textLinkState(len(textLinkBrackets)) {
		pair := int(state-linkURLOpen) % len(textLinkBrackets)
		if symbol(textLinkBrackets[pair][1]) {
			return linkURL
		}
		return textLinkQuery(token, linkURLOpenQ+textLinkState(pair), linkURLOpenSyms+textLinkState(pair))
	}
	return linkNone
}

// textLinkQuery is where a link's query goes on token: on to ending, to
// going on without ending there (a dot, comma, bracket…), or nowhere.
func textLinkQuery(token textLinkToken, ending, going textLinkState) textLinkState {
	switch {
	case token.domain() || textLinkSymbol(token, textLinkURLSymbols, true):
		return ending
	case textLinkSymbol(token, textLinkURLTrailSymbols, false):
		return going
	}
	return linkNone
}

// browserLinkHref is a link as a browser reads it, as the web's `new
// URL(link).href`: its scheme and host in lower case, the host's Unicode in
// punycode, an address's empty path "/", and what is not ASCII escaped.
func browserLinkHref(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return link
	}
	if parsed.Opaque != "" || parsed.Host == "" && parsed.Scheme != "file" {
		return parsed.String()
	}
	host := strings.ToLower(parsed.Hostname())
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		host = ascii
	}
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	parsed.Host = host
	if parsed.Path == "" && parsed.Scheme != "mailto" {
		parsed.Path = "/"
	}
	parsed.RawQuery = escapeNonASCII(parsed.RawQuery)
	return parsed.String()
}

func escapeNonASCII(text string) string {
	var out strings.Builder
	for _, b := range []byte(text) {
		if b >= 0x80 {
			fmt.Fprintf(&out, "%%%02X", b)
			continue
		}
		out.WriteByte(b)
	}
	return out.String()
}
