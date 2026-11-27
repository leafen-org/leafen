package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A change of a page's blocks alone — a line made, moved or taken away, with
// its text — says so in its stored receipt (graphChangedParts.PageBlocks): a
// reader of the page behind it reads its blocks again, not its whole graph,
// and a page showing only rows of its databases (a linked view, a relation's
// target) reads nothing for it. Before, such a change said nothing, and on a
// page holding a database of 5,000 rows each page linking that database read
// its 51 windows again, some 7 seconds on a NAS-class server, for one
// paragraph added (2026-11-25). A change that makes, turns, moves or takes
// away a block holding a database — whose container is that block, its
// place the block's — changes the page's databases with it, and says
// nothing.

// maxGraphChangedBlocks is the most blocks a page reads with its changes;
// more read the whole graph.
const maxGraphChangedBlocks = 1000

// graphBlockReachesPastPage tells a block type whose block holds a database
// or another page's synced copies: a change to it changes more than the
// page's blocks.
func graphBlockReachesPastPage(blockType string) bool {
	switch blockType {
	case graphBlockTypeInlineDatabase, graphBlockTypeChildDatabase, graphBlockTypeSyncedBlock:
		return true
	}
	return false
}

// graphBlockChangeKeepsDatabases tells an operation on one of the page's
// blocks that, made as it asks, holds no database and brings back no page:
// a database's block, a synced block's copies and a page turned back into a
// block reach past the page's blocks.
func graphBlockChangeKeepsDatabases(operation graphOperation) bool {
	switch {
	case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
		return !graphBlockReachesPastPage(operation.Block.Type) && !operation.Block.RestorePageConversion
	case operation.Kind == graphOperationKindMoveBlock && operation.Move != nil:
		return true
	case operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil:
		return true
	}
	return false
}

// graphBlocksHoldDatabases tells a change of blocks that named a block
// holding one of the page's databases as it was before it (`before`), or
// took one away under a block it deleted (`deltas`, the blocks it changed).
func graphBlocksHoldDatabases(before *pageHistoryDatabaseCapture, operations []graphOperation, deltas []pageHistoryGraphDelta) bool {
	if before == nil {
		return true
	}
	held := func(id string) bool {
		_, container := before.containerBlocks[id]
		return container
	}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && held(operation.Block.ID),
			operation.Kind == graphOperationKindMoveBlock && operation.Move != nil && held(operation.Move.BlockID),
			operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil && held(operation.Deletion.BlockID):
			return true
		}
	}
	for _, delta := range deltas {
		if delta.Deleted && held(delta.BlockID) {
			return true
		}
	}
	return false
}

// pageDatabaseContainerBlocks is the blocks holding the page's database
// containers, in the trash or not: each container's block, which is also its
// ID.
func pageDatabaseContainerBlocks(app core.App, owner, pageID string) (map[string]struct{}, error) {
	stored := []struct {
		ID      string `db:"id"`
		BlockID string `db:"block_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[block_id]] FROM {{leafen_database_containers}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).All(&stored); err != nil {
		return nil, err
	}
	blocks := make(map[string]struct{}, 2*len(stored))
	for _, container := range stored {
		blocks[container.ID] = struct{}{}
		blocks[container.BlockID] = struct{}{}
	}
	return blocks, nil
}

// recordServerGraphBlocksChange records a change the server made to a page's
// blocks alone (saveServerStructuralBatch) — a sub-page's block placed or
// taken away, a wiki's link, a block moved to its page's end — saying so
// when the blocks it noted (history) reach no database or other page, so a
// page behind it reads its blocks and a page linking its databases nothing;
// one whose blocks weren't noted says nothing.
func recordServerGraphBlocksChange(
	app core.App,
	ownerID, pageID, mutationID string,
	update []byte,
	history *pageHistoryGraphCapture,
) error {
	return recordServerGraphChangeReceipt(app, ownerID, pageID, mutationID, update, storedGraphMutationReceipt{
		graphMutationCounts: graphMutationCounts{Blocks: 1},
		graphChangedParts:   graphChangedParts{PageBlocks: history != nil && !history.reachesPastPage},
	})
}
