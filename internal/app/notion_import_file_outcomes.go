package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedNotionFileOutcome struct {
	storedNotionFileTask
	PageID    string `db:"page_id"`
	PageTitle string `db:"page_title"`
}

func readNotionImportFileOutcomes(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	limit, err := notionImportFileOutcomeLimit(event.Request.URL.Query().Get("limit"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the file result range.")
	}
	cursor := strings.TrimSpace(event.Request.URL.Query().Get("cursor"))
	var stored []storedNotionFileOutcome
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	err = withAuthorizedNotionImportRead(event.Request.Context(), event.App, user.Id, workspaceID, func(txApp core.App) error {
		job, readErr := loadStoredJob(txApp, jobID, user.Id)
		if errors.Is(readErr, sql.ErrNoRows) ||
			(readErr == nil && (job.WorkspaceID != workspaceID || job.Kind != notionImportJobKind)) {
			return sql.ErrNoRows
		}
		if readErr != nil {
			return readErr
		}
		stored, readErr = loadNotionImportFileOutcomeRows(txApp, job.ID, cursor, limit+1)
		return readErr
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportNotFound, "Import job not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportNotFound, "Couldn't read the import result.", err)
	}
	hasMore := len(stored) > limit
	if hasMore {
		stored = stored[:limit]
	}
	items := make([]notionImportFileOutcome, 0, len(stored))
	for _, task := range stored {
		items = append(items, notionImportFileOutcome{
			SourceID: task.SourceID, Status: task.Status,
			ErrorCode: task.ErrorCode, FileID: task.FileID,
			PageID: task.PageID, PageTitle: task.PageTitle,
		})
	}
	nextCursor := ""
	if hasMore {
		nextCursor = items[len(items)-1].SourceID
	}
	return event.JSON(http.StatusOK, notionImportFileOutcomePage{
		Items: items, NextCursor: nextCursor, HasMore: hasMore,
	})
}

func notionImportFileOutcomeLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return notionImportFileOutcomeItems, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > notionImportFileOutcomeItems {
		return 0, errors.New(errorCodeInvalidInput)
	}
	return limit, nil
}

// loadNotionImportFileOutcomeRows reads a page of an import's settled file
// tasks, each with the page or database page it sits in: a file in a toggle,
// a column or another block belongs to the page that block is on.
func loadNotionImportFileOutcomeRows(app core.App, jobID, cursor string, limit int) ([]storedNotionFileOutcome, error) {
	stored := make([]storedNotionFileOutcome, 0, limit)
	err := app.DB().NewQuery(`
		WITH RECURSIVE chosen AS (
			SELECT task.[[source_id]] AS [[task_source]], source.[[parent_source_id]] AS [[ancestor]]
			FROM {{leafen_notion_import_file_tasks}} task
			JOIN {{leafen_notion_import_sources}} source
				ON source.[[job_id]] = task.[[job_id]] AND source.[[source_id]] = task.[[source_id]]
			WHERE task.[[job_id]] = {:jobId} AND task.[[status]] != 'pending' AND task.[[source_id]] > {:cursor}
			ORDER BY task.[[source_id]]
			LIMIT {:limit}
		), climb([[task_source]], [[ancestor]]) AS (
			SELECT [[task_source]], [[ancestor]] FROM chosen
			UNION
			SELECT climb.[[task_source]], up.[[parent_source_id]]
			FROM climb JOIN {{leafen_notion_import_sources}} up
				ON up.[[job_id]] = {:jobId} AND up.[[source_id]] = climb.[[ancestor]]
			WHERE up.[[kind]] NOT IN ('page', 'row')
		)
		SELECT task.[[source_id]], task.[[name]], task.[[url_cipher]], task.[[status]], task.[[file_id]],
			task.[[error_code]], task.[[attempt]], COALESCE(mapping.[[native_id]], '') AS [[page_id]],
			COALESCE(page.[[title]], '') AS [[page_title]]
		FROM chosen
		JOIN {{leafen_notion_import_file_tasks}} task
			ON task.[[job_id]] = {:jobId} AND task.[[source_id]] = chosen.[[task_source]]
		LEFT JOIN climb ON climb.[[task_source]] = chosen.[[task_source]] AND EXISTS (
			SELECT 1 FROM {{leafen_notion_import_sources}} holder
			WHERE holder.[[job_id]] = {:jobId} AND holder.[[source_id]] = climb.[[ancestor]]
				AND holder.[[kind]] IN ('page', 'row')
		)
		LEFT JOIN {{leafen_notion_import_sources}} page
			ON page.[[job_id]] = {:jobId} AND page.[[source_id]] = climb.[[ancestor]]
		LEFT JOIN {{leafen_notion_import_mappings}} mapping
			ON mapping.[[job_id]] = {:jobId} AND mapping.[[source_id]] = page.[[source_id]]
				AND mapping.[[kind]] = page.[[kind]]
		ORDER BY task.[[source_id]]
	`).Bind(dbx.Params{
		"jobId": jobID, "cursor": cursor, "limit": limit,
	}).All(&stored)
	return stored, err
}
