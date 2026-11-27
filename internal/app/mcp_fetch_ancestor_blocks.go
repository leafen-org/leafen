package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page or a database standing in a page's body inside a block — a column
// and its column list, a toggle — has those blocks between it and the page
// above it, and Notion's fetch lists them there, nearest first, as pages
// named by their words (seen 2026-11-21: a row of a database in a column
// lists the column and the column list with title="", a toggle by its text,
// each at its own ID's address), and its path leaves them out. A page's own
// fetch gives no ancestor-path at all while such a block stands anywhere
// above it, whatever its path says, nor does a block's, but for a column
// list's with none above it (mcpAncestorsShownForPage, mcp_fetch_block.go).

// mcpAncestorBlock is a block standing above a page or database in its page.
type mcpAncestorBlock struct {
	ID    string `db:"id"`
	Type  string `db:"type"`
	Depth int    `db:"depth"`
}

// mcpAncestorBlockDepth bounds the walk up a page's blocks.
const mcpAncestorBlockDepth = 64

// loadMCPAncestorBlocks lists the blocks holding a block in a page's body,
// nearest first; none for one standing at the top of it or not in it.
func loadMCPAncestorBlocks(app core.App, pageID, blockID string) ([]mcpAncestorBlock, error) {
	blocks := []mcpAncestorBlock{}
	if pageID == "" || blockID == "" {
		return blocks, nil
	}
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[above]]([[id]], [[parent_id]], [[type]], [[depth]]) AS (
			SELECT [[id]], [[parent_id]], [[type]], 0 FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[page_id]] = {:page}
			UNION ALL
			SELECT [[block]].[[id]], [[block]].[[parent_id]], [[block]].[[type]], [[above]].[[depth]] + 1
			FROM {{leafen_blocks}} AS [[block]] JOIN [[above]] ON [[block]].[[id]] = [[above]].[[parent_id]]
			WHERE [[block]].[[page_id]] = {:page} AND [[above]].[[depth]] < {:limit}
		)
		SELECT [[id]], [[type]], [[depth]] FROM [[above]] WHERE [[depth]] > 0 ORDER BY [[depth]]
	`).Bind(dbx.Params{"id": blockID, "page": pageID, "limit": mcpAncestorBlockDepth}).All(&blocks)
	return blocks, err
}

// blocks adds the blocks a page holds a page or database in, nearest first,
// named by nothing until their words are read (mcpAncestorBlockTitles).
func (walk *mcpAncestorWalk) blocks(pageID, heldID string) error {
	blocks, err := loadMCPAncestorBlocks(walk.app, pageID, heldID)
	for _, block := range blocks {
		walk.ancestors = append(walk.ancestors, mcpFetchAncestor{
			Kind: mcpFetchAncestorPage, URL: mcpPageURL(walk.authority, block.ID),
			BlockID: block.ID, BlockType: block.Type, BlockPageID: pageID,
		})
	}
	return err
}

// mcpAncestorBlockTitles names the blocks in an ancestor-path by their words
// as they stand, "" for a column or a column list: only a fetch's text
// shows them, and a path leaves them out, so the pages' texts are read for
// it alone.
func mcpAncestorBlockTitles(app core.App, ancestors []mcpFetchAncestor) error {
	documents := map[string]*crdt.Doc{}
	for index := range ancestors {
		ancestor := &ancestors[index]
		if ancestor.BlockID == "" || !mcpBlockHoldsWords(ancestor.BlockType) {
			continue
		}
		document, read := documents[ancestor.BlockPageID]
		if !read {
			loaded, found, err := loadYgoDoc(app, ancestor.BlockPageID)
			if err != nil {
				return err
			}
			if !found {
				loaded = nil
			}
			document, documents[ancestor.BlockPageID] = loaded, loaded
		}
		if document != nil {
			_, ancestor.Title = rowPageBlockTextAsItStands(document, ancestor.BlockID, false)
		}
	}
	return nil
}

// mcpBlockHoldsWords is whether a block of the type has words of its own to
// be named by: a column and a column list have none.
func mcpBlockHoldsWords(blockType string) bool {
	return blockType != graphBlockTypeColumn && blockType != graphBlockTypeColumnList && !graphBlockWordless(blockType)
}

// mcpAncestorsShownForPage is the ancestor-path a page's fetch gives: none
// while a block stands anywhere above it, as Notion's (seen 2026-11-21: a
// page in a column, one under it and one in a toggle all fetch with an empty
// ancestor-path, their path naming the pages above).
func mcpAncestorsShownForPage(ancestors []mcpFetchAncestor) []mcpFetchAncestor {
	for _, ancestor := range ancestors {
		if ancestor.BlockID != "" {
			return nil
		}
	}
	return ancestors
}
