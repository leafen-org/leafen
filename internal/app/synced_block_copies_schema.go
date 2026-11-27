package app

// A synced block's copies are found from its original, as Notion lists the
// pages a synced block is in: the blocks index the copies of a workspace by
// the original they show.

const (
	syncedBlockCopiesVersion = 305
	syncedBlockCopiesName    = "native_synced_block_copies"
)

// syncedBlockOriginalSQL is the original a synced block copy shows, as the
// copy index holds it; a query finds copies by it with the index's own
// block type.
const syncedBlockOriginalSQL = `(CASE WHEN json_valid([[content]]) THEN json_extract([[content]], '$.syncedBlockId') END)`
