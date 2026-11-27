package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The background runners that pick uploads by a due time — unfinished-upload
// cleanup and the Notion external-file import — read the uploads table by
// that time's own index, named here. The data keeps no planner statistics
// (sqlite_planner_statistics.go), and for status IN (…) SQLite would take the
// status-led completed-file index (status, completed_at, file_id), or the
// workspace one for a workspace, read every upload of those statuses whether
// due or not, and sort them all to keep the first few. Read by the due index
// instead, a turn walks only the due range in due order and stops at its
// limit (measured 2026-11-19 on 100,000 completed and 25,000 unfinished
// uploads: the cleanup turn's read 11 ms → 0.13 ms, its frontier
// 19 ms → 0.02 ms, the import pick 7 ms → 0.05 ms).

const productFileUploadWorkspaceStatusIndex = "idx_leafen_file_uploads_workspace_status_upload"

// productFileUploadCleanupDueQuery reads the uploads whose cleanup is due by
// now — one workspace's when workspaceID is set — in due order, up to a
// cleanup turn's limit.
func productFileUploadCleanupDueQuery(
	app core.App,
	now int64,
	workspaceID string,
) *dbx.Query {
	query := productFileUploadSelect + `
		INDEXED BY [[` + productFileUploadCleanupDueIndex + `]]
		WHERE [[cleanup_due_at]] > 0 AND [[cleanup_due_at]] <= {:now}
			AND [[status]] IN ({:pending}, {:cleanupPending})
	`
	params := dbx.Params{
		"now": now, "pending": fileUploadStatusPending,
		"cleanupPending": fileUploadStatusCleanupPending,
		"limit":          fileTransferUploadCleanupTurnLimit,
	}
	if workspaceID != "" {
		query += " AND [[workspace_id]]={:workspaceId}"
		params["workspaceId"] = workspaceID
	}
	query += " ORDER BY [[cleanup_due_at]],[[upload_id]] LIMIT {:limit}"
	return app.DB().NewQuery(query).Bind(params)
}

// productFileUploadCleanupFrontierQuery says whether more uploads are due by
// now and when the earliest unfinished one falls due. Across the server that
// is the due index's first unfinished entry; for one workspace it is the
// smallest due time among that workspace's unfinished uploads, read by the
// workspace index so other workspaces' uploads are not walked.
func productFileUploadCleanupFrontierQuery(
	app core.App,
	now int64,
	workspaceID string,
) *dbx.Query {
	dueIndex := " INDEXED BY [[" + productFileUploadCleanupDueIndex + "]]"
	unfinished := " AND [[status]] IN ({:pending}, {:cleanupPending})"
	params := dbx.Params{
		"now": now, "pending": fileUploadStatusPending,
		"cleanupPending": fileUploadStatusCleanupPending,
	}
	workspace := ""
	nextDue := `(SELECT [[cleanup_due_at]] FROM {{leafen_file_uploads}}` + dueIndex + `
			WHERE [[cleanup_due_at]] > 0` + unfinished + `
			ORDER BY [[cleanup_due_at]] LIMIT 1)`
	if workspaceID != "" {
		workspace = " AND [[workspace_id]]={:workspaceId}"
		params["workspaceId"] = workspaceID
		nextDue = `(SELECT MIN([[cleanup_due_at]]) FROM {{leafen_file_uploads}}
			INDEXED BY [[` + productFileUploadWorkspaceStatusIndex + `]]
			WHERE [[workspace_id]]={:workspaceId}` + unfinished + `
				AND [[cleanup_due_at]] > 0)`
	}
	return app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_file_uploads}}` + dueIndex + `
			WHERE [[cleanup_due_at]] > 0 AND [[cleanup_due_at]] <= {:now}` +
		unfinished + workspace + ` LIMIT 1
		) AS [[has_more]], ` + nextDue + ` AS [[next_due]]
	`).Bind(params)
}

// notionFileUploadImportReadyQuery reads the Notion external-file import
// whose next attempt is the earliest one due by now.
func notionFileUploadImportReadyQuery(app core.App, now int64) *dbx.Query {
	return app.DB().NewQuery(productFileUploadSelect + `
		INDEXED BY [[` + notionFileUploadImportIndex + `]]
		WHERE [[kind]]={:kind} AND [[notion_mode]]={:mode}
			AND [[status]] IN ({:pending},{:settling})
			AND [[notion_import_next_at]]>0 AND [[notion_import_next_at]]<={:now}
		ORDER BY [[notion_import_next_at]],[[upload_id]] LIMIT 1
	`).Bind(dbx.Params{
		"kind": fileUploadKindNotionExternal, "mode": notionFileUploadModeExternal,
		"pending": fileUploadStatusPending, "settling": fileUploadStatusSettling, "now": now,
	})
}

// notionFileUploadImportNextQuery reads when the earliest waiting Notion
// external-file import is next tried, or 0 when none waits.
func notionFileUploadImportNextQuery(app core.App) *dbx.Query {
	return app.DB().NewQuery(`
		SELECT COALESCE(MIN([[notion_import_next_at]]),0) FROM {{leafen_file_uploads}}
		INDEXED BY [[` + notionFileUploadImportIndex + `]]
		WHERE [[kind]]={:kind} AND [[status]] IN ({:pending},{:settling})
			AND [[notion_import_next_at]]>0
	`).Bind(dbx.Params{
		"kind":    fileUploadKindNotionExternal,
		"pending": fileUploadStatusPending, "settling": fileUploadStatusSettling,
	})
}
