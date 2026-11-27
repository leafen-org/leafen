package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source keeps the template it was made from, as Notion's keeps its
// workflow template: the new database panel moves the templates a page
// already holds to the end of its suggestions (seen 2026-11-11). Renaming
// the database or changing its properties keeps it; it is written once, as
// the template's database is made.

func recordDatabaseStarterOrigin(app core.App, dataSourceID, templateKey string) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_data_sources}} SET [[starter_template]]={:template} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"template": templateKey, "id": dataSourceID}).Execute()
	return err
}

// readDatabaseStarterUsed answers which templates the page holding a new
// database already shows, as Notion's suggestions read them (seen
// 2026-11-11): the databases among its own blocks, inline or linked, and its
// full-page database sub-pages; not those of pages further down or beside
// it. A full-page database's own panel reads the page holding it.
func readDatabaseStarterUsed(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if pageID == "" || len(pageID) > maxGraphIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseStarterInvalid, "Check the page.")
	}
	auth, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if err != nil || auth.Page.GetBool("in_trash") {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	response := databaseStarterUsedResponse{Templates: []string{}}
	holder := pageID
	if isFullPageDatabase(auth.Page) {
		holder = auth.Page.GetString("parent_id")
		if holder == "" {
			return event.JSON(http.StatusOK, response)
		}
		if parent, err := authorizePage(event.App, user.Id, holder, permissionWorkspaceRead); err != nil || parent.Page.GetBool("in_trash") {
			return event.JSON(http.StatusOK, response)
		}
	}
	// The databases are found by the page they are on; the workspace is read
	// after (+), or SQLite, which keeps no statistics, reads the workspace's
	// every container.
	err = event.App.DB().NewQuery(`SELECT DISTINCT [[source]].[[starter_template]]
	 FROM {{leafen_database_containers}} [[container]]
	 JOIN {{leafen_database_container_sources}} [[member]] ON [[member]].[[container_id]]=[[container]].[[id]]
	 JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[member]].[[data_source_id]]
	 WHERE [[container]].[[parent_block_id]]='' AND [[source]].[[starter_template]]!=''
	 AND +[[container]].[[workspace_id]]={:workspace}
	 AND ([[container]].[[page_id]]={:holder} OR [[container]].[[page_id]] IN (
	   SELECT [[id]] FROM {{leafen_pages}} WHERE [[parent_id]]={:holder} AND [[kind]]={:database} AND [[in_trash]]=FALSE))
	 AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} WHERE [[container_id]]=[[container]].[[id]])
	 ORDER BY 1`).
		Bind(dbx.Params{"holder": holder, "workspace": auth.Workspace.WorkspaceID, "database": pageKindDatabase}).Column(&response.Templates)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't read the page's databases.", err)
	}
	return event.JSON(http.StatusOK, response)
}
