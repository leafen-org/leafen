package app

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// blockMoveDocument is a page's structure document as one window of a move
// or copy works in it: each parent's order read and checked once, then kept
// as the window takes blocks out and puts them in, and each block's metadata
// read as it is asked for. Read again for every block, the order of a long
// parent, or the page's every block's metadata, made a window's work grow
// with the page and a move of a toggle of 30,000 lines take the square of it
// (2026-11-23).
type blockMoveDocument struct {
	doc      *crdt.Doc
	orders   *structuralOrderView
	metadata *structuralMetadataView
	// reachesPastPage tells the window took out, put in or placed again a
	// block holding a database or another page's synced copies
	// (graphBlockReachesPastPage): its change of the page says nothing of
	// what it changed (block_move_graph_change.go).
	reachesPastPage bool
}

func newBlockMoveDocument(doc *crdt.Doc) *blockMoveDocument {
	return &blockMoveDocument{
		doc: doc, orders: newStructuralOrderView(doc), metadata: newStructuralMetadataView(doc),
	}
}

// detach takes a block out of its parent's order and its metadata out.
func (document *blockMoveDocument) detach(blockID, parentID string) error {
	index, err := document.orders.indexOf(parentID, blockID)
	if err != nil {
		return err
	}
	if err := document.noteReach(blockID); err != nil {
		return err
	}
	if index < 0 {
		return fmt.Errorf("block %s absent from parent %s order: %w", blockID, parentID, errBlockMoveConflict)
	}
	document.doc.Transact(func(transaction *crdt.Transaction) {
		transaction.GetArray(blockOrderRoot(parentID)).Delete(transaction, index, 1)
		transaction.GetMap(ygoBlockMetadataRoot).Delete(transaction, blockID)
	})
	document.orders.removed(parentID, index)
	document.metadata.remove(blockID)
	return nil
}

// attach puts a block new to the document after another under its parent,
// or first, with its metadata.
func (document *blockMoveDocument) attach(
	blockID, parentID string,
	afterID *string,
	entry structuralBlockMetadata,
) error {
	if _, exists, err := document.metadata.get(blockID); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("block %s metadata already exists at target: %w", blockID, errBlockMoveConflict)
	}
	if err := document.orders.insert(blockID, parentID, afterID); err != nil {
		return err
	}
	document.doc.Transact(func(transaction *crdt.Transaction) {
		transaction.GetMap(ygoBlockMetadataRoot).Set(transaction, blockID, map[string]any{
			"parentId": parentID, "type": entry.Type, "content": entry.Content,
		})
	})
	entry.ParentID = parentID
	document.metadata.set(blockID, entry)
	document.reachesPastPage = document.reachesPastPage || graphBlockReachesPastPage(entry.Type)
	return nil
}

// noteReach notes a block the window takes out by its metadata, which its
// callers read just before (cached).
func (document *blockMoveDocument) noteReach(blockID string) error {
	entry, exists, err := document.metadata.get(blockID)
	document.reachesPastPage = document.reachesPastPage || exists && graphBlockReachesPastPage(entry.Type)
	return err
}

// last is the last block under a parent, nil when it has none.
func (document *blockMoveDocument) last(parentID string) (*string, error) {
	entry, err := document.orders.entry(parentID)
	if err != nil || len(entry.ids) == 0 {
		return nil, err
	}
	last := entry.ids[len(entry.ids)-1]
	return &last, nil
}

// adoptBlockMoveChildren puts a moved block's children, which moved before
// it, back inside it on the destination, first and in their order, all at
// once: one taken out and put in at a time, each read the page's every
// block's metadata and its parents' whole orders (2026-11-23).
func adoptBlockMoveChildren(
	app core.App,
	job storedBlockMoveJob,
	item storedBlockMoveItem,
	destination *blockMoveDocument,
	affected map[string]map[string]struct{},
) error {
	children := []storedBlockMoveItem{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]],[[block_id]],[[parent_id]],[[position]],[[depth]],
			[[discovery_cursor]],[[discovered]],[[moved]]
		FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[parent_id]]={:parentId} AND [[moved]]=1
		ORDER BY [[position]],[[block_id]]
	`).Bind(dbx.Params{"jobId": job.ID, "parentId": item.BlockID}).All(&children); err != nil {
		return err
	}
	if len(children) == 0 {
		return nil
	}
	ids := make([]string, len(children))
	entries := make([]structuralBlockMetadata, len(children))
	leaving := map[string]map[string]struct{}{}
	for index, child := range children {
		entry, exists, err := destination.metadata.get(child.BlockID)
		if err != nil {
			return err
		}
		if !exists || entry.ParentID == item.BlockID {
			return errBlockMoveConflict
		}
		ids[index], entries[index] = child.BlockID, entry
		destination.reachesPastPage = destination.reachesPastPage || graphBlockReachesPastPage(entry.Type)
		if leaving[entry.ParentID] == nil {
			leaving[entry.ParentID] = map[string]struct{}{}
		}
		leaving[entry.ParentID][child.BlockID] = struct{}{}
	}
	parents := make([]string, 0, len(leaving))
	for parentID := range leaving {
		parents = append(parents, parentID)
	}
	sort.Strings(parents)
	for _, parentID := range parents {
		if err := destination.removeAll(parentID, leaving[parentID]); err != nil {
			return err
		}
		affected[job.DestinationPageID][parentID] = struct{}{}
	}
	values := stringsToAny(ids)
	destination.doc.Transact(func(transaction *crdt.Transaction) {
		transaction.GetArray(blockOrderRoot(item.BlockID)).Insert(transaction, 0, values)
		metadata := transaction.GetMap(ygoBlockMetadataRoot)
		for index, id := range ids {
			metadata.Set(transaction, id, map[string]any{
				"parentId": item.BlockID, "type": entries[index].Type, "content": entries[index].Content,
			})
		}
	})
	destination.orders.forget(item.BlockID)
	for index, id := range ids {
		entries[index].ParentID = item.BlockID
		destination.metadata.set(id, entries[index])
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_blocks}} SET [[parent_id]]={:parentId}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[id]] IN (
			SELECT [[block_id]] FROM {{leafen_block_move_items}}
			WHERE [[job_id]]={:jobId} AND [[parent_id]]={:parentId} AND [[moved]]=1)
	`).Bind(dbx.Params{
		"parentId": item.BlockID, "ownerId": job.OwnerID,
		"pageId": job.DestinationPageID, "jobId": job.ID,
	}).Execute(); err != nil {
		return err
	}
	affected[job.DestinationPageID][item.BlockID] = struct{}{}
	return nil
}

