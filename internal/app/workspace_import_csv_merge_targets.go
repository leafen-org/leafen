package app

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	workspaceCSVMergeTargetsLimit     = 25
	workspaceCSVMergeTargetsScanLimit = 100
	workspaceCSVMergeTargetsMaxQuery  = 128
)

func registerWorkspaceCSVMergeRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.GET(apiRoot+workspaceCSVMergeTargetsListPath, listWorkspaceCSVMergeTargets)
	event.Router.GET(apiRoot+workspaceCSVMergeTargetReadPath, readWorkspaceCSVMergeTarget)
	event.Router.POST(
		apiRoot+workspaceCSVMergeCreatePath,
		jobs.createWorkspaceCSVMerge,
	).Bind(apis.BodyLimit(workspaceImportRequestBytes))
}

// listWorkspaceCSVMergeTargets are the databases a CSV's rows can join, and
// a file window's files: the workspace's databases the user may add rows to
// (database_added_rows.go — People never), most recently changed first, by
// title or on one page. Databases a CSV import made have no database block,
// so they are listed from their sources.
func listWorkspaceCSVMergeTargets(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	values := event.Request.URL.Query()
	query := strings.TrimSpace(values.Get("q"))
	pageID := strings.TrimSpace(values.Get("pageId"))
	afterUpdated, afterID, cursorOK := parseWorkspaceCSVMergeTargetsCursor(strings.TrimSpace(values.Get("cursor")))
	if workspaceID == "" || utf8.RuneCountInString(query) > workspaceCSVMergeTargetsMaxQuery ||
		len(pageID) > maxGraphIdentifierLength || !cursorOK {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceImportInvalid, "Check the database search.")
	}
	workspace, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "csv-merge-targets")
	}
	candidates := []struct {
		ID        string `db:"id"`
		PageID    string `db:"page_id"`
		Title     string `db:"title"`
		PageTitle string `db:"page_title"`
		Updated   int64  `db:"updated"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[page_id]], [[source]].[[title]], [[source]].[[updated]],
			[[page]].[[title]] AS [[page_title]]
		FROM {{leafen_data_sources}} AS [[source]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[source]].[[page_id]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[workspace_id]] = {:workspaceId}
			AND [[page]].[[in_trash]] = FALSE AND [[page]].[[notion_import_staging]] = FALSE
			AND ` + databaseSourceTakesAddedRowsSQL("[[source]].[[id]]") + `
			AND ({:pageId} = '' OR [[source]].[[page_id]] = {:pageId})
			AND ({:query} = '' OR instr(lower([[source]].[[title]]), lower({:query})) > 0)
			AND ({:afterId} = '' OR [[source]].[[updated]] < {:afterUpdated}
				OR ([[source]].[[updated]] = {:afterUpdated} AND [[source]].[[id]] < {:afterId}))
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_sources}} AS [[membership]]
				JOIN {{leafen_database_container_trash}} AS [[trash]]
					ON [[trash]].[[container_id]] = [[membership]].[[container_id]]
				WHERE [[membership]].[[data_source_id]] = [[source]].[[id]]
					AND [[membership]].[[source_container_id]] = ''
			)
		ORDER BY [[source]].[[updated]] DESC, [[source]].[[id]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": workspace.OwnerID, "workspaceId": workspaceID, "pageId": pageID, "query": query,
		"afterUpdated": afterUpdated, "afterId": afterID, "limit": workspaceCSVMergeTargetsScanLimit + 1,
	}).All(&candidates); err != nil {
		return workspaceImportAPIError(event, err, "csv-merge-targets")
	}
	list := workspaceCSVMergeTargetList{Items: []workspaceCSVMergeTargetSummary{}}
	for index, candidate := range candidates {
		if index == workspaceCSVMergeTargetsScanLimit || len(list.Items) == workspaceCSVMergeTargetsLimit {
			list.NextCursor = strconv.FormatInt(candidates[index-1].Updated, 10) + ":" + candidates[index-1].ID
			break
		}
		// A database under a page in the trash takes no rows (workspace_import_target_gone.go).
		if _, err := admitWorkspaceImportTarget(event.App, user.Id, workspaceID, candidate.PageID); err != nil {
			continue
		}
		list.Items = append(list.Items, workspaceCSVMergeTargetSummary{
			DataSourceID: candidate.ID, PageID: candidate.PageID, Title: candidate.Title, PageTitle: candidate.PageTitle,
		})
	}
	return event.JSON(http.StatusOK, list)
}

// parseWorkspaceCSVMergeTargetsCursor reads the last database a page of
// targets listed: when it changed and its ID.
func parseWorkspaceCSVMergeTargetsCursor(cursor string) (int64, string, bool) {
	if cursor == "" {
		return 0, "", true
	}
	updated, id, found := strings.Cut(cursor, ":")
	parsed, err := strconv.ParseInt(updated, 10, 64)
	if !found || err != nil || id == "" || len(id) > maxGraphIdentifierLength {
		return 0, "", false
	}
	return parsed, id, true
}
