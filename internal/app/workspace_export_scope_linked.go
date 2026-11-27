package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A readable page export shows a linked database whose source lives outside
// the exported pages as its view shows it, as Notion does. When the person
// exporting can read the source page, the source, the linked database's
// views and the source's properties and rows join the inventory; the source
// page, its row pages and its other views stay out. A relation to a
// database outside the exported pages reads the same way: the related source
// joins, so its rows show by title and its values reach rollups. A database
// page exported on its own — in every readable format and as its own
// Markdown — follows its database's relations so, its own database's too,
// as Notion's export names a row page's related pages (seen 2026-11-21:
// "관계: 대상 하나 (https://app.notion.com/p/〈id〉?pvs=21), …"); what the
// export already takes of its own database, its properties and its row,
// joins once. Native archives keep refusing such links instead.

const (
	// A wiki's data source names the properties holding its pages' owners
	// and verification, so that an archive brings the wiki back; any data
	// source carries its sub-item, dependency and task settings, its
	// dependency binding and what it was made as
	// (data_source_starter_identity.go), so that an archive brings them back
	// with it. A view names the database that owns it, as a linked database
	// owns its views, in a page's export as in a workspace's.
	workspaceExportDataSourceItemSQL = `json_patch(json_patch(json_patch(json_patch(json_patch(json_patch(json_patch(json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'title', [[items]].[[title]], 'icon', [[items]].[[icon]], 'iconType', [[items]].[[icon_type]]), CASE WHEN [[items]].[[title_spans]] != '' AND json_valid([[items]].[[title_spans]]) THEN json_object('titleSpans', json([[items]].[[title_spans]])) ELSE '{}' END), CASE WHEN [[items]].[[description]] != '' OR [[items]].[[description_hidden]] = 1 THEN json_patch(json_object('description', [[items]].[[description]], 'descriptionHidden', json(CASE WHEN [[items]].[[description_hidden]] = 1 THEN 'true' ELSE 'false' END)), CASE WHEN [[items]].[[description_spans]] != '' AND json_valid([[items]].[[description_spans]]) THEN json_object('descriptionSpans', json([[items]].[[description_spans]])) ELSE '{}' END) ELSE '{}' END), CASE WHEN [[items]].[[cover]] != '' THEN json_object('cover', [[items]].[[cover]], 'coverPosition', [[items]].[[cover_position]]) ELSE '{}' END), CASE WHEN [[items]].[[wiki]] = 1 THEN json_object('wiki', json('true'), 'wikiOwnerPropertyId', [[items]].[[wiki_owner_property_id]], 'wikiVerificationPropertyId', [[items]].[[wiki_verification_property_id]]) ELSE '{}' END), COALESCE((SELECT json_object('taskFeatures', json_object('subitems', json([[features]].[[subitems_json]]), 'dependencies', json([[features]].[[dependencies_json]]), 'tasks', json([[features]].[[tasks_json]]))) FROM {{leafen_database_task_features}} AS [[features]] WHERE [[features]].[[data_source_id]] = [[items]].[[id]]), '{}')), COALESCE((SELECT json_object('dependencyBinding', json_object('predecessorPropertyId', [[binding]].[[predecessor_property_id]], 'successorPropertyId', [[binding]].[[successor_property_id]], 'dataKey', [[binding]].[[data_key]])) FROM {{leafen_database_dependency_bindings}} AS [[binding]] WHERE [[binding]].[[data_source_id]] = [[items]].[[id]]), '{}')), CASE WHEN [[items]].[[starter_template]] != '' OR [[items]].[[database_type]] != '' OR [[items]].[[untitled_name]] != '' THEN json_object('starterIdentity', json_object('starterTemplate', [[items]].[[starter_template]], 'databaseType', [[items]].[[database_type]], 'untitledName', [[items]].[[untitled_name]])) ELSE '{}' END)`
	workspaceExportViewItemSQL       = `json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'dataSourceId', [[items]].[[data_source_id]], 'containerId', NULLIF([[items]].[[container_id]], ''), 'type', [[items]].[[type]], 'name', [[items]].[[name]], 'position', [[items]].[[position]], 'config', json([[items]].[[config]]))`
	workspaceExportPropertyItemSQL   = `json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'dataSourceId', [[items]].[[data_source_id]], 'type', [[items]].[[type]], 'name', [[items]].[[name]], 'position', [[items]].[[position]], 'config', json([[items]].[[config]]), 'deletedAt', (SELECT strftime('%Y-%m-%dT%H:%M:%fZ', [[deleted_at]] / 1000.0, 'unixepoch') FROM {{leafen_database_property_trash}} WHERE [[property_id]]=[[items]].[[id]] AND [[deleting]]=0))`
)

