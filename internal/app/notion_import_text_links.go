package app

import (
	"net/url"
	"regexp"
	"strings"
)

// A Notion text link to the web, an email address or a phone number stays as
// written. One to a page in the workspace — a path ending in the page's ID,
// and maybe a block's, Notion's web address of the page (its "URL" paste,
// app.notion.com/p/ID#block), or an anchor naming a block on the page the
// link stands on — names the Notion page or block until the import has
// brought everything in, as a page mention does, and publishing points it at
// the imported page and block. A web address of a page the import did not
// bring keeps its address; any other link Leafen can't open loses its
// address, keeps its words and is reported.

const (
	notionImportPendingPageLinkPrefix = "/p/" + notionImportPendingPagePrefix
	notionImportPendingLinkAnchor     = "#" + notionImportPendingBlockPrefix
	// notionImportPendingLinkKept parts a pending link from the web address
	// it was written as, which it keeps when the import did not bring its
	// page.
	notionImportPendingLinkKept = " "
)

var (
	notionLinkedIDPattern   = regexp.MustCompile(`(?:^|-)([0-9a-f]{32})$`)
	notionLinkedUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// notionAddressPagePattern is a Notion address's last path part that
	// names a page: its ID, with or without dashes, maybe after its title or
	// anything else (seen 2026-11-11: Notion's Markdown reads "x<ID>" as the
	// page's address, "<ID>x" not).
	notionAddressPagePattern  = regexp.MustCompile(`^.*?([0-9a-f]{32})$|^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
	notionAddressBlockPattern = regexp.MustCompile(`^[0-9a-f]{32}$|^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// notionLinkedID reads a Notion ID from a link's path segment or anchor, in the
// dashed form the API names objects by, or "".
func notionLinkedID(value string) string {
	value = strings.ToLower(value)
	if notionLinkedUUIDPattern.MatchString(value) {
		return value
	}
	match := notionLinkedIDPattern.FindStringSubmatch(value)
	if match == nil {
		return ""
	}
	id := match[1]
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

// notionAddressPageIDs are the page, in the dashed form, and the block, ""
// for none, that a Notion address's path and fragment name, as Notion's page
// Markdown reads one (seen 2026-11-11): a path of one part, the page's ID in
// lowercase with or without its dashes, maybe after its title ("Title-ID"),
// or of two after "p/" — or, at Notion's web address (web), after the
// workspace's name; its query aside. The fragment is the block's ID, with or
// without dashes; any other fragment names the page alone. ok is false for a
// path that names no page (/ID/x, /pages/ID, an ID in capitals).
func notionAddressPageIDs(path, fragment string, web bool) (pageID, blockID string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if !strings.HasPrefix(path, "/") || len(parts) > 2 || len(parts) == 2 && parts[0] != "p" && (!web || parts[0] == "") {
		return "", "", false
	}
	match := notionAddressPagePattern.FindStringSubmatch(parts[len(parts)-1])
	if match == nil {
		return "", "", false
	}
	pageID = notionLinkedID(match[1] + match[2])
	if notionAddressBlockPattern.MatchString(fragment) {
		blockID = notionLinkedID(fragment)
	}
	return pageID, blockID, true
}

// notionWebAddress is whether a link is to Notion's own web address, which
// Notion opens as its page (notion.so, www.notion.so, app.notion.com, and
// notion.site or a site at it), as its page Markdown reads one (seen
// 2026-11-11; notion.com, www.notion.com and api.notion.com are not).
func notionWebAddress(parsed *url.URL) bool {
	if scheme := strings.ToLower(parsed.Scheme); scheme != "https" && scheme != "http" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "notion.so" || host == "www.notion.so" || host == "app.notion.com" || host == "notion.site" ||
		strings.HasSuffix(host, ".notion.site")
}

// notionImportTextLink is what a Notion text link is staged with, or "" when
// Leafen can't keep it.
func notionImportTextLink(link string) string {
	parsed, err := url.Parse(link)
	switch {
	case err != nil && (strings.HasPrefix(link, "/") || strings.HasPrefix(link, "#")):
		return ""
	case strings.HasPrefix(link, "#"):
		// A block on the page the link stands on.
		if !notionAddressBlockPattern.MatchString(parsed.Fragment) {
			return ""
		}
		return notionImportPendingPageLinkPrefix + notionImportPendingLinkAnchor + notionLinkedID(parsed.Fragment)
	case strings.HasPrefix(link, "/"):
		// A path in Notion that names no page means nothing here.
		pageID, blockID, named := notionAddressPageIDs(parsed.Path, parsed.Fragment, false)
		if !named {
			return ""
		}
		return notionImportPendingTextLink(pageID, blockID)
	case !validGraphHeldTextLink(link):
		// Any address Notion holds a span may hold: one its JSON write took
		// ("a%20b.com", "javascript:a b.com") or its "v1" write kept.
		return ""
	case err == nil && notionWebAddress(parsed):
		if pageID, blockID, named := notionAddressPageIDs(parsed.Path, parsed.Fragment, true); named {
			return notionImportPendingTextLink(pageID, blockID) + notionImportPendingLinkKept + link
		}
	}
	return link
}

func notionImportPendingTextLink(pageID, blockID string) string {
	pending := notionImportPendingPageLinkPrefix + pageID
	if blockID != "" {
		pending += notionImportPendingLinkAnchor + blockID
	}
	return pending
}

func notionImportPendingPageLink(value any) bool {
	text, _ := value.(string)
	return strings.HasPrefix(text, notionImportPendingPageLinkPrefix)
}

// resolveLink returns where a pending text link goes now: the imported page,
// at the imported block; a block by an anchor on the link's own page; the
// web address a link was written as when the import did not bring its page,
// or that page in Notion; or "" when it did not bring the block an anchor
// alone names.
func (resolver *notionImportReferenceResolver) resolveLink(value string) (string, error) {
	value, kept, _ := strings.Cut(value, notionImportPendingLinkKept)
	pageID, blockID, _ := strings.Cut(strings.TrimPrefix(value, notionImportPendingPageLinkPrefix), notionImportPendingLinkAnchor)
	if pageID == "" {
		// In Notion's form, its ID without dashes, which the app opens and
		// the API reads as written, as Notion keeps the anchor.
		nativeBlock, err := resolver.lookup(blockID, "block")
		if err != nil || nativeBlock == "" {
			return "", err
		}
		if notionLinkedUUIDPattern.MatchString(nativeBlock) {
			return "#" + strings.ReplaceAll(nativeBlock, "-", ""), nil
		}
		return "#block-" + nativeBlock, nil
	}
	nativeID, err := resolver.resolve("pageId", notionImportPendingPagePrefix+pageID)
	if err != nil {
		return "", err
	}
	if nativeID == "" {
		// A page the import did not bring: the web address the link was
		// written as, else the page in Notion (notion_import_link_outside.go).
		if kept != "" {
			return kept, nil
		}
		resolver.outside = true
		return notionImportOutsideAddress(notionImportPendingPagePrefix+pageID, notionImportPendingBlockPrefix+blockID), nil
	}
	target := "/p/" + nativeID
	if blockID != "" {
		nativeBlock, err := resolver.lookup(blockID, "block")
		if err != nil {
			return "", err
		}
		// A link at the page itself — Notion's address of a full-page
		// database with its own ID after # — opens the page (block_address_anchor.go).
		if nativeBlock != "" && !blockIsPageItself(resolver.app, nativeID, nativeBlock) {
			target += "#block-" + nativeBlock
		}
	}
	return target, nil
}
