package app

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/net/idna"
)

// A page keeps a link's address as it was written, as Notion's does: its
// Markdown writes it back so. Notion's API reads a text link's url and href
// out in another form (seen 2026-11-10), Node's url.parse and url.format
// with the query read and written again: the scheme and host in lowercase,
// a host's other letters in punycode, "/" for a web address's empty path, a
// backslash before the query a slash, the characters a URL may not hold
// bare (space " ' < > \ ^ ` { | }) percent-encoded, and the query's
// parameters written as Node's querystring writes them (?x is ?x=). The
// port, dot segments and percent-encoding already there stay. An address
// without a scheme (example.com/a b, //example.com) is read so too, all of it
// path, its letters' case kept (example.com/a%20b). A link to one of the
// app's pages, or to a block on one, reads as Notion's form for it
// (notionAPITextLink), the form a write takes (notionTextLinkRequest).

var (
	notionLinkSchemePattern = regexp.MustCompile(`^[A-Za-z0-9.+-]+:`)
	notionLinkPortPattern   = regexp.MustCompile(`:[0-9]*$`)
	// The host's names in punycode, as Node's url.domainToASCII writes them.
	notionLinkHostProfile = idna.New(idna.MapForLookup(), idna.Transitional(false), idna.StrictDomainName(false),
		idna.CheckHyphens(false), idna.VerifyDNSLength(false))
)

// notionLinkSlashedSchemes are the schemes whose address has "//" and a path.
var notionLinkSlashedSchemes = map[string]bool{
	"http:": true, "https:": true, "ftp:": true, "gopher:": true, "file:": true, "ws:": true, "wss:": true,
}

// notionLinkSimplePathPattern is a path Node's url.parse reads as it is, on
// its fast path: after / or // (not ///), up to a query, no whitespace.
var notionLinkSimplePathPattern = regexp.MustCompile(`^//?(?:[^/?\s\x0b\p{Z}\x{feff}][^?\s\x0b\p{Z}\x{feff}]*)?(?:\?[^\s\x0b\p{Z}\x{feff}]*)?$`)

