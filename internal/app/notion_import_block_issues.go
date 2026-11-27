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

const maxNotionImportBlockIssueCursorBytes = 1024

// storedNotionImportBlockIssue is a page an import's problems of one kind,
// subject and why were on, and how many; read by the items of what didn't
// come whole (notion_import_issue_places.go), also the item's kind, how
// many places it has in all and whether the place is a template.
type storedNotionImportBlockIssue struct {
	Key        string `db:"group_key"`
	PageID     string `db:"page_id"`
	PageTitle  string `db:"page_title"`
	BlockType  string `db:"block_type"`
	Reason     string `db:"reason"`
	Count      int    `db:"issue_count"`
	Kind       string `db:"item_kind"`
	Places     int    `db:"places"`
	InTemplate bool   `db:"in_template"`
}

// readNotionImportBlockIssues lists an import's block, property and view
// issues by the page they were on, the Notion block type (for a property or
// view, its name) and the reason, with how many blocks, properties or views
// each covers. A block inside other blocks, or a database's property or view,
// is placed on the page that holds them.
func readNotionImportBlockIssues(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	limit, err := notionImportBlockIssueLimit(event.Request.URL.Query().Get("limit"))
	cursor := event.Request.URL.Query().Get("cursor")
	if err != nil || len(cursor) > maxNotionImportBlockIssueCursorBytes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the block result range.")
	}
	stored := []storedNotionImportBlockIssue{}
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
		stored, readErr = loadNotionImportBlockIssues(txApp, job.ID, cursor, limit+1)
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
	// Each page named as the app names one with no title, by its kind.
	pageIDs := make([]string, 0, len(stored))
	for _, issue := range stored {
		pageIDs = append(pageIDs, issue.PageID)
	}
	kinds, err := loadPageNameKinds(event.App, pageIDs)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportNotFound, "Couldn't read the import result.", err)
	}
	items := make([]notionImportBlockIssue, 0, len(stored))
	for _, issue := range stored {
		items = append(items, notionImportBlockIssue{
			Key: issue.Key, PageID: issue.PageID, PageTitle: issue.PageTitle,
			PageKind: kinds[issue.PageID].Kind, ViewBlock: kinds[issue.PageID].ViewBlock, SourceTitles: kinds[issue.PageID].SourceTitles,
			BlockType: issue.BlockType, Reason: issue.Reason, Count: issue.Count,
		})
	}
	nextCursor := ""
	if hasMore {
		nextCursor = items[len(items)-1].Key
	}
	return event.JSON(http.StatusOK, notionImportBlockIssuePage{Items: items, NextCursor: nextCursor, HasMore: hasMore})
}

// loadNotionImportBlockIssues reads a window of an import's issues in their
// groups, the page they were on first, item by item — the items of what
// didn't come whole name their first pages (notion_import_issue_places.go). A
// database row is a page too: a view or block in one is listed on it
// (2026-11-06: the report counted 62 views and named 13, those in rows left
// out), by its title in Leafen, as its staged row keeps none. An issue goes up to its page
// however deep; each step is taken once (UNION), so a chain that comes back
// on itself ends (parent_chain.go).
func loadNotionImportBlockIssues(app core.App, jobID, cursor string, limit int) ([]storedNotionImportBlockIssue, error) {
	stored := []storedNotionImportBlockIssue{}
	err := app.DB().NewQuery(`
			WITH RECURSIVE [[issue]] AS (
				SELECT [[sequence]], [[parent_source_id]] AS [[ancestor_id]], [[parent_kind]] AS [[ancestor_kind]],
					[[title]] AS [[block_type]], [[text]] AS [[reason]]
				FROM {{leafen_notion_import_sources}}
				WHERE [[job_id]]={:jobId} AND [[kind]]='issue'
				UNION
				SELECT [[issue]].[[sequence]], [[parent]].[[parent_source_id]], [[parent]].[[parent_kind]],
					[[issue]].[[block_type]], [[issue]].[[reason]]
				FROM [[issue]]
				JOIN {{leafen_notion_import_sources}} AS [[parent]]
					ON [[parent]].[[job_id]]={:jobId} AND [[parent]].[[source_id]]=[[issue]].[[ancestor_id]]
				WHERE [[issue]].[[ancestor_kind]] IN ('block', {:propertyParent}, {:viewParent})
			), [[grouped]] AS (
				SELECT [[ancestor_id]] || '|' || [[block_type]] || '|' || [[reason]] AS [[group_key]],
					[[ancestor_id]] AS [[page_source_id]], [[block_type]], [[reason]], COUNT(*) AS [[issue_count]]
				FROM [[issue]] WHERE [[ancestor_kind]] IN ('page', 'row')
				GROUP BY [[ancestor_id]], [[block_type]], [[reason]]
			)
			SELECT [[grouped]].[[group_key]], COALESCE([[mapping]].[[native_id]], '') AS [[page_id]],
				COALESCE(NULLIF([[leafen_page]].[[title]], ''), [[page]].[[title]], '') AS [[page_title]], [[grouped]].[[block_type]],
				[[grouped]].[[reason]], [[grouped]].[[issue_count]]
			FROM [[grouped]]
			LEFT JOIN {{leafen_notion_import_mappings}} AS [[mapping]]
				ON [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[source_id]]=[[grouped]].[[page_source_id]]
				AND [[mapping]].[[kind]] IN ('page', 'row')
			LEFT JOIN {{leafen_notion_import_sources}} AS [[page]]
				ON [[page]].[[job_id]]={:jobId} AND [[page]].[[source_id]]=[[grouped]].[[page_source_id]]
			LEFT JOIN {{leafen_pages}} AS [[leafen_page]] ON [[leafen_page]].[[id]]=[[mapping]].[[native_id]]
			WHERE [[grouped]].[[group_key]] > {:cursor}
			ORDER BY [[grouped]].[[group_key]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
		"jobId": jobID, "cursor": cursor, "limit": limit,
		"propertyParent": notionImportPropertyIssueParentKind, "viewParent": notionImportViewIssueParentKind,
	}).All(&stored)
	return stored, err
}

func notionImportBlockIssueLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return notionImportBlockIssueItems, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > notionImportBlockIssueItems {
		return 0, errors.New(errorCodeInvalidInput)
	}
	return limit, nil
}
