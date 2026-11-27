package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where the source moved a block (2026-10-14: a paragraph moved in Notion
// during the migration kept its old place in Leafen after the run again). A
// run again moves a block an earlier run made to its new place in the
// source — after the nearest block before it there that stands in the same
// parent and that nobody moved in Leafen, or first — when the source moved
// it since the run before and
// nobody moved it in Leafen since; one somebody moved stays, and the report
// counts it. A block goes to another parent on its page as the source put it
// in or out of another block, and a sub-page's block moves as a block does;
// one the source moved to another page goes there (notion_migration_block_page_move.go).
//
// To tell, the ledger keeps where each run left each block: its parent in
// Leafen and its place among the migration's blocks there, and its parent
// and place in the source the run read. A block moved is one outside the
// longest run of its siblings still in their old order, in Leafen or in the
// source, so a line put in or taken out beside it doesn't count as moving it.

const (
	notionMigrationStepPlace = "place"
	// A turn takes pages until it has read this many of the ledger's blocks.
	notionMigrationPlaceWindow = 2000
)

// notionMigrationBlockPlace is where a run left a block: its parent in Leafen
// ("" for the page) and its place among the migration's blocks there, and
// its parent in the source the run read ("kind:id") and its place there.
type notionMigrationBlockPlace struct {
	Parent         string `json:"parent"`
	Index          int    `json:"index"`
	SourceParent   string `json:"sourceParent,omitempty"`
	SourcePosition int    `json:"sourcePosition,omitzero"`
	// Leaving marks a block the source moved to another page, for the run to
	// send there once it has put every page's blocks in order.
	Leaving bool `json:"leaving,omitempty"`
	// MovingTo is the page a run sent the block to, as the source moved it.
	MovingTo string `json:"movingTo,omitempty"`
	// Arrived marks a block a run's move brought to this page: the run after
	// puts it in its place there as the source has it, unless somebody moved
	// it since.
	Arrived bool `json:"arrived,omitzero"`
}

// notionMigrationPagePlaces is one page as a run sees it: its blocks in
// order, the ledger's blocks on it, and the source's place of each block on
// it this run read.
type notionMigrationPagePlaces struct {
	parents map[string]string
	order   map[string][]string
	ledger  map[string]notionMigrationPlaceEntry
	// staged are the blocks and sub-pages on the page by native ID.
	staged map[string]notionMigrationStagedPlace
	// pageSources are the source IDs of the page itself.
	pageSources map[string]struct{}
}

type notionMigrationPlaceEntry struct {
	SourceID string `db:"source_id"`
	NativeID string `db:"native_id"`
	Place    string `db:"place"`
	// Kind is "block", or "page" for a sub-page whose block stands on the page.
	Kind string `db:"kind"`
}

type notionMigrationStagedPlace struct {
	NativeID       string `db:"native_id"`
	SourceID       string `db:"source_id"`
	Kind           string `db:"kind"`
	ParentSourceID string `db:"parent_source_id"`
	ParentKind     string `db:"parent_kind"`
	Position       int    `db:"position"`
}

func (staged notionMigrationStagedPlace) parent() string {
	return staged.ParentKind + ":" + staged.ParentSourceID
}

// loadNotionMigrationPagePlaces reads a page for a migration's run.
func loadNotionMigrationPagePlaces(app core.App, owner, jobID, migrationID, pageID string) (*notionMigrationPagePlaces, error) {
	page := &notionMigrationPagePlaces{
		parents: map[string]string{}, order: map[string][]string{}, ledger: map[string]notionMigrationPlaceEntry{},
		staged: map[string]notionMigrationStagedPlace{}, pageSources: map[string]struct{}{},
	}
	// The page's blocks the migration made, and its sub-pages, whose blocks
	// carry their IDs.
	entries := []notionMigrationPlaceEntry{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]],[[native_id]],[[place]],[[kind]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[page_id]]={:pageId} AND [[kind]]='block'
		UNION ALL
		SELECT [[ledger]].[[source_id]],[[ledger]].[[native_id]],[[ledger]].[[place]],[[ledger]].[[kind]]
		FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
		JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[ledger]].[[native_id]]
		WHERE [[ledger]].[[migration_id]]={:migrationId} AND [[ledger]].[[kind]]='page' AND [[block]].[[page_id]]={:pageId}
			AND [[block]].[[owner]]={:owner} AND [[block]].[[type]]='child_page'
	`).Bind(dbx.Params{"migrationId": migrationID, "pageId": pageID, "owner": owner}).All(&entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		page.ledger[entry.NativeID] = entry
	}
	blocks := []struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]] FROM {{leafen_blocks}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
		ORDER BY [[parent_id]],[[position]],[[id]]
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).All(&blocks); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(blocks))
	for _, block := range blocks {
		page.parents[block.ID] = block.ParentID
		page.order[block.ParentID] = append(page.order[block.ParentID], block.ID)
		ids = append(ids, block.ID)
	}
	encoded, err := json.Marshal(append(ids, pageID))
	if err != nil {
		return nil, err
	}
	// A database's block is named after its database, which the source gives
	// as a database; a sub-page's block has the sub-page's ID.
	staged := []notionMigrationStagedPlace{}
	if err := app.DB().NewQuery(`
		SELECT [[mapping]].[[native_id]],[[mapping]].[[source_id]],[[source]].[[kind]],[[source]].[[parent_source_id]],
			[[source]].[[parent_kind]],[[source]].[[position]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_notion_import_sources}} AS [[source]] ON [[source]].[[job_id]]=[[mapping]].[[job_id]]
			AND [[source]].[[source_id]]=CASE WHEN [[mapping]].[[source_id]] LIKE '%:block'
				THEN substr([[mapping]].[[source_id]], 1, length([[mapping]].[[source_id]])-6) ELSE [[mapping]].[[source_id]] END
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]] IN ('block','page')
			AND [[mapping]].[[native_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"jobId": jobID, "ids": string(encoded)}).All(&staged); err != nil {
		return nil, err
	}
	for _, place := range staged {
		page.staged[place.NativeID] = place
	}
	sources := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='page' AND [[native_id]]={:pageId}
	`).Bind(dbx.Params{"jobId": jobID, "pageId": pageID}).Column(&sources); err != nil {
		return nil, err
	}
	for _, source := range sources {
		page.pageSources[source] = struct{}{}
	}
	return page, nil
}

// leafenIndex is a block's place among the ledger's blocks in its parent.
func (page *notionMigrationPagePlaces) leafenIndex(nativeID string) int {
	index := 0
	for _, id := range page.order[page.parents[nativeID]] {
		if id == nativeID {
			return index
		}
		if _, ledger := page.ledger[id]; ledger {
			index++
		}
	}
	return -1
}

// notionMigrationPlacePages lists, after a cursor, the pages holding the
// ledger's blocks, up to a window of blocks.
func notionMigrationPlacePages(app core.App, migrationID, cursor string) ([]string, bool, error) {
	pages := []struct {
		PageID string `db:"page_id"`
		Blocks int    `db:"blocks"`
	}{}
	// A page holding only sub-pages the migration made counts too.
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], COUNT(*) AS [[blocks]] FROM (
			SELECT [[page_id]] FROM {{leafen_notion_migration_ledger}}
			WHERE [[migration_id]]={:migrationId} AND [[kind]]='block' AND [[page_id]]>{:cursor}
			UNION ALL
			SELECT [[block]].[[page_id]] FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
			JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[ledger]].[[native_id]]
			WHERE [[ledger]].[[migration_id]]={:migrationId} AND [[ledger]].[[kind]]='page' AND [[block]].[[type]]='child_page'
				AND [[block]].[[page_id]]>{:cursor}
		)
		GROUP BY [[page_id]] ORDER BY [[page_id]] LIMIT 200
	`).Bind(dbx.Params{"migrationId": migrationID, "cursor": cursor}).All(&pages); err != nil {
		return nil, false, err
	}
	chosen, blocks := []string{}, 0
	for _, page := range pages {
		if len(chosen) > 0 && blocks+page.Blocks > notionMigrationPlaceWindow {
			return chosen, true, nil
		}
		chosen, blocks = append(chosen, page.PageID), blocks+page.Blocks
	}
	return chosen, len(pages) == 200, nil
}

