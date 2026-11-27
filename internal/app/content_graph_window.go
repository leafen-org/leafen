package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's graph is read in windows, each the items after the last one read,
// in one order: the page's blocks (by parent, place and id), its databases,
// their sources, views and properties, the rows of each source (by source,
// place and id), and the page's text document. Each part reads from its
// index only as many items as the window still holds, after the cursor. A
// window built every block of its page and every row of its databases, then
// cut them to the window: each window cost the whole page, and opening a page
// its square (2026-10-18 soak: a page of 859 blocks, 33ms a window, and more
// each hour as it grew). All parts read one moment of the database, in one
// read snapshot.

func loadContentGraphWindow(app core.App, owner, pageID, cursor string, limit int) (graphReadResponse, error) {
	return loadContentGraphWindowWithLinkedScope(
		app, owner, pageID, linkedDatabaseReadScope{}, cursor, limit,
	)
}

func loadContentGraphWindowWithLinkedScope(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	cursor string,
	limit int,
) (graphReadResponse, error) {
	return loadContentGraphWindowParts(app, owner, pageID, linkedScope, cursor, limit, true)
}

// loadContentGraphWindowParts reads a window of the page's graph, its
// databases' rows or not: a reader wanting only the page's blocks and its
// databases' sources, views and properties reads no row (2026-11-11: a row's
// page read the thousand row windows of its database of 100,000 for its
// source's name).
func loadContentGraphWindowParts(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	cursor string,
	limit int,
	withRows bool,
) (graphReadResponse, error) {
	var response graphReadResponse
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		response, err = readContentGraphWindow(txApp, owner, pageID, linkedScope, cursor, limit, withRows, nil)
		return err
	})
	return response, err
}

// graphWindowRead is a window being read: the items so far and how many
// more it takes; with onlyRows, those of the page's rows alone, with
// databases its databases' containers, sources, views and properties too,
// and with blocks its blocks.
type graphWindowRead struct {
	app       core.App
	after     graphWindowCursor
	params    dbx.Params
	room      int
	items     []storedGraphReadItem
	onlyRows  []string
	databases bool
	blocks    bool
	// trashed is an inline database alone in the trash the page shows too (content_graph_trashed_database.go).
	trashed *graphTrashedDatabase
}

// from is the keys a part reads after, and whether it reads: a part before
// the cursor's was read in earlier windows, the cursor's reads after its keys.
func (window *graphWindowRead) from(part string) ([]string, bool) {
	databasePart := part >= graphWindowContainers && part <= graphWindowProperties
	if window.room <= 0 || part < window.after.part ||
		window.onlyRows != nil && part != graphWindowRows && !(window.databases && databasePart) &&
			!(window.blocks && part == graphWindowBlocks) {
		return nil, false
	}
	if part == window.after.part {
		return window.after.keys, true
	}
	return nil, true
}

// read runs a part's query, which selects k1 to k3 and the payload, given
// its keys' condition (in place of /*after*/) and the room left ({:limit}).
func (window *graphWindowRead) read(part, kind, query string, columns []string, keys []string, more dbx.Params) error {
	params := dbx.Params{}
	for name, value := range window.params {
		params[name] = value
	}
	for name, value := range more {
		params[name] = value
	}
	params["limit"] = window.room
	found := []struct {
		K1      string `db:"k1"`
		K2      string `db:"k2"`
		K3      string `db:"k3"`
		Payload string `db:"payload"`
	}{}
	query = strings.Replace(query, "/*after*/", afterKeys(params, columns, keys), 1)
	if err := window.app.DB().NewQuery(query).Bind(params).All(&found); err != nil {
		return err
	}
	for _, item := range found {
		itemKeys := []string{item.K1, item.K2, item.K3}[:graphWindowKeyCounts[part]]
		window.items = append(window.items, storedGraphReadItem{
			Kind: kind, SortKey: encodeGraphWindowCursor(part, itemKeys), Payload: item.Payload,
		})
	}
	window.room -= len(found)
	return nil
}

