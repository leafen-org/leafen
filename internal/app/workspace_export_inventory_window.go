package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceExportInventoryStage struct {
	prefix string
	load   func(core.App, workspaceAuthorization, workspaceExportPageFilter, string, int) ([]storedWorkspaceExportInventoryItem, error)
}

var workspaceExportInventoryStages = []workspaceExportInventoryStage{
	{prefix: "01", load: loadWorkspaceExportPageWindow},
	{prefix: "02", load: loadWorkspaceExportBlockWindow},
	{prefix: "03", load: loadWorkspaceExportDataSourceWindow},
	{prefix: "04", load: loadWorkspaceExportViewWindow},
	{prefix: "05", load: loadWorkspaceExportPropertyWindow},
	{prefix: "06", load: loadWorkspaceExportRowWindow},
	{prefix: "07", load: loadWorkspaceExportYgoWindow},
	{prefix: "08", load: loadWorkspaceExportTemplateWindow},
	{prefix: "09", load: loadWorkspaceExportDatabaseContainerWindow},
	{prefix: "10", load: loadWorkspaceExportBlockTrashWindow},
	{prefix: "11", load: loadWorkspaceExportCustomEmojiWindow},
	{prefix: "12", load: loadWorkspaceExportRowPageBlockWindow},
	{prefix: "13", load: loadWorkspaceExportRowBlockTrashWindow},
	{prefix: "14", load: loadWorkspaceExportCommentWindow},
	{prefix: "15", load: loadWorkspaceExportTeamspaceWindow},
	{prefix: "16", load: loadWorkspaceExportAutomationWindow},
}

