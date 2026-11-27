package app

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// Where a span of text may link, in every place that takes or holds rich
// text, as the editor's safeUrl and textLinkHref take it (richtextUrlPolicy.ts):
// a web address, an email address or phone number, a web address written
// without its scheme as Notion's API keeps one, which the app opens over
// https, or a path or anchor in the app. A web address is one a browser
// opens: a host after the scheme however many slashes or backslashes stand
// between, and a port that is one. Written without its scheme, its host is
// localhost or a name under a top-level domain, after any slashes, as
// Notion's API takes one (seen 2026-11-10: example.com, //a.com/x,
// user:pass@example.com, localhost:3000/a and bücher.de are taken; example,
// 127.0.0.1, host.example and example.xn--p1ai are not).

// parseGraphLinkURL parses a link's address as a browser takes it, a % that
// encodes no byte being itself, as Notion keeps such an address (seen
// 2026-11-10); the address stays as written.
func parseGraphLinkURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err == nil || !strings.Contains(value, "%") {
		return parsed, err
	}
	hex := func(at int) bool {
		return at < len(value) && strings.IndexByte("0123456789abcdefABCDEF", value[at]) >= 0
	}
	var escaped strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '%' && !(hex(index+1) && hex(index+2)) {
			escaped.WriteString("%25")
			continue
		}
		escaped.WriteByte(value[index])
	}
	return url.Parse(escaped.String())
}

// validGraphTextLink is whether a span of text may link to value.
func validGraphTextLink(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxGraphExternalFileURLBytes {
		return false
	}
	switch {
	case strings.HasPrefix(value, "#"):
		return true
	case strings.HasPrefix(value, "//"):
		return graphSchemelessWebLink(value)
	case strings.HasPrefix(value, "/"):
		return true
	}
	if scheme, rest, found := strings.Cut(value, ":"); found {
		switch strings.ToLower(scheme) {
		case "http", "https":
			_, opens := graphWebLinkHostname(rest)
			return opens
		case "mailto", "tel":
			return true
		}
	}
	return graphSchemelessWebLink(value)
}

