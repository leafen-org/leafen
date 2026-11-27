package organization

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// contentCustodianKind is one kind of item a custodian window holds: where
// its items are, the two indexes that keep a workspace's items by who made
// and who last edited each, in id order, and what a window says of one.
type contentCustodianKind struct {
	name, table, workspace, madeIndex, editedIndex, only, columns string
}

// The kinds in the order a window reads them: by name, then each by id.
var contentCustodianKinds = []contentCustodianKind{
	{
		name: "block", table: "leafen_blocks", workspace: "workspace_id",
		madeIndex: "idx_leafen_blocks_workspace_created_by_id", editedIndex: "idx_leafen_blocks_workspace_last_edited_by_id",
		columns: `[[item]].[[page_id]], [[item]].[[created_by]], [[item]].[[last_edited_by]],
			json_object(
				'id', [[item]].[[id]], 'pageId', [[item]].[[page_id]],
				'parentId', [[item]].[[parent_id]], 'position', [[item]].[[position]],
				'type', [[item]].[[type]], 'content', json([[item]].[[content]])
			) AS [[payload]]`,
	},
	{
		name: "page", table: "leafen_pages", workspace: "workspace",
		madeIndex: "idx_leafen_pages_workspace_created_by_id", editedIndex: "idx_leafen_pages_workspace_last_edited_by_id",
		only: ` AND [[data_source_id]] = ''`,
		columns: `[[item]].[[id]] AS [[page_id]], [[item]].[[created_by]], [[item]].[[last_edited_by]],
			json_object(
				'id', [[item]].[[id]], 'title', [[item]].[[title]],
				'content', [[item]].[[content]], 'inTrash', [[item]].[[in_trash]]
			) AS [[payload]]`,
	},
}

// contentCustodianIDChunk is how many custodians one statement reads: two
// terms each of a compound SELECT, of which SQLite takes 500.
const contentCustodianIDChunk = 100

// listContentCustodianWindow is the next items the custodians made or last
// edited in the workspace, after (afterKind, afterID) in the order of kind
// and id, one more than a window holds when there are more. It reads what
// the window holds, not what the custodians made: each custodian's items
// are read in id order from the cursor, a window's worth at most, and only
// the window's own items are read whole (2026-11-18: each window read the
// workspace's every page and block).
func listContentCustodianWindow(
	app core.App,
	workspaceID string,
	custodianIDs []string,
	afterKind, afterID string,
) ([]leafen.ContentCustodianItem, bool, error) {
	if workspaceID == "" || len(custodianIDs) == 0 {
		return nil, false, leafen.ErrLegalHoldUnavailable
	}
	normalized, err := normalizeLegalHoldIDs(custodianIDs)
	if err != nil || len(normalized) != len(custodianIDs) {
		return nil, false, leafen.ErrLegalHoldUnavailable
	}
	limit := leafen.ContentCustodianWindowLimit + 1
	items := []leafen.ContentCustodianItem{}
	for _, kind := range contentCustodianKinds {
		// The cursor is past this kind, in it, or before it.
		after := ""
		if afterKind > kind.name || len(items) >= limit {
			continue
		} else if afterKind == kind.name {
			after = afterID
		}
		ids, err := contentCustodianWindowIDs(app, kind, workspaceID, normalized, after, limit-len(items))
		if err != nil {
			return nil, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		if len(ids) == 0 {
			continue
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		found := []leafen.ContentCustodianItem{}
		if err := app.DB().NewQuery(`
			SELECT '` + kind.name + `' AS [[kind]], [[item]].[[id]], ` + kind.columns + `
			FROM {{` + kind.table + `}} [[item]]
			WHERE [[item]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			ORDER BY [[item]].[[id]]
		`).Bind(dbx.Params{"ids": string(encoded)}).All(&found); err != nil {
			return nil, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		items = append(items, found...)
	}
	hasMore := len(items) > leafen.ContentCustodianWindowLimit
	if hasMore {
		items = items[:leafen.ContentCustodianWindowLimit]
	}
	return items, hasMore, nil
}

// contentCustodianWindowIDs is the first limit ids after the cursor, in id
// order, of the kind's items in the workspace that one of the custodians
// made or last edited: the first limit of each custodian's own, by each of
// the kind's two indexes, of which the first limit are the answer.
func contentCustodianWindowIDs(
	app core.App, kind contentCustodianKind, workspaceID string, custodianIDs []string, after string, limit int,
) ([]string, error) {
	ids := []string{}
	for start := 0; start < len(custodianIDs); start += contentCustodianIDChunk {
		chunk := custodianIDs[start:min(start+contentCustodianIDChunk, len(custodianIDs))]
		params := dbx.Params{"workspaceId": workspaceID, "after": after, "limit": limit}
		terms := make([]string, 0, 2*len(chunk))
		for index, custodianID := range chunk {
			key := fmt.Sprintf("custodian%d", index)
			params[key] = custodianID
			for _, by := range [][2]string{{kind.madeIndex, "created_by"}, {kind.editedIndex, "last_edited_by"}} {
				terms = append(terms, `SELECT [[id]] FROM (
					SELECT [[id]] FROM {{`+kind.table+`}} INDEXED BY [[`+by[0]+`]]
					WHERE [[`+kind.workspace+`]] = {:workspaceId} AND [[`+by[1]+`]] = {:`+key+`} AND [[id]] > {:after}`+kind.only+`
					ORDER BY [[id]] LIMIT {:limit})`)
			}
		}
		found := []string{}
		if err := app.DB().NewQuery(`SELECT [[id]] FROM (` + strings.Join(terms, " UNION ") + `) ORDER BY [[id]] LIMIT {:limit}`).
			Bind(params).Column(&found); err != nil {
			return nil, err
		}
		ids = append(ids, found...)
	}
	if len(custodianIDs) <= contentCustodianIDChunk {
		return ids, nil
	}
	// Custodians read by different statements share items: the same id once,
	// and the first limit of them all.
	sort.Strings(ids)
	unique := ids[:0]
	for index, id := range ids {
		if index == 0 || id != ids[index-1] {
			unique = append(unique, id)
		}
	}
	return unique[:min(limit, len(unique))], nil
}
