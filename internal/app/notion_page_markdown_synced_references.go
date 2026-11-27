package app

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A synced block reference in a page's Markdown names its original by an
// address, which the write resolves as Notion's API does (seen 2026-11-10,
// "v1" and "v2"): the block the address's fragment names when the fragment
// is a block's ID, or else the block or page its path ends in, whatever
// page the path names. A reference to anything but an original synced block
// the reader may read is refused, in Leafen's words, naming what the address
// points to by Notion's API name for its kind (notionMarkdownSyncedKinds); so is an address that isn't
// a page's or a block's on this server or one of Notion's, and one naming a
// block or page here the reader may not read is one the connection can't
// reach. One naming nothing here makes a copy syncing from no block
// (synced_block_sourceless.go), as Notion's does since 2026-11-21 (a 500 on
// 2026-11-10); before, Leafen refused it.
// Leafen's own form, "#block-" and the original's ID, keeps that ID as it
// is for the writers that read it (Leafen's MCP); the public API reads it
// as Notion does, the page its path names.
//
// A new page's first body may hold a reference to a page, a database's page
// too, as Notion's create makes one (seen 2026-11-27, "v1" and "v2", page and
// data source parents): a copy of that page's body
// (synced_block_page_original.go). Any other write naming a page is refused,
// as Notion's insert and replace are. One naming a database is refused on
// every write (notion_page_markdown_synced_database.go).
//
// One of Notion's web addresses (notion.so, app.notion.com, notion.site)
// names its block or page by the same parts, as Notion takes it (seen
// 2026-11-21, "v2": a page's address with the block after #, with the page's
// title before its ID, or the block's ID as the path); by an ID nothing here
// answers to, it names what a Notion move into the workspace made from that
// Notion block or page (notionMentionedIn), as a link to it does
// (notion_address_links_here.go), and refers to that; one naming nothing a
// move brought either is a copy syncing from none, as this server's.
// Before 2026-11-21 every Notion address was refused, and a reference by
// this server's address naming a moved block by its Notion ID as one the
// connection can't reach.

// notionMarkdownSyncedAddressKey keeps a reference's address in its content
// until the write resolves it (notionMarkdownSyncedReferences).
const notionMarkdownSyncedAddressKey = "notionMarkdownSyncedAddress"

// notionMarkdownSyncedKinds are Notion's names for the kinds of block a
// reference may point to (seen 2026-11-10); another kind is named as the
// API names it.
var notionMarkdownSyncedKinds = map[string]string{
	graphBlockTypeParagraph: "text", graphBlockTypeHeading1: "header", graphBlockTypeHeading2: "sub_header",
	graphBlockTypeHeading3: "sub_sub_header", graphBlockTypeToggleHeading1: "header",
	graphBlockTypeToggleHeading2: "sub_header", graphBlockTypeToggleHeading3: "sub_sub_header",
	graphBlockTypeBulletedListItem: "bulleted_list", graphBlockTypeNumberedListItem: "numbered_list",
	graphBlockTypeLinkToPage: "alias", graphBlockTypeChildPage: "page",
}