// graphWebLinkHostname is the host a web address opens at, given what
// follows its scheme, as a browser reads it; false for one no browser opens.
func graphWebLinkHostname(rest string) (string, bool) {
	rest = strings.TrimLeft(rest, `/\`)
	// Backslashes before the query or fragment are slashes.
	if split := strings.IndexAny(rest, "?#"); split >= 0 {
		rest = strings.ReplaceAll(rest[:split], `\`, "/") + rest[split:]
	} else {
		rest = strings.ReplaceAll(rest, `\`, "/")
	}
	parsed, err := parseGraphLinkURL("https://" + rest)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	if port := parsed.Port(); port != "" {
		if number, err := strconv.Atoi(port); err != nil || number > 65535 {
			return "", false
		}
	}
	return parsed.Hostname(), true
}

// graphSchemelessWebLink is whether an address written without its scheme
// is a web address the app opens over https.
func graphSchemelessWebLink(value string) bool {
	hostname, opens := graphWebLinkHostname(value)
	if !opens {
		return false
	}
	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	if hostname == "localhost" {
		return true
	}
	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if label == "" {
			return false
		}
	}
	return len(labels) > 1 && graphTopLevelDomain(labels[len(labels)-1])
}

// graphTopLevelDomain is whether a name is a top-level domain, as Notion's
// list of them has it: in its own letters (рф, not xn--p1ai) and without
// .onion (seen 2026-11-10).
func graphTopLevelDomain(label string) bool {
	if strings.HasPrefix(label, "xn--") || label == "onion" {
		return false
	}
	ascii, err := idna.Lookup.ToASCII(label)
	if err != nil {
		return false
	}
	suffix, icann := publicsuffix.PublicSuffix(ascii)
	return icann && suffix == ascii
}

// A page Markdown "v1" write keeps the address a link is written with as it
// is, as Notion's does (seen 2026-11-11, read back as written and in its
// JSON as notionAPILinkAddress reads it): "a b.com", "<a b.com>", "example",
// "./x", " example.com", "  https://example.com/x.png " and "https://" link
// so. It makes no link of an address of nothing but whitespace, one naming a
// scheme other than http, https, mailto or tel (javascript:, data:, ftp:,
// sms:, a:b, localhost:3000, 1a:b; read without the tabs and line breaks a
// browser leaves out), one after // with no host, or a web one whose host a
// browser doesn't open: one holding a space or another character no host
// holds, as written or percent-encoded ("https://exa mple.com",
// "https://ex%20ample.com", "https://exa|mple.com"), or a port that is no
// number up to 65535. A "v1" table cell's link also keeps a web address
// whose host a space parts (graphWrittenCellTextLink). A span holds such a
// link (validGraphHeldTextLink), which the app opens as textLinkHref takes
// it; the API and the editor link only to what validGraphTextLink takes.

// validGraphHeldTextLink is whether a span of text may hold value as its
// link: one it may link to, an address a "v1" write kept as written, or one
// a JSON write took as written (graphRequestTextLink).
func validGraphHeldTextLink(value string) bool {
	return validGraphTextLink(value) || graphKeptTextLink(value) || graphRequestTextLink(value)
}

// A JSON write through the public API takes a text link's address as
// Notion's does (seen 2026-11-11, some 230 addresses), keeping it as written
// whitespace and all, which the app opens as textLinkHref takes it. With
// whitespace taken off its ends, it is an address that holds, read from its
// start:
//   - mailto: or tel: at the start of a word (any case): taken, as
//     "mailto:", "zz mailto:a" and "tel:1 2" are;
//   - else http://, https://, ftp:// or file:// at the start of a word, and
//     words after it: what stands up to the next whitespace decides, taken
//     when it is an http or https address a browser's URL parser takes
//     (graphRequestWebAddress): "https://a b" and "zz https://a" are taken,
//     "https://a%20b.com", "https://a|b.com c.com", "https://a.123" and
//     "ftp://a.com" are not;
//   - else a host anywhere in it (graphRequestFoundHost), the first one
//     deciding: "a b.com", "a%20b.com", "%3Ca%20b.com%3E", "localhost a",
//     "b.com-x", "/a b.com", "#a.com" and "javascript:a b.com" are taken,
//     "a b", "a%20b.example", "b_.com", "127.0.0.1 a" and "a b.com:99999" are
//     not.
// Anything else is not: "example", "./x", "#x", "/p/abc", "sms:1",
// "javascript:x".

var (
	graphRequestMailPattern    = regexp.MustCompile(`(?:^|[^\p{L}\p{N}])(?:mailto|tel):`)
	graphRequestSchemedPattern = regexp.MustCompile(`(?:^|[^\p{L}\p{N}])(https?|ftp|file)://`)
	graphRequestWebHostProfile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.CheckJoiners(true), idna.Transitional(false),
		idna.StrictDomainName(false), idna.CheckHyphens(false), idna.VerifyDNSLength(false))
)

// graphRequestTextLink is whether a JSON write takes value as a text link's
// address, as Notion's does.
func graphRequestTextLink(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxGraphExternalFileURLBytes {
		return false
	}
	lower := strings.ToLower(value)
	if graphRequestMailPattern.MatchString(lower) {
		return true
	}
	for _, match := range graphRequestSchemedPattern.FindAllStringSubmatchIndex(lower, -1) {
		rest := lower[match[1]:]
		if end := strings.IndexFunc(rest, unicode.IsSpace); end >= 0 {
			rest = rest[:end]
		}
		// "https://" before whitespace is no address; one after it may be.
		if rest == "" {
			continue
		}
		scheme := lower[match[2]:match[3]]
		return (scheme == "http" || scheme == "https") && graphRequestWebAddress(rest)
	}
	return graphRequestFoundHost(lower)
}

