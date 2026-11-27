package app

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// A window of a block move or copy changes its pages' blocks alone, and says
// so in its stored receipt (graphChangedParts.PageBlocks) when none of the
// blocks it took out, put in or placed again holds a database or another
// page's synced copies (blockMoveDocument.reachesPastPage), as a person's
// change of the same blocks says it (content_graph_changed_blocks.go): a
// reader of either page behind it reads its blocks again, and a page linking
// their databases nothing. Before, each window said nothing, and moving or
// copying a line into or out of a page holding a database of 5,000 rows — a
// button's draft put in by its run too — had the page's other tabs and every
// page linking the database read their 51 windows again, some 7 seconds on a
// NAS-class server (2026-11-25). A database's own block moving with it says
// nothing, nor does its data moving (recordBlockMoveDatabaseGraphChanges).
func recordBlockMoveStructuralChange(
	app core.App,
	job storedBlockMoveJob,
	pageID string,
	update []byte,
	rollback bool,
	count int,
	blocksOnly bool,
) error {
	return recordBlockMoveGraphChange(
		app, job, pageID, update, rollback, "blocks", storedGraphMutationReceipt{
			graphMutationCounts: graphMutationCounts{Blocks: count},
			graphChangedParts:   graphChangedParts{PageBlocks: blocksOnly},
		},
	)
}

// recordBlockMoveGraphChange records a move's or copy's change of a page
// with the receipt it is kept with (recordServerGraphChangeReceipt).
func recordBlockMoveGraphChange(
	app core.App,
	job storedBlockMoveJob,
	pageID string,
	update []byte,
	rollback bool,
	kind string,
	receipt any,
) error {
	direction := "forward"
	if rollback {
		direction = "rollback"
	}
	mutationID := fmt.Sprintf(
		"block-move:%s:%d:%s:%s:%s", job.ID, job.Revision, direction, kind, pageID,
	)
	return recordServerGraphChangeReceipt(app, job.OwnerID, pageID, mutationID, update, receipt)
}
