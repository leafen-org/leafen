package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type pageHistoryGraphDelta struct {
	BlockID  string          `json:"blockId"`
	AfterID  string          `json:"afterId"`
	ParentID string          `json:"parentId"`
	Type     string          `json:"type"`
	Content  json.RawMessage `json:"content"`
	Created  int64           `json:"created"`
	Updated  int64           `json:"updated"`
	Deleted  bool            `json:"deleted,omitempty"`
	// Moved marks a block the change moved, apart from the blocks whose
	// place shifted around it.
	Moved bool `json:"moved,omitempty"`
}

type pageHistoryTextDelta struct {
	BlockID string       `json:"blockId"`
	Delta   []crdt.Delta `json:"delta"`
}

type pageHistoryEventEnvelope struct {
	Format      int                     `json:"format"`
	Update      []byte                  `json:"update"`
	GraphDeltas []pageHistoryGraphDelta `json:"graphDeltas,omitempty"`
	TextDeltas  []pageHistoryTextDelta  `json:"textDeltas,omitempty"`
	// Databases are the page's databases the change left different.
	Databases pageHistoryDatabaseChanges `json:"databases,omitempty"`
	// Rows are its databases' rows the change added, edited or removed.
	Rows []pageHistoryRowChange `json:"rows,omitempty"`
}

type pageHistoryGraphCapture struct {
	parents    map[string]struct{}
	changedIDs map[string]struct{}
	deletedIDs map[string]struct{}
	movedIDs   map[string]struct{}
	// placedIn are the parents a block is put in or taken from, apart from
	// those of the blocks deleted.
	placedIn map[string]struct{}
	// reachesPastPage tells a change naming or taking away a block that
	// holds a database or another page's synced copies, as the page stood
	// before it (content_graph_changed_blocks.go).
	reachesPastPage bool
}

func beginPageHistoryGraphCapture(
	doc *crdt.Doc, operations []graphOperation,
) (*pageHistoryGraphCapture, error) {
	metadata := newStructuralMetadataView(doc)
	capture := &pageHistoryGraphCapture{
		parents: map[string]struct{}{}, changedIDs: map[string]struct{}{}, deletedIDs: map[string]struct{}{},
		movedIDs: map[string]struct{}{}, placedIn: map[string]struct{}{},
	}
	place := func(parentID string) {
		capture.parents[parentID] = struct{}{}
		capture.placedIn[parentID] = struct{}{}
	}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			block := operation.Block
			parentID := ""
			if block.ParentID != nil {
				parentID = *block.ParentID
			}
			place(parentID)
			capture.changedIDs[block.ID] = struct{}{}
			capture.reachesPastPage = capture.reachesPastPage || graphBlockReachesPastPage(block.Type)
			if previous, exists, err := metadata.get(block.ID); err != nil {
				return nil, err
			} else if exists {
				place(previous.ParentID)
				capture.reachesPastPage = capture.reachesPastPage || graphBlockReachesPastPage(previous.Type)
			}
		case graphOperationKindMoveBlock:
			move := operation.Move
			capture.changedIDs[move.BlockID] = struct{}{}
			capture.movedIDs[move.BlockID] = struct{}{}
			previous, _, err := metadata.get(move.BlockID)
			if err != nil {
				return nil, err
			}
			place(previous.ParentID)
			capture.reachesPastPage = capture.reachesPastPage || graphBlockReachesPastPage(previous.Type)
			parentID := ""
			if move.ParentID != nil {
				parentID = *move.ParentID
			}
			place(parentID)
		case graphOperationKindDeleteBlock:
			whole, err := metadata.all()
			if err != nil {
				return nil, err
			}
			ids, collectErr := collectStructuralSubtree(whole, operation.Deletion.BlockID)
			if collectErr != nil {
				return nil, collectErr
			}
			for _, id := range ids {
				capture.parents[whole[id].ParentID] = struct{}{}
				capture.deletedIDs[id] = struct{}{}
				capture.reachesPastPage = capture.reachesPastPage || graphBlockReachesPastPage(whole[id].Type)
			}
		}
	}
	return capture, nil
}

type pageHistoryProjectionBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
	Created  int64  `db:"created"`
	Updated  int64  `db:"updated"`
}

type pageHistoryOpenBlockSpan struct {
	BlockID  string `db:"block_id"`
	AfterID  string `db:"after_id"`
	ParentID string `db:"parent_id"`
	Type     string `db:"type"`
	Content  string `db:"content"`
	Created  int64  `db:"created_at"`
	Updated  int64  `db:"updated_at"`
}

func (capture *pageHistoryGraphCapture) finish(
	app core.App, pageID string,
) ([]pageHistoryGraphDelta, error) {
	current := map[string]pageHistoryProjectionBlock{}
	desiredAfter := map[string]string{}
	parents := make([]string, 0, len(capture.parents))
	for parentID := range capture.parents {
		parents = append(parents, parentID)
	}
	sort.Strings(parents)
	emptied, err := capture.emptiedParentNeighbors(app, pageID)
	if err != nil {
		return nil, err
	}
	for _, parentID := range parents {
		if neighbors, found := emptied[parentID]; found {
			if neighbors.next != nil {
				current[neighbors.next.ID] = *neighbors.next
				desiredAfter[neighbors.next.ID] = neighbors.before
			}
			continue
		}
		blocks, predecessor, suffix, err := capture.changedSuffix(app, pageID, parentID)
		if err != nil {
			return nil, err
		}
		if !suffix {
			blocks = []pageHistoryProjectionBlock{}
			if err := app.DB().NewQuery(`
				SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
				FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} AND [[parent_id]]={:parentId}
				ORDER BY [[position]],[[id]]
			`).Bind(dbx.Params{"pageId": pageID, "parentId": parentID}).All(&blocks); err != nil {
				return nil, err
			}
			if predecessor, err = pageHistoryPredecessorBeforeParent(app, pageID, parentID); err != nil {
				return nil, err
			}
		}
		for _, block := range blocks {
			current[block.ID] = block
			desiredAfter[block.ID] = predecessor
			predecessor = block.ID
		}
		next, found, err := pageHistoryFirstBlockAfterParent(app, pageID, parentID)
		if err != nil {
			return nil, err
		}
		if found {
			current[next.ID] = next
			desiredAfter[next.ID] = predecessor
		}
	}
	for id := range capture.changedIDs {
		if _, exists := current[id]; exists {
			continue
		}
		block, found, err := loadPageHistoryProjectionBlock(app, pageID, id)
		if err != nil {
			return nil, err
		}
		if found {
			current[id] = block
		}
	}
	open, err := loadPageHistoryOpenBlockSpans(app, pageID, current)
	if err != nil {
		return nil, err
	}
	deltas := make([]pageHistoryGraphDelta, 0, len(current)+len(capture.deletedIDs))
	moves := &pageHistoryMoveReader{
		app: app, pageID: pageID, desiredAfter: desiredAfter, open: open, current: current, deleted: capture.deletedIDs,
	}
	for id, block := range current {
		afterID, trackedOrder := desiredAfter[id]
		previous, existed := open[id]
		metadataChanged := !existed || previous.ParentID != block.ParentID || previous.Type != block.Type ||
			previous.Content != block.Content || previous.Created != block.Created || previous.Updated != block.Updated
		if !metadataChanged && (!trackedOrder || previous.AfterID == afterID) {
			continue
		}
		if !trackedOrder {
			afterID = previous.AfterID
		}
		_, moved := capture.movedIDs[id]
		if moved {
			if moved, err = moves.moved(id, block); err != nil {
				return nil, err
			}
		}
		deltas = append(deltas, pageHistoryGraphDelta{
			BlockID: id, AfterID: afterID, ParentID: block.ParentID, Type: block.Type,
			Content: json.RawMessage(block.Content), Created: block.Created, Updated: block.Updated, Moved: moved,
		})
	}
	for id := range capture.deletedIDs {
		if _, recreated := current[id]; recreated {
			continue
		}
		deltas = append(deltas, pageHistoryGraphDelta{BlockID: id, Deleted: true})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].BlockID < deltas[j].BlockID })
	return deltas, nil
}