// workspaceExportRowItemSQL is a database page as an export carries it, from
// a workspace or a page alike: its row, its page's icon and style — cover,
// font, small text, full width, table of contents and inline comment
// display — its archive and trash marks, and its reactions
// (page_reaction_archive.go), so that an archive brings it back as it was.
var workspaceExportRowItemSQL = withWorkspaceExportPageReactions(`json_patch(json_object(
		'id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'dataSourceId', [[items]].[[data_source_id]],
		'subitemParentId', NULLIF([[items]].[[subitem_parent_id]], ''), 'position', [[items]].[[position]],
		'trashedAt', (SELECT strftime('%Y-%m-%dT%H:%M:%fZ', [[trashed_at]] / 1000.0, 'unixepoch')
			FROM {{leafen_database_row_trash}} WHERE [[row_id]]=[[items]].[[id]]),
		'trashRootId', (SELECT [[root_row_id]] FROM {{leafen_database_row_trash}} WHERE [[row_id]]=[[items]].[[id]])),
	COALESCE((SELECT json_object('icon', [[row_page]].[[icon]], 'iconType', [[row_page]].[[icon_type]],
			'cover', [[row_page]].[[cover]], 'coverPosition', [[row_page]].[[cover_position]], 'font', [[row_page]].[[font]],
			'smallText', json(CASE WHEN [[row_page]].[[small_text]] THEN 'true' ELSE 'false' END),
			'fullWidth', json(CASE WHEN [[row_page]].[[full_width]] THEN 'true' ELSE 'false' END),
			'tableOfContentsDisplay', [[row_page]].[[table_of_contents_display]],
			'inlineCommentsDisplay', [[row_page]].[[inline_comments_display]],
			'archivedAt', NULLIF([[row_page]].[[archived_at]], ''))
		FROM {{leafen_pages}} AS [[row_page]] WHERE [[row_page]].[[id]]=[[items]].[[id]]),
		json_object('icon', '', 'iconType', 'none')))`, commentTargetDatabaseRow, "[[items]].[[id]]")

