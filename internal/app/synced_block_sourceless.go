package app

import "github.com/pocketbase/pocketbase/core"

// A synced block reference written through page Markdown whose address names
// no block or page here, nor anything a Notion move brought, makes a copy
// syncing from no block, as Notion's public API "v2" makes one (seen twice
// 2026-11-21, a page's address with an unknown block after #, an unknown
// page's, an unknown block's alone, at notion.so and app.notion.com, insert
// and create alike; on 2026-11-10 it failed with a 500). Notion's copy shows
// nothing (a block of no height), holds no blocks, reads in JSON as
// synced_from {"type": "block_id"} with no block_id, and in Markdown as a
// reference to itself on the page read, its tags with nothing between ("v2"
// <synced-block-reference url="…#<its ID>"></synced-block-reference>, "v1"
// <synced_block_reference url="…"> on a line of its own and its closing tag
// on the next). A reference to such a copy is refused as one to any copy.
//
// Such a copy names syncedBlockSourceless as its original: a UUID no block
// is given, so every reader of a copy whose original is not found reads it
// as one (its blocks none), and the readers above tell it apart. It never
// names the ID the address gave, which may be a block of another workspace.
const syncedBlockSourceless = "00000000-0000-0000-0000-000000000000"

// syncedBlockNamed is the block a synced block reference's Markdown names:
// its original, or itself for a copy syncing from no block
// (syncedBlockSourceless).
func syncedBlockNamed(original, blockID string) string {
	if original == syncedBlockSourceless {
		return blockID
	}
	return original
}

// notionSourcelessCopyChildren answers a read or an append under a copy
// syncing from no block: a read lists none, as Notion's (seen 2026-11-21);
// an append, where Notion's failed (a 500, seen 2026-11-21), is refused, in
// Leafen's words.
func notionSourcelessCopyChildren(event *core.RequestEvent, appending bool) error {
	if !appending {
		return notionNoChildren(event)
	}
	return notionPageValidationError(event, "This synced block syncs from no block, so it holds no blocks to add to: append the blocks beside it, or under an original synced block.")
}