// readContentGraphWindow reads a window of the page's graph or, given the
// parts its changes changed, those of its rows the page still shows, with
// its databases' containers, sources, views and properties and its blocks
// beside them when those changed (content_graph_changed_rows.go).
func readContentGraphWindow(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	cursor string,
	limit int,
	withRows bool,
	changed *graphChangedParts,
) (graphReadResponse, error) {
	after, err := parseGraphWindowCursor(cursor)
	if err != nil {
		return graphReadResponse{}, err
	}
	params, trashed := dbx.Params{"owner": owner, "pageId": pageID}, linkedScope.trashed
	trashed.bind(params)
	pageIDs := map[string]struct{}{}
	containerIDs := make([]string, 0, len(linkedScope.sourceContainerPages))
	for containerID, sourcePageID := range linkedScope.sourceContainerPages {
		containerIDs = append(containerIDs, containerID)
		if sourcePageID != pageID {
			pageIDs[sourcePageID] = struct{}{}
		}
	}
	sort.Strings(containerIDs)
	// Other-page relation targets show their source, properties and rows,
	// never their views.
	relationSourceIDs := make([]string, 0, len(linkedScope.relationSourcePages))
	for sourceID, sourcePageID := range linkedScope.relationSourcePages {
		relationSourceIDs = append(relationSourceIDs, sourceID)
		pageIDs[sourcePageID] = struct{}{}
	}
	sort.Strings(relationSourceIDs)
	relationSources := ""
	for index, sourceID := range relationSourceIDs {
		key := fmt.Sprintf("relationSource%d", index)
		params[key] = sourceID
		relationSources += " UNION ALL SELECT {:" + key + "}"
	}
	orderedPageIDs := make([]string, 0, len(pageIDs))
	for sourcePageID := range pageIDs {
		orderedPageIDs = append(orderedPageIDs, sourcePageID)
	}
	sort.Strings(orderedPageIDs)
	containerPlaceholders := make([]string, len(containerIDs))
	for index, containerID := range containerIDs {
		key := fmt.Sprintf("linkedContainer%d", index)
		params[key] = containerID
		containerPlaceholders[index] = "{:" + key + "}"
	}
	pagePlaceholders := make([]string, len(orderedPageIDs))
	for index, sourcePageID := range orderedPageIDs {
		key := fmt.Sprintf("linkedPage%d", index)
		params[key] = sourcePageID
		pagePlaceholders[index] = "{:" + key + "}"
	}
	// A database whose page went to the trash alone leaves the views linked to
	// it its rows but nowhere to add one, as Notion's sprint board (seen 2026-11-05).
	trashedSourcePageIDs := []string{}
	for index, sourcePageID := range orderedPageIDs {
		inTrash, err := pageInTrashPath(app, sourcePageID)
		if err != nil {
			return graphReadResponse{}, err
		}
		if inTrash {
			trashedSourcePageIDs = append(trashedSourcePageIDs, pagePlaceholders[index])
		}
	}
	sourcePageInTrash := "0"
	if len(trashedSourcePageIDs) > 0 {
		sourcePageInTrash = "[[source]].[[page_id]] IN (" + strings.Join(trashedSourcePageIDs, ", ") + ")"
	}
	authorizedContainer := func(alias string) string {
		if len(containerPlaceholders) == 0 {
			return "0"
		}
		return fmt.Sprintf("%s.[[source_container_id]] IN (%s)", alias, strings.Join(containerPlaceholders, ", "))
	}
	externalPage := func(alias string) string {
		if len(pagePlaceholders) == 0 {
			return "0"
		}
		return fmt.Sprintf("%s.[[page_id]] IN (%s)", alias, strings.Join(pagePlaceholders, ", "))
	}
	containerVisible := fmt.Sprintf(`
		(NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[container_trash]]
			WHERE [[container_trash]].[[container_id]] = [[container]].[[id]]
		) OR `+trashed.containerSQL("[[container]].[[id]]")+`) AND ([[container]].[[kind]] != 'linked' OR
		EXISTS (
			SELECT 1 FROM {{leafen_database_containers}} AS [[local_source]]
			WHERE [[local_source]].[[id]] = [[container]].[[source_container_id]]
				AND [[local_source]].[[page_id]] = {:pageId}
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_database_container_trash}} AS [[source_trash]]
					WHERE [[source_trash]].[[container_id]] = [[local_source]].[[id]]
				)
		) OR %s)`, authorizedContainer("[[container]]"))
	// Linked members are recorded per membership, so a database may show its
	// own sources and linked ones together.
	linkedDataSources := fmt.Sprintf(`
		SELECT [[membership]].[[data_source_id]]
		FROM {{leafen_database_containers}} AS [[linked]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[linked]].[[id]]
		WHERE [[linked]].[[owner]] = {:owner} AND [[linked]].[[page_id]] = {:pageId}
			AND [[membership]].[[source_container_id]] != ''
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[linked_trash]]
				WHERE [[linked_trash]].[[container_id]] = [[linked]].[[id]]
			) AND (
				EXISTS (
					SELECT 1 FROM {{leafen_database_containers}} AS [[local_source]]
					WHERE [[local_source]].[[id]] = [[membership]].[[source_container_id]]
						AND [[local_source]].[[page_id]] = {:pageId}
				) OR %s
			)`, authorizedContainer("[[membership]]")) + relationSources
	linkedViews := fmt.Sprintf(`
		SELECT [[membership]].[[view_id]]
		FROM {{leafen_database_containers}} AS [[linked]]
		JOIN {{leafen_database_container_views}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[linked]].[[id]]
		WHERE [[linked]].[[owner]] = {:owner} AND [[linked]].[[page_id]] = {:pageId}
			AND [[linked]].[[kind]] = 'linked'
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[linked_trash]]
				WHERE [[linked_trash]].[[container_id]] = [[linked]].[[id]]
			) AND (
				EXISTS (
					SELECT 1 FROM {{leafen_database_containers}} AS [[local_source]]
					WHERE [[local_source]].[[id]] = [[linked]].[[source_container_id]]
						AND [[local_source]].[[page_id]] = {:pageId}
				) OR %s
			)`, authorizedContainer("[[linked]]"))

	var snapshotRevision int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0) FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(params).Row(&snapshotRevision); err != nil {
		return graphReadResponse{}, err
	}
	window := &graphWindowRead{app: app, after: after, params: params, room: limit + 1, trashed: trashed}
	if changed != nil {
		window.onlyRows = append([]string{}, changed.OnlyRows...)
		window.databases, window.blocks = changed.Databases, changed.PageBlocks
	}
	if keys, read := window.from(graphWindowBlocks); read {
		trashed.readBlock(window, keys)
		if err := window.read(graphWindowBlocks, graphReadKindBlock, `
			SELECT [[block]].[[parent_id]] AS [[k1]], [[block]].[[position]] AS [[k2]], [[block]].[[id]] AS [[k3]],
				json_object(
					'id', [[block]].[[id]], 'parentId', NULLIF([[block]].[[parent_id]], ''),
					'afterId', (
						SELECT [[before]].[[id]] FROM {{leafen_blocks}} [[before]] INDEXED BY [[idx_leafen_blocks_page_order]]
						WHERE [[before]].[[owner]] = [[block]].[[owner]] AND [[before]].[[page_id]] = [[block]].[[page_id]]
							AND [[before]].[[parent_id]] = [[block]].[[parent_id]]
							AND ([[before]].[[position]], [[before]].[[id]]) < ([[block]].[[position]], [[block]].[[id]])
						ORDER BY [[before]].[[position]] DESC, [[before]].[[id]] DESC LIMIT 1
					),
					'position', [[block]].[[position]], 'type', [[block]].[[type]],
					'content', CASE
						WHEN [[block]].[[type]] = 'paragraph' THEN json_remove(json([[block]].[[content]]), '$.text')
						ELSE json([[block]].[[content]])
					END
				) AS [[payload]]
			FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_page_order]]
			WHERE [[block]].[[owner]] = {:owner} AND [[block]].[[page_id]] = {:pageId} AND /*after*/
			ORDER BY [[block]].[[parent_id]], [[block]].[[position]], [[block]].[[id]]
			LIMIT {:limit}
		`, []string{"[[block]].[[parent_id]]", "[[block]].[[position]]", "[[block]].[[id]]"}, keys, nil); err != nil {
			return graphReadResponse{}, err
		}
	}
	if keys, read := window.from(graphWindowContainers); read {
		if err := window.read(graphWindowContainers, graphReadKindDatabaseContainer, fmt.Sprintf(`
			SELECT [[position]] AS [[k1]], [[id]] AS [[k2]], '' AS [[k3]],
				json_object(
					'id', [[id]], 'pageId', [[page_id]], 'blockId', [[block_id]],
					'parentBlockId', NULLIF([[parent_block_id]], ''), 'position', [[position]],
					'kind', [[kind]], 'sourceContainerId', NULLIF([[source_container_id]], ''),
					'dataSourceIds', CASE WHEN %[1]s THEN json(COALESCE((
						SELECT json_group_array([[id]]) FROM (
							SELECT [[data_source_id]] AS [[id]]
							FROM {{leafen_database_container_sources}}
							WHERE [[container_id]] = [[container]].[[id]]
							ORDER BY [[position]], [[data_source_id]]
						)
					), '[]')) ELSE json('[]') END,
					'viewIds', CASE WHEN %[1]s THEN json(COALESCE((
						SELECT json_group_array([[id]]) FROM (
							SELECT [[view_id]] AS [[id]]
							FROM {{leafen_database_container_views}}
							WHERE [[container_id]] = [[container]].[[id]]
							ORDER BY [[position]], [[view_id]]
						)
					), '[]')) ELSE json('[]') END
				) AS [[payload]]
			FROM {{leafen_database_containers}} AS [[container]]
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
				AND (NOT EXISTS (
					SELECT 1 FROM {{leafen_database_container_trash}} AS [[container_trash]]
					WHERE [[container_trash]].[[container_id]] = [[container]].[[id]]
				) OR `+trashed.containerSQL("[[container]].[[id]]")+`) AND /*after*/
			ORDER BY [[position]], [[id]]
			LIMIT {:limit}
		`, containerVisible), []string{"[[container]].[[position]]", "[[container]].[[id]]"}, keys, nil); err != nil {
			return graphReadResponse{}, err
		}
	}
	if keys, read := window.from(graphWindowDataSources); read {
		if err := window.read(graphWindowDataSources, graphReadKindDataSource, fmt.Sprintf(`
			SELECT [[source]].[[id]] AS [[k1]], '' AS [[k2]], '' AS [[k3]],
				json_patch(json_patch(json_patch(json_object('id', [[source]].[[id]], 'pageId', [[source]].[[page_id]], 'title', [[source]].[[title]],
					'icon', %[6]s, 'iconType', %[7]s,
					'description', [[source]].[[description]],
					'descriptionHidden', json(CASE WHEN [[source]].[[description_hidden]] THEN 'true' ELSE 'false' END),
					'cover', [[source]].[[cover]], 'coverPosition', [[source]].[[cover_position]]),
					CASE WHEN json_valid([[source]].[[description_spans]]) AND [[source]].[[description_spans]] != ''
						THEN json_object('descriptionSpans', json([[source]].[[description_spans]])) ELSE '{}' END),
					CASE WHEN json_valid([[source]].[[title_spans]]) AND [[source]].[[title_spans]] != ''
						THEN json_object('titleSpans', json([[source]].[[title_spans]])) ELSE '{}' END),
					CASE WHEN %[4]s THEN json_object('sourcePageInTrash', json('true'))
						WHEN %[5]s THEN json_object('sourceInTrash', json('true')) ELSE '{}' END) AS [[payload]]
			FROM {{leafen_data_sources}} AS [[source]]
			WHERE [[source]].[[owner]] = {:owner} AND %[1]s AND (
				[[source]].[[page_id]] = {:pageId} OR (
					%[2]s AND [[source]].[[id]] IN (%[3]s)
				)
			) AND /*after*/
			ORDER BY [[source]].[[id]]
			LIMIT {:limit}
		`, trashed.sourceShownSQL("[[source]].[[id]]"), externalPage("[[source]]"), linkedDataSources, sourcePageInTrash,
			trashed.sourceInTrashSQL("[[source]].[[id]]"), dataSourceIconSQL("[[source]]"), dataSourceIconTypeSQL("[[source]]")),
			[]string{"[[source]].[[id]]"}, keys, nil); err != nil {
			return graphReadResponse{}, err
		}
	}
	if keys, read := window.from(graphWindowViews); read {
		if err := window.read(graphWindowViews, graphReadKindView, fmt.Sprintf(`
			SELECT [[view]].[[position]] AS [[k1]], [[view]].[[id]] AS [[k2]], '' AS [[k3]],
				json_object('id', [[view]].[[id]], 'dataSourceId', [[view]].[[data_source_id]], 'containerId', NULLIF([[view]].[[container_id]], ''), 'type', [[view]].[[type]], 'name', [[view]].[[name]], 'position', [[view]].[[position]], 'config', json([[view]].[[config]])) AS [[payload]]
			FROM {{leafen_database_views}} AS [[view]] WHERE [[view]].[[owner]] = {:owner} AND %[1]s AND (
				[[view]].[[page_id]] = {:pageId} OR (
					%[2]s AND [[view]].[[id]] IN (%[3]s)
				)
			) AND /*after*/
			ORDER BY [[view]].[[position]], [[view]].[[id]]
			LIMIT {:limit}
		`, trashed.sourceShownSQL("[[view]].[[data_source_id]]"), externalPage("[[view]]"), linkedViews),
			[]string{"[[view]].[[position]]", "[[view]].[[id]]"}, keys, nil); err != nil {
			return graphReadResponse{}, err
		}
	}
	if keys, read := window.from(graphWindowProperties); read {
		if err := window.read(graphWindowProperties, graphReadKindProperty, fmt.Sprintf(`
			SELECT [[property]].[[position]] AS [[k1]], [[property]].[[id]] AS [[k2]], '' AS [[k3]],
				json_object('id', [[property]].[[id]], 'dataSourceId', [[property]].[[data_source_id]], 'type', [[property]].[[type]], 'name', [[property]].[[name]], 'position', [[property]].[[position]], 'config', json([[property]].[[config]])) AS [[payload]]
			FROM {{leafen_database_properties}} AS [[property]] WHERE [[property]].[[owner]] = {:owner} AND %[1]s AND (
				[[property]].[[page_id]] = {:pageId} OR (
					%[2]s AND [[property]].[[data_source_id]] IN (%[3]s)
				)
			) AND /*after*/
			ORDER BY [[property]].[[position]], [[property]].[[id]]
			LIMIT {:limit}
		`, trashed.sourceShownSQL("[[property]].[[data_source_id]]")+" AND "+databasePropertyNotTrashedSQL("[[property]].[[id]]"),
			externalPage("[[property]]"), linkedDataSources),
			[]string{"[[property]].[[position]]", "[[property]].[[id]]"}, keys, nil); err != nil {
			return graphReadResponse{}, err
		}
	}
	if keys, read := window.from(graphWindowRows); read && withRows {
		if err := window.readRows(keys, externalPage, linkedDataSources); err != nil {
			return graphReadResponse{}, err
		}
	}
	if keys, read := window.from(graphWindowDocument); read {
		if err := window.read(graphWindowDocument, graphReadKindYgoDocument, `
			SELECT [[room]] AS [[k1]], '' AS [[k2]], '' AS [[k3]],
				json_object('pageId', [[room]], 'formatVersion', [[format_version]]) AS [[payload]]
			FROM {{leafen_ygo_documents}} WHERE [[room]] = {:pageId} AND /*after*/
			LIMIT {:limit}
		`, []string{"[[room]]"}, keys, nil); err != nil {
			return graphReadResponse{}, err
		}
	}

	stored := window.items
	hasMore := len(stored) > limit
	if hasMore {
		stored = stored[:limit]
	}
	items, err := graphReadItemsWithRowValues(app, owner, stored)
	if err != nil {
		return graphReadResponse{}, err
	}
	// A database out of everyone's sight leaves what the page keeps of it
	// (database_gone_links_read.go).
	if items, err = withoutGoneDatabaseLinksRead(app, pageID, items); err != nil {
		return graphReadResponse{}, err
	}
	response := graphReadResponse{
		PageID: pageID, Items: items, SnapshotRevision: snapshotRevision, HasMore: hasMore,
	}
	if hasMore && len(stored) > 0 {
		encoded := encodeGraphCursor(stored[len(stored)-1].SortKey)
		response.NextCursor = &encoded
	}
	return response, nil
}

