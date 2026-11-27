package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A sub-page turned back into its block takes the page's own discussions to
// that block, as Notion's does (seen 2026-10-14: the page comment became
// the new line's); otherwise they would go with the page to the trash and,
// with it, for good. Restoring the page takes the block's back. The block
// keeps the page's ID, so the discussions name it there.
func moveConvertedPageComments(app core.App, pageID, parentID string, toBlock bool) error {
	if !tableKnown(app, "leafen_comments") || parentID == "" {
		return nil
	}
	fromPage, fromBlock, toPage, toBlockID := pageID, "", parentID, pageID
	if !toBlock {
		fromPage, fromBlock, toPage, toBlockID = parentID, pageID, pageID, ""
	}
	params := dbx.Params{"fromPage": fromPage, "fromBlock": fromBlock, "toPage": toPage, "toBlock": toBlockID}
	if tableKnown(app, "leafen_file_references") {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_file_references}} SET [[page_id]]={:toPage}
			WHERE [[page_id]]={:fromPage} AND [[owner_kind]]='comment' AND [[owner_id]] IN (
				SELECT [[id]] FROM {{leafen_comments}}
				WHERE [[page_id]]={:fromPage} AND [[target_kind]]='page' AND [[target_id]]={:fromPage}
					AND [[block_id]]={:fromBlock})
		`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_comments}} SET [[page_id]]={:toPage},[[target_id]]={:toPage},[[block_id]]={:toBlock}
		WHERE [[page_id]]={:fromPage} AND [[target_kind]]='page' AND [[target_id]]={:fromPage}
			AND [[block_id]]={:fromBlock}
	`).Bind(params).Execute()
	return err
}
