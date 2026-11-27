package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A result found by the words of a document said only its page — not which
// of the page's files held them (2026-10-16: a phrase only budget.xlsx holds
// found "문서 모음" and nothing more). The result's line is the words as they
// stand in the search body, which takes a file block's name and a document's
// words as they are, so the file whose name or words hold the line is the one
// it came from: a file block of the page, in the page's order, or else a file
// a database item's property holds, leaving out properties whose access rules
// keep them from search.

// workspaceSearchFileOf names the file a result's line came from, or nothing.
func workspaceSearchFileOf(app core.App, pageID, dataSourceID, line string) (*workspaceSearchFile, error) {
	if line == "" {
		return nil, nil
	}
	found := struct {
		BlockID string `db:"block_id"`
		Name    string `db:"name"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[block]].[[id]] AS [[block_id]], COALESCE(json_extract([[block]].[[content]], '$.name'), '') AS [[name]]
		FROM {{leafen_blocks}} [[block]]
		LEFT JOIN {{leafen_file_text_extracts}} [[text]] ON [[text]].[[file_id]] = json_extract([[block]].[[content]], '$.fileId')
			AND ([[text]].[[status]] = 'done' OR [[text]].[[partial]] != '')
		WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[type]] IN ('file', 'pdf')
			AND (instr(COALESCE([[text]].[[text]], ''), {:line}) > 0
				OR [[block]].[[type]] = 'file' AND instr(trim(COALESCE(json_extract([[block]].[[content]], '$.name'), '')), {:line}) > 0)
		ORDER BY [[block]].[[parent_id]], [[block]].[[position]], [[block]].[[id]]
		LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "line": line}).One(&found)
	if err == nil {
		return &workspaceSearchFile{BlockID: found.BlockID, Name: found.Name}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) || dataSourceID == "" {
		return nil, ignoreNoRows(err)
	}
	rules, err := loadDatabasePropertyAccessRules(app, []string{dataSourceID})
	if err != nil {
		return nil, err
	}
	rows := []struct {
		PropertyID string `db:"property_id"`
		Name       string `db:"name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[value]].[[property_id]], COALESCE(json_extract([[attachment]].[[value]], '$.name'), '') AS [[name]]
		FROM {{leafen_database_row_values}} [[value]]
		CROSS JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[value]].[[property_id]]
		CROSS JOIN json_each(CASE WHEN json_valid([[value]].[[value]]) THEN [[value]].[[value]] ELSE '[]' END) AS [[attachment]]
		CROSS JOIN {{leafen_file_text_extracts}} [[text]] ON [[text]].[[file_id]] = json_extract([[attachment]].[[value]], '$.fileId')
		WHERE [[value]].[[row_id]] = {:rowId} AND [[value]].[[type]] = 'files'
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
			AND ([[text]].[[status]] = 'done' OR [[text]].[[partial]] != '') AND instr([[text]].[[text]], {:line}) > 0
		ORDER BY [[property]].[[position]], [[property]].[[id]], [[attachment]].[[key]]
	`).Bind(dbx.Params{"rowId": pageID, "line": line}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if rules[dataSourceID][row.PropertyID] == nil {
			return &workspaceSearchFile{Name: row.Name}, nil
		}
	}
	return nil, nil
}

func ignoreNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
