package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The issues a Notion import meets on its blocks — a text that lost or kept
// a reference, a block its publication left out or kept in another form, one
// left out while sources are applied — are kept as sources of their own, one
// for each block and reason, which the import's report and window count.

// recordNotionImportBlockTextIssue reports that a block's rich text kept its
// words but lost a mention or link to something the import did not bring, or
// kept it as a link to it in Notion.
func recordNotionImportBlockTextIssue(app core.App, jobID string, block storedNotionBlock, reason string) error {
	sourceID := ""
	err := app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='block' AND [[native_id]]={:nativeId}
	`).Bind(dbx.Params{"jobId": jobID, "nativeId": block.ID}).Row(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return recordNotionImportPublicationIssue(app, jobID, sourceID, block.Type, reason)
}

// recordNotionImportPublicationIssue reports a block its page's publication
// left out or kept in another form, where the Notion block was.
func recordNotionImportPublicationIssue(app core.App, jobID, sourceID, blockType, reason string) error {
	_, err := insertNotionImportBlockIssue(app, jobID, sourceID, blockType, reason)
	return err
}

// recordNotionImportApplyIssue reports a block left out while sources are still
// being applied. It adds to the import's items, since the apply turns read it
// after the source that found it.
func recordNotionImportApplyIssue(app core.App, jobID, sourceID, blockType, reason string) error {
	inserted, err := insertNotionImportBlockIssue(app, jobID, sourceID, blockType, reason)
	if err != nil || inserted == 0 {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[total_items]] = [[total_items]] + {:inserted} WHERE [[id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID, "inserted": inserted}).Execute()
	return err
}

func insertNotionImportBlockIssue(app core.App, jobID, sourceID, blockType, reason string) (int64, error) {
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_sources}}
			([[job_id]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]],
			 [[text]], [[blueprint]], [[discovery_status]], [[discovery_cursor]], [[discovery_position]], [[created]])
		SELECT [[job_id]], [[source_id]] || '#' || {:reason}, 'issue', [[parent_source_id]], [[parent_kind]],
			[[position]], {:blockType}, {:reason}, 0, 'complete', '', 0, {:created}
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sourceId": sourceID, "blockType": blockType, "reason": reason,
		"created": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