// notionAPILinkAddress is a stored link's address as Notion's API reads it out.
func notionAPILinkAddress(link string) string {
	address := strings.TrimFunc(link, func(r rune) bool { return r < 33 || r == '\u00a0' || r == '\ufeff' })
	scheme := strings.ToLower(notionLinkSchemePattern.FindString(address))
	if scheme == "javascript:" {
		return link
	}
	// Backslashes before the query or fragment are slashes.
	if split := strings.IndexAny(address, "?#"); split >= 0 {
		address = strings.ReplaceAll(address[:split], `\`, "/") + address[split:]
	} else {
		address = strings.ReplaceAll(address, `\`, "/")
	}
	// A path alone, with no # in it nor a user before its query, reads as
	// it is ("//a|b.com", "/a|b"; seen 2026-11-11).
	beforeQuery, _, _ := strings.Cut(address, "?")
	if scheme == "" && !strings.Contains(address, "#") && !strings.Contains(beforeQuery, "@") &&
		notionLinkSimplePathPattern.MatchString(address) {
		return address
	}
	rest := address[len(scheme):]
	slashes := scheme != "" && strings.HasPrefix(rest, "//")
	if slashes {
		rest = rest[2:]
	}
	auth, host, hostname := "", "", ""
	if scheme != "" && (slashes || !notionLinkSlashedSchemes[scheme]) {
		auth, hostname, host, rest = notionLinkHost(rest)
	}
	rest = notionLinkEscaped(rest)
	fragment, query, hasQuery := "", "", false
	if at := strings.IndexByte(rest, '#'); at >= 0 {
		rest, fragment = rest[:at], rest[at:]
	}
	if at := strings.IndexByte(rest, '?'); at >= 0 {
		rest, query, hasQuery = rest[:at], rest[at+1:], true
	}
	path := rest
	if notionLinkSlashedSchemes[scheme] && hostname != "" && path == "" {
		path = "/"
	}
	path = strings.NewReplacer("#", "%23", "?", "%3F").Replace(path)
	out := strings.Builder{}
	out.WriteString(scheme)
	if slashes || notionLinkSlashedSchemes[scheme] && host != "" {
		out.WriteString("//")
		if path != "" && path[0] != '/' {
			path = "/" + path
		}
	}
	if host != "" {
		if auth != "" {
			out.WriteString(notionLinkAuthEscaped(auth) + "@")
		}
		out.WriteString(host)
	}
	out.WriteString(path)
	if search := notionLinkQuery(query, hasQuery); search != "" {
		out.WriteString("?" + strings.ReplaceAll(search, "#", "%23"))
	}
	out.WriteString(fragment)
	return out.String()
}

// notionPageLinkPattern is Notion's form for a link to a page, or to a block
// on one, by their IDs with no dashes: the only path its API takes for a
// link, any other refused as an anchor is (seen 2026-11-10).
var notionPageLinkPattern = regexp.MustCompile(`^/([0-9a-f]{32})(?:#([0-9a-f]{32}))?$`)

// notionTextLinkRequest is a text link's address as a write through the
// public API keeps it, or Notion's refusal of it: an address Notion's JSON
// write takes (graphRequestTextLink) as written, whitespace about it and all
// (seen 2026-11-11: "  example.com" reads back so in "v1"), and a link to a
// page or a block on one in Notion's form kept as the app's own link to it.
func notionTextLinkRequest(raw string) (string, error) {
	link := strings.TrimSpace(raw)
	if match := notionPageLinkPattern.FindStringSubmatch(link); match != nil {
		return notionAppPageLink(match[1], match[2]), nil
	}
	if !graphRequestTextLink(link) {
		return "", &notionMentionRefusal{invalidLink: true}
	}
	return raw, nil
}

// notionAppPageLink is the app's link to a page, or to a block on it, that a
// Notion-form link names by public IDs: the page by its record's ID where its
// public ID says it, else by the public ID, which the app asks the server to
// open (page_links.go); the block by its ID, none for the page itself.
func notionAppPageLink(pageHex, blockHex string) string {
	publicID := notionLinkedID(pageHex)
	pageID, err := resolvePageOwnPublicID(publicID)
	if err != nil {
		pageID = publicID
	}
	link := "/p/" + pageID
	if block := notionLinkedID(blockHex); block != "" && !sameAddressID(block, publicID) && !sameAddressID(block, pageID) {
		link += "#block-" + block
	}
	return link
}

// notionAppAddressPattern is an address kept at the app's address
// (notionMarkdownAppAddress): /p/ and an ID's 32 hexadecimal digits, a
// block's after # or none.
var notionAppAddressPattern = regexp.MustCompile(`^/p/[0-9a-f]{32}(?:#[0-9a-f]{32})?$`)

// notionMarkdownAppAddress is the address a page Markdown write keeps for
// one of Notion's naming a page, a database, a data source (collection://ID)
// or a block by its ID, which
// Notion's write keeps at its app's address, https://app.notion.com/p/ID
// (seen 2026-11-11, "v1", "v2" and its MCP alike): a web address of Notion's
// (notionWebAddress, with any user or port, its scheme or none) or a path
// in its app, whose last part ends in the ID in lowercase, maybe after a
// title, and after the workspace's name or p/ (notionAddressPageIDs; not
// /pages/ID, which names no page there). Its query is left off, and its
// anchor too unless it names a block by its ID: Notion's (#ID), and in a
// path the app's own (#block-ID; appAnchorBlockID), as this server's full
// address keeps it, where Notion's write leaves #block- off its addresses
// (seen 2026-11-23, "v2"). The ID is kept as a path, /p/ and its 32
// hexadecimal digits, that reads with this server's address before it
// (notionAPITextLink), as Notion's reads its app's — a web address only
// where its ID answers here (notion_address_links_here.go).
func notionMarkdownAppAddress(raw string) (string, bool) {
	address := strings.TrimSpace(raw)
	if address == "" || strings.ContainsFunc(address, unicode.IsSpace) {
		return "", false
	}
	// A data source's address links its ID there too (seen 2026-11-11:
	// "[a](collection://ID)"), and a view's (seen 2026-11-13:
	// "[a](view://ID)"; notionMarkdownViewAddress).
	source, collection := strings.CutPrefix(address, notionMarkdownDataSourceAddress)
	if view, viewed := strings.CutPrefix(address, notionMarkdownViewAddress); viewed {
		source, collection = view, true
	}
	if collection {
		id, named := normalizeNotionUUIDRequest(source)
		if !named {
			return "", false
		}
		return "/p/" + strings.ReplaceAll(id, "-", ""), true
	}
	inApp := strings.HasPrefix(address, "/") && !strings.HasPrefix(address, "//")
	if !inApp {
		switch {
		case strings.HasPrefix(address, "//"):
			address = "https:" + address
		case !notionLinkSchemePattern.MatchString(address):
			address = "https://" + address
		}
		parsed, err := url.Parse(address)
		if err != nil || !notionWebAddress(parsed) {
			return "", false
		}
		address = parsed.Path
		if parsed.Fragment != "" {
			address += "#" + parsed.Fragment
		}
		if address == "" {
			return "", false
		}
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", false
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasPrefix(path, "/pages/") {
		return "", false
	}
	pageID, blockID, named := notionAddressPageIDs(path, parsed.Fragment, true)
	if !named {
		return "", false
	}
	if own := appAnchorBlockID(parsed.Fragment, pageID); blockID == "" && inApp && notionAddressBlockPattern.MatchString(own) {
		blockID = notionLinkedID(own)
	}
	kept := "/p/" + strings.ReplaceAll(pageID, "-", "")
	if blockID != "" {
		kept += "#" + strings.ReplaceAll(blockID, "-", "")
	}
	return kept, true
}

// notionAppAddressLink is whether a stored link is one kept at the app's
// address (notionMarkdownAppAddress).
func notionAppAddressLink(link string) bool {
	return notionAppAddressPattern.MatchString(link)
}

// notionMarkdownOwnPageRef is whether an ID in the app's own form of a
// page's address is one a page of the app could have: a page's record ID or
// a public ID (pageOwnPublicID), or Notion's form of one.
func notionMarkdownOwnPageRef(ref string) bool {
	if _, err := pageOwnPublicID(ref); err == nil {
		return true
	}
	_, _, named := notionAddressPageIDs("/"+ref, "", false)
	return named
}

// notionAPITextLink is a stored text link's address as Notion's API reads it
// out for a connection of the workspace, its JSON and its page Markdown
// alike: the app's link to a page (/p/ID), or to a block on one
// (/p/ID#block-ID, or #block-ID on the page read, pageID), in Notion's form
// for it (/ID, /ID#block, by their IDs without dashes), so that what a read
// gives writes back; any other as notionAPILinkAddress reads it — Notion's
// own anchor (#block, as a page Markdown write keeps it) as written. One
// with whitespace about it, as a "v1" write keeps it, names no page. A path
// in the app that names no page, which no JSON write takes as written
// (/p/abc, /x/y), reads with the server's address, origin, before it, as
// Notion's reads its app's address before one a page Markdown write made
// (seen 2026-11-11: "[a](/p/abc)" reads https://app.notion.com/p/abc); the
// page keeps the path alone, so a server moved to another address reads its
// new one. One a page Markdown write kept at the app's
// address (notionAppAddressLink) reads with the server's address before it,
// as Notion's reads its app's address (https://app.notion.com/p/ID). Without
// an origin it reads as written.
func notionAPITextLink(app core.App, workspaceID, pageID, link, origin string) string {
	if notionAppAddressLink(link) {
		return notionAPILinkAddress(origin + link)
	}
	if link == strings.TrimSpace(link) {
		if read, ok := notionAPIAppLink(app, workspaceID, pageID, link); ok {
			return read
		}
	}
	if origin != "" && notionAppPathLink(link) {
		return notionAPILinkAddress(origin + link)
	}
	return notionAPILinkAddress(link)
}

// notionAppPathLink is whether a link is a path in the app, whitespace
// after it or not (as Notion's, seen 2026-11-11: "[a](/p/abc )" and
// "[a](/\a/b)" have its app's address before them, "[a]( /p/abc)" and
// "[a](//a)" not), that no JSON write takes as written: not Notion's form of
// a page link (/ID) nor a path that holds a host (/a.com).
func notionAppPathLink(link string) bool {
	return strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//") &&
		!notionPageLinkPattern.MatchString(strings.TrimSpace(link)) && !graphRequestTextLink(link)
}

// notionAPIAppLink is the app's link to a page or a block in Notion's form
// (notionAPITextLink); false for any other link.
func notionAPIAppLink(app core.App, workspaceID, pageID, link string) (string, bool) {
	address := strings.TrimSpace(link)
	if strings.HasPrefix(address, "#block-") && pageID != "" {
		address = "/p/" + pageID + address
	}
	return notionAPIAppPageLink(app, workspaceID, address)
}

func notionAPIAppPageLink(app core.App, workspaceID, link string) (string, bool) {
	path, anchor, _ := strings.Cut(link, "#")
	ref, named := strings.CutPrefix(path, "/p/")
	if !named {
		return "", false
	}
	ref, err := url.PathUnescape(ref)
	if err != nil || !workspaceImportReferenceIDPattern.MatchString(ref) {
		return "", false
	}
	pageID, err := notionPagePublicID(app, ref)
	if err != nil {
		// A public ID the app asks the server to open, as written.
		normalized, isUUID := normalizeNotionUUIDRequest(ref)
		if !isUUID {
			return "", false
		}
		pageID = normalized
	}
	address := "/" + strings.ReplaceAll(pageID, "-", "")
	// The block by the app's anchor (#block-ID), or by Notion's, as page
	// Markdown named a block (#ID, with or without dashes); an anchor that
	// names none reads as the page.
	blockID := ""
	if blockRef, own := strings.CutPrefix(anchor, "block-"); own && workspaceImportReferenceIDPattern.MatchString(blockRef) {
		blockID, _ = notionBlockPublicID(app, workspaceID, blockRef)
	} else if notionAddressBlockPattern.MatchString(anchor) {
		blockID = notionLinkedID(anchor)
	}
	if blockID == "" {
		return address, true
	}
	return address + "#" + strings.ReplaceAll(blockID, "-", ""), true
}

// notionLinkHost takes an address's authority off its rest: the user, the
// host's name in lowercase and punycode, the host with its port, and the
// rest after it. A tab or line break goes from it, as Node's url.parse
// takes them out up to where the host ends ("https://exa\tmple.com" is
// https://example.com/, seen 2026-11-11).
func notionLinkHost(rest string) (auth, hostname, host, after string) {
	end, at, nonHost := -1, -1, -1
	for index := 0; index < len(rest) && end < 0; index++ {
		switch rest[index] {
		case '\t', '\n', '\r':
			rest = rest[:index] + rest[index+1:]
			index--
		case ' ', '"', '%', '\'', ';', '<', '>', '\\', '^', '`', '{', '|', '}':
			if nonHost < 0 {
				nonHost = index
			}
		case '#', '/', '?':
			if nonHost < 0 {
				nonHost = index
			}
			end = index
		case '@':
			at, nonHost = index, -1
		}
	}
	start := 0
	if at >= 0 {
		auth, _ = url.PathUnescape(rest[:at])
		start = at + 1
	}
	if nonHost < 0 {
		host, after = rest[start:], ""
	} else {
		host, after = rest[start:nonHost], rest[nonHost:]
	}
	port := notionLinkPortPattern.FindString(host)
	hostname = host[:len(host)-len(port)]
	if port == ":" {
		port = ""
	}
	ipv6 := strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]")
	if !ipv6 {
		// A name ends at a character a host can't hold; the rest is path.
		if cut := strings.IndexAny(hostname, `/\#?:`); cut >= 0 {
			after = "/" + hostname[cut:] + after
			hostname = hostname[:cut]
		}
		hostname = strings.ToLower(hostname)
		if len(hostname) > 255 {
			hostname = ""
		}
		if hostname != "" {
			if ascii, err := notionLinkHostProfile.ToASCII(hostname); err == nil {
				hostname = ascii
			} else {
				hostname = ""
			}
		}
	} else if after == "" || after[0] != '/' {
		after = "/" + after
	}
	return auth, hostname, hostname + port, after
}

// notionLinkEscaped percent-encodes the characters an address may not hold
// bare, as Node's url.parse does.
func notionLinkEscaped(rest string) string {
	return strings.NewReplacer("\t", "%09", "\n", "%0A", "\r", "%0D", " ", "%20", `"`, "%22", "'", "%27", "<", "%3C",
		">", "%3E", `\`, "%5C", "^", "%5E", "`", "%60", "{", "%7B", "|", "%7C", "}", "%7D").Replace(rest)
}

// notionLinkAuthEscaped writes an address's user as Node's url.format does.
func notionLinkAuthEscaped(auth string) string {
	out := strings.Builder{}
	for index := 0; index < len(auth); index++ {
		character := auth[index]
		if notionLinkUnreserved(character) || strings.IndexByte("!'()*:", character) >= 0 {
			out.WriteByte(character)
			continue
		}
		out.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(character)|0x100, 16)[1:]))
	}
	return out.String()
}

func notionLinkUnreserved(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || strings.IndexByte("-._~", character) >= 0
}

// notionLinkQuery is a query read and written again as Node's querystring
// does: its parameters in order, those named by a whole number first, each
// name and value percent-encoded but for letters, digits and -_.!~*'().
func notionLinkQuery(query string, present bool) string {
	if !present || query == "" {
		return ""
	}
	type parameter struct {
		name   string
		values []string
	}
	parameters := []*parameter{}
	byName := map[string]*parameter{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		name, value = notionLinkQueryUnescaped(name), notionLinkQueryUnescaped(value)
		if found, ok := byName[name]; ok {
			found.values = append(found.values, value)
			continue
		}
		byName[name] = &parameter{name: name, values: []string{value}}
		parameters = append(parameters, byName[name])
	}
	index := func(name string) (int, bool) {
		number, err := strconv.ParseUint(name, 10, 32)
		return int(number), err == nil && number < 1<<32-1 && strconv.FormatUint(number, 10) == name
	}
	slices.SortStableFunc(parameters, func(left, right *parameter) int {
		leftIndex, leftNumbered := index(left.name)
		rightIndex, rightNumbered := index(right.name)
		switch {
		case leftNumbered && rightNumbered:
			return leftIndex - rightIndex
		case leftNumbered:
			return -1
		case rightNumbered:
			return 1
		}
		return 0
	})
	written := []string{}
	for _, parameter := range parameters {
		for _, value := range parameter.values {
			written = append(written, notionLinkQueryEscaped(parameter.name)+"="+notionLinkQueryEscaped(value))
		}
	}
	return strings.Join(written, "&")
}

func notionLinkQueryUnescaped(value string) string {
	value = strings.ReplaceAll(value, "+", " ")
	if unescaped, err := url.PathUnescape(value); err == nil {
		return unescaped
	}
	return value
}

func notionLinkQueryEscaped(value string) string {
	out := strings.Builder{}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if notionLinkUnreserved(character) || strings.IndexByte("!'()*", character) >= 0 {
			out.WriteByte(character)
			continue
		}
		out.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(character)|0x100, 16)[1:]))
	}
	return out.String()
}
