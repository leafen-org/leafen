package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database whose block alone went to the trash, its page out of
// it, opens alone under the trash's banner — from the trash's list and from
// its mentions — as Notion's does (seen 2026-11-24 in Chrome at 1440 under
// a Notion test page, two tabs: "W2559 블록 DB" opened by its mention at
// /p/<its ID> with "… 님이 이 페이지를 휴지통으로 이동했습니다", "페이지 복원"
// and "영구 삭제하기"; restored, it stays open, out of the trash). Its page's
// graph, asked for it (trashedDatabase=<its ID>, content_graph.go), carries
// it beside the page's own: its block as the trash keeps it, marked
// trashedAlone, its container, and its sources in the trash (sourceInTrash),
// which the page shows read only, with their views, properties and rows.
// Asked otherwise, the graph leaves it out as before.

// graphTrashedDatabase is the database a page's graph is asked to carry.
type graphTrashedDatabase struct {
	containerID string
	sourceIDs   []string
	// block is its block's read item, as the graph's blocks are read.
	block string
}

// loadGraphTrashedDatabase finds the page's database of that ID whose block
// alone is in the trash — in the recoverable trash of blocks, or the deleted
// databases' — or nil for one that isn't, or whose page is in the trash too
// (its page's graph carries it then, PageView's banner its page's).
func loadGraphTrashedDatabase(app core.App, owner, pageID, containerID string) (*graphTrashedDatabase, error) {
	if inTrash, err := pageInTrashPath(app, pageID); err != nil || inTrash {
		return nil, err
	}
	found := struct {
		Snapshot string `db:"snapshot"`
		Content  string `db:"content"`
		Sources  string `db:"sources"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[tombstone]].[[snapshot_json]] AS [[snapshot]], '' AS [[content]], COALESCE((
			SELECT json_group_array([[member]].[[data_source_id]]) FROM {{leafen_database_container_sources}} [[member]]
			WHERE [[member]].[[container_id]] = [[container]].[[id]] AND [[member]].[[source_container_id]] = ''), '[]') AS [[sources]]
		FROM {{leafen_database_container_trash}} [[trash]]
		JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[trash]].[[container_id]]
		JOIN {{leafen_block_trash_tombstones}} [[tombstone]]
			ON [[tombstone]].[[root_block_id]] = [[trash]].[[root_block_id]] AND [[tombstone]].[[page_id]] = [[container]].[[page_id]]
		WHERE [[trash]].[[container_id]] = {:id} AND [[container]].[[page_id]] = {:pageId} AND [[container]].[[owner]] = {:owner}
		UNION ALL
		SELECT '', [[deleted]].[[block_content]], [[deleted]].[[data_source_ids]]
		FROM {{leafen_deleted_databases}} [[deleted]]
		WHERE [[deleted]].[[block_id]] = {:id} AND [[deleted]].[[page_id]] = {:pageId} AND [[deleted]].[[owner]] = {:owner}
		LIMIT 1
	`).Bind(dbx.Params{"id": containerID, "pageId": pageID, "owner": owner}).One(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	block := recoverableBlockTrashBlock{ID: containerID, Type: graphBlockTypeInlineDatabase, Content: json.RawMessage(found.Content)}
	if found.Snapshot != "" {
		snapshot := recoverableBlockTrashSnapshot{}
		if err := json.Unmarshal([]byte(found.Snapshot), &snapshot); err != nil {
			return nil, err
		}
		kept := false
		for _, candidate := range snapshot.Blocks {
			if candidate.ID == containerID && candidate.Type == graphBlockTypeInlineDatabase {
				block, kept = candidate, true
			}
		}
		if !kept {
			return nil, nil
		}
	}
	content := map[string]any{}
	if err := json.Unmarshal(block.Content, &content); err != nil {
		return nil, err
	}
	content["trashedAlone"] = true
	payload, err := json.Marshal(map[string]any{
		"id": containerID, "parentId": nil, "afterId": nil, "position": block.Position, "type": block.Type, "content": content,
	})
	if err != nil {
		return nil, err
	}
	trashed := &graphTrashedDatabase{containerID: containerID, block: string(payload)}
	if err := json.Unmarshal([]byte(found.Sources), &trashed.sourceIDs); err != nil {
		return nil, err
	}
	return trashed, nil
}

// bind names its container and sources to the graph read's queries.
func (trashed *graphTrashedDatabase) bind(params dbx.Params) {
	if trashed == nil {
		return
	}
	params["trashedContainerId"] = trashed.containerID
	for index, sourceID := range trashed.sourceIDs {
		params[fmt.Sprintf("trashedSource%d", index)] = sourceID
	}
}

// containerSQL holds for its container.
func (trashed *graphTrashedDatabase) containerSQL(expression string) string {
	if trashed == nil {
		return "0"
	}
	return expression + " = {:trashedContainerId}"
}

// sourceSQL holds for its sources.
func (trashed *graphTrashedDatabase) sourceSQL(expression string) string {
	if trashed == nil || len(trashed.sourceIDs) == 0 {
		return "0"
	}
	names := make([]string, len(trashed.sourceIDs))
	for index := range trashed.sourceIDs {
		names[index] = fmt.Sprintf("{:trashedSource%d}", index)
	}
	return expression + " IN (" + strings.Join(names, ", ") + ")"
}

// sourceShownSQL is databaseSourceShownSQL, holding for its sources too.
func (trashed *graphTrashedDatabase) sourceShownSQL(expression string) string {
	if trashed == nil {
		return databaseSourceShownSQL(expression)
	}
	return "(" + databaseSourceShownSQL(expression) + " OR " + trashed.sourceSQL(expression) + ")"
}

// sourceInTrashSQL is when a source the page shows reads as in the trash: kept
// in its database there (databaseSourceTrashedInPlaceSQL), or one of its.
func (trashed *graphTrashedDatabase) sourceInTrashSQL(expression string) string {
	return "(" + databaseSourceTrashedInPlaceSQL(expression) + " OR " + trashed.sourceSQL(expression) + ")"
}

// readBlock puts its block in the window, first of the page's blocks.
func (trashed *graphTrashedDatabase) readBlock(window *graphWindowRead, keys []string) {
	if trashed == nil || keys != nil || window.room <= 0 {
		return
	}
	window.items = append(window.items, storedGraphReadItem{
		Kind: graphReadKindBlock, SortKey: encodeGraphWindowCursor(graphWindowBlocks, []string{"", "", ""}), Payload: trashed.block,
	})
	window.room--
}

// afterKeys is the condition for items whose key columns come after keys.
func afterKeys(params dbx.Params, columns []string, keys []string) string {
	if keys == nil {
		return "1"
	}
	names := make([]string, len(keys))
	for index, key := range keys {
		name := fmt.Sprintf("after%d", index)
		params[name] = key
		names[index] = "{:" + name + "}"
	}
	return "(" + strings.Join(columns, ", ") + ") > (" + strings.Join(names, ", ") + ")"
}
