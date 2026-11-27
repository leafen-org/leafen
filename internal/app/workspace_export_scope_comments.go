package app

import "github.com/pocketbase/pocketbase/core"

// loadWorkspaceExportScopedCommentWindow captures the discussions of the
// exported pages: always for a page's archive, which keeps what the page holds
// as a workspace's does, and for a readable export when it asks for them.
func loadWorkspaceExportScopedCommentWindow(
	app core.App,
	authorization workspaceAuthorization,
	job storedJob,
	payload workspaceExportJobPayload,
	cursor string,
	limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	wanted := payload.Format == workspaceExportFormatPageNativeArchive ||
		workspaceExportRendersDocuments(payload.Format) && payload.Options.IncludeComments
	if !wanted || payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "14", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterCreated"], params["afterId"] = after[0], after[1]
	// The discussions on the page holding a database stay out of its archive.
	holder := "TRUE"
	if workspaceExportDatabaseArchive(payload) {
		holder = "NOT [[scope]].[[is_root]]"
	}
	return queryWorkspaceExportStage(app, `
		SELECT 'comment' AS [[kind]],
			'14:' || [[items]].[[created_at]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportCommentPayloadSQL+` AS [[payload]]
		FROM {{leafen_comments}} AS [[items]]
		JOIN {{leafen_workspace_export_scope_pages}} AS [[scope]]
			ON [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND `+holder+`
			AND `+workspaceExportCommentOutsideRowFenceSQL+`
			AND ([[items]].[[created_at]], [[items]].[[id]]) > ({:afterCreated}, {:afterId})
		ORDER BY [[items]].[[created_at]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}