// pageHistoryEmptiedParent is what a parent the change emptied leaves in
// the page's order: the block before its place and the first block after it.
type pageHistoryEmptiedParent struct {
	before string
	next   *pageHistoryProjectionBlock
}

// emptiedParentNeighbors reads, for every parent the change emptied — a
// block deleted with all its children, nothing put in it — the blocks around
// its place, as finish reads them for a parent with none left, all at once:
// four reads for each, a 20,100-block deletion read its 4,000 toggles' places
// while it held the one write connection (2026-11-08).
func (capture *pageHistoryGraphCapture) emptiedParentNeighbors(
	app core.App, pageID string,
) (map[string]pageHistoryEmptiedParent, error) {
	parents := []string{}
	for parentID := range capture.parents {
		_, deleted := capture.deletedIDs[parentID]
		if _, placed := capture.placedIn[parentID]; deleted && !placed {
			parents = append(parents, parentID)
		}
	}
	if len(parents) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(parents)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ParentID string `db:"parent_id"`
		BeforeID string `db:"before_id"`
		NextID   string `db:"next_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[parent]].[[value]] AS [[parent_id]],
			COALESCE((SELECT [[id]] FROM {{leafen_blocks}}
				WHERE ` + pageHistoryBlockOwnerSQL + ` AND [[page_id]]={:pageId} AND [[parent_id]]<[[parent]].[[value]]
				ORDER BY [[parent_id]] DESC,[[position]] DESC,[[id]] DESC LIMIT 1), '') AS [[before_id]],
			COALESCE((SELECT [[id]] FROM {{leafen_blocks}}
				WHERE ` + pageHistoryBlockOwnerSQL + ` AND [[page_id]]={:pageId} AND [[parent_id]]>[[parent]].[[value]]
				ORDER BY [[parent_id]],[[position]],[[id]] LIMIT 1), '') AS [[next_id]]
		FROM json_each({:parents}) AS [[parent]]
	`).Bind(dbx.Params{"pageId": pageID, "parents": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	nextIDs := []string{}
	for _, row := range rows {
		if row.NextID != "" {
			nextIDs = append(nextIDs, row.NextID)
		}
	}
	next := map[string]*pageHistoryProjectionBlock{}
	if len(nextIDs) > 0 {
		if encoded, err = json.Marshal(nextIDs); err != nil {
			return nil, err
		}
		blocks := []pageHistoryProjectionBlock{}
		if err := app.DB().NewQuery(`
			SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
			FROM {{leafen_blocks}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND +[[page_id]]={:pageId}
		`).Bind(dbx.Params{"pageId": pageID, "ids": string(encoded)}).All(&blocks); err != nil {
			return nil, err
		}
		for index := range blocks {
			next[blocks[index].ID] = &blocks[index]
		}
	}
	emptied := make(map[string]pageHistoryEmptiedParent, len(rows))
	for _, row := range rows {
		emptied[row.ParentID] = pageHistoryEmptiedParent{before: row.BeforeID, next: next[row.NextID]}
	}
	return emptied, nil
}

// pageHistoryBlockOwnerSQL names the page's owner, whose blocks the page
// order index keeps by page, parent and place; without it a read of a few
// blocks around a place went through all of the page's.
const pageHistoryBlockOwnerSQL = `[[owner]]=(SELECT [[owner]] FROM {{leafen_pages}} WHERE [[id]]={:pageId})`

// changedSuffix reads a parent's blocks the change touched when they are its
// last ones — blocks added at its end, as each turn of a long import adds
// them — with the block before them, so the blocks before those, whose places
// stay, aren't read again (2026-10-15: every block of the parent read on each
// turn, at 15,000 blocks a tenth of the import's time); suffix false
// otherwise, and when none of them is there.
func (capture *pageHistoryGraphCapture) changedSuffix(
	app core.App, pageID, parentID string,
) ([]pageHistoryProjectionBlock, string, bool, error) {
	ids := make([]string, 0, len(capture.changedIDs))
	for id := range capture.changedIDs {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, "", false, err
	}
	params := dbx.Params{"pageId": pageID, "parentId": parentID, "ids": string(encoded)}
	blocks := []pageHistoryProjectionBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND +[[page_id]]={:pageId} AND +[[parent_id]]={:parentId}
		ORDER BY [[position]],[[id]]
	`).Bind(params).All(&blocks); err != nil || len(blocks) == 0 {
		return nil, "", false, err
	}
	params["position"], params["id"] = blocks[0].Position, blocks[0].ID
	var later int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}} WHERE ` + pageHistoryBlockOwnerSQL + ` AND [[page_id]]={:pageId}
			AND [[parent_id]]={:parentId} AND ([[position]],[[id]])>({:position},{:id})
			AND [[id]] NOT IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(params).Row(&later); err != nil || later != 0 {
		return nil, "", false, err
	}
	before := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}} WHERE ` + pageHistoryBlockOwnerSQL + ` AND [[page_id]]={:pageId}
			AND [[parent_id]]={:parentId} AND [[id]] NOT IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[position]] DESC,[[id]] DESC LIMIT 1
	`).Bind(params).Column(&before); err != nil {
		return nil, "", false, err
	}
	if len(before) == 1 {
		return blocks, before[0], true, nil
	}
	predecessor, err := pageHistoryPredecessorBeforeParent(app, pageID, parentID)
	return blocks, predecessor, err == nil, err
}