// removeAll takes blocks out of a parent's order, a run of neighbours at a
// time from the end, so the places before each run stay as read.
func (document *blockMoveDocument) removeAll(parentID string, blocks map[string]struct{}) error {
	entry, err := document.orders.entry(parentID)
	if err != nil {
		return err
	}
	indexes := make([]int, 0, len(blocks))
	for index, id := range entry.ids {
		if _, leaves := blocks[id]; leaves {
			indexes = append(indexes, index)
		}
	}
	if len(indexes) != len(blocks) {
		return fmt.Errorf("blocks absent from parent %s order: %w", parentID, errBlockMoveConflict)
	}
	document.doc.Transact(func(transaction *crdt.Transaction) {
		order := transaction.GetArray(blockOrderRoot(parentID))
		for end := len(indexes) - 1; end >= 0; {
			start := end
			for start > 0 && indexes[start-1] == indexes[start]-1 {
				start--
			}
			order.Delete(transaction, indexes[start], end-start+1)
			end = start - 1
		}
	})
	document.orders.forget(parentID)
	return nil
}

// blockMoveRollbackAnchor is the block a block moved back goes after on its
// page: the nearest of its siblings before it there. Inside another block
// of the move every sibling is one of the move's blocks, standing in their
// places' order, so it is found in the parent's order by halves; read for
// each block from the move's every sibling, a toggle of 30,000 lines took
// some 15 ms a line (2026-11-23). A sibling the move doesn't know, as beside
// a block the move starts from, is looked for as before.
func blockMoveRollbackAnchor(
	app core.App,
	job storedBlockMoveJob,
	item storedBlockMoveItem,
	source *blockMoveDocument,
	places map[string]string,
) (*string, error) {
	if item.Depth == 0 {
		return blockMoveRollbackAfterID(app, job, item)
	}
	entry, err := source.orders.entry(item.ParentID)
	if err != nil {
		return nil, err
	}
	order := entry.ids
	unknown := false
	before := func(id string) (bool, error) {
		place, known := places[id]
		if !known {
			err := app.DB().NewQuery(`
				SELECT [[position]] FROM {{leafen_block_move_items}}
				WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId}
			`).Bind(dbx.Params{"jobId": job.ID, "blockId": id}).Row(&place)
			if errors.Is(err, sql.ErrNoRows) {
				unknown = true
				return false, nil
			}
			if err != nil {
				return false, err
			}
			places[id] = place
		}
		return place < item.Position || place == item.Position && id < item.BlockID, nil
	}
	low, high := 0, len(order)
	for low < high && !unknown {
		middle := int(uint(low+high) >> 1)
		earlier, err := before(order[middle])
		if err != nil {
			return nil, err
		}
		if earlier {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if unknown {
		return blockMoveRollbackAfterID(app, job, item)
	}
	if low == 0 {
		return nil, nil
	}
	after := order[low-1]
	return &after, nil
}

// nextBlockMoveForwardWindow is the blocks a move's next window takes.
// Blocks move once the blocks inside them have — the deepest still here
// first, every one of whose children has gone — and the blocks the move
// starts from go to the end of the page last, in their order. Blocks inside
// others go from the last: taken from the front, every block after them took
// a new place in each window, and the window rewrote them all (2026-11-23: a
// toggle of 30,000 lines, some 14 million writes). Their order comes back
// when their parent takes them in (adoptBlockMoveChildren). Asked of every
// block left whether a child of it was still here, each window read them all
// twice over.
func nextBlockMoveForwardWindow(app core.App, job storedBlockMoveJob) ([]storedBlockMoveItem, error) {
	var deepest sql.NullInt64
	if err := app.DB().NewQuery(`
		SELECT MAX([[depth]]) FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]=0
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&deepest); err != nil {
		return nil, err
	}
	items := []storedBlockMoveItem{}
	if !deepest.Valid {
		return items, nil
	}
	order := `[[position]] DESC,[[block_id]] DESC`
	if deepest.Int64 == 0 {
		order = `[[position]],[[block_id]]`
	}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[block_id]],[[target_block_id]],[[parent_id]],[[position]],[[depth]],
			[[discovery_cursor]],[[discovered]],[[moved]]
		FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]=0 AND [[depth]]={:depth}
		ORDER BY ` + order + ` LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "depth": deepest.Int64, "limit": maxBlockMoveItemsPerTurn + 1,
	}).All(&items)
	return items, err
}
