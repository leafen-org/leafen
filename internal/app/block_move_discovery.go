package app

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// blockMoveDiscoveryItemsPerWindow is how many of a move's blocks one
// discovery window may look inside, while it has found fewer than a window's
// blocks. A window looked inside one block, so every block with nothing inside
// took a window of its own, and each listed its children by the page's every
// block: a toggle of 30,000 lines took 30,000 windows, some half an hour,
// while both its pages were held (2026-11-23).
const blockMoveDiscoveryItemsPerWindow = 512

// blockMoveDiscoveryCursorMark starts a discovery cursor naming the last
// block listed by its place and ID, the order of the parent's index. A cursor
// without it is a block ID, listed in ID order as a move admitted before
// 2026-11-23 listed its children; such a move goes on in that order.
const blockMoveDiscoveryCursorMark = "\x1f"

// discoverBlockMoveItems looks inside the move's blocks not yet looked
// inside, the shallowest first, and keeps the blocks it finds there to move
// with them. It says when no block is left to look inside.
func discoverBlockMoveItems(app core.App, job storedBlockMoveJob) (bool, error) {
	inserted, done := 0, false
	for examined := 0; examined < blockMoveDiscoveryItemsPerWindow && inserted < maxBlockMoveItemsPerTurn; examined++ {
		item := storedBlockMoveItem{}
		err := app.DB().NewQuery(`
			SELECT [[job_id]],[[block_id]],[[target_block_id]],[[parent_id]],[[position]],[[depth]],
				[[discovery_cursor]],[[discovered]],[[moved]]
			FROM {{leafen_block_move_items}}
			WHERE [[job_id]]={:jobId} AND [[discovered]]=0
			ORDER BY [[depth]],[[block_id]] LIMIT 1
		`).Bind(dbx.Params{"jobId": job.ID}).One(&item)
		if errors.Is(err, sql.ErrNoRows) {
			done = true
			break
		}
		if err != nil {
			return false, err
		}
		// A database's block brings its data along (block_move_database_inventory.go).
		if item.TargetBlockID != "" {
			if err := discoverBlockMoveDatabaseScope(app, job, item.BlockID); err != nil {
				return false, err
			}
		}
		found, err := discoverBlockTransferChildren(app, job, item, maxBlockMoveItemsPerTurn-inserted)
		if err != nil {
			return false, err
		}
		inserted += found
	}
	if inserted > 0 || !done {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_block_move_jobs}}
			SET [[discovered_count]]=[[discovered_count]]+{:inserted},
				[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt}
			WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
		`).Bind(dbx.Params{
			"inserted": inserted, "updatedAt": time.Now().UTC().UnixMilli(), "jobId": job.ID,
		}).Execute(); err != nil {
			return false, err
		}
	}
	return done, nil
}

// discoverBlockTransferChildren keeps up to limit of a block's children,
// those after its cursor, and marks the block looked inside once none is
// left. It says how many it kept.
func discoverBlockTransferChildren(
	app core.App,
	job storedBlockMoveJob,
	item storedBlockMoveItem,
	limit int,
) (int, error) {
	parentID, childDepth := item.BlockID, item.Depth+1
	if item.TargetBlockID == "" {
		parentID, childDepth = "", 0
		placement, err := loadBlockMovePlacement(app, job.ID)
		if err != nil {
			return 0, err
		}
		if placement.CopyChildrenParent != "" {
			parentID = placement.CopyChildrenParent
		}
	}
	children, err := listBlockMoveChildren(app, job, parentID, item.DiscoveryCursor, limit+1)
	if err != nil {
		return 0, err
	}
	hasMore := len(children) > limit
	if hasMore {
		children = children[:limit]
	}
	now := time.Now().UTC().UnixMilli()
	inserted := 0
	for _, child := range children {
		targetBlockID := child.ID
		if job.Operation == blockMoveOperationCopy {
			// Copies get UUIDs, which the Public API addresses directly.
			targetBlockID = uuid.NewString()
		}
		result, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_block_move_items}}
				([[job_id]],[[block_id]],[[target_block_id]],[[parent_id]],[[position]],[[depth]],[[created_at]],[[updated_at]])
			VALUES ({:jobId},{:blockId},{:targetBlockId},{:parentId},{:position},{:depth},{:createdAt},{:updatedAt})
			ON CONFLICT ([[job_id]],[[block_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": job.ID, "blockId": child.ID, "targetBlockId": targetBlockID,
			"parentId": child.ParentID,
			"position": child.Position, "depth": childDepth,
			"createdAt": now, "updatedAt": now,
		}).Execute()
		if err != nil {
			return 0, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		inserted += int(rows)
	}
	cursor := item.DiscoveryCursor
	if len(children) > 0 {
		last := children[len(children)-1]
		cursor = nextBlockMoveDiscoveryCursor(item.DiscoveryCursor, last)
	}
	discovered := 1
	if hasMore {
		discovered = 0
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_items}}
		SET [[discovery_cursor]]={:cursor},[[discovered]]={:discovered},[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId} AND [[discovered]]=0
	`).Bind(dbx.Params{
		"cursor": cursor, "discovered": discovered, "updatedAt": now,
		"jobId": job.ID, "blockId": item.BlockID,
	}).Execute(); err != nil {
		return 0, err
	}
	// A discovery-only root goes once its blocks are known.
	if item.TargetBlockID == "" && !hasMore {
		if _, err := app.DB().Delete(blockMoveItemsTable, dbx.HashExp{"job_id": job.ID, "block_id": item.BlockID}).Execute(); err != nil {
			return 0, err
		}
	}
	return inserted, nil
}

