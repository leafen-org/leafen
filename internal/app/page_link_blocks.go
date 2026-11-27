package app

import (
	"net/url"

	"github.com/pocketbase/pocketbase/core"
)

// A block's own address — /p/ and its ID, as notion-fetch's ancestor-path
// names a column or a toggle above a page (mcp_fetch_ancestor_blocks.go) —
// opens the page holding it at the block (#block-), as Notion's opens a
// block's address on its page, for a reader who may read that page.

// resolveBlockPageLink answers where a block's ID opens: its page at it, or
// nowhere for one that names no block or a page's or database's.
func resolveBlockPageLink(app core.App, principalID, id string) (map[string]string, error) {
	block, found := mcpBlockNamed(app, id)
	if !found {
		return map[string]string{}, nil
	}
	path, err := resolvePageLinkPath(app, principalID, block.PageID)
	if err != nil || path == "" {
		return map[string]string{}, err
	}
	return map[string]string{"path": path + mcpDatabaseLinkFragment + url.PathEscape(block.ID)}, nil
}
