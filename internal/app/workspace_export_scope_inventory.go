package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceExportScopedInventoryStage struct {
	prefix string
	load   func(core.App, workspaceAuthorization, storedJob, workspaceExportJobPayload, string, int) ([]storedWorkspaceExportInventoryItem, error)
}

var workspaceExportScopedInventoryStages = []workspaceExportScopedInventoryStage{
	{prefix: "01", load: loadWorkspaceExportScopedPageWindow},
	{prefix: "02", load: loadWorkspaceExportScopedBlockWindow},
	{prefix: "03", load: loadWorkspaceExportScopedDataSourceWindow},
	{prefix: "04", load: loadWorkspaceExportScopedViewWindow},
	{prefix: "05", load: loadWorkspaceExportScopedPropertyWindow},
	{prefix: "06", load: loadWorkspaceExportScopedRowWindow},
	{prefix: "07", load: loadWorkspaceExportScopedYgoWindow},
	{prefix: "08", load: loadWorkspaceExportScopedTemplateWindow},
	{prefix: "09", load: loadWorkspaceExportScopedDatabaseContainerWindow},
	{prefix: "10", load: loadWorkspaceExportScopedBlockTrashWindow}, {prefix: "11", load: loadWorkspaceExportScopedTemplateRootWindow},
	{prefix: "12", load: loadWorkspaceExportScopedRowPageBlockWindow},
	{prefix: "13", load: loadWorkspaceExportScopedRowBlockTrashWindow},
	{prefix: "14", load: loadWorkspaceExportScopedCommentWindow},
	{prefix: "15", load: loadWorkspaceExportLinkedSourceWindow},
	{prefix: "16", load: loadWorkspaceExportLinkedViewWindow},
	{prefix: "17", load: loadWorkspaceExportLinkedPropertyWindow},
	{prefix: "18", load: loadWorkspaceExportLinkedRowWindow},
	{prefix: "19", load: loadWorkspaceExportScopedAutomationWindow},
}