// recordNotionMigrationBlockPlaces keeps in the ledger where the run left
// each block on the pages it read, a window of pages a turn; true while more
// remain. A block the source didn't give this time keeps its place there.
func recordNotionMigrationBlockPlaces(app core.App, job storedJob, payload *notionImportJobPayload) (bool, error) {
	pages, more, err := notionMigrationPlacePages(app, payload.MigrationID, payload.PlaceCursor)
	if err != nil {
		return false, err
	}
	for _, pageID := range pages {
		payload.PlaceCursor = pageID
		page, err := loadNotionMigrationPagePlaces(app, payload.AuthorityOwnerID, job.ID, payload.MigrationID, pageID)
		if err != nil {
			return false, err
		}
		read := false
		for nativeID := range page.ledger {
			if _, found := page.staged[nativeID]; found {
				read = true
				break
			}
		}
		if !read {
			continue
		}
		for nativeID, entry := range page.ledger {
			if _, standing := page.parents[nativeID]; !standing {
				continue
			}
			place := notionMigrationBlockPlace{}
			if entry.Place != "" {
				_ = json.Unmarshal([]byte(entry.Place), &place)
			}
			place.Parent, place.Index = page.parents[nativeID], page.leafenIndex(nativeID)
			// A block a move brought here takes its place the run after; one
			// still on its way to another page keeps where it goes.
			place.Arrived, place.Leaving = place.MovingTo == pageID, false
			if place.Arrived {
				place.MovingTo = ""
			}
			if staged, found := page.staged[nativeID]; found {
				place.SourceParent, place.SourcePosition = staged.parent(), staged.Position
			}
			encoded, err := json.Marshal(place)
			if err != nil {
				return false, err
			}
			if string(encoded) == entry.Place {
				continue
			}
			if _, err := app.DB().NewQuery(`
				UPDATE {{leafen_notion_migration_ledger}} SET [[place]]={:place}
				WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
			`).Bind(dbx.Params{"place": string(encoded), "migrationId": payload.MigrationID, "sourceId": entry.SourceID,
				"kind": entry.Kind}).Execute(); err != nil {
				return false, err
			}
		}
	}
	return more, nil
}

