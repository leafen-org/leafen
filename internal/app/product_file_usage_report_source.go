package app

import (
	"context"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedProductFileUsageReportPendingSource struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	Name         string `db:"name"`
	ContentType  string `db:"content_type"`
	Size         int64  `db:"size"`
	Status       string `db:"status"`
	CreatedAt    int64  `db:"created_at"`
	CleanupDueAt int64  `db:"cleanup_due_at"`
}

func loadProductFileUsageReportSourceWindow(
	ctx context.Context,
	app core.App,
	workspaceID string,
	cursor string,
	limit int,
	now int64,
) (productFileUsageReportSourceWindow, error) {
	result := productFileUsageReportSourceWindow{Items: []productFileUsageReportSource{}}
	if workspaceID == "" || limit < 1 || limit > fileUsageReportUploadWindow {
		return result, errFileUsageReportStateInvalid
	}
	if strings.HasPrefix(cursor, "pending:") {
		return loadProductFileUsageReportPendingWindow(
			ctx, app, workspaceID, strings.TrimPrefix(cursor, "pending:"), limit, now,
		)
	}
	after := ""
	if cursor != "" {
		if !strings.HasPrefix(cursor, "files:") {
			return result, errFileUsageReportStateInvalid
		}
		after = strings.TrimPrefix(cursor, "files:")
	}
	files := []productFileUsageReportSource{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace]] AS [[workspace_id]],[[original_name]] AS [[name]],
			[[content_type]],CAST([[size]] AS INTEGER) AS [[size]],'uploaded' AS [[status]],
			[[created]] AS [[created_at]]
		FROM {{leafen_files}} INDEXED BY [[idx_leafen_file_usage_report_files]]
		WHERE [[workspace]]={:workspaceId} AND [[id]]>{:after}
		ORDER BY [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "after": after, "limit": limit + 1,
	}).All(&files)
	if err != nil {
		return result, err
	}
	result.ReadRows += len(files)
	if len(files) > limit {
		result.Items = files[:limit]
		result.HasMore = true
		result.Next = "files:" + result.Items[len(result.Items)-1].ID
		return result, nil
	}
	result.Items = files
	remaining := limit - len(result.Items)
	pending, err := loadProductFileUsageReportPendingWindow(
		ctx, app, workspaceID, "", remaining, now,
	)
	if err != nil {
		return result, err
	}
	result.ReadRows += pending.ReadRows
	result.Items = append(result.Items, pending.Items...)
	result.HasMore = pending.HasMore
	result.Next = pending.Next
	return result, nil
}

func loadProductFileUsageReportPendingWindow(
	ctx context.Context,
	app core.App,
	workspaceID string,
	after string,
	limit int,
	now int64,
) (productFileUsageReportSourceWindow, error) {
	result := productFileUsageReportSourceWindow{Items: []productFileUsageReportSource{}}
	queryLimit := limit + 1
	if queryLimit < 1 {
		queryLimit = 1
	}
	rows := []storedProductFileUsageReportPendingSource{}
	err := app.DB().NewQuery(`
		SELECT [[upload_id]] AS [[id]],[[workspace_id]],[[original_name]] AS [[name]],
			[[content_type]],[[total_size]] AS [[size]],[[status]],[[created_at]],[[cleanup_due_at]]
		FROM {{leafen_file_uploads}} INDEXED BY [[idx_leafen_file_usage_report_uploads]]
		WHERE [[workspace_id]]={:workspaceId} AND [[upload_id]]>{:after}
			AND [[status]] IN ('pending','settling','cleanup_pending') AND [[file_id]]=''
		ORDER BY [[upload_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "after": after, "limit": queryLimit,
	}).All(&rows)
	if err != nil {
		return result, err
	}
	result.ReadRows = len(rows)
	consumed := len(rows)
	if consumed > limit {
		consumed = limit
		result.HasMore = true
	}
	for _, row := range rows[:consumed] {
		status := "pending"
		if row.Status == fileUploadStatusCleanupPending ||
			(row.CleanupDueAt > 0 && row.CleanupDueAt <= now) {
			status = "expired"
		}
		result.Items = append(result.Items, productFileUsageReportSource{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name,
			ContentType: row.ContentType, Size: row.Size, Status: status,
			CreatedAt: time.UnixMilli(row.CreatedAt).UTC().Format(time.RFC3339Nano),
		})
	}
	if result.HasMore {
		result.Next = "pending:"
		if len(result.Items) > 0 {
			result.Next += result.Items[len(result.Items)-1].ID
		}
	}
	return result, nil
}

func loadProductFileUsageReportWorkspaces(
	ctx context.Context,
	app core.App,
	organizationID string,
	after string,
) ([]productFileUsageReportWorkspace, bool, int, error) {
	rows := []productFileUsageReportWorkspace{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[name]],'' AS [[domain]]
		FROM {{leafen_workspaces}} INDEXED BY [[idx_leafen_file_usage_report_workspaces]]
		WHERE [[organization]]={:organizationId} AND [[id]]>{:after}
		ORDER BY [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "after": after,
		"limit": fileUsageReportBatchWorkspaceWindow + 1,
	}).All(&rows)
	if err != nil {
		return nil, false, 0, err
	}
	readRows := len(rows)
	hasMore := len(rows) > fileUsageReportBatchWorkspaceWindow
	if hasMore {
		rows = rows[:fileUsageReportBatchWorkspaceWindow]
	}
	return rows, hasMore, readRows, nil
}