// listBlockMoveChildren reads a parent's children after a cursor through the
// parent's index, so a block with none finds it at once however long its
// page: listed in ID order the page's every block was read for it.
func listBlockMoveChildren(
	app core.App,
	job storedBlockMoveJob,
	parentID, cursor string,
	limit int,
) ([]storedBlockMoveRoot, error) {
	params := dbx.Params{
		"ownerId": job.sourceOwner(), "sourcePageId": job.SourcePageID,
		"parentId": parentID, "limit": limit,
	}
	children := []storedBlockMoveRoot{}
	if cursor != "" && !strings.HasPrefix(cursor, blockMoveDiscoveryCursorMark) {
		params["cursor"] = cursor
		err := app.DB().NewQuery(`
			SELECT [[id]],[[parent_id]],[[position]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
			WHERE [[owner]]={:ownerId} AND [[page_id]]={:sourcePageId}
				AND [[parent_id]]={:parentId} AND [[id]]>{:cursor}
			ORDER BY [[id]] LIMIT {:limit}
		`).Bind(params).All(&children)
		return children, err
	}
	params["position"], params["id"] = splitBlockMoveDiscoveryCursor(cursor)
	err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:sourcePageId}
			AND [[parent_id]]={:parentId} AND ([[position]],[[id]])>({:position},{:id})
		ORDER BY [[position]],[[id]] LIMIT {:limit}
	`).Bind(params).All(&children)
	return children, err
}

// nextBlockMoveDiscoveryCursor names the last child listed in the order its
// cursor lists by.
func nextBlockMoveDiscoveryCursor(cursor string, last storedBlockMoveRoot) string {
	if cursor != "" && !strings.HasPrefix(cursor, blockMoveDiscoveryCursorMark) {
		return last.ID
	}
	return blockMoveDiscoveryCursorMark + last.Position + blockMoveDiscoveryCursorMark + last.ID
}

// splitBlockMoveDiscoveryCursor reads a place-and-ID cursor; an empty one
// lists from the first child.
func splitBlockMoveDiscoveryCursor(cursor string) (string, string) {
	rest := strings.TrimPrefix(cursor, blockMoveDiscoveryCursorMark)
	separator := strings.LastIndex(rest, blockMoveDiscoveryCursorMark)
	if separator < 0 {
		return "", ""
	}
	return rest[:separator], rest[separator+len(blockMoveDiscoveryCursorMark):]
}
