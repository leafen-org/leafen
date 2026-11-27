package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A restore writes the page's blocks anew, which gave every file the
// restore's time: a sync tool that goes by times fetched every file again,
// those the restore left as they were too (2026-10-16, the drive's WebDAV). A
// block holding the same file before and after keeps its times and who made
// and last changed it; a block of words is left to the restore, whose words
// live apart from the row.

type pageHistoryFileTime struct {
	ID           string `db:"id"`
	Type         string `db:"type"`
	FileID       string `db:"file_id"`
	Created      int64  `db:"created"`
	Updated      int64  `db:"updated"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
}

// pageHistoryKeptFileTimes are the page's file blocks as they stand, by block.
func pageHistoryKeptFileTimes(app core.App, pageID string) (map[string]pageHistoryFileTime, error) {
	rows := []pageHistoryFileTime{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], COALESCE(json_extract([[content]], '$.fileId'), '') AS [[file_id]],
			[[created]], [[updated]], [[created_by]], [[last_edited_by]]
		FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_search_projection]]
		WHERE [[page_id]] = {:pageId} AND [[type]] ` + managedFileBlockTypesSQL + `
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	kept := make(map[string]pageHistoryFileTime, len(rows))
	for _, row := range rows {
		if row.FileID != "" {
			kept[row.ID] = row
		}
	}
	return kept, nil
}

// keepPageHistoryFileTimes gives a restored block holding the file it held
// before its times back.
func keepPageHistoryFileTimes(app core.App, kept map[string]pageHistoryFileTime, block pageHistoryBlockWire) error {
	before, found := kept[block.ID]
	if !found || before.Type != block.Type {
		return nil
	}
	content := struct {
		FileID string `json:"fileId"`
	}{}
	if json.Unmarshal(block.Content, &content) != nil || content.FileID != before.FileID {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_blocks}} SET [[created]] = {:created}, [[updated]] = {:updated},
			[[created_by]] = {:createdBy}, [[last_edited_by]] = {:lastEditedBy}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": block.ID, "created": before.Created, "updated": before.Updated,
		"createdBy": before.CreatedBy, "lastEditedBy": before.LastEditedBy}).Execute()
	return err
}
