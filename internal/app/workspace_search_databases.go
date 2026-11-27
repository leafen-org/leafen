package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database held in a page's body and a database template are found by
// search as Notion's finds them (seen 2026-11-10): the database by its name,
// "데이터베이스" after it, the path of the page holding it, and opened alone
// as its own page; the template by its name and its body, "템플릿" after it,
// "… / its database" for its path, and opened as its page; both also by the
// title of a page above them, as the pages under it are (pathMatches). Each is
// indexed beside pages under its own ID — the database's block's, the
// template's page's — and read through the page holding the database, as its
// items are: its readers alone find it. A full-page database is its own page
// and is found as one, as a wiki is by its page; a view block shows no
// database of its own and isn't.
// What names one marks it to be read again (schema_baseline.json);
// whether it is in the trash, a view block, or a template deleted from its
// database's menu is asked as a search reads it.

// searchDatabaseEntry is a database or template the index holds: the page it
// is read through — the page whose body holds the database, or the page of
// the template's database.
type searchDatabaseEntry struct {
	ID       string `db:"id"`
	Template bool   `db:"template"`
	PageID   string `db:"page_id"`
}

// loadSearchDatabaseEntries reads, of the IDs, those naming a database held
// in a page's body or a template.
func loadSearchDatabaseEntries(app core.App, ids []string) (map[string]searchDatabaseEntry, error) {
	entries := map[string]searchDatabaseEntry{}
	if len(ids) == 0 {
		return entries, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []searchDatabaseEntry{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], FALSE AS [[template]], [[page_id]] FROM {{leafen_database_containers}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[kind]] = 'source'
		UNION ALL
		SELECT [[id]], TRUE AS [[template]], [[page_id]] FROM {{leafen_database_templates}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		entries[row.ID] = row
	}
	return entries, nil
}

// searchFirstSourceSQL names the first source of the database container row
// aliased that names it (databaseNamingSources), which names and draws it:
// one in the trash with its views only when every one is.
func searchFirstSourceSQL(container string) string {
	return `(SELECT [[first_member]].[[data_source_id]] FROM {{leafen_database_container_sources}} [[first_member]]
		WHERE [[first_member]].[[container_id]] = ` + container + `.[[id]]
		ORDER BY ` + databaseSourceGoneWithViewsSQL(container+".[[id]]", "[[first_member]].[[data_source_id]]") + `,
			[[first_member]].[[position]], [[first_member]].[[data_source_id]] LIMIT 1)`
}

// searchSourceNotDeletedSQL is true while a data source isn't one of a
// database whose block was deleted, which waits in the trash
// (deleted_databases.go): its items and templates are found with it.
func searchSourceNotDeletedSQL(workspace, dataSource string) string {
	return `NOT EXISTS (
					SELECT 1 FROM {{leafen_deleted_databases}} [[deleted_database]], json_each([[deleted_database]].[[data_source_ids]]) [[deleted_member]]
					WHERE [[deleted_database]].[[workspace_id]] = ` + workspace + ` AND [[deleted_member]].[[value]] = ` + dataSource + `)`
}

// searchDatabaseEntryLiveSQL is true for an index row, aliased, of a live
// database held in a page's body of the workspace — in a page out of the
// trash, not deleted with its block, and no view block — or a template of
// it kept in its database's menu, whose database is out of the trash.
func searchDatabaseEntryLiveSQL(projection string) string {
	return `EXISTS (
				SELECT 1 FROM {{leafen_database_containers}} [[live_container]]
				JOIN {{leafen_pages}} [[live_holder]] ON [[live_holder]].[[id]] = [[live_container]].[[page_id]]
				WHERE [[live_container]].[[id]] = ` + projection + `.[[page_id]] AND [[live_container]].[[kind]] = 'source'
					AND [[live_container]].[[owner]] = {:ownerId} AND [[live_container]].[[workspace_id]] = {:workspaceId}
					AND [[live_holder]].[[kind]] != '` + pageKindDatabase + `' AND [[live_holder]].[[in_trash]] = 0
					AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[live_trash]]
						WHERE [[live_trash]].[[container_id]] = [[live_container]].[[id]])
					AND NOT ` + databaseViewBlockSQL("[[live_container]]") + `
					AND NOT ` + wikiDatabaseContainerSQL("[[live_container]]") + `
			) OR EXISTS (
				SELECT 1 FROM {{leafen_database_templates}} [[live_template]]
				WHERE [[live_template]].[[id]] = ` + projection + `.[[page_id]]
					AND [[live_template]].[[owner]] = {:ownerId} AND [[live_template]].[[workspace_id]] = {:workspaceId}
					AND NOT EXISTS (SELECT 1 FROM {{leafen_database_toolbar_template_state}} [[live_template_state]]
						WHERE [[live_template_state]].[[template_id]] = [[live_template]].[[id]] AND [[live_template_state]].[[deleted_at]] != '')
					AND ` + databaseSourceNotTrashedSQL("[[live_template]].[[data_source_id]]") + `
					AND ` + searchSourceNotDeletedSQL("{:workspaceId}", "[[live_template]].[[data_source_id]]") + `
			)`
}

// wikiDatabaseContainerSQL is true for a wiki's own database, aliased: the
// wiki is found as its page, its database not again beside it, as Notion's
// search lists a wiki once (seen 2026-11-11).
func wikiDatabaseContainerSQL(container string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} [[wiki_place]]
		JOIN {{leafen_data_sources}} [[wiki_source]] ON [[wiki_source]].[[id]] = [[wiki_place]].[[data_source_id]]
		WHERE [[wiki_place]].[[container_id]] = ` + container + `.[[id]] AND [[wiki_source]].[[wiki]] = 1)`
}

// buildSearchInlineDatabaseProjection indexes a database held in a page's
// body by its name as the one rule names it (databaseNameFrom) — its first
// naming source's, or one of several sources' own — found without its
// mentions' words as a page title is (page_name_mentions.go); found is false
// when the ID names no database. A full-page database's own, a linked
// view's, or one whose page is gone has no entry.
func buildSearchInlineDatabaseProjection(app core.App, dirty storedSearchDirty) (searchPageProjection, bool, error) {
	container := struct {
		Owner       string `db:"owner"`
		WorkspaceID string `db:"workspace_id"`
		Kind        string `db:"kind"`
		HolderKind  string `db:"holder_kind"`
		Holder      bool   `db:"holder"`
		Wiki        bool   `db:"wiki"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[kind]],
			COALESCE([[holder]].[[kind]], '') AS [[holder_kind]], [[holder]].[[id]] IS NOT NULL AS [[holder]],
			` + wikiDatabaseContainerSQL("[[container]]") + ` AS [[wiki]]
		FROM {{leafen_database_containers}} [[container]]
		LEFT JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": dirty.PageID}).One(&container)
	if errors.Is(err, sql.ErrNoRows) {
		return searchPageProjection{}, false, nil
	}
	if err != nil {
		return searchPageProjection{}, false, err
	}
	if container.WorkspaceID != dirty.WorkspaceID {
		return searchPageProjection{}, false, errors.New(errorCodeWorkspaceSearchProjectionFailed)
	}
	if container.Kind != databaseContainerKindSource || !container.Holder || container.HolderKind == pageKindDatabase || container.Wiki {
		return searchPageProjection{PageID: dirty.PageID, SourceVersion: dirty.Version, Deleted: true}, true, nil
	}
	// Its name by the one rule: one of several sources by its own name once it
	// has one (inline_database_own_name.go), else by its first naming source,
	// which heads its sources' rows (workspace_search_database_sources.go).
	names, err := databaseContainerNames(app, []string{dirty.PageID})
	if err != nil {
		return searchPageProjection{}, false, err
	}
	name := names[dirty.PageID]
	title, spans := name.Title, name.Spans
	if len(name.Sources) > 0 {
		title, spans = name.Sources[0].Title, name.Sources[0].Spans
	}
	if spelledName(spans) != title {
		spans = nil
	}
	// Each of several sources' names finds it too (workspace_search_database_sources.go).
	sourceWords, err := searchSeveralSourceWords(app, dirty.PageID)
	if err != nil {
		return searchPageProjection{}, false, err
	}
	return searchPageProjection{
		PageID: dirty.PageID, WorkspaceID: dirty.WorkspaceID, OwnerID: container.Owner,
		Title: title, TitleFolded: foldSearchText(searchWordsOf(spans, title) + sourceWords),
		SourceVersion: dirty.Version,
	}, true, nil
}