// readRows reads the rows of each source the page shows, source by source:
// its own databases' (the sources its rows are of, found from the index a
// source at a time) and the linked and related sources on other pages, each
// from the rows of its page. A row lives on its source's page.
func (window *graphWindowRead) readRows(keys []string, externalPage func(string) string, linkedDataSources string) error {
	sources := []struct {
		SourceID string `db:"source_id"`
		PageID   string `db:"page_id"`
	}{}
	if err := window.app.DB().NewQuery(fmt.Sprintf(`
		WITH RECURSIVE [[own]]([[source_id]]) AS (
			SELECT MIN([[data_source_id]]) FROM {{leafen_database_rows}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			UNION ALL
			SELECT (
				SELECT MIN([[data_source_id]]) FROM {{leafen_database_rows}}
				WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] > [[own]].[[source_id]]
			) FROM [[own]] WHERE [[own]].[[source_id]] IS NOT NULL
		)
		SELECT [[own]].[[source_id]], {:pageId} AS [[page_id]] FROM [[own]]
		WHERE [[own]].[[source_id]] IS NOT NULL AND %s
		UNION
		SELECT [[source]].[[id]], [[source]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[owner]] = {:owner} AND %s AND [[source]].[[id]] IN (%s) AND %s
		ORDER BY 1, 2
	`, window.trashed.sourceShownSQL("[[own]].[[source_id]]"), externalPage("[[source]]"), linkedDataSources,
		window.trashed.sourceShownSQL("[[source]].[[id]]"))).Bind(window.params).All(&sources); err != nil {
		return err
	}
	for index, source := range sources {
		if window.room <= 0 {
			return nil
		}
		// A source is read once, from its own page.
		if index > 0 && sources[index-1].SourceID == source.SourceID {
			continue
		}
		var after []string
		if keys != nil {
			if source.SourceID < keys[0] {
				continue
			}
			if source.SourceID == keys[0] {
				after = keys[1:]
			}
		}
		more := dbx.Params{"sourcePageId": source.PageID, "sourceId": source.SourceID}
		if err := window.read(graphWindowRows, graphReadKindRow, `
			SELECT [[row]].[[data_source_id]] AS [[k1]], [[row]].[[position]] AS [[k2]], [[row]].[[id]] AS [[k3]],
				`+graphRowPayloadColumn+` AS [[payload]]
			`+graphRowPayloadFrom+`
			WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:sourcePageId}
				AND [[row]].[[data_source_id]] = {:sourceId}`+window.onlyRowsSQL(more)+`
				AND `+databaseRowNotTrashedSQL("[[row]].[[id]]")+` AND /*after*/
			ORDER BY [[row]].[[position]], [[row]].[[id]]
			LIMIT {:limit}
		`, []string{"[[row]].[[position]]", "[[row]].[[id]]"}, after, more); err != nil {
			return err
		}
	}
	return nil
}