// notionMigrationSteady is the members of a group still in their old order:
// the longest run of them whose old places rise in their order now.
func notionMigrationSteady(ids []string, old map[string]int) map[string]struct{} {
	tails, previous := []int{}, make([]int, len(ids))
	for index, id := range ids {
		key := old[id]
		at := sort.Search(len(tails), func(i int) bool { return old[ids[tails[i]]] >= key })
		previous[index] = -1
		if at > 0 {
			previous[index] = tails[at-1]
		}
		if at == len(tails) {
			tails = append(tails, index)
		} else {
			tails[at] = index
		}
	}
	steady := map[string]struct{}{}
	if len(tails) == 0 {
		return steady
	}
	for index := tails[len(tails)-1]; index >= 0; index = previous[index] {
		steady[ids[index]] = struct{}{}
	}
	return steady
}

// notionMigrationMoved is what moved among blocks since their old places,
// each group (a parent) compared by order: one in another group moved, and
// so did one outside its group's steady run.
func notionMigrationMoved(groupNow, groupOld map[string]string, orderNow map[string][]string, old map[string]int) map[string]struct{} {
	moved := map[string]struct{}{}
	for id, group := range groupNow {
		if groupOld[id] != group {
			moved[id] = struct{}{}
		}
	}
	for _, ids := range orderNow {
		members := []string{}
		for _, id := range ids {
			if _, gone := moved[id]; !gone {
				members = append(members, id)
			}
		}
		steady := notionMigrationSteady(members, old)
		for _, id := range members {
			if _, kept := steady[id]; !kept {
				moved[id] = struct{}{}
			}
		}
	}
	return moved
}