func loadWorkspaceExportInventoryWindow(
	app core.App,
	authorization workspaceAuthorization,
	filter workspaceExportPageFilter,
	cursor string,
	limit int,
) ([]storedWorkspaceExportInventoryItem, bool, error) {
	if limit < 1 {
		return nil, false, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	start := 0
	if cursor != "" {
		start = -1
		for index, stage := range workspaceExportInventoryStages {
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
	for index := start; index < len(workspaceExportInventoryStages); index++ {
		remaining := limit - len(rows)
		stageCursor := ""
		if index == start {
			stageCursor = cursor
		}
		window, err := workspaceExportInventoryStages[index].load(
			app, authorization, filter, stageCursor, remaining+1,
		)
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

func workspaceExportStageCursor(cursor, prefix string, fields int) ([]string, error) {
	values := make([]string, fields)
	if cursor == "" {
		return values, nil
	}
	header := prefix + ":"
	if !strings.HasPrefix(cursor, header) {
		return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	rest := strings.TrimPrefix(cursor, header)
	switch fields {
	case 1:
		values[0] = rest
	case 2:
		last := strings.LastIndex(rest, ":")
		if last < 1 || last == len(rest)-1 {
			return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
		}
		values[0], values[1] = rest[:last], rest[last+1:]
	case 3:
		first, last := strings.Index(rest, ":"), strings.LastIndex(rest, ":")
		if first < 1 || last <= first || last == len(rest)-1 {
			return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
		}
		values[0], values[1], values[2] = rest[:first], rest[first+1:last], rest[last+1:]
	default:
		return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	return values, nil
}

func queryWorkspaceExportStage(
	app core.App,
	query string,
	params dbx.Params,
	limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	params["limit"] = limit
	rows := []storedWorkspaceExportInventoryItem{}
	err := app.DB().NewQuery(query).Bind(params).All(&rows)
	return rows, err
}

func exportStageParams(authorization workspaceAuthorization, filter workspaceExportPageFilter) dbx.Params {
	return dbx.Params{
		"workspaceId": authorization.WorkspaceID,
		"ownerId":     authorization.OwnerID,
		"actorId":     authorization.PrincipalID,
		"jobId":       filter.JobID,
	}
}

func loadWorkspaceExportPageWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "01", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterCreated"], params["afterId"] = after[0], after[1]
	return queryWorkspaceExportStage(app, `
		SELECT 'page' AS [[kind]], '01:' || [[created]] || ':' || [[id]] AS [[sort_key]],
			[[id]] AS [[page_id]], json_patch(json_patch(json_patch(json_object(
				'id', [[id]], 'workspaceId', [[workspace]], 'teamspaceId', [[teamspace_id]],
				'title', [[title]], 'content', [[content]],
				'revision', [[revision]], 'cursor', [[cursor]], 'parentId', `+filter.parentSQL("[[pages]]")+`,
				'position', [[position]],
				'isLocked', json(CASE WHEN [[is_locked]] THEN 'true' ELSE 'false' END),
				'inTrash', json(CASE WHEN [[in_trash]] THEN 'true' ELSE 'false' END),
				'trashedAt', NULLIF([[trashed_at]], ''),
				'archivedAt', NULLIF([[archived_at]], ''),
				'icon', [[icon]], 'iconType', [[icon_type]], 'cover', [[cover]],
				'coverPosition', [[cover_position]], 'font', [[font]],
				'smallText', json(CASE WHEN [[small_text]] THEN 'true' ELSE 'false' END),
				'fullWidth', json(CASE WHEN [[full_width]] THEN 'true' ELSE 'false' END),
				'backlinksDisplay', [[backlinks_display]],
				'pageCommentsDisplay', [[page_comments_display]],
				'tableOfContentsDisplay', [[table_of_contents_display]],
				'inlineCommentsDisplay', [[inline_comments_display]],
				'kind', [[kind]],
				'movedDatabase', `+databasePageSignpostContainerSQL("[[pages]]")+`,
				'createdBy', [[created_by]], 'lastEditedBy', [[last_edited_by]],
				'updatedAt', [[updated]]), `+workspaceExportTitleSpansPatch("[[pages]]")+`), `+workspaceExportPageReactionsPatch(commentTargetPage, "[[pages]].[[id]]")+`), `+workspaceExportPageAnchorPatch("[[pages]]")+`) AS [[payload]]
		FROM {{leafen_pages}} AS [[pages]]
		WHERE [[workspace]] = {:workspaceId} AND [[owner]] = {:ownerId}
			AND [[notion_import_staging]] = FALSE AND [[data_source_id]] = '' AND `+filter.sql("[[pages]]")+`
			AND ([[created]] > {:afterCreated} OR ([[created]] = {:afterCreated} AND [[id]] > {:afterId}))
		ORDER BY [[created]], [[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportBlockWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportGraphWindow(app, authorization, filter, cursor, limit, "02", `
		SELECT 'block' AS [[kind]], '02:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]],
				'parentId', NULLIF([[items]].[[parent_id]], ''), 'position', [[items]].[[position]],
				'type', [[items]].[[type]], 'content', json([[items]].[[content]])) AS [[payload]]
		FROM {{leafen_blocks}} AS [[items]] JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
			AND [[pages]].[[data_source_id]] = ''
	`)
}

func loadWorkspaceExportDataSourceWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "03", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterPage"], params["afterId"] = after[0], after[1]
	return queryWorkspaceExportStage(app, `
		SELECT 'dataSource' AS [[kind]], '03:' || [[items]].[[page_id]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportDataSourceItemSQL+` AS [[payload]]
		FROM {{leafen_data_sources}} AS [[items]] JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND `+filter.sql("[[pages]]")+`
			AND ([[items]].[[page_id]], [[items]].[[id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportViewWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportGraphWindow(app, authorization, filter, cursor, limit, "04", `
		SELECT 'view' AS [[kind]], '04:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportViewItemSQL+` AS [[payload]]
		FROM {{leafen_database_views}} AS [[items]] JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
	`)
}

func loadWorkspaceExportPropertyWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	// A database page shared on its own shows its database's properties.
	return loadWorkspaceExportGraphWindowAlso(app, authorization, filter, cursor, limit, "05", `
		SELECT 'property' AS [[kind]], '05:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], json_object('id', [[items]].[[id]], 'pageId', [[items]].[[page_id]],
				'dataSourceId', [[items]].[[data_source_id]], 'type', [[items]].[[type]], 'name', [[items]].[[name]],
				'position', [[items]].[[position]], 'config', json([[items]].[[config]]),
				'deletedAt', (SELECT strftime('%Y-%m-%dT%H:%M:%fZ', [[deleted_at]] / 1000.0, 'unixepoch') FROM {{leafen_database_property_trash}} WHERE [[property_id]]=[[items]].[[id]] AND [[deleting]]=0)) AS [[payload]]
		FROM {{leafen_database_properties}} AS [[items]] JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
	`, filter.sharedSourceSQL("[[items]].[[data_source_id]]"))
}

func loadWorkspaceExportRowWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportGraphWindowAlso(app, authorization, filter, cursor, limit, "06", `
		SELECT 'row' AS [[kind]], '06:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportRowItemSQL+` AS [[payload]]
		FROM {{leafen_database_rows}} AS [[items]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]]=[[items]].[[id]]
			AND `+pageWithoutOwnDeletionFenceSQL("[[items]].[[id]]")+`
	`, filter.sharedRowSQL("[[items]].[[id]]"))
}

// workspaceExportCommentPayloadSQL is a comment as an archive carries it.
const workspaceExportCommentPayloadSQL = `json_object(
				'id', [[items]].[[id]], 'pageId', [[items]].[[page_id]],
				'targetKind', [[items]].[[target_kind]], 'targetId', [[items]].[[target_id]],
				'blockId', [[items]].[[block_id]], 'parentId', [[items]].[[parent_id]],
				'body', json([[items]].[[body_json]]),
				'resolved', json(CASE WHEN [[items]].[[resolved]] THEN 'true' ELSE 'false' END),
				'createdAt', [[items]].[[created_at]], 'editedAt', [[items]].[[edited_at]],
				'authorId', [[items]].[[author_id]],
				'reactions', json((SELECT json_group_array(json_object('emoji', [[reaction]].[[emoji]], 'userId', [[reaction]].[[user_id]]))
					FROM (SELECT [[emoji]], [[user_id]] FROM {{leafen_comment_reactions}}
						WHERE [[comment_id]] = [[items]].[[id]] ORDER BY [[created_at]], [[user_id]], [[emoji]]) AS [[reaction]])),
				'threadState', json((SELECT json_object(
						'repliesMuted', json(CASE WHEN [[state]].[[replies_muted]] THEN 'true' ELSE 'false' END),
						'unread', json(CASE WHEN [[state]].[[marked_unread]] THEN 'true' ELSE 'false' END))
					FROM {{leafen_comment_thread_states}} AS [[state]]
					WHERE [[state]].[[comment_id]] = [[items]].[[id]] AND [[state]].[[user_id]] = {:actorId})))`

// workspaceExportCommentOutsideRowFenceSQL keeps a database page's comments,
// which are stored on the page holding its database, out of an export while
// the database page is behind its own deletion fence.
var workspaceExportCommentOutsideRowFenceSQL = `([[items]].[[target_kind]] != '` + commentTargetDatabaseRow + `'
				OR ` + pageWithoutOwnDeletionFenceSQL("[[items]].[[target_id]]") + `)`

// loadWorkspaceExportCommentWindow reads a window of comments with their
// reactions, each person's emoji in the order used, and the exporting
// person's own state of each thread.
func loadWorkspaceExportCommentWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "14", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterCreated"], params["afterId"] = after[0], after[1]
	return queryWorkspaceExportStage(app, `
		SELECT 'comment' AS [[kind]],
			'14:' || [[items]].[[created_at]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportCommentPayloadSQL+` AS [[payload]]
		FROM {{leafen_comments}} AS [[items]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[pages]].[[owner]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND `+filter.sql("[[pages]]")+`
			AND `+workspaceExportCommentOutsideRowFenceSQL+`
			AND ([[items]].[[created_at]], [[items]].[[id]]) > ({:afterCreated}, {:afterId})
		ORDER BY [[items]].[[created_at]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportGraphWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
	prefix string, selectSQL string,
) ([]storedWorkspaceExportInventoryItem, error) {
	return loadWorkspaceExportGraphWindowAlso(app, authorization, filter, cursor, limit, prefix, selectSQL, "FALSE")
}

// loadWorkspaceExportGraphWindowAlso reads a window of page graph items, of
// the pages the export takes and those the also condition names. Each
// window, here and in the other stages, goes on after the last item by a
// row-value comparison, which the index seeks to: written as ORs it read
// every item before it again (2026-11-11: a table of 100,000 rows went from
// 1,600 items a second at its start to 300 at its end).
func loadWorkspaceExportGraphWindowAlso(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
	prefix string, selectSQL string, also string,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, prefix, 3)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	return queryWorkspaceExportStage(app, selectSQL+`
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND (`+filter.sql("[[pages]]")+` OR `+also+`)
			AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportYgoWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "07", 1)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterId"], params["ygoFormatVersion"] = after[0], ygoDocumentFormatVersion
	return queryWorkspaceExportStage(app, `
		SELECT 'ygoDocument' AS [[kind]], '07:' || [[pages]].[[id]] AS [[sort_key]],
			[[pages]].[[id]] AS [[page_id]], json_object('pageId', [[pages]].[[id]], 'formatVersion', {:ygoFormatVersion}) AS [[payload]]
		FROM {{leafen_pages}} AS [[pages]]
		WHERE [[pages]].[[workspace]] = {:workspaceId} AND [[pages]].[[owner]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND [[pages]].[[id]] > {:afterId}
			AND [[pages]].[[data_source_id]] = '' AND `+filter.sql("[[pages]]")+`
			AND (EXISTS (SELECT 1 FROM {{leafen_ygo_documents}} WHERE [[room]] = [[pages]].[[id]])
				OR EXISTS (SELECT 1 FROM {{leafen_ygo_structural_documents}} WHERE [[room]] = [[pages]].[[id]]))
		ORDER BY [[pages]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportTemplateWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "08", 3)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	return queryWorkspaceExportStage(app, `
		SELECT 'template' AS [[kind]],
			'08:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], json_object(
				'id', [[items]].[[id]], 'pageId', [[items]].[[page_id]],
				'dataSourceId', [[items]].[[data_source_id]], 'name', [[items]].[[name]],
				'icon', [[items]].[[icon]], 'title', [[items]].[[title]],
				'properties', json([[items]].[[properties_json]]),
				'propertySpans', json([[items]].[[property_spans_json]]), 'blocks', json_array(),
				'isDefault', json(CASE WHEN [[items]].[[is_default]] THEN 'true' ELSE 'false' END),
				'position', [[items]].[[position]], 'createdAt', [[items]].[[created_at]],
				'updatedAt', [[items]].[[updated_at]]) AS [[payload]]
		FROM {{leafen_database_templates}} AS [[items]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND `+filter.sql("[[pages]]")+`
			AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

// workspaceExportDatabaseContainerItemSQL is a database as an archive
// carries it, a page's or a workspace's, by the rules an import checks
// (workspace_import_archive_database_containers.go): where its block stands
// or stood, its views source by source, each source's by place, but with its
// block in the trash not those it owns, which travel in the blockTrash entry.
const workspaceExportDatabaseContainerItemSQL = `json_object(
				'id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'blockId', [[items]].[[block_id]],
				'parentBlockId', NULLIF(COALESCE((SELECT [[parent_id]] FROM {{leafen_blocks}}
					WHERE [[id]] = [[items]].[[block_id]]), [[items]].[[parent_block_id]]), ''),
				'position', COALESCE((SELECT [[position]] FROM {{leafen_blocks}}
					WHERE [[id]] = [[items]].[[block_id]]), [[items]].[[position]]),
				'kind', [[items]].[[kind]], 'sourceContainerId', NULLIF([[items]].[[source_container_id]], ''),
				'dataSourceIds', json(COALESCE((
					SELECT json_group_array([[ordered]].[[id]]) FROM (
						SELECT [[membership]].[[data_source_id]] AS [[id]]
						FROM {{leafen_database_container_sources}} AS [[membership]]
						WHERE [[membership]].[[container_id]] = [[items]].[[id]]
						ORDER BY [[membership]].[[position]], [[membership]].[[data_source_id]]
					) AS [[ordered]]
				), '[]')),
				'viewIds', json(COALESCE((
					SELECT json_group_array([[ordered]].[[id]]) FROM (
						SELECT [[membership]].[[view_id]] AS [[id]]
						FROM {{leafen_database_container_views}} AS [[membership]]
						JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[membership]].[[view_id]]
						JOIN {{leafen_database_container_sources}} AS [[source]]
							ON [[source]].[[container_id]] = [[membership]].[[container_id]]
							AND [[source]].[[data_source_id]] = [[view]].[[data_source_id]]
						WHERE [[membership]].[[container_id]] = [[items]].[[id]]
						ORDER BY [[source]].[[position]], [[source]].[[data_source_id]], [[view]].[[position]], [[view]].[[id]]
					) AS [[ordered]]
				), '[]'))
			)`

func loadWorkspaceExportDatabaseContainerWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "09", 3)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterPage"], params["afterPosition"], params["afterId"] = after[0], after[1], after[2]
	return queryWorkspaceExportStage(app, `
		SELECT 'databaseContainer' AS [[kind]],
			'09:' || [[items]].[[page_id]] || ':' || [[items]].[[position]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportDatabaseContainerItemSQL+` AS [[payload]]
		FROM {{leafen_database_containers}} AS [[items]]
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND `+filter.sql("[[pages]]")+`
			AND ([[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]]) > ({:afterPage}, {:afterPosition}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[position]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportBlockTrashWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "10", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
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
		JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner_id]] = {:ownerId}
			AND [[pages]].[[notion_import_staging]] = FALSE AND [[pages]].[[data_source_id]] = '' AND `+filter.sql("[[pages]]")+`
			AND ([[items]].[[page_id]], [[items]].[[root_block_id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[root_block_id]] LIMIT {:limit}
	`, params, limit)
}

// loadWorkspaceExportCustomEmojiWindow exports the workspace's custom emoji,
// which are no page's: its own and those it carries (custom_emoji_carried.go),
// which come back carried.
func loadWorkspaceExportCustomEmojiWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "11", 1)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterId"] = after[0]
	return queryWorkspaceExportStage(app, `
		SELECT 'customEmoji' AS [[kind]], '11:' || [[id]] AS [[sort_key]],
			[[id]] AS [[page_id]], json_object(
				'id', [[id]], 'name', [[name]], 'fileId', [[file_id]],
				'createdAt', [[created_at]], 'updatedAt', [[updated_at]],
				'carried', json(CASE WHEN [[carried]]=1 THEN 'true' ELSE 'false' END)) AS [[payload]]
		FROM {{leafen_custom_emojis}}
		WHERE [[workspace_id]]={:workspaceId} AND [[workspace_import_job_id]]=''
			AND [[id]]>{:afterId}
		ORDER BY [[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportRowPageBlockWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "12", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterRow"], params["afterId"] = after[0], after[1]
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
			AND [[pages]].[[notion_import_staging]]=FALSE
			AND (`+filter.sql("[[pages]]")+` OR `+filter.sharedRowSQL("[[rows]].[[id]]")+`)
			AND `+pageWithoutOwnDeletionFenceSQL("[[rows]].[[id]]")+`
			AND ([[items]].[[page_id]]>{:afterRow}
				OR ([[items]].[[page_id]]={:afterRow} AND [[items]].[[id]]>{:afterId}))
		ORDER BY [[items]].[[page_id]],[[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}

func loadWorkspaceExportRowBlockTrashWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "13", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterRow"], params["afterId"] = after[0], after[1]
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
			AND [[pages]].[[notion_import_staging]]=FALSE AND `+filter.sql("[[pages]]")+`
			AND `+pageWithoutOwnDeletionFenceSQL("[[rows]].[[id]]")+`
			AND ([[items]].[[page_id]]>{:afterRow}
				OR ([[items]].[[page_id]]={:afterRow} AND [[items]].[[root_block_id]]>{:afterId}))
		ORDER BY [[items]].[[page_id]],[[items]].[[root_block_id]] LIMIT {:limit}
	`, params, limit)
}
