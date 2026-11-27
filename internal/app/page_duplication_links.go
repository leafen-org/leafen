package app

import (
	"net/url"
	"strings"
)

// A copy's links name its copies as its mentions do, as Notion's duplicate
// does (seen 2026-11-23 twice, in two tabs, a page under the test page with
// a sub-page, an inline database and its row: a link to the sub-page — at
// notion.so, at app.notion.com, with a block's anchor — to the row and to
// the database became /p/ and the copy's ID, the anchor the copied block's,
// in the body and in the database's text cell alike; a link to a page
// outside the copied page kept its address). Here a link naming, by an
// address of this server's — the app's /p/ID or the title before the ID,
// the API's /pages/ID, leafen://page/ID, whatever host before them — a page,
// a row page or a database the copy carries goes to the copy's app address,
// /p/ and its 32 hexadecimal digits, its anchor at the copied block; any
// other link stays as written. A copied page's mention names the copied
// block of its anchor the same way. The rule holds wherever a copy carries
// text: a body's text (remapPageDuplicationTextDelta), a database's cells,
// titles and descriptions and a template's cells (remapPageDuplicationSpans),
// a block's own rich text (rewritePageDuplicationReferences), a copied
// row page's body and a copied database's renamed mentions.

// pageDuplicationLinkNames is the page, row page or database (by its page or
// its block) a link names on this server, and the block its anchor names,
// or "" for a link that names none here.
func pageDuplicationLinkNames(link string) (string, string) {
	address := strings.TrimSpace(link)
	if len(address) > len("leafen://page/") && strings.EqualFold(address[:len("leafen://page/")], "leafen://page/") {
		_, fragment, _ := strings.Cut(address, "#")
		return linkedPageID(address), mcpLinkedBlockID(fragment)
	}
	return mcpInternalPageID(address)
}

// collectPageDuplicationLink adds what a link names to the pages and blocks
// a copy maps: its target as a page and as a database's block, and its
// anchor's block.
func collectPageDuplicationLink(link string, pageIDs, blockIDs map[string]struct{}) {
	pageID, blockID := pageDuplicationLinkNames(link)
	if pageID == "" {
		return
	}
	pageIDs[pageID] = struct{}{}
	blockIDs[pageID] = struct{}{}
	if blockID != "" {
		blockIDs[blockID] = struct{}{}
	}
}

// pageDuplicationLinkCopy is a link pointed at the copies of what it names
// (pages: of pages and row pages; blocks: of blocks, a database by its
// block), or "" for one naming nothing the copy carries.
func pageDuplicationLinkCopy(link string, pages, blocks map[string]string) string {
	pageID, blockID := pageDuplicationLinkNames(link)
	if pageID == "" {
		return ""
	}
	target := pages[pageID]
	if target == "" {
		target = blocks[pageID]
	}
	if target == "" {
		return ""
	}
	public, err := pageOwnPublicID(target)
	if err != nil {
		public = target
	}
	address := "/p/" + strings.ReplaceAll(public, "-", "")
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err == nil && parsed.RawQuery != "" && !strings.HasPrefix(strings.ToLower(link), "leafen://") {
		address += "?" + parsed.RawQuery
	}
	if blockID == "" {
		return address
	}
	fragment := ""
	if err == nil {
		fragment = parsed.Fragment
	}
	copied := blocks[blockID]
	if copied == "" {
		return address + "#" + fragment
	}
	if strings.HasPrefix(fragment, "block-") {
		return address + "#block-" + copied
	}
	return address + "#" + strings.ReplaceAll(copied, "-", "")
}

// collectPageDuplicationTextTargets adds what a run of text names — its
// mention's page, database and anchor block, its link's page and anchor — to
// the IDs a copy maps: pages to its page and row copies, the rest to its
// block copies.
func collectPageDuplicationTextTargets(attributes map[string]any, pageIDs, blockIDs map[string]struct{}) {
	if id, ok := pageDuplicationString(attributes["pageId"]); ok {
		pageIDs[id] = struct{}{}
	}
	if id, ok := pageDuplicationString(attributes["databaseId"]); ok {
		blockIDs[id] = struct{}{}
	}
	if id, ok := pageDuplicationString(attributes["blockId"]); ok {
		blockIDs[id] = struct{}{}
	}
	if link, ok := pageDuplicationString(attributes["link"]); ok {
		collectPageDuplicationLink(link, pageIDs, blockIDs)
	}
}

// pageDuplicationTextCopyAttributes are the attributes of a run of text that
// change in the copy — a mention or a link naming what the copy carries,
// named by its copy — or nil when none does.
func pageDuplicationTextCopyAttributes(attributes map[string]any, pages, blocks map[string]string) map[string]any {
	changed := map[string]any{}
	if pageID, ok := pageDuplicationString(attributes["pageId"]); ok && pages[pageID] != "" {
		changed["pageId"] = pages[pageID]
		if blockID, ok := pageDuplicationString(attributes["blockId"]); ok && blocks[blockID] != "" {
			changed["blockId"] = blocks[blockID]
		}
	}
	if databaseID, ok := pageDuplicationString(attributes["databaseId"]); ok && blocks[databaseID] != "" {
		changed["databaseId"] = blocks[databaseID]
	}
	if link, ok := pageDuplicationString(attributes["link"]); ok {
		if copied := pageDuplicationLinkCopy(link, pages, blocks); copied != "" {
			changed["link"] = copied
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return changed
}

// collectPageDuplicationSpanTargets and pageDuplicationSpanCopy are the
// same rule for text kept as spans (a cell, a title, a description).
func collectPageDuplicationSpanTargets(spans []graphTextSpan, pageIDs, blockIDs map[string]struct{}) {
	for _, span := range spans {
		collectPageDuplicationTextTargets(map[string]any{
			"pageId": span.PageID, "databaseId": span.DatabaseID, "blockId": span.BlockID, "link": span.Link,
		}, pageIDs, blockIDs)
	}
}

func pageDuplicationSpanCopy(spans []graphTextSpan, pages, blocks map[string]string) {
	for index := range spans {
		span := &spans[index]
		changed := pageDuplicationTextCopyAttributes(map[string]any{
			"pageId": span.PageID, "databaseId": span.DatabaseID, "blockId": span.BlockID, "link": span.Link,
		}, pages, blocks)
		if value, ok := changed["pageId"].(string); ok {
			span.PageID = value
		}
		if value, ok := changed["blockId"].(string); ok {
			span.BlockID = value
		}
		if value, ok := changed["databaseId"].(string); ok {
			span.DatabaseID = value
		}
		if value, ok := changed["link"].(string); ok {
			span.Link = value
		}
	}
}