// placeNotionMigrationMovedBlocks moves, a window of pages a turn, the
// blocks the source moved since the run before and nobody moved in Leafen;
// true while more remain.
func (r *durableJobRunner) placeNotionMigrationMovedBlocks(
	ctx context.Context, job storedJob, payload *notionImportJobPayload,
) (bool, error) {
	if r.ygo == nil {
		return false, errNotionMigrationRestructure
	}
	if payload.ReorderCursor == "" {
		if err := updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
			report.BlocksMoved, report.BlocksMoveKept = 0, 0
		}); err != nil {
			return false, err
		}
		// Blocks that went to other pages since are known by their pages now.
		if err := rehomeNotionMigrationBlocks(r.app, payload.MigrationID); err != nil {
			return false, err
		}
	}
	pages, more, err := notionMigrationPlacePages(r.app, payload.MigrationID, payload.ReorderCursor)
	if err != nil {
		return false, err
	}
	movedCount, keptCount := 0, 0
	for _, pageID := range pages {
		payload.ReorderCursor = pageID
		page, err := loadNotionMigrationPagePlaces(r.app, payload.AuthorityOwnerID, job.ID, payload.MigrationID, pageID)
		if err != nil {
			return false, err
		}
		operations, kept, away := page.moves()
		keptCount += kept
		if err := markNotionMigrationBlocksLeaving(r.app, payload.MigrationID, page, away); err != nil {
			return false, err
		}
		// The moves go one after another, as many as a change takes at once.
		for start := 0; start < len(operations); start += maxGraphMutationOperations {
			chunk := operations[start:min(start+maxGraphMutationOperations, len(operations))]
			_, err = r.ygo.applyContentGraphMutationContext(ctx, job.Owner, pageID, graphMutationInput{
				MutationID: fmt.Sprintf("notion-migration-place-%s-%s-%d", job.ID, pageID, start), Operations: chunk,
			}, contentGraphMutationHooks{
				Authorize: func(txApp core.App) (pageAuthorization, error) {
					return authorizePageBody(txApp, job.Owner, pageID, permissionWorkspaceWrite)
				},
			})
			if isPermissionDenied(err) {
				// The person can't change the page now; the blocks stay.
				keptCount += len(operations) - start
				break
			}
			if err != nil {
				return false, err
			}
			movedCount += len(chunk)
		}
	}
	if !more {
		sent, err := sendNotionMigrationBlocksAway(r.app, job, payload)
		if err != nil {
			return false, err
		}
		movedCount += sent
	}
	if movedCount > 0 || keptCount > 0 {
		if err := updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
			report.BlocksMoved += movedCount
			report.BlocksMoveKept += keptCount
		}); err != nil {
			return false, err
		}
	}
	return more, nil
}

