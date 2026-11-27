package app

import (
	"errors"
	"net/url"
	"strings"
)

// Notion's notion-create-pages reads a new page's <unknown/> lines as the
// public API's "v2" create does (notionMarkdownCreateUnknownError): one on a
// line of its own, and one a list's item or a quote holds
// (notionMarkdownCreateHeldUnknown), refused by the first of them, a to-do's
// being its text. It says why in the API's words without the API's prefix
// (notionMarkdownCreateUnknownPrefix) and with their {…} filled in,
// {expectedUrl} with nothing (seen 2026-11-17 in two calls each: "- <unknown
// url=\"https://example.com\"/>" and "<unknown url=\"https://example.com\"/>"
// are both another site's URL, "> <unknown …/>" too, "<unknown url=\"abc\"/>"
// no URL at all, and a page here that no page is a reference it can't make,
// named by the ID dashed; one in the second of two pages is said without
// the page's place). Its reading's own refusals ("<unknown/>",
// "<unknown url=…>x</unknown>": notionMarkdownRefusedHead and its lines)
// name the page by its place, "pages[1]: …", only when the call makes more
// than one (mcpCreatePageMarkdownRefusal).

// mcpCreatePageMarkdownRefusal refuses a page's content as its reading
// refuses it, naming the page by its place among more than one.
func mcpCreatePageMarkdownRefusal(pages, index int, reason string) error {
	if pages > 1 {
		return mcpInvalid("pages[%d]: %s", index, reason)
	}
	return mcpInvalid("%s", reason)
}

// mcpCreatePagesItemRefusal is a refusal of one of a call's several pages,
// said before any is made, as mcpCreatePageMarkdownRefusal words it: its
// place in pages before why, and that the call made none, so the agent fixes
// that one and sends them all again (2026-11-26: an agent making 200 rows,
// told only why, split them into fives, then ones, to find which). A call
// of one page, and an error of another kind, is as it was.
func mcpCreatePagesItemRefusal(err error, pages, index int) error {
	if pages < 2 || !errors.Is(err, errMCPToolInvalid) {
		return err
	}
	reason := strings.TrimPrefix(err.Error(), errMCPToolInvalid.Error()+": ")
	return mcpCreatePageMarkdownRefusal(pages, index, strings.TrimSuffix(reason, ".")+" — none of the call's pages was made")
}

// mcpCreatePageContentRefusal refuses a new page's content before its
// blocks are planned: what its "v2" reading refused (read), then a synced
// block reference that would change its original, then a list item or quote
// holding an <unknown/> line before any line of its own holds one; nil
// otherwise.
func mcpCreatePageContentRefusal(content string, read error, origin string, pages, index int) error {
	if read != nil {
		return mcpCreatePageMarkdownRefusal(pages, index, read.Error())
	}
	// It may hold a synced block reference, not change its original.
	if refused := notionMarkdownSyncedRefusal(content, true, false); refused != "" {
		return mcpInvalid("%s", refused)
	}
	return mcpCreatePageHeldUnknown(content, origin, pages, index)
}

// mcpCreateRowContentRefusal refuses a new database page's content before
// any page is made, as Notion's does (seen 2026-11-17: no row made), its
// body being written after the page is (createMCPDatabasePages): what its
// "v2" reading refused, then an <unknown/> line an item or quote holds, then
// one of its own; nil otherwise.
func mcpCreateRowContentRefusal(content, origin string, pages, index int) error {
	if content == "" {
		return nil
	}
	read, _, err := notionMarkdownVersion2Input(content)
	if err != nil {
		return mcpCreatePageMarkdownRefusal(pages, index, err.Error())
	}
	if refused := mcpCreatePageHeldUnknown(content, origin, pages, index); refused != nil {
		return refused
	}
	_, err = notionMarkdownReplacementPlan(read, &notionPageMarkdownProjection{SitePages: notionMarkdownSitePagesAt(origin, true)}, false)
	if unknown := (notionMarkdownNewUnknownError{}); errors.As(err, &unknown) {
		return mcpCreatePageUnknownRefusal(unknown, origin, pages, index)
	}
	return nil
}

// mcpCreatePageHeldUnknown refuses a new page whose list item or quote holds
// an <unknown/> line before any line of its own holds one; nil otherwise.
func mcpCreatePageHeldUnknown(content, origin string, pages, index int) error {
	held := notionMarkdownCreateUnknownError{}
	if !errors.As(notionMarkdownCreateHeldUnknown(content, true), &held) {
		return nil
	}
	return mcpCreatePageUnknownRefusal(held.unknown, origin, pages, index)
}

// mcpCreatePageUnknownRefusal is Notion's refusal of a new page's first
// <unknown/> line, origin being this server's.
func mcpCreatePageUnknownRefusal(unknown notionMarkdownNewUnknownError, origin string, pages, index int) error {
	answer := notionMarkdownCreateUnknownError{unknown: unknown, version2: true}.answer(origin)
	reason, addressed := strings.CutPrefix(answer, notionMarkdownCreateUnknownPrefix)
	if !addressed {
		return mcpCreatePageMarkdownRefusal(pages, index, answer)
	}
	address := ""
	if len(unknown.lines) > 0 {
		if match := notionMarkdownUnknownPattern.FindStringSubmatch(unknown.lines[0]); match != nil {
			address = strings.TrimSpace(notionMarkdownAttributes(match[1])["url"])
		}
	}
	block := ""
	if parsed, err := url.Parse(address); err == nil {
		if id := notionMarkdownAddressPageID(parsed); id != "" {
			block = id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
		}
	}
	return mcpInvalid("%s", strings.NewReplacer("{url}", address, "{blockId}", block, "{expectedUrl}", "").Replace(reason))
}
