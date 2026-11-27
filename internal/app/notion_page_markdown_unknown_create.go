package app

import (
	"net/url"
	"regexp"
	"strings"
)

// A new page's Markdown can't make a block from an <unknown/> line, whatever
// it names, and Notion says why by the first such line (seen 2026-11-10 and
// 2026-11-13, "v1" and "v2" alike, its messages' {…} left as Notion
// leaves them, in Leafen's words): an address that names a page here is a reference it can't make;
// one on another site, or here naming no page, or naming one, a view or a
// block by an ID no UUID could be, isn't a page's or a view's
// (notionMarkdownAddressPage); one of another scheme isn't the web's; one
// without a host or scheme doesn't parse, and a bare name is no address.
// "v2" first refuses each line without its url, or with an empty one, as it
// reads the Markdown; "v1" fails to make a block of it. One holding text
// between its tags "v2" refuses, unread; "v1" reads it as text.

const (
	notionMarkdownCreateUnknownPrefix     = "Couldn't make blocks from the Markdown: "
	notionMarkdownCreateUnknownFailed     = "one of them failed"
	notionMarkdownCreateUnknownReference  = `A database reference can't be made: this workspace has no block {blockId} from URL "{url}"`
	notionMarkdownCreateUnknownOtherSite  = "Not the URL of a page or database: {url}"
	notionMarkdownCreateUnknownNotWeb     = "Not a web URL: {url}"
	notionMarkdownCreateUnknownUnparsed   = "This URL can't be read: {url}{expectedUrl}"
	notionMarkdownCreateUnknownShorthand  = `"{url}" is neither a full URL nor an ID or short form this can read{expectedUrl}`
	notionMarkdownCreateUnknownMissingURL = `<unknown> needs a url, as in <unknown url="...">...</unknown>.`
	notionMarkdownCreateUnknownEmptyURL   = `<unknown> has an empty "url".`
	notionMarkdownCreateUnknownPaired     = "<unknown> closes itself: write it as <unknown />."
)

// notionMarkdownCreateUnknownText escapes the <unknown> lines holding text
// between their tags in a new page's "v1" Markdown, which reads them as
// text (seen 2026-11-10).
func notionMarkdownCreateUnknownText(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := notionMarkdownUnknownPattern.FindStringSubmatch(trimmed); match != nil && !strings.HasSuffix(trimmed, "/>") {
			lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[index] = lead + `\` + trimmed
		}
	}
	return strings.Join(lines, "\n")
}

// notionMarkdownUUIDPattern is an ID a UUID could be: a version from 1 to 8
// and the standard variant, or all zeros or all f.
var notionMarkdownUUIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{12}[1-8][0-9a-f]{3}[89ab][0-9a-f]{15}|0{32}|f{32})$`)

// notionMarkdownCreateUnknownError is a new page's Markdown holding
// <unknown/> lines, in the Markdown version it was written in.
type notionMarkdownCreateUnknownError struct {
	unknown  notionMarkdownNewUnknownError
	version2 bool
}

func (e notionMarkdownCreateUnknownError) Error() string { return e.answer("") }

// answer is Notion's answer, an address here being one on origin.
func (e notionMarkdownCreateUnknownError) answer(origin string) string {
	if e.version2 {
		faults := []string{}
		for _, line := range e.unknown.lines {
			match := notionMarkdownUnknownPattern.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			address, named := notionMarkdownAttributes(match[1])["url"]
			switch {
			case !strings.HasSuffix(line, "/>"):
				faults = append(faults, notionMarkdownCreateUnknownPaired)
			case !named:
				faults = append(faults, notionMarkdownCreateUnknownMissingURL)
			case address == "":
				faults = append(faults, notionMarkdownCreateUnknownEmptyURL)
			}
		}
		if len(faults) > 0 {
			return notionMarkdownRefusedHead + strings.Join(faults, "\n")
		}
	}
	if len(e.unknown.lines) == 0 {
		return notionMarkdownCreateUnknownPrefix + notionMarkdownCreateUnknownFailed
	}
	match := notionMarkdownUnknownPattern.FindStringSubmatch(e.unknown.lines[0])
	address := ""
	if match != nil {
		address = strings.TrimSpace(notionMarkdownAttributes(match[1])["url"])
	}
	return notionMarkdownCreateUnknownPrefix + notionMarkdownCreateUnknownAddress(address, origin)
}

// notionMarkdownCreateUnknownAddress is why an <unknown/> line's address
// makes no block.
func notionMarkdownCreateUnknownAddress(address, origin string) string {
	if address == "" {
		return notionMarkdownCreateUnknownFailed
	}
	parsed, err := url.Parse(address)
	here, _ := url.Parse(origin)
	switch {
	case err != nil:
		return notionMarkdownCreateUnknownUnparsed
	case parsed.Scheme == "" && !strings.HasPrefix(address, "/"):
		return notionMarkdownCreateUnknownShorthand
	case parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https":
		return notionMarkdownCreateUnknownNotWeb
	case parsed.Scheme != "" && parsed.Host == "":
		return notionMarkdownCreateUnknownUnparsed
	case parsed.Host != "" && (here == nil || !strings.EqualFold(parsed.Host, here.Host)):
		return notionMarkdownCreateUnknownOtherSite
	case !notionMarkdownAddressPage(parsed):
		return notionMarkdownCreateUnknownOtherSite
	}
	return notionMarkdownCreateUnknownReference
}

// notionMarkdownAddressPage is whether an address here names a page, and a
// view and a block of it, as Notion reads an <unknown/> line's (seen
// 2026-11-13): its path's last part, after a part or none before it once a
// leading "p" is left out, ends in the page's ID — 32 hexadecimal digits in small letters, after its title
// or not, or the whole part a dashed UUID in either case — one a UUID could
// be; the view its v names, or else the block its fragment names, when it
// is 32 such digits and nothing more, by an ID a UUID could be too.
func notionMarkdownAddressPage(parsed *url.URL) bool {
	if notionMarkdownAddressPageID(parsed) == "" {
		return false
	}
	named := parsed.Fragment
	if view := parsed.Query().Get("v"); notionMarkdownBareIDPattern.MatchString(view) {
		named = view
	}
	return !notionMarkdownBareIDPattern.MatchString(named) || notionMarkdownUUIDPattern.MatchString(named)
}

// notionMarkdownAddressPageID is the ID of the page an address's path
// names, as notionMarkdownAddressPage reads it, in 32 small hexadecimal
// digits; "" for none.
func notionMarkdownAddressPageID(parsed *url.URL) string {
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) > 1 && parts[0] == "p" {
		parts = parts[1:]
	}
	if len(parts) > 2 {
		return ""
	}
	last := parts[len(parts)-1]
	page := ""
	if notionMarkdownDashedUUIDPattern.MatchString(last) {
		page = strings.ToLower(strings.ReplaceAll(last, "-", ""))
	} else if found := notionMarkdownTrailingIDPattern.FindString(last); found != "" {
		page = found
	}
	if !notionMarkdownUUIDPattern.MatchString(page) {
		return ""
	}
	return page
}