// buildSearchTemplateProjection indexes a database template by its name —
// its page's title, or the name it was made with while it has none, as its
// database's menu names it — and its body, as a row's.
func buildSearchTemplateProjection(app core.App, dirty storedSearchDirty, page *core.Record) (searchPageProjection, error) {
	template := struct {
		Owner string `db:"owner"`
		Name  string `db:"name"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[template]].[[owner]], ` + databaseTemplateNameSQL("[[template]]") + ` AS [[name]]
		FROM {{leafen_database_templates}} [[template]] WHERE [[template]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": page.Id}).One(&template)
	if errors.Is(err, sql.ErrNoRows) {
		return searchPageProjection{PageID: dirty.PageID, SourceVersion: dirty.Version, Deleted: true}, nil
	}
	if err != nil {
		return searchPageProjection{}, err
	}
	partial := false
	parts, err := appendSearchRowPageBody(app, dirty.WorkspaceID, page.Id, map[string]string{}, nil, nil, &partial)
	if err != nil {
		return searchPageProjection{}, err
	}
	body := strings.Join(parts, "\n")
	return searchPageProjection{
		PageID: dirty.PageID, WorkspaceID: dirty.WorkspaceID, OwnerID: template.Owner,
		Title: template.Name, TitleFolded: foldSearchText(template.Name),
		Body: body, BodyFolded: foldSearchText(body), SourceVersion: dirty.Version, Partial: partial,
	}, nil
}

// workspaceSearchInlineDatabaseResultsSQL reads found databases held in
// pages' bodies as results: named and drawn by their first source, placed
// under the page holding them, and edited when their source last was, by
// that page's last editor, as nothing keeps who edits a database.
var workspaceSearchInlineDatabaseResultsSQL = `
		SELECT [[projection]].[[page_id]], [[container]].[[workspace_id]], [[holder]].[[teamspace_id]],
			[[projection]].[[title]], [[projection]].[[title_folded]], [[container]].[[page_id]] AS [[parent_id]],
			COALESCE(` + dataSourceIconSQL("[[source]]") + `, '') AS [[icon]], COALESCE(` + dataSourceIconTypeSQL("[[source]]") + `, '') AS [[icon_type]],
			strftime('%Y-%m-%d %H:%M:%fZ', COALESCE([[source]].[[updated]], [[container]].[[updated]]) / 1000.0, 'unixepoch') AS [[updated]],
			strftime('%Y-%m-%d %H:%M:%fZ', COALESCE([[source]].[[created]], [[container]].[[created]]) / 1000.0, 'unixepoch') AS [[created]],
			[[holder]].[[created_by]], [[holder]].[[last_edited_by]], '` + pageKindDatabase + `' AS [[kind]],
			'' AS [[preview]], [[projection]].[[partial]], FALSE AS [[passage_later]], TRUE AS [[inline_database]]
		FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		CROSS JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[projection]].[[page_id]]
		CROSS JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[container]].[[page_id]]
		LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = ` + searchFirstSourceSQL("[[container]]") + `
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
			AND [[projection]].[[workspace_id]] = {:workspaceId}
			AND [[projection]].[[owner_id]] = {:ownerId}
			AND [[container]].[[owner]] = {:ownerId}
			AND [[container]].[[workspace_id]] = {:workspaceId}`

// workspaceSearchTemplateResultsSQL reads found templates as results: as a
// database's item, under the page holding the database, with its page's
// icon and times.
var workspaceSearchTemplateResultsSQL = `
		SELECT [[projection]].[[page_id]], [[template]].[[workspace_id]], [[record]].[[teamspace_id]],
			[[projection]].[[title]], [[projection]].[[title_folded]],
			[[template]].[[page_id]] AS [[parent_id]], [[template]].[[page_id]] AS [[row_container_page_id]],
			[[template]].[[data_source_id]] AS [[row_data_source_id]],
			[[record]].[[icon]], [[record]].[[icon_type]], [[record]].[[updated]], [[record]].[[created]],
			[[record]].[[created_by]], [[record]].[[last_edited_by]],
			` + workspaceSearchPreviewSQL + ` AS [[preview]], [[projection]].[[partial]],
			` + workspaceSearchPassageLaterSQL + ` AS [[passage_later]], TRUE AS [[template]]
		FROM {{leafen_search_pages_fts}} [[fts]]
		CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
			ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
		CROSS JOIN {{leafen_database_templates}} [[template]] ON [[template]].[[id]] = [[projection]].[[page_id]]
		CROSS JOIN {{leafen_pages}} [[record]] ON [[record]].[[id]] = [[template]].[[id]]
		WHERE {{leafen_search_pages_fts}} MATCH {:match}
			AND [[projection]].[[workspace_id]] = {:workspaceId}
			AND [[projection]].[[owner_id]] = {:ownerId}
			AND [[template]].[[owner]] = {:ownerId}
			AND [[template]].[[workspace_id]] = {:workspaceId}`

// loadSearchInlineDatabaseFacts reads, for a search's filters and time sort,
// the facts of databases held in pages' bodies: where the page holding one
// is, which is its place; who made and last edited that page; when its
// first source was made and last edited.
func loadSearchInlineDatabaseFacts(app core.App, encodedIDs string) ([]workspaceSearchCandidateFacts, error) {
	facts := []workspaceSearchCandidateFacts{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[holder]].[[created_by]], [[holder]].[[last_edited_by]], [[holder]].[[teamspace_id]],
			CAST(COALESCE([[source]].[[created]], [[container]].[[created]]) AS TEXT) AS [[created]],
			CAST(COALESCE([[source]].[[updated]], [[container]].[[updated]]) AS TEXT) AS [[updated]],
			[[container]].[[page_id]] AS [[anchor]], '' AS [[data_source_id]], '' AS [[archived]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[container]].[[page_id]]
		LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = ` + searchFirstSourceSQL("[[container]]") + `
		WHERE [[container]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[container]].[[kind]] = 'source'
	`).Bind(dbx.Params{"ids": encodedIDs}).All(&facts)
	return facts, err
}

