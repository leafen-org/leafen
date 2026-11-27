package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func ensurePageHistoryProjectionBaseline(app core.App, pageID string) error {
	// One span tells the page has its spans; counting them all read the
	// page's whole history at each preview window.
	var existing int
	if err := app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_page_history_block_spans}}
		WHERE [[page_id]]={:pageId})`).Bind(dbx.Params{"pageId": pageID}).Row(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	checkpoint, err := loadPageHistoryCheckpoint(app, pageID, 0)
	if err != nil {
		return err
	}
	blocks := []pageHistoryBlockState{}
	if err := json.Unmarshal([]byte(checkpoint.BlocksJSON), &blocks); err != nil {
		return err
	}
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].ParentID != blocks[j].ParentID {
			return blocks[i].ParentID < blocks[j].ParentID
		}
		if blocks[i].Position != blocks[j].Position {
			return blocks[i].Position < blocks[j].Position
		}
		return blocks[i].ID < blocks[j].ID
	})
	collaboration := crdt.New()
	if err := crdt.ApplyUpdateV1(collaboration, checkpoint.CollaborationState, nil); err != nil {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	workspaceID := page.GetString("workspace")
	predecessor := ""
	for _, block := range blocks {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_history_block_spans}}
				([[workspace_id]],[[page_id]],[[block_id]],[[valid_from]],[[valid_through]],
				 [[after_id]],[[parent_id]],[[type]],[[content]],[[created_at]],[[updated_at]])
			VALUES ({:workspaceId},{:pageId},{:blockId},0,NULL,
				{:afterId},{:parentId},{:type},{:content},{:created},{:updated})
			ON CONFLICT ([[page_id]],[[block_id]],[[valid_from]]) DO NOTHING
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "pageId": pageID, "blockId": block.ID,
			"afterId": predecessor, "parentId": block.ParentID, "type": block.Type,
			"content": string(block.Content), "created": block.CreatedAt, "updated": block.UpdatedAt,
		}).Execute(); err != nil {
			return err
		}
		// The text keeps its formatting, mentions and equations.
		runs := pageHistoryBlockTextRuns(collaboration, block.ID)
		rich, err := encodePageHistoryTextRuns(runs)
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_history_text_spans}}
				([[workspace_id]],[[page_id]],[[block_id]],[[valid_from]],[[valid_through]],[[plain_text]],[[rich_text]])
			VALUES ({:workspaceId},{:pageId},{:blockId},0,NULL,{:plainText},{:richText})
			ON CONFLICT ([[page_id]],[[block_id]],[[valid_from]]) DO NOTHING
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "pageId": pageID,
			"blockId": block.ID, "plainText": pageHistoryTextRunsText(runs), "richText": rich,
		}).Execute(); err != nil {
			return err
		}
		predecessor = block.ID
	}
	return nil
}

// replacePageHistoryProjectionAtVersion sets the history's open spans from
// the page's checkpoint at a version: the one documents has just taken, else
// the stored one.
func replacePageHistoryProjectionAtVersion(
	app core.App, pageID string, version int, documents *pageHistoryDocuments,
) error {
	blocksJSON, collaboration, _, err := documents.checkpointAt(app, pageID, version, false)
	if err != nil {
		return err
	}
	blocks := []pageHistoryBlockState{}
	if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	workspaceID := page.GetString("workspace")
	// The open spans from their own indexes: the planner took the history's
	// and read every span the page ever had (2026-11-18).
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_history_block_spans}}
		INDEXED BY [[idx_leafen_page_history_block_spans_open]]
		SET [[valid_through]]={:through} WHERE [[page_id]]={:pageId} AND [[valid_through]] IS NULL;
		UPDATE {{leafen_page_history_text_spans}} INDEXED BY [[idx_leafen_page_history_text_spans_open]]
		SET [[valid_through]]={:through} WHERE [[page_id]]={:pageId} AND [[valid_through]] IS NULL`).Bind(
		dbx.Params{"through": version - 1, "pageId": pageID},
	).Execute(); err != nil {
		return err
	}
	predecessor := ""
	for _, block := range blocks {
		if _, err := app.DB().Insert(pageHistoryBlockSpansTable, dbx.Params{
			"workspace_id": workspaceID, "page_id": pageID, "block_id": block.ID,
			"valid_from": version, "valid_through": nil, "after_id": predecessor,
			"parent_id": block.ParentID, "type": block.Type, "content": string(block.Content),
			"created_at": block.CreatedAt, "updated_at": block.UpdatedAt,
		}).Execute(); err != nil {
			return err
		}
		runs := pageHistoryBlockTextRuns(collaboration, block.ID)
		rich, err := encodePageHistoryTextRuns(runs)
		if err != nil {
			return err
		}
		if _, err := app.DB().Insert(pageHistoryTextSpansTable, dbx.Params{
			"workspace_id": workspaceID, "page_id": pageID, "block_id": block.ID,
			"valid_from": version, "valid_through": nil, "plain_text": pageHistoryTextRunsText(runs),
			"rich_text": rich,
		}).Execute(); err != nil {
			return err
		}
		predecessor = block.ID
	}
	return nil
}
