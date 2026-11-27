package app

import (
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Block addresses — a page's address at one of its blocks (#block-ID) — in
// one rule, as the app's (web/src/lib/blockAddress.ts): an address names a
// block only when the block is on the page it opens and is not that page
// itself. A full-page database's own block is its page, so an address at it
// opens the page with nothing chosen, as Notion's link to a full-page
// database opens it, and its own address with its ID after #, unchosen
// (seen 2026-11-22).

// sameAddressID reports whether two IDs name the same page or block, written
// with dashes or without (Notion's 32 hexadecimal digits), in either case.
func sameAddressID(left, right string) bool {
	return left != "" && strings.EqualFold(strings.ReplaceAll(left, "-", ""), strings.ReplaceAll(right, "-", ""))
}

// blockIsPageItself reports whether a block an address names is the page it
// opens: the page's own ID, or the block of its full-page database (the
// page's ID changes as its database turns inline and back, the block's
// doesn't). Without an app, by the IDs alone.
func blockIsPageItself(app core.App, pageID, blockID string) bool {
	if sameAddressID(pageID, blockID) {
		return true
	}
	if app == nil || pageID == "" || blockID == "" {
		return false
	}
	own, err := loadFullPageDatabaseBlock(app, pageID)
	return err == nil && sameAddressID(own, blockID)
}

// appAnchorBlockID is the block the app's own anchor in a page's address
// names (its fragment, block-ID): the ID as written, or "" for any other
// anchor and for one at the page itself (pageID), which opens the page
// unchosen.
func appAnchorBlockID(fragment, pageID string) string {
	block, found := strings.CutPrefix(fragment, "block-")
	if !found || !workspaceImportReferenceIDPattern.MatchString(block) || sameAddressID(block, pageID) {
		return ""
	}
	return block
}

// pageBlockAnchor is the anchor a page's address takes for a block on it:
// "#block-" and the block's ID, or none for no block or the page itself.
func pageBlockAnchor(app core.App, pageID, blockID string) string {
	if blockID == "" || blockIsPageItself(app, pageID, blockID) {
		return ""
	}
	return "#block-" + url.PathEscape(blockID)
}