// notionMarkdownSyncedReferences names the original of each synced block
// reference the Markdown's operations make by its address, or refuses the
// write; public is whether the public API writes it, creates whether it
// writes a new page's first body.
func notionMarkdownSyncedReferences(
	app core.App, principalID string, current pageAuthorization, allows func(core.App, pageAuthorization) bool,
	origin string, public, creates bool, operations []graphOperation,
) error {
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeSyncedBlock {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal(operation.Block.Content, &content) != nil {
			continue
		}
		address, addressed := content[notionMarkdownSyncedAddressKey].(string)
		if !addressed {
			continue
		}
		delete(content, notionMarkdownSyncedAddressKey)
		if kept, _ := content["syncedBlockId"].(string); kept == "" || public {
			blockID, pageID, err := notionMarkdownSyncedOriginal(app, principalID, current, allows, origin, address, creates)
			if err != nil {
				return err
			}
			content["syncedBlockId"], content["syncedPageId"] = blockID, pageID
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		operation.Block.Content = encoded
	}
	return nil
}

// notionMarkdownSyncedOriginal is the original synced block, and its page,
// an address names; a page named where pages may be (creates) is both.
func notionMarkdownSyncedOriginal(
	app core.App, principalID string, current pageAuthorization, allows func(core.App, pageAuthorization) bool,
	origin, address string, creates bool,
) (string, string, error) {
	invalid := graphInputError(errorCodeGraphReferenceInvalid, "Not the URL of a page or block here: "+address)
	parsed, err := url.Parse(address)
	here, hereErr := url.Parse(origin)
	notions := err == nil && notionWebAddress(parsed)
	switch {
	case err == nil && parsed.Scheme == "" && !strings.HasPrefix(address, "/"):
		return "", "", graphInputError(errorCodeGraphReferenceInvalid, `"`+address+
			`" is neither a full URL nor an ID or short form this can read: give the URL of a page or block.`)
	case !notions && (err != nil || hereErr != nil || parsed.Scheme != "http" && parsed.Scheme != "https" ||
		!strings.EqualFold(parsed.Host, here.Host)):
		return "", "", invalid
	}
	id := notionMarkdownAddressHex(parsed.Fragment)
	if len(strings.ReplaceAll(parsed.Fragment, "-", "")) != len(id) {
		id = ""
	}
	if id == "" {
		segments := strings.Split(parsed.Path, "/")
		id = notionMarkdownAddressHex(segments[len(segments)-1])
	}
	if id == "" {
		return "", "", invalid
	}
	publicID := id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	unreachable := graphInputError(errorCodeGraphReferenceInvalid, notionUnreachableBlockTarget)
	reads := func(pageID string) bool {
		page, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
		return err == nil && !page.Page.GetBool("in_trash") && page.Workspace.WorkspaceID == current.Workspace.WorkspaceID &&
			(allows == nil || allows(app, page))
	}
	points := func(kind string) error {
		return graphInputError(errorCodeGraphReferenceInvalid, "A synced block reference points to an original synced block, and "+
			address+" is a "+kind+" block: use the url a <synced_block> tag gives.")
	}
	blockID, _ := resolveNotionBlockID(app, publicID)
	block, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil {
		return "", "", err
	}
	found = found && block.WorkspaceID == current.Workspace.WorkspaceID
	pageID, pageErr := resolveNotionPageRecordID(app, publicID)
	page := !found && pageErr == nil && reads(pageID)
	// A sub-page's block names its page by the page's own ID.
	if found && block.Type == graphBlockTypeChildPage && creates {
		found, pageID, page = false, block.ID, reads(block.ID)
	}
	if !found && !page {
		// One a Notion move brought names what the move made.
		moved, err := notionMentionedIn(app, current.Workspace.WorkspaceID, publicID)
		switch {
		case err != nil:
			return "", "", err
		case moved.kind == "page":
			pageID, page = moved.nativeID, reads(moved.nativeID)
		case moved.kind == "block" || moved.kind == "database":
			if block, found, err = loadStoredNotionBlock(app, moved.nativeID); err != nil {
				return "", "", err
			}
			found = found && block.WorkspaceID == current.Workspace.WorkspaceID
		case !moved.here:
			return syncedBlockSourceless, "", nil
		}
	}
	if found {
		if !reads(block.PageID) {
			return "", "", unreachable
		}
		stored := map[string]any{}
		_ = json.Unmarshal([]byte(block.Content), &stored)
		if original, _ := stored["syncedBlockId"].(string); block.Type == graphBlockTypeSyncedBlock && original == "" {
			return block.ID, block.PageID, nil
		}
		kind := block.Type
		if named, ok := notionMarkdownSyncedKinds[kind]; ok {
			kind = named
		} else if kind == graphBlockTypeSyncedBlock {
			kind = "transclusion_reference"
		} else if kind == graphBlockTypeInlineDatabase {
			return "", "", notionMarkdownSyncedDatabase(address, block.ParentID == "" && isDatabasePage(app, block.PageID))
		}
		return "", "", points(kind)
	}
	if page && isDatabasePage(app, pageID) {
		return "", "", notionMarkdownSyncedDatabase(address, true)
	}
	if page && creates {
		return pageID, pageID, nil
	}
	if page {
		return "", "", points("page")
	}
	return "", "", unreachable
}