func workspaceExportInScope(pageExpression string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[scope]]
		WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = ` + pageExpression + `)`
}

// workspaceExportLinkedSources returns, as a JSON list, the sources that
// linked databases and relations on the exported pages reach outside them
// and that the person exporting can read.
func workspaceExportLinkedSources(
	app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload,
) (string, error) {
	documents := workspaceExportRendersDocuments(payload.Format) || payload.Format == workspaceExportFormatDatabaseCSV
	rowMarkdown := payload.Format == workspaceExportFormatPageMarkdown && payload.Scope.Kind == workspaceExportScopePage
	if !documents && !rowMarkdown || (payload.Scope.Kind != workspaceExportScopePage && payload.Scope.Kind != workspaceExportScopeDatabase) {
		return "[]", nil
	}
	// A page export reaches out through the linked databases and relations on
	// its pages; a database export through the database's own relations.
	linkedSQL := `
		SELECT DISTINCT [[membership]].[[data_source_id]], [[data]].[[page_id]] AS [[source_page_id]]
		FROM {{leafen_database_containers}} AS [[linked]]
		JOIN {{leafen_database_container_sources}} AS [[membership]] ON [[membership]].[[container_id]] = [[linked]].[[id]]
		JOIN {{leafen_database_containers}} AS [[source]]
			ON [[source]].[[id]] = [[membership]].[[source_container_id]] AND [[source]].[[owner]] = [[linked]].[[owner]]
			AND [[source]].[[workspace_id]] = [[linked]].[[workspace_id]] AND [[source]].[[kind]] = 'source'
		JOIN {{leafen_data_sources}} AS [[data]]
			ON [[data]].[[id]] = [[membership]].[[data_source_id]] AND [[data]].[[owner]] = [[linked]].[[owner]]
			AND [[data]].[[workspace_id]] = [[linked]].[[workspace_id]]
		WHERE [[linked]].[[owner]] = {:ownerId} AND [[linked]].[[workspace_id]] = {:workspaceId}
			AND [[membership]].[[source_container_id]] != ''
			AND ` + workspaceExportInScope("[[linked]].[[page_id]]") + `
			AND NOT ` + workspaceExportInScope("[[data]].[[page_id]]") + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[linked_trash]]
				WHERE [[linked_trash]].[[container_id]] = [[linked]].[[id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[source_trash]]
				WHERE [[source_trash]].[[container_id]] = [[source]].[[id]])
			AND ` + databaseSourceNotTrashedSQL("[[membership]].[[data_source_id]]") + `
		UNION`
	relationFrom := `{{leafen_workspace_export_scope_pages}} AS [[scope]]
		CROSS JOIN {{leafen_database_properties}} AS [[relation]]
			ON [[relation]].[[owner]] = {:ownerId} AND [[relation]].[[page_id]] = [[scope]].[[page_id]]`
	relationScope := `[[scope]].[[job_id]] = {:jobId} AND NOT ` + workspaceExportInScope("[[data]].[[page_id]]")
	if payload.Scope.Kind == workspaceExportScopeDatabase {
		linkedSQL = ""
		relationFrom = `{{leafen_data_sources}} AS [[target]]
		CROSS JOIN {{leafen_database_properties}} AS [[relation]]
			ON [[relation]].[[owner]] = [[target]].[[owner]] AND [[relation]].[[page_id]] = [[target]].[[page_id]]
			AND [[relation]].[[data_source_id]] = [[target]].[[id]]`
		relationScope = `[[target]].[[id]] = {:targetId} AND [[target]].[[owner]] = {:ownerId} AND [[data]].[[id]] != {:targetId}`
	}
	relationTargets := func(from, scope string) string {
		return `
		SELECT DISTINCT [[data]].[[id]] AS [[data_source_id]], [[data]].[[page_id]] AS [[source_page_id]]
		FROM ` + from + `
		JOIN {{leafen_data_sources}} AS [[data]]
			ON [[data]].[[id]] = json_extract([[relation]].[[config]], '$.relationDatabaseId')
			AND [[data]].[[owner]] = [[relation]].[[owner]] AND [[data]].[[workspace_id]] = [[relation]].[[workspace_id]]
		WHERE [[data]].[[workspace_id]] = {:workspaceId}
			AND [[relation]].[[type]] = 'relation' AND json_valid([[relation]].[[config]])
			AND ` + scope + `
			AND ` + databaseSourceNotTrashedSQL("[[data]].[[id]]")
	}
	// A database page at the export's root reaches out through its own
	// database's relations, which no exported page holds.
	rootRelations := relationTargets(`{{leafen_pages}} AS [[root_page]]
		JOIN {{leafen_database_properties}} AS [[relation]]
			ON [[relation]].[[owner]] = {:ownerId} AND [[relation]].[[data_source_id]] = [[root_page]].[[data_source_id]]`,
		`[[root_page]].[[id]] = {:targetId} AND [[root_page]].[[data_source_id]] != ''
			AND NOT `+workspaceExportInScope("[[data]].[[page_id]]"))
	query := linkedSQL + relationTargets(relationFrom, relationScope)
	if rowMarkdown {
		query = rootRelations
	} else if payload.Scope.Kind == workspaceExportScopePage {
		query += "\n\t\tUNION" + rootRelations
	}
	candidates := []struct {
		DataSourceID string `db:"data_source_id"`
		SourcePageID string `db:"source_page_id"`
	}{}
	err := app.DB().NewQuery(query + "\n\t\tORDER BY 1").Bind(scopedExportParams(authorization, job, payload)).All(&candidates)
	if err != nil {
		return "", err
	}
	readable := map[string]bool{}
	sources := []string{}
	for _, candidate := range candidates {
		allowed, checked := readable[candidate.SourcePageID]
		if !checked {
			page, err := authorizePage(app, job.Owner, candidate.SourcePageID, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) {
				return "", err
			}
			allowed = err == nil && page.Workspace.OwnerID == authorization.OwnerID &&
				page.Workspace.WorkspaceID == authorization.WorkspaceID
			readable[candidate.SourcePageID] = allowed
		}
		if allowed {
			sources = append(sources, candidate.DataSourceID)
		}
	}
	encoded, err := json.Marshal(sources)
	return string(encoded), err
}

func loadWorkspaceExportLinkedSourceWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	sources, err := workspaceExportLinkedSources(app, authorization, job, payload)
	if err != nil || sources == "[]" {
		return nil, err
	}
	after, err := workspaceExportStageCursor(cursor, "15", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterId"], params["sources"] = after[0], after[1], sources
	return queryWorkspaceExportStage(app, `
		SELECT 'dataSource' AS [[kind]], '15:' || [[items]].[[page_id]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'title', [[items]].[[title]],
				'icon', [[items]].[[icon]], 'iconType', [[items]].[[icon_type]], 'external', json('true')) AS [[payload]]
		FROM {{leafen_data_sources}} AS [[items]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[items]].[[id]] IN (SELECT [[value]] FROM json_each({:sources}))`+workspaceExportBesideRootSQL(payload, "[[items]].[[id]]")+`
			AND ([[items]].[[page_id]], [[items]].[[id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

// workspaceExportBesideRootSQL leaves out, of a page export's related
// sources, the database of a database page at its root, whose properties and
// row the export takes already (loadWorkspaceExportScopedPropertyWindow,
// loadWorkspaceExportScopedRowWindow) and whose page stays out with the rest
// of it, so that the root stands on its own (sharedRows) and its database's
// other rows only name what its relations point at.
func workspaceExportBesideRootSQL(payload workspaceExportJobPayload, sourceID string) string {
	if payload.Scope.Kind != workspaceExportScopePage {
		return ""
	}
	return `
			AND ` + sourceID + ` NOT IN (SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:targetId} AND [[data_source_id]] != '')`
}

// Only the views the exported linked databases show join; the source's own
// views stay out.
func loadWorkspaceExportLinkedViewWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportLinkedItemWindow(app, authorization, job, payload, cursor, "16", workspaceExportItemView,
		"leafen_database_views", workspaceExportViewItemSQL, `AND NOT `+workspaceExportInScope("[[items]].[[page_id]]")+`
			AND [[items]].[[id]] IN (
				SELECT [[membership]].[[view_id]] FROM {{leafen_database_container_views}} AS [[membership]]
				JOIN {{leafen_database_containers}} AS [[linked]] ON [[linked]].[[id]] = [[membership]].[[container_id]]
				WHERE [[linked]].[[owner]] = {:ownerId} AND `+workspaceExportInScope("[[linked]].[[page_id]]")+`
			)`, limit)
}

func loadWorkspaceExportLinkedPropertyWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportLinkedItemWindow(app, authorization, job, payload, cursor, "17", workspaceExportItemProperty,
		"leafen_database_properties", workspaceExportPropertyItemSQL, workspaceExportBesideRootSQL(payload, "[[items]].[[data_source_id]]"), limit)
}

func loadWorkspaceExportLinkedRowWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportLinkedItemWindow(app, authorization, job, payload, cursor, "18", workspaceExportItemRow,
		"leafen_database_rows", workspaceExportRowItemSQL, "AND "+pageWithoutOwnDeletionFenceSQL("[[items]].[[id]]")+
			" AND [[items]].[[id]] != {:targetId}", limit)
}

func loadWorkspaceExportLinkedItemWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor, prefix, kind, table, valueSQL, filter string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	sources, err := workspaceExportLinkedSources(app, authorization, job, payload)
	if err != nil || sources == "[]" {
		return nil, err
	}
	after, err := workspaceExportStageCursor(cursor, prefix, 3)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	params["sources"] = sources
	return queryWorkspaceExportStage(app, `
		SELECT '`+kind+`' AS [[kind]], '`+prefix+`:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+valueSQL+` AS [[payload]]
		FROM {{`+table+`}} AS [[items]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[items]].[[data_source_id]] IN (SELECT [[value]] FROM json_each({:sources})) `+filter+`
			AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}