// inlineDatabaseNameSpans is the spans a database's name is spelled from:
// its first source's, while they spell its name.
func inlineDatabaseNameSpans(app core.App, containerID, title string) []graphTextSpan {
	encoded := ""
	if app.DB().NewQuery(`
		SELECT COALESCE([[source]].[[title_spans]], '') FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = `+searchFirstSourceSQL("[[container]]")+`
		WHERE [[container]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": containerID}).Row(&encoded) != nil {
		return nil
	}
	spans := decodeStoredTextSpans(encoded)
	if spelledName(spans) != title {
		return nil
	}
	return spans
}

// searchInlineDatabaseLines is the line each database held in a page's body
// is of that page's search text: its name as the one rule names it
// (databaseNameFrom), without its mentions' words, as a sub-page's title is
// a line (searchPageChildLines) and as Notion's search finds the page by it
// (seen 2026-11-10: "Zorvex" finds the page holding "Zorvex inline" with
// that line) — one of several sources without a name of its own by their
// names listed, so the page is found by any of them, not by its first's
// alone. Named blocks narrow it to them; a view block, which shows
// another's database, is none. Nor is the page's own database — a
// full-page database's, a wiki's — whose name is the page's title, not a
// line of its body: Notion's search row for a wiki or a database shows no
// body line by it (seen 2026-11-11).
func searchInlineDatabaseLines(app core.App, pageID string, named []string) (map[string]string, error) {
	only := ""
	params := dbx.Params{"pageId": pageID, "type": graphBlockTypeInlineDatabase}
	if named != nil {
		encoded, err := json.Marshal(named)
		if err != nil {
			return nil, err
		}
		only, params["ids"] = ` AND [[block]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))`, string(encoded)
	}
	rows := []struct {
		ID string `db:"id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block]].[[id]]
		FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_search_projection]]
		JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[block]].[[id]]
		WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[type]] = {:type}` + only + `
			AND [[container]].[[kind]] = 'source' AND NOT ` + databaseViewBlockSQL("[[container]]") + `
			AND NOT ` + wikiDatabaseContainerSQL("[[container]]") + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_pages}} [[own_holder]]
				WHERE [[own_holder]].[[id]] = [[container]].[[page_id]] AND [[own_holder]].[[kind]] = '` + pageKindDatabase + `')
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	names, err := databaseContainerNames(app, ids)
	if err != nil {
		return nil, err
	}
	lines := make(map[string]string, len(rows))
	for _, id := range ids {
		if words := searchDatabaseNameWords(names[id]); strings.TrimSpace(words) != "" {
			lines[id] = words
		}
	}
	return lines, nil
}

// searchDatabaseNameWords is a database's name as search finds it, without
// its mentions' words: its own, or for one without a name of several
// sources their names listed, each found as it is.
func searchDatabaseNameWords(name databaseContainerName) string {
	if len(name.Sources) == 0 {
		return searchWordsOf(spellingSpans(name.Spans, name.Title), name.Title)
	}
	words := make([]string, 0, len(name.Sources))
	for _, source := range name.Sources {
		if spoken := strings.TrimSpace(searchWordsOf(spellingSpans(source.Spans, source.Title), source.Title)); spoken != "" {
			words = append(words, spoken)
		}
	}
	return strings.Join(words, ", ")
}

// spellingSpans is a name's spans while they spell it, none otherwise.
func spellingSpans(spans []graphTextSpan, title string) []graphTextSpan {
	if spelledName(spans) != title {
		return nil
	}
	return spans
}