func pageHistoryPredecessorBeforeParent(app core.App, pageID, parentID string) (string, error) {
	var id string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}}
		WHERE ` + pageHistoryBlockOwnerSQL + ` AND [[page_id]]={:pageId} AND [[parent_id]]<{:parentId}
		ORDER BY [[parent_id]] DESC,[[position]] DESC,[[id]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "parentId": parentID}).Row(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func pageHistoryFirstBlockAfterParent(
	app core.App, pageID, parentID string,
) (pageHistoryProjectionBlock, bool, error) {
	block := pageHistoryProjectionBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE ` + pageHistoryBlockOwnerSQL + ` AND [[page_id]]={:pageId} AND [[parent_id]]>{:parentId}
		ORDER BY [[parent_id]],[[position]],[[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "parentId": parentID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return block, false, nil
	}
	return block, err == nil, err
}

func loadPageHistoryProjectionBlock(
	app core.App, pageID, blockID string,
) (pageHistoryProjectionBlock, bool, error) {
	block := pageHistoryProjectionBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} AND [[id]]={:blockId}
	`).Bind(dbx.Params{"pageId": pageID, "blockId": blockID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return block, false, nil
	}
	return block, err == nil, err
}

func loadPageHistoryOpenBlockSpans(
	app core.App, pageID string, blocks map[string]pageHistoryProjectionBlock,
) (map[string]pageHistoryOpenBlockSpan, error) {
	result := map[string]pageHistoryOpenBlockSpan{}
	ids := make([]string, 0, len(blocks))
	for id := range blocks {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += pageHistoryBlockPageSize {
		end := min(start+pageHistoryBlockPageSize, len(ids))
		params := dbx.Params{"pageId": pageID}
		bindings := make([]string, end-start)
		for index, id := range ids[start:end] {
			key := fmt.Sprintf("block%d", index)
			params[key] = id
			bindings[index] = "{:" + key + "}"
		}
		rows := []pageHistoryOpenBlockSpan{}
		if err := app.DB().NewQuery(`
			SELECT [[block_id]],[[after_id]],[[parent_id]],[[type]],[[content]],[[created_at]],[[updated_at]]
			FROM {{leafen_page_history_block_spans}}
			WHERE [[page_id]]={:pageId} AND [[valid_through]] IS NULL
				AND [[block_id]] IN (` + strings.Join(bindings, ",") + `)
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.BlockID] = row
		}
	}
	return result, nil
}