var (
	notionMarkdownDashedUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	notionMarkdownTrailingIDPattern = regexp.MustCompile(`[0-9a-f]{32}$`)
	notionMarkdownBareIDPattern     = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// notionMarkdownAddressHex is the ID a part of an address ends in, its
// dashes left out: 32 hexadecimal digits, in small letters; "" for none.
func notionMarkdownAddressHex(part string) string {
	part = strings.ToLower(strings.ReplaceAll(part, "-", ""))
	if len(part) < 32 {
		return ""
	}
	id := part[len(part)-32:]
	for _, character := range id {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return ""
		}
	}
	return id
}

// notionMarkdownCreateHeldUnknown is the first <unknown/> line of a new
// page's Markdown that a list's item or a <quote> tag holds as its text,
// which Notion refuses as it refuses one on a line of its own, by the first
// of them all (seen 2026-11-14 and 2026-11-15: "- <unknown url=\"…\"/>",
// "1. <unknown url=\"…\"/>", "- a⏎  - <unknown url=\"…\"/>" and one in a
// callout, a toggle or a column are refused as no page's or database's URL,
// as "<unknown url=\"…\"/>" is, "- <unknown url=\"mailto:…\"/>⏎<unknown
// url=\"…\"/>" as no web URL), or nil. "v2"'s stands after the marks
// of the items and quotes it stands in ("> - <unknown url=\"…\"/>", "- >
// <unknown url=\"…\"/>", "<quote>⏎<unknown url=\"…\"/>⏎</quote>"), a to-do's
// being text, its values in double quotes; "v1"'s after an item's marks and
// a "- " to-do's box ("- [ ] <unknown url=\"…\"/>"; "* [ ] " is an item's
// text), a quote's being text, its values in single quotes or none too
// (notionMarkdownVersion1ReadTag; "- <unknown/>" says "Failed to create
// block"). Code and equations are text, and so is a "v2" line four columns
// in at the top (notionMarkdownVersion2TopWritten). A line of its own the
// reading refuses first leaves it nil. "v2"'s tags are read with the spaces
// around their attributes' = taken out (notionMarkdownVersion2SpacedAttributes).
func notionMarkdownCreateHeldUnknown(markdown string, version2 bool) error {
	if version2 {
		markdown, _, _ = notionMarkdownVersion2SpacedAttributes(markdown)
	}
	quotes, fence, equation := 0, "", false
	top := notionMarkdownVersion2TopWritten{}
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		case equation:
			equation = trimmed != "$$"
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			continue
		case trimmed == "$$":
			equation = true
			continue
		case !version2:
		case notionMarkdownCreateQuoteOpening.MatchString(trimmed):
			quotes++
			continue
		case trimmed == "</quote>" && quotes > 0:
			quotes--
			continue
		}
		marks := notionMarkdownVersion2MarkerRun.FindString(trimmed)
		if !version2 {
			marks = notionMarkdownVersion1ItemMarks(trimmed)
		}
		text := trimmed[len(marks):]
		if version2 && top.written(line) || notionMarkdownUpdateMarkedUnknown.FindString(text) == "" ||
			version2 && notionMarkdownUnknownPattern.FindString(text) == "" {
			continue
		}
		if strings.TrimSpace(marks) == "" && quotes == 0 {
			return nil
		}
		if read, ok := notionMarkdownVersion1ReadTag(text); ok && !version2 {
			text = read.written
		}
		address := notionMarkdownAttributes(text)["url"]
		return notionMarkdownCreateUnknownError{
			unknown: notionMarkdownNewUnknownError{addresses: []string{address}, lines: []string{text}}, version2: version2,
		}
	}
	return nil
}

// notionMarkdownCreateQuoteOpening is a <quote> tag alone on its line.
var notionMarkdownCreateQuoteOpening = regexp.MustCompile(`^<quote(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>$`)