// graphRequestWebAddress is whether what follows an http or https address's
// "://" is one a browser's URL parser takes: after any more slashes or
// backslashes, a user before the last @, then a host it reads —
// percent-decoded, its names in punycode, none of the characters no host
// holds, an IPv4 address where its last name is a number — and a port of
// digits up to 65535 or none.
func graphRequestWebAddress(rest string) bool {
	rest = strings.TrimLeft(rest, `/\`)
	if end := strings.IndexAny(rest, `/\?#`); end >= 0 {
		rest = rest[:end]
	}
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		rest = rest[at+1:]
	}
	if strings.HasPrefix(rest, "[") {
		return graphWrittenWebHost(rest, false)
	}
	host, port, _ := strings.Cut(rest, ":")
	if port != "" && !graphRequestPort(port) {
		return false
	}
	decoded, err := url.PathUnescape(host)
	if err != nil || decoded == "" || !utf8.ValidString(decoded) {
		return false
	}
	ascii, err := graphRequestWebHostProfile.ToASCII(decoded)
	if err != nil || ascii == "" || strings.ContainsFunc(ascii, func(r rune) bool {
		return r <= ' ' || r == 0x7f || strings.ContainsRune(`#%/:<>?@[\]^|`, r)
	}) {
		return false
	}
	names := strings.Split(ascii, ".")
	if len(names) > 1 && names[len(names)-1] == "" {
		names = names[:len(names)-1]
	}
	last := names[len(names)-1]
	if _, number := graphRequestIPv4Number(last); !number && (last == "" || strings.Trim(last, "0123456789") != "") {
		return true
	}
	// A host whose last name is a number is an IPv4 address or none.
	if len(names) > 4 {
		return false
	}
	for index, name := range names {
		value, number := graphRequestIPv4Number(name)
		limit := uint64(255)
		if index == len(names)-1 {
			limit = 1<<(8*(5-len(names))) - 1
		}
		if !number || value > limit {
			return false
		}
	}
	return true
}

// graphRequestIPv4Number reads a name of an IPv4 address as a browser does:
// decimal, hexadecimal after 0x, or octal after a 0.
func graphRequestIPv4Number(name string) (uint64, bool) {
	if name == "" {
		return 0, false
	}
	base := 10
	switch lower := strings.ToLower(name); {
	case strings.HasPrefix(lower, "0x"):
		name, base = name[2:], 16
	case len(name) > 1 && name[0] == '0':
		name, base = name[1:], 8
	}
	if name == "" {
		return 0, true
	}
	value, err := strconv.ParseUint(name, base, 64)
	if err != nil {
		// Beyond 64 bits, it is still a number, too big for any address.
		var numeric *strconv.NumError
		return 1 << 63, errors.As(err, &numeric) && numeric.Err == strconv.ErrRange
	}
	return value, true
}

// graphRequestPort is whether a port's digits name one: up to 65535.
func graphRequestPort(port string) bool {
	if strings.Trim(port, "0123456789") != "" {
		return false
	}
	port = strings.TrimLeft(port, "0")
	number, err := strconv.Atoi(port)
	return port == "" || err == nil && number <= 65535
}

// graphRequestFoundHost is whether a host stands in value (in lowercase): a
// name of letters or digits, a dot and a top-level domain — in ASCII letters
// up to the next character that is none, or in letters of its own script
// (b.com1, b.comé, b.рф) — or localhost between characters that are no
// ASCII letters; a port after it must be one. The first host decides.
func graphRequestFoundHost(value string) bool {
	for index := 0; index < len(value); index++ {
		end := 0
		switch {
		case value[index] == '.':
			before, _ := utf8.DecodeLastRuneInString(value[:index])
			if index == 0 || !unicode.IsLetter(before) && !unicode.IsDigit(before) {
				continue
			}
			rest := value[index+1:]
			name := rest[:len(rest)-len(strings.TrimLeftFunc(rest, func(r rune) bool { return r >= 'a' && r <= 'z' }))]
			if name == "" {
				name = rest[:len(rest)-len(strings.TrimLeftFunc(rest, func(r rune) bool {
					return r >= 0x80 && (unicode.IsLetter(r) || unicode.IsMark(r))
				}))]
			}
			if name == "" || !graphTopLevelDomain(name) {
				continue
			}
			end = index + 1 + len(name)
		case strings.HasPrefix(value[index:], "localhost") &&
			(index == 0 || value[index-1] < 'a' || value[index-1] > 'z'):
			end = index + len("localhost")
			if end < len(value) && value[end] >= 'a' && value[end] <= 'z' {
				continue
			}
		default:
			continue
		}
		after := value[end:]
		if !strings.HasPrefix(after, ":") {
			return true
		}
		digits := after[1:]
		digits = digits[:len(digits)-len(strings.TrimLeft(digits, "0123456789"))]
		return graphRequestPort(digits)
	}
	return false
}

