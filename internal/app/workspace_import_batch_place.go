package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// workspaceImportBatchPlacement is where a batch's page goes among its
// siblings in its parent's body: in the batch's order, as Notion's ZIP
// import lists a folder's folders first and then its files (seen
// 2026-11-07). Pages are made as documents go in, so a folder with nothing
// to import is made last; it goes right after the nearest sibling before it
// that is made, or at the start of a folder's page when only siblings after
// it are. A document's own page keeps its text first: there it goes at the
// end, unless its text already stood it where it names it. The sidebar
// follows the block (placeChildPageBlock).
func workspaceImportBatchPlacement(txApp core.App, jobID string, row workspaceImportBatchPageRow) (*pageBodyPlacement, error) {
	if row.ParentPosition < 0 || !row.ParentPageID.Valid || row.ParentPageID.String == "" {
		return nil, nil
	}
	params := dbx.Params{"jobId": jobID, "parent": row.ParentPosition, "position": row.Position, "parentPage": row.ParentPageID.String}
	var anchor string
	err := txApp.DB().NewQuery(`
		SELECT [[block]].[[id]] FROM {{leafen_workspace_import_batch_items}} AS [[item]]
		JOIN {{leafen_blocks}} AS [[block]]
			ON [[block]].[[page_id]] = {:parentPage} AND [[block]].[[parent_id]] = '' AND [[block]].[[type]] = 'child_page'
			AND json_extract([[block]].[[content]], '$.childPageId') = [[item]].[[page_id]]
		WHERE [[item]].[[job_id]] = {:jobId} AND [[item]].[[parent_position]] = {:parent}
			AND [[item]].[[position]] < {:position} AND [[item]].[[page_id]] <> ''
		ORDER BY [[item]].[[position]] DESC LIMIT 1
	`).Bind(params).Row(&anchor)
	switch {
	case err == nil:
		return &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemBlock, AfterID: anchor}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	var parentKind string
	if err := txApp.DB().NewQuery(`
		SELECT [[kind]] FROM {{leafen_workspace_import_batch_items}} WHERE [[job_id]] = {:jobId} AND [[position]] = {:parent}
	`).Bind(params).Row(&parentKind); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if parentKind == "folder" {
		return &pageBodyPlacement{Type: "start"}, nil
	}
	return nil, nil
}

// alignWorkspaceImportSubPages moves in the sidebar the sub-pages a
// document's text stood where it names them (leafen-subpage), when their
// pages were made before it: their blocks moved, and the sidebar follows as
// it does a person's move (child_page_blocks.go).
func alignWorkspaceImportSubPages(
	txApp core.App, authorization pageAuthorization, doc *crdt.Doc, pageID string, items []workspaceImportStagedItem,
) error {
	placed := []string{}
	for _, item := range items {
		if item.Type == graphBlockTypeChildPage {
			placed = append(placed, item.ID)
		}
	}
	if len(placed) == 0 {
		return nil
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	return alignChildPageSidebar(
		txApp, doc, metadata, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID, pageID, placed,
	)
}
