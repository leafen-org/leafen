package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database of two or more sources of its own goes by a name of
// its own, apart from its sources', as Notion's does (seen 2026-11-11: the
// title typed on it opened alone names it in its sidebar row, path and
// window, its sources keeping theirs; a source added to one of one source,
// on the screen or through the API, leaves it unnamed). Its block keeps that
// name with its formatting (pageTitleSpans); without one it goes by its
// sources' names listed ("A 및 B", untitledDatabaseSourcesName). One of one
// source goes by that source's name; one sent to the trash with its views
// (databaseTrashedWithViewsField) no longer counts, as Notion's (seen
// 2026-11-11: one of two so sent leaves it "새 데이터베이스", its other
// untitled source the same). Its sidebar row and favorite, its piece
// of a path, its search entry, a page preview naming it and its API title
// (notion_inline_database_projection.go) read it here.

// inlineDatabaseKeptName is the name a database block keeps of its own, with
// its formatting; none where it keeps none.
func inlineDatabaseKeptName(content []byte) databaseNameRead {
	spans := blockPageNameSpans(content, "pageTitleSpans")
	if len(spans) == 0 {
		return databaseNameRead{}
	}
	return databaseNameRead{Title: spelledName(spans), Spans: spans}
}

// inlineDatabaseNamesOfSeveral names, of the databases listed by their
// blocks' IDs (their containers'), each one held inline with two or more
// sources of its own by its own name as a reader's closer reads it, or as
// written for no closer (search finds it without its mentions' words): an
// empty title where it has none, read by its sources' names. A database of
// one source, a full-page one's or a linked view is left out, its first
// source naming it.
func inlineDatabaseNamesOfSeveral(app core.App, closer func() searchMentionCloser, containerIDs []string) (map[string]databaseNameRead, error) {
	names := map[string]databaseNameRead{}
	encoded, err := json.Marshal(containerIDs)
	if len(containerIDs) == 0 || err != nil {
		return names, err
	}
	rows := []struct {
		ID      string `db:"id"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], COALESCE(` + databaseBlockContentSQL("[[container]]", "[[block]]") + `, '') AS [[content]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[container]].[[page_id]]
		LEFT JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
		WHERE [[container]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[container]].[[kind]] = 'source'
			AND [[holder]].[[kind]] != '` + pageKindDatabase + `'
			AND (SELECT COUNT(*) FROM {{leafen_database_container_sources}} [[place]]
				WHERE [[place]].[[container_id]] = [[container]].[[id]]
					AND [[place]].[[data_source_id]] NOT IN (
						SELECT [[gone]].[[value]] FROM json_each(COALESCE(NULLIF(` + databaseBlockContentSQL("[[container]]", "[[block]]") + `, ''), '{}'),
							'$.` + databaseTrashedWithViewsField + `') AS [[gone]])) > 1
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		kept := inlineDatabaseKeptName([]byte(row.Content))
		if closer == nil {
			names[row.ID] = kept
			continue
		}
		names[row.ID] = databaseNameSpansAsRead(closer, kept.Title, kept.Spans)
	}
	return names, nil
}

// markSearchInlineDatabaseNamesDirty queues the search entries of the
// databases whose blocks a graph change writes with a name of their own, as
// a source renamed queues its database's (schema_baseline.json).
func markSearchInlineDatabaseNamesDirty(app core.App, operations []graphOperation) error {
	ids := []string{}
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind == graphOperationKindUpsertBlock && block != nil && block.Type == graphBlockTypeInlineDatabase &&
			strings.Contains(string(block.Content), `"pageTitleSpans"`) {
			ids = append(ids, block.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[id]], [[workspace_id]], 1, unixepoch('subsec') * 1000 FROM {{leafen_database_containers}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[kind]] = 'source'
		ON CONFLICT ([[page_id]]) DO UPDATE SET [[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{"ids": string(encoded)}).Execute(); err != nil {
		return err
	}
	wakeWorkspaceSearchIndexer(app)
	return nil
}