// moves plans a page's moves, in the source's order, and counts the blocks
// the source moved that stay because somebody moved them in Leafen. A block
// that arrived from another page takes its place as the source has it,
// unless somebody moved it since; one the source moved to another page is
// named apart (away), for a move to that page.
func (page *notionMigrationPagePlaces) moves() ([]graphOperation, int, []string) {
	places := map[string]notionMigrationBlockPlace{}
	leafenNow, leafenOld, leafenOrder, leafenIndex := map[string]string{}, map[string]string{}, map[string][]string{}, map[string]int{}
	sourceNow, sourceOld, sourceIndex := map[string]string{}, map[string]string{}, map[string]int{}
	arrived := []string{}
	for nativeID, entry := range page.ledger {
		parent, standing := page.parents[nativeID]
		place := notionMigrationBlockPlace{}
		if !standing || entry.Place == "" || json.Unmarshal([]byte(entry.Place), &place) != nil {
			continue
		}
		if place.Arrived {
			if _, staged := page.staged[nativeID]; staged && parent == place.Parent {
				arrived = append(arrived, nativeID)
			}
			continue
		}
		places[nativeID] = place
		leafenNow[nativeID], leafenOld[nativeID], leafenIndex[nativeID] = parent, place.Parent, place.Index
		if staged, found := page.staged[nativeID]; found && place.SourceParent != "" {
			sourceNow[nativeID], sourceOld[nativeID], sourceIndex[nativeID] = staged.parent(), place.SourceParent, place.SourcePosition
		}
	}
	for parent, ids := range page.order {
		for _, id := range ids {
			if _, known := places[id]; known && leafenNow[id] == leafenOld[id] {
				leafenOrder[parent] = append(leafenOrder[parent], id)
			}
		}
	}
	movedInLeafen := notionMigrationMoved(leafenNow, leafenOld, leafenOrder, leafenIndex)
	sourceOrder := map[string][]string{}
	for id, parent := range sourceNow {
		if sourceOld[id] == parent {
			sourceOrder[parent] = append(sourceOrder[parent], id)
		}
	}
	for parent := range sourceOrder {
		ids := sourceOrder[parent]
		sort.Slice(ids, func(i, j int) bool { return page.staged[ids[i]].Position < page.staged[ids[j]].Position })
	}
	movedInSource := notionMigrationMoved(sourceNow, sourceOld, sourceOrder, sourceIndex)

	// Each source parent's children on the page, in the source's order, and
	// the blocks on it by their source.
	children, blocksBySource := map[string][]string{}, map[string]string{}
	for id, staged := range page.staged {
		children[staged.parent()] = append(children[staged.parent()], id)
		if staged.Kind != "page" {
			blocksBySource[staged.SourceID] = id
		}
	}
	for parent := range children {
		ids := children[parent]
		sort.Slice(ids, func(i, j int) bool { return page.staged[ids[i]].Position < page.staged[ids[j]].Position })
	}
	candidates := []string{}
	kept := 0
	for id := range movedInSource {
		if _, moved := movedInLeafen[id]; moved {
			kept++
			continue
		}
		candidates = append(candidates, id)
	}
	candidates = append(candidates, arrived...)
	away := []string{}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := page.staged[candidates[i]], page.staged[candidates[j]]
		if a.parent() != b.parent() {
			return a.parent() < b.parent()
		}
		return a.Position < b.Position
	})
	order := make(map[string][]string, len(page.order))
	parents := make(map[string]string, len(page.parents))
	for parent, ids := range page.order {
		order[parent] = append([]string(nil), ids...)
	}
	for id, parent := range page.parents {
		parents[id] = parent
	}
	operations := []graphOperation{}
	for _, id := range candidates {
		staged := page.staged[id]
		// Its parent on this page: the page, or a block on it.
		target, found := "", false
		if staged.ParentKind == "page" || staged.ParentKind == "row" {
			_, found = page.pageSources[staged.ParentSourceID]
		} else if staged.ParentKind == "block" {
			target, found = blocksBySource[staged.ParentSourceID]
		}
		if !found {
			// The source moved a block to another page, unless it moved its
			// parent too, with it.
			if staged.Kind != "page" {
				away = append(away, id)
			}
			continue
		}
		if notionMigrationInside(parents, target, id) {
			kept++
			continue
		}
		// It follows the nearest block before it in the source that stands in
		// the parent, passing by one somebody moved in Leafen.
		var after *string
		siblings := children[staged.parent()]
		for index := len(siblings) - 1; index >= 0; index-- {
			sibling := siblings[index]
			if _, moved := movedInLeafen[sibling]; moved || page.staged[sibling].Position >= staged.Position || sibling == id {
				continue
			}
			if parents[sibling] == target && structuralIndex(order[target], sibling) >= 0 {
				anchor := sibling
				after = &anchor
				break
			}
		}
		current := structuralIndex(order[target], id)
		if parents[id] == target && current >= 0 {
			if after == nil && current == 0 || after != nil && current > 0 && order[target][current-1] == *after {
				continue
			}
		}
		from := order[parents[id]]
		if at := structuralIndex(from, id); at >= 0 {
			order[parents[id]] = append(from[:at:at], from[at+1:]...)
		}
		to, insertAt := order[target], 0
		if after != nil {
			insertAt = structuralIndex(to, *after) + 1
		}
		order[target] = append(to[:insertAt:insertAt], append([]string{id}, to[insertAt:]...)...)
		parents[id] = target
		move := &graphBlockMove{BlockID: id, AfterID: after}
		if target != "" {
			parent := target
			move.ParentID = &parent
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindMoveBlock, Move: move})
	}
	return operations, kept, away
}

// notionMigrationInside reports whether a block is the target or holds it.
func notionMigrationInside(parents map[string]string, target, id string) bool {
	for seen := 0; target != "" && seen <= len(parents); seen++ {
		if target == id {
			return true
		}
		target = parents[target]
	}
	return false
}
