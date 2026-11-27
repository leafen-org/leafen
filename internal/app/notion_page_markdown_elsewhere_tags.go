package app

import (
	"net/url"
	"strings"
)

// A <page> or <database> tag naming none of the page's own sub-pages and
// databases by its url is refused, as Notion refuses it, "v1" and "v2" alike
// (seen 2026-11-15), unless it names a page of this site, which a page tag
// mentions (planNotionMarkdownTags). A new page's Markdown says why its
// address makes no block, as an <unknown/> line's would
// (notionMarkdownCreateUnknownAddress: "<page url=\"https://example.com\">x</page>"
// and "<database url=\"https://example.com\">x</database>" an address on
// another site, "<page url=\"mailto:…\">x</page>" one of another scheme,
// "<page url=\"abc\">x</page>" a bare name; closed in itself, between blank
// lines, or a tab or two spaces in, the same). An update says it can't add
// one so, with a page's or a database's corrections and how "v1" or "v2"
// mentions one inline (notionMarkdownElsewhereError.words), a bare name
// that it is none (notionMarkdownMarkedUnknownRefusal).

// notionMarkdownElsewhereError refuses a tag naming no block of the page.
type notionMarkdownElsewhereError struct {
	tag, address string
	// version1 is whether the update's Markdown is the public API's "v1".
	version1 bool
}

func (e notionMarkdownElsewhereError) Error() string {
	if parsed, err := url.Parse(e.address); e.address != "" && err == nil && parsed.Scheme == "" && !strings.HasPrefix(e.address, "/") {
		return notionMarkdownMarkedUnknownRefusal(e.address)
	}
	if e.tag == "page" {
		mention := "`<mention url=\"...\" />`"
		if e.version1 {
			mention = "`<mention-page>`"
		}
		return "A page can't be added by writing its tag with a URL: a tag that carries a URL keeps a block that is " +
			"already there, and is no link. What to write instead:\n" +
			"- `<page url=\"...\">` stands for a page block where the page's content has it. To move the page within " +
			"this page, move its tag to the new place in the Markdown, taking it out where it was. To give the page " +
			"another parent, update that page's `parent`. A page just made under this page (parent.type=\"page\") already " +
			"has its block at the end of this page's content: read this page, then in one update of it take that " +
			"`<page url=\"...\">` tag from the end and put it where it belongs, with two content updates in the one " +
			"call if needed. Where a page block was asked for, keep the block rather than a link. " +
			"To point to a page inside text, use " + mention + " or a link."
	}
	mention := "`<mention url=\"...\" />`"
	if e.version1 {
		mention = "`<mention-database>`"
	}
	return "A database can't be added by writing its tag with a URL: a tag that carries a URL keeps a block that is " +
		"already there, and is no link. What to write instead:\n" +
		"- `<database>` makes no new database. To move a database within this page, move its tag to the new place " +
		"in the Markdown. To show a database from another page, make a linked view of it from its `url` or " +
		"`data-source-url`. To point to a database inside text, use " +
		mention + " or a link."
}

// created is a new page's refusal of the tag, its address on origin's site;
// one naming a block of this site, which a database tag names elsewhere, is
// an update's.
func (e notionMarkdownElsewhereError) created(origin string) string {
	if e.address == "" || notionMarkdownCreateUnknownAddress(e.address, origin) == notionMarkdownCreateUnknownReference {
		return e.Error()
	}
	return notionMarkdownCreateUnknownPrefix + notionMarkdownCreateUnknownAddress(e.address, origin)
}

// notionMarkdownMentionsElsewhere tells a page tag's address that names a
// page of site, which it mentions; any address does where the site is
// unknown.
func notionMarkdownMentionsElsewhere(address string, site *notionMarkdownSitePages) bool {
	return site == nil || notionMarkdownCreateUnknownAddress(address, site.Origin) == notionMarkdownCreateUnknownReference
}