func loadWorkspaceExportScopedInventoryWindow(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	payload workspaceExportJobPayload,
	cursor string,
	limit int,
) ([]storedWorkspaceExportInventoryItem, bool, error) {
	if limit < 1 {
		return nil, false, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	start := 0
	if cursor != "" {
		start = -1
		for index, stage := range workspaceExportScopedInventoryStages {
			if strings.HasPrefix(cursor, stage.prefix+":") {
				start = index
				break
			}
		}
		if start < 0 {
			return nil, false, errors.New(errorCodeWorkspaceExportArtifactInvalid)
		}
	}
	rows := make([]storedWorkspaceExportInventoryItem, 0, limit)
	for index := start; index < len(workspaceExportScopedInventoryStages); index++ {
		remaining := limit - len(rows)
		stageCursor := ""
		if index == start {
			stageCursor = cursor
		}
		window, err := workspaceExportScopedInventoryStages[index].load(app, authorization, job, payload, stageCursor, remaining+1)
		if err != nil {
			return nil, false, err
		}
		if len(window) > remaining {
			return append(rows, window[:remaining]...), false, nil
		}
		rows = append(rows, window...)
	}
	return rows, true, nil
}

func scopedExportParams(authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload) dbx.Params {
	return dbx.Params{
		"workspaceId": authorization.WorkspaceID,
		"ownerId":     authorization.OwnerID,
		"actorId":     authorization.PrincipalID,
		"jobId":       job.ID,
		"targetId":    payload.Scope.TargetID,
	}
}

func scopedExportNeedsPages(payload workspaceExportJobPayload) bool {
	return payload.Format == workspaceExportFormatPageMarkdown || workspaceExportRendersDocuments(payload.Format) ||
		payload.Format == workspaceExportFormatNotionMarkdown && payload.Scope.Kind != workspaceExportScopeBlockSubtree ||
		payload.Format == workspaceExportFormatPageNativeArchive
}

func scopedExportNeedsDatabaseGraph(payload workspaceExportJobPayload) bool {
	return payload.Format == workspaceExportFormatDatabaseCSV || workspaceExportRendersDocuments(payload.Format) ||
		payload.Format == workspaceExportFormatPageNativeArchive ||
		payload.Format == workspaceExportFormatNotionMarkdown && payload.Scope.Kind == workspaceExportScopeDatabase
}

func loadWorkspaceExportScopedPageWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if !scopedExportNeedsPages(payload) {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "01", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterCreated"], params["afterId"] = after[0], after[1]
	return queryWorkspaceExportStage(app, `
		SELECT 'page' AS [[kind]], '01:' || [[pages]].[[created]] || ':' || [[pages]].[[id]] AS [[sort_key]],
			[[pages]].[[id]] AS [[page_id]], json_patch(json_patch(json_patch(json_object(
				'id', [[pages]].[[id]], 'workspaceId', [[pages]].[[workspace]], 'title', [[pages]].[[title]], 'content', [[pages]].[[content]],
				'revision', [[pages]].[[revision]], 'cursor', [[pages]].[[cursor]], 'parentId',
				CASE WHEN [[scope]].[[is_root]] THEN '' ELSE [[pages]].[[parent_id]] END,
				'position', [[pages]].[[position]],
				'isLocked', json(CASE WHEN [[pages]].[[is_locked]] THEN 'true' ELSE 'false' END),
				'inTrash', json(CASE WHEN [[pages]].[[in_trash]] THEN 'true' ELSE 'false' END),
				'trashedAt', NULLIF([[pages]].[[trashed_at]], ''),
				'archivedAt', NULLIF([[pages]].[[archived_at]], ''),
				'icon', [[pages]].[[icon]], 'iconType', [[pages]].[[icon_type]], 'cover', [[pages]].[[cover]],
				'coverPosition', [[pages]].[[cover_position]], 'font', [[pages]].[[font]],
				'smallText', json(CASE WHEN [[pages]].[[small_text]] THEN 'true' ELSE 'false' END),
				'fullWidth', json(CASE WHEN [[pages]].[[full_width]] THEN 'true' ELSE 'false' END),
				'backlinksDisplay', [[pages]].[[backlinks_display]],
				'pageCommentsDisplay', [[pages]].[[page_comments_display]],
				'tableOfContentsDisplay', [[pages]].[[table_of_contents_display]],
				'inlineCommentsDisplay', [[pages]].[[inline_comments_display]],
				'kind', [[pages]].[[kind]],
				'createdBy', [[pages]].[[created_by]], 'lastEditedBy', [[pages]].[[last_edited_by]],
				'updatedAt', [[pages]].[[updated]]), `+workspaceExportTitleSpansPatch("[[pages]]")+`), `+workspaceExportPageReactionsPatch(commentTargetPage, "[[pages]].[[id]]")+`), `+workspaceExportPageAnchorPatch("[[pages]]")+`) AS [[payload]]
		FROM {{leafen_workspace_export_scope_pages}} AS [[scope]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[scope]].[[page_id]]
		WHERE [[scope]].[[job_id]] = {:jobId} AND NOT [[scope]].[[row_page]] AND [[pages]].[[workspace]] = {:workspaceId}
			AND [[pages]].[[owner]] = {:ownerId} AND [[pages]].[[notion_import_staging]] = FALSE
			AND ([[pages]].[[created]], [[pages]].[[id]]) > ({:afterCreated}, {:afterId})
		ORDER BY [[pages]].[[created]], [[pages]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedBlockWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format == workspaceExportFormatDatabaseCSV {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "02", 3)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	// A database page's body goes with its database, as row page blocks.
	join := "JOIN {{leafen_workspace_export_scope_pages}} AS [[scope]] ON [[scope]].[[page_id]] = [[items]].[[page_id]]"
	key := "[[scope]].[[job_id]] = {:jobId} AND NOT [[scope]].[[row_page]]"
	if payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		join = "JOIN {{leafen_workspace_export_scope_blocks}} AS [[scope]] ON [[scope]].[[block_id]] = [[items]].[[id]]"
		key = "[[scope]].[[job_id]] = {:jobId}"
	} else if workspaceExportDatabaseArchive(payload) {
		// Of the page holding a database, its archive takes the database's block.
		key += " AND (NOT [[scope]].[[is_root]] OR [[items]].[[id]] = (SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[id]] = " +
			workspaceExportDatabaseArchiveContainerSQL + "))"
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'block' AS [[kind]], '02:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]],
				'parentId', NULLIF([[items]].[[parent_id]], ''), 'position', [[items]].[[position]],
				'type', [[items]].[[type]], 'content', json([[items]].[[content]])) AS [[payload]]
		FROM {{leafen_blocks}} AS [[items]] `+join+`
		WHERE `+key+` AND [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedDataSourceWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if !scopedExportNeedsDatabaseGraph(payload) {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "03", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterId"] = after[0], after[1]
	filter := "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} [[scope]] WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = [[items]].[[page_id]])"
	if workspaceExportDatabaseArchive(payload) {
		filter = "(" + workspaceExportBeyondRootSQL("[[items]].[[page_id]]") + " OR " +
			workspaceExportDatabaseArchiveSourceSQL("[[items]].[[id]]") + ")"
	} else if payload.Scope.Kind == workspaceExportScopeDatabase {
		filter = "[[items]].[[id]] = {:targetId}"
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'dataSource' AS [[kind]], '03:' || [[items]].[[page_id]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportDataSourceItemSQL+` AS [[payload]]
		FROM {{leafen_data_sources}} AS [[items]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId} AND `+filter+`
			AND ([[items]].[[page_id]], [[items]].[[id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedViewWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportScopedDatabaseItemWindow(app, authorization, job, payload, cursor, "04", workspaceExportItemView, "leafen_database_views", workspaceExportViewItemSQL, "", limit)
}

// loadWorkspaceExportScopedPropertyWindow reads the properties of the
// scope's databases, and of the database of a database page exported on its
// own, without the rest of that database.
func loadWorkspaceExportScopedPropertyWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportScopedDatabaseItemWindow(app, authorization, job, payload, cursor, "05", workspaceExportItemProperty, "leafen_database_properties", workspaceExportPropertyItemSQL,
		workspaceExportRootRowSourceSQL("[[items]].[[data_source_id]]"), limit)
}

// loadWorkspaceExportScopedRowWindow reads the rows of the scope's
// databases, and the row of a database page exported on its own.
func loadWorkspaceExportScopedRowWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportScopedDatabaseItemWindow(app, authorization, job, payload, cursor, "06", workspaceExportItemRow, "leafen_database_rows",
		workspaceExportRowItemSQL,
		workspaceExportRootRowSQL("[[items]].[[id]]"), limit)
}

// loadWorkspaceExportScopedDatabaseItemWindow reads a database table's items
// on the scope's pages, or the scope's database, and the items rootSQL takes.
func loadWorkspaceExportScopedDatabaseItemWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor, prefix, kind, table, valueSQL, rootSQL string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	// A database page's own Markdown takes just its row and its database's
	// properties, for the lines naming them under its title.
	rootOnly := ""
	if !scopedExportNeedsDatabaseGraph(payload) {
		switch {
		case payload.Format != workspaceExportFormatPageMarkdown || payload.Scope.Kind != workspaceExportScopePage:
			return nil, nil
		case kind == workspaceExportItemProperty:
			rootOnly = "[[items]].[[data_source_id]] = (SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:targetId})"
		case kind == workspaceExportItemRow:
			rootOnly = "[[items]].[[id]] = {:targetId}"
		default:
			return nil, nil
		}
	}
	after, err := workspaceExportStageCursor(cursor, prefix, 3)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	filter := "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} [[scope]] WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = [[items]].[[page_id]])"
	if workspaceExportDatabaseArchive(payload) {
		filter = workspaceExportDatabaseArchiveItemSQL(table)
	} else if payload.Scope.Kind == workspaceExportScopeDatabase {
		filter = "[[items]].[[data_source_id]] = {:targetId}"
	} else if rootOnly != "" {
		filter = rootOnly
	} else if rootSQL != "" {
		filter = "(" + filter + " OR " + rootSQL + ")"
	}
	if table == "leafen_database_rows" {
		filter += " AND " + pageWithoutOwnDeletionFenceSQL("[[items]].[[id]]")
	}
	query := `SELECT '` + kind + `' AS [[kind]], '` + prefix + `:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]], [[items]].[[page_id]], ` + valueSQL + ` AS [[payload]] FROM {{` + table + `}} AS [[items]] WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId} AND ` + filter + ` AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId}) ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}`
	return queryWorkspaceExportStage(app, query, params, limit)
}

// scopedExportNeedsText reports the formats that read block text, whose
// authority is each page's Ygo document rather than the block rows.
func scopedExportNeedsText(payload workspaceExportJobPayload) bool {
	return payload.Format == workspaceExportFormatPageNativeArchive || workspaceExportRendersDocuments(payload.Format) ||
		payload.Format == workspaceExportFormatPageMarkdown || payload.Format == workspaceExportFormatNotionMarkdown
}

func loadWorkspaceExportScopedYgoWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if !scopedExportNeedsText(payload) {
		return nil, nil
	}
	scopeTable := "leafen_workspace_export_scope_pages"
	if payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		scopeTable = "leafen_workspace_export_scope_blocks"
	}
	after, err := workspaceExportStageCursor(cursor, "07", 1)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterId"], params["ygoFormatVersion"] = after[0], ygoDocumentFormatVersion
	// A page's Markdown reads a database page's text from its document too
	// (2026-10-14: a database page came out as its title alone); the other
	// formats carry a database page's body with its text (rowPageBlock).
	pages := "[[pages]].[[data_source_id]] = ''"
	if payload.Format == workspaceExportFormatPageMarkdown || payload.Format == workspaceExportFormatNotionMarkdown {
		pages = "TRUE"
	} else if workspaceExportDatabaseArchive(payload) {
		// The page holding a database comes back as the database's page,
		// which holds no text.
		pages += " AND " + workspaceExportBeyondRootSQL("[[pages]].[[id]]")
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'ygoDocument' AS [[kind]], '07:' || [[pages]].[[id]] AS [[sort_key]], [[pages]].[[id]] AS [[page_id]],
			json_object('pageId', [[pages]].[[id]], 'formatVersion', {:ygoFormatVersion}) AS [[payload]]
		FROM {{leafen_pages}} AS [[pages]]
		WHERE EXISTS (SELECT 1 FROM {{`+scopeTable+`}} AS [[scope]]
				WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = [[pages]].[[id]])
			AND `+pages+` AND [[pages]].[[id]] > {:afterId}
			AND (EXISTS (SELECT 1 FROM {{leafen_ygo_documents}} WHERE [[room]] = [[pages]].[[id]])
				OR EXISTS (SELECT 1 FROM {{leafen_ygo_structural_documents}} WHERE [[room]] = [[pages]].[[id]]))
		ORDER BY [[pages]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedTemplateWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format != workspaceExportFormatPageNativeArchive {
		return nil, nil
	}
	return loadWorkspaceExportScopedDatabaseItemWindow(app, authorization, job, payload, cursor, "08", workspaceExportItemTemplate, "leafen_database_templates", `json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'dataSourceId', [[items]].[[data_source_id]], 'name', [[items]].[[name]], 'icon', [[items]].[[icon]], 'title', [[items]].[[title]], 'properties', json([[items]].[[properties_json]]), 'propertySpans', json([[items]].[[property_spans_json]]), 'blocks', json_array(), 'isDefault', json(CASE WHEN [[items]].[[is_default]] THEN 'true' ELSE 'false' END), 'position', [[items]].[[position]], 'createdAt', [[items]].[[created_at]], 'updatedAt', [[items]].[[updated_at]])`, "", limit)
}

func loadWorkspaceExportScopedDatabaseContainerWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format != workspaceExportFormatPageNativeArchive && !workspaceExportRendersDocuments(payload.Format) {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "09", 3)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	filter := "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} [[scope]] WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = [[items]].[[page_id]])"
	if payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		filter = "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_blocks}} [[scope]] WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[block_id]] = [[items]].[[block_id]])"
	} else if workspaceExportDatabaseArchive(payload) {
		filter = "(" + workspaceExportBeyondRootSQL("[[items]].[[page_id]]") + " OR [[items]].[[id]] = " +
			workspaceExportDatabaseArchiveContainerSQL + ")"
	} else if payload.Scope.Kind == workspaceExportScopeDatabase {
		filter = "EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} [[scope]] WHERE [[scope]].[[container_id]] = [[items]].[[id]] AND [[scope]].[[data_source_id]] = {:targetId})"
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'databaseContainer' AS [[kind]],
			'09:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportDatabaseContainerItemSQL+` AS [[payload]]
		FROM {{leafen_database_containers}} AS [[items]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId} AND `+filter+`
			AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedBlockTrashWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format != workspaceExportFormatPageNativeArchive || payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "10", 2)
	if err != nil {
		return nil, err
	}
	// The page holding a database keeps its removed blocks from the database's archive.
	holder := "TRUE"
	if workspaceExportDatabaseArchive(payload) {
		holder = "NOT [[scope]].[[is_root]]"
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterId"] = after[0], after[1]
	return queryWorkspaceExportStage(app, `
		SELECT 'blockTrash' AS [[kind]],
			'10:' || [[items]].[[page_id]] || ':' || [[items]].[[root_block_id]] AS [[sort_key]],
			[[items]].[[page_id]], json_object(
				'rootBlockId', [[items]].[[root_block_id]], 'pageId', [[items]].[[page_id]],
				'parentId', [[items]].[[parent_id]], 'predecessorId', [[items]].[[predecessor_id]],
				'successorId', [[items]].[[successor_id]], 'snapshot', json([[items]].[[snapshot_json]]),
				'blockCount', [[items]].[[block_count]], 'deletedAt', [[items]].[[deleted_at]],
				'expiresAt', [[items]].[[expires_at]]) AS [[payload]]
		FROM {{leafen_block_trash_tombstones}} AS [[items]]
		JOIN {{leafen_workspace_export_scope_pages}} AS [[scope]] ON [[scope]].[[page_id]] = [[items]].[[page_id]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[scope]].[[job_id]] = {:jobId} AND NOT [[scope]].[[row_page]] AND `+holder+`
			AND [[items]].[[workspace_id]] = {:workspaceId}
			AND [[items]].[[owner_id]] = {:ownerId} AND [[pages]].[[notion_import_staging]] = FALSE
			AND ([[items]].[[page_id]], [[items]].[[root_block_id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[root_block_id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedRowPageBlockWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format != workspaceExportFormatPageNativeArchive && !workspaceExportRendersDocuments(payload.Format) ||
		payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "12", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterRow"], params["afterId"] = after[0], after[1]
	filter := "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[scope]] WHERE [[scope]].[[job_id]]={:jobId} AND [[scope]].[[page_id]]=[[rows]].[[page_id]])"
	if workspaceExportDatabaseArchive(payload) {
		filter = workspaceExportDatabaseArchiveRowsSQL
	} else if payload.Scope.Kind == workspaceExportScopeDatabase {
		filter = "[[rows]].[[data_source_id]]={:targetId}"
	} else {
		// A database page exported on its own brings its body.
		filter = "(" + filter + " OR " + workspaceExportRootRowSQL("[[rows]].[[id]]") + ")"
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'rowPageBlock' AS [[kind]],
			'12:' || [[items]].[[page_id]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[rows]].[[page_id]], json_object(
				'id', [[items]].[[id]], 'pageId', [[rows]].[[page_id]], 'rowId', [[items]].[[page_id]],
				'parentId', [[items]].[[parent_id]], 'position', [[items]].[[position]],
				'type', [[items]].[[type]], 'content', json([[items]].[[content]]),
				'createdAt', [[items]].[[created]], 'updatedAt', [[items]].[[updated]]) AS [[payload]]
		FROM {{leafen_blocks}} AS [[items]]
		JOIN {{leafen_database_rows}} AS [[rows]] ON [[rows]].[[id]]=[[items]].[[page_id]]
			AND [[rows]].[[workspace_id]]=[[items]].[[workspace_id]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]]=[[rows]].[[page_id]]
		WHERE [[rows]].[[workspace_id]]={:workspaceId} AND [[rows]].[[owner]]={:ownerId}
			AND [[pages]].[[notion_import_staging]]=FALSE AND `+filter+`
			AND `+pageWithoutOwnDeletionFenceSQL("[[rows]].[[id]]")+`
			AND ([[items]].[[page_id]]>{:afterRow}
				OR ([[items]].[[page_id]]={:afterRow} AND [[items]].[[id]]>{:afterId}))
		ORDER BY [[items]].[[page_id]],[[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportScopedRowBlockTrashWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format != workspaceExportFormatPageNativeArchive || payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "13", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterRow"], params["afterId"] = after[0], after[1]
	filter := "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[scope]] WHERE [[scope]].[[job_id]]={:jobId} AND [[scope]].[[page_id]]=[[rows]].[[page_id]])"
	if workspaceExportDatabaseArchive(payload) {
		filter = workspaceExportDatabaseArchiveRowsSQL
	} else if payload.Scope.Kind == workspaceExportScopeDatabase {
		filter = "[[rows]].[[data_source_id]]={:targetId}"
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'rowBlockTrash' AS [[kind]],
			'13:' || [[items]].[[page_id]] || ':' || [[items]].[[root_block_id]] AS [[sort_key]],
			[[rows]].[[page_id]] AS [[page_id]], json_object(
				'rootBlockId', [[items]].[[root_block_id]], 'pageId', [[rows]].[[page_id]],
				'rowId', [[items]].[[page_id]], 'parentId', [[items]].[[parent_id]],
				'predecessorId', [[items]].[[predecessor_id]], 'successorId', [[items]].[[successor_id]],
				'snapshot', json([[items]].[[snapshot_json]]), 'blockCount', [[items]].[[block_count]],
				'deletedAt', [[items]].[[deleted_at]], 'expiresAt', [[items]].[[expires_at]]) AS [[payload]]
		FROM {{leafen_block_trash_tombstones}} AS [[items]]
		JOIN {{leafen_database_rows}} AS [[rows]] ON [[rows]].[[id]]=[[items]].[[page_id]]
			AND [[rows]].[[workspace_id]]=[[items]].[[workspace_id]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]]=[[rows]].[[page_id]]
		WHERE [[items]].[[workspace_id]]={:workspaceId} AND [[items]].[[owner_id]]={:ownerId}
			AND [[pages]].[[notion_import_staging]]=FALSE AND `+filter+`
			AND `+pageWithoutOwnDeletionFenceSQL("[[rows]].[[id]]")+`
			AND ([[items]].[[page_id]]>{:afterRow}
				OR ([[items]].[[page_id]]={:afterRow} AND [[items]].[[root_block_id]]>{:afterId}))
		ORDER BY [[items]].[[page_id]],[[items]].[[root_block_id]] LIMIT {:limit}
	`, params, limit)
}

func countWorkspaceExportScopedInventory(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload) (int, error) {
	if payload.Scope.Kind == workspaceExportScopeWorkspace {
		return countWorkspaceExportOpenInventory(app, authorization, job, payload)
	}
	if err := validateWorkspaceExportScopedDatabaseContainerReferences(app, authorization, job, payload); err != nil {
		return 0, err
	}
	count := 0
	cursor := ""
	for {
		rows, done, err := loadWorkspaceExportScopedInventoryWindow(app, authorization, job, payload, cursor, workspaceExportChunkItems)
		if err != nil {
			return 0, err
		}
		count += len(rows)
		if done {
			return count, nil
		}
		if len(rows) == 0 {
			return 0, errors.New(errorCodeWorkspaceExportArtifactInvalid)
		}
		cursor = rows[len(rows)-1].SortKey
	}
}

func validateWorkspaceExportScopedDatabaseContainerReferences(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	payload workspaceExportJobPayload,
) error {
	if payload.Format != workspaceExportFormatPageNativeArchive || payload.Scope.Kind == workspaceExportScopeWorkspace {
		return nil
	}
	linkedScope := `EXISTS (
		SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[scope]]
		WHERE [[scope]].[[job_id]]={:jobId} AND [[scope]].[[page_id]]=[[linked]].[[page_id]]
	)`
	sourceScope := `EXISTS (
		SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[scope]]
		WHERE [[scope]].[[job_id]]={:jobId} AND [[scope]].[[page_id]]=[[source]].[[page_id]]
	)`
	if payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		linkedScope = `EXISTS (
			SELECT 1 FROM {{leafen_workspace_export_scope_blocks}} AS [[scope]]
			WHERE [[scope]].[[job_id]]={:jobId} AND [[scope]].[[block_id]]=[[linked]].[[block_id]]
		)`
		sourceScope = `EXISTS (
			SELECT 1 FROM {{leafen_workspace_export_scope_blocks}} AS [[scope]]
			WHERE [[scope]].[[job_id]]={:jobId} AND [[scope]].[[block_id]]=[[source]].[[block_id]]
		)`
	} else if workspaceExportDatabaseArchive(payload) {
		linkedScope = workspaceExportDatabaseArchiveContainerScopeSQL("[[linked]]")
		sourceScope = workspaceExportDatabaseArchiveContainerScopeSQL("[[source]]")
	}
	return validateWorkspaceExportLinksInside(app, authorization, job, payload.Scope.TargetID, linkedScope, sourceScope)
}