// graphKeptTextLink is whether value is an address a "v1" write keeps as
// written as a link's, in a paragraph's text or a table cell's.
func graphKeptTextLink(value string) bool {
	return graphWrittenTextLink(value) || graphWrittenCellTextLink(value)
}

// graphWrittenCellTextLink is whether a "v1" table cell links a web address
// as written whose host a space parts, which a paragraph leaves unlinked: its
// JSON reads the host before the space and the rest as its path, as Node's
// url.parse does (seen 2026-11-10 and 2026-11-11: "[a](https://example.com
// \"t\")" in a cell links https://example.com/%20%22t%22, read back so).
func graphWrittenCellTextLink(value string) bool {
	address := strings.TrimSpace(value)
	scheme := strings.ToLower(notionLinkSchemePattern.FindString(address))
	if len(value) > maxGraphExternalFileURLBytes || scheme != "http:" && scheme != "https:" {
		return false
	}
	rest := address[len(scheme):]
	host := strings.TrimLeft(rest, `/\`)
	space := strings.IndexByte(host, ' ')
	if end := strings.IndexAny(host, `/\?#`); space <= 0 || end >= 0 && end < space {
		return false
	}
	return graphWrittenWebHost(host[:space], false)
}

// graphWrittenTextLink is whether a "v1" write links an address as written.
func graphWrittenTextLink(value string) bool {
	if len(value) > maxGraphExternalFileURLBytes {
		return false
	}
	address := strings.TrimSpace(strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(value))
	if address == "" {
		return false
	}
	switch scheme := strings.ToLower(notionLinkSchemePattern.FindString(address)); scheme {
	case "mailto:", "tel:":
		return true
	case "http:", "https:":
		return graphWrittenWebHost(address[len(scheme):], true)
	case "":
		// One after // is a web address without its scheme.
		if strings.HasPrefix(strings.ReplaceAll(address, `\`, "/"), "//") {
			return graphWrittenWebHost(address, false)
		}
		return true
	}
	return false
}

// graphWrittenWebHost is whether the host a web address names after its
// scheme is one a browser opens, as its URL parser reads it: after any
// slashes or backslashes and a user, percent-decoded, holding no space and
// none of #%/:<>?@[\]^|, or an IPv6 address between brackets, with a port of
// digits up to 65535 or none; no host at all only where empty allows it.
func graphWrittenWebHost(rest string, empty bool) bool {
	rest = strings.TrimLeft(rest, `/\`)
	if end := strings.IndexAny(rest, `/\?#`); end >= 0 {
		rest = rest[:end]
	}
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		rest = rest[at+1:]
	}
	host, port := rest, ""
	if closing := strings.IndexByte(rest, ']'); strings.HasPrefix(rest, "[") && closing > 0 {
		host, port = rest[1:closing], rest[closing+1:]
		if port != "" && port[0] != ':' || strings.Trim(host, "0123456789abcdefABCDEF:.") != "" {
			return false
		}
		port = strings.TrimPrefix(port, ":")
	} else {
		if colon := strings.LastIndexByte(rest, ':'); colon >= 0 {
			host, port = rest[:colon], rest[colon+1:]
		}
		decoded := host
		if unescaped, err := url.PathUnescape(host); err == nil {
			decoded = unescaped
		} else if strings.Contains(host, "%") {
			// A % that encodes no byte stays, which no host holds.
			return false
		}
		for index := 0; index < len(decoded); index++ {
			if decoded[index] <= ' ' || decoded[index] == 0x7f || strings.IndexByte(`#%/:<>?@[\]^|`, decoded[index]) >= 0 {
				return false
			}
		}
		if host == "" && !empty {
			return false
		}
	}
	if port == "" {
		return true
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 0 && number <= 65535 && strings.Trim(port, "0123456789") == ""
}
