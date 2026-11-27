package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A linked starter reads only the selected, authorized source's view metadata.
// The cursor retains that source's membership order without reading its rows.
func readDatabaseStarterSourceViews(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	containerID := strings.TrimSpace(event.Request.PathValue("containerId"))
	sourceID := strings.TrimSpace(event.Request.URL.Query().Get("dataSourceId"))
	cursor := event.Request.URL.Query().Get("cursor")
	after := struct {
		ContainerID string
		SourceID    string
		Position    string
		ID          string
	}{}
	invalid := func() error {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseStarterInvalid, "Check the database view scope.")
	}
	if containerID == "" || sourceID == "" || len(containerID) > maxGraphIdentifierLength || len(sourceID) > maxGraphIdentifierLength || len(cursor) > databaseStarterSourceCursorBytes {
		return invalid()
	}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil || after.ContainerID != containerID || after.SourceID != sourceID || after.Position == "" || after.ID == "" {
			return invalid()
		}
	}
	container := struct {
		PageID      string `db:"page_id"`
		WorkspaceID string `db:"workspace_id"`
		Owner       string `db:"owner"`
	}{}
	err := event.App.DB().NewQuery(`SELECT [[container]].[[page_id]],[[container]].[[workspace_id]],[[container]].[[owner]]
	 FROM {{leafen_database_containers}} [[container]]
	 JOIN {{leafen_database_container_sources}} [[member]] ON [[member]].[[container_id]]=[[container]].[[id]]
	 WHERE [[container]].[[id]]={:container} AND [[container]].[[kind]]='source'
	 AND [[member]].[[data_source_id]]={:source} AND [[member]].[[source_container_id]]=''
	 AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} WHERE [[container_id]]=[[container]].[[id]])
	 LIMIT 1`).Bind(dbx.Params{"container": containerID, "source": sourceID}).One(&container)
	if err != nil {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseNotFound, "Database not found.")
	}
	auth, err := authorizePage(event.App, user.Id, container.PageID, permissionWorkspaceRead)
	if err != nil || auth.Page.GetBool("in_trash") || auth.Workspace.OwnerID != container.Owner || auth.Workspace.WorkspaceID != container.WorkspaceID {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseNotFound, "Database not found.")
	}
	stored := []struct {
		ID       string `db:"id"`
		Position string `db:"position"`
		Type     string `db:"type"`
		Name     string `db:"name"`
		Config   string `db:"config"`
	}{}
	err = event.App.DB().NewQuery(`SELECT [[view]].[[id]],[[member]].[[position]],[[view]].[[type]],[[view]].[[name]],[[view]].[[config]]
	 FROM {{leafen_database_container_views}} [[member]]
	 JOIN {{leafen_database_views}} [[view]] ON [[view]].[[id]]=[[member]].[[view_id]]
	 WHERE [[member]].[[container_id]]={:container} AND [[view]].[[data_source_id]]={:source}
	 AND ([[member]].[[position]]>{:position} OR ([[member]].[[position]]={:position} AND [[member]].[[view_id]]>{:id}))
	 ORDER BY [[member]].[[position]],[[member]].[[view_id]] LIMIT {:limit}`).Bind(dbx.Params{"container": containerID, "source": sourceID, "position": after.Position, "id": after.ID, "limit": databaseStarterSourceViewLimit + 1}).All(&stored)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't read the database views.", err)
	}
	response := databaseStarterSourceViewsResponse{Views: []graphDatabaseView{}}
	for index, view := range stored {
		if index == databaseStarterSourceViewLimit {
			break
		}
		response.Views = append(response.Views, graphDatabaseView{ID: view.ID, ContainerID: containerID, DataSourceID: sourceID, Position: view.Position, Type: view.Type, Name: view.Name, Config: json.RawMessage(view.Config)})
	}
	if len(stored) > databaseStarterSourceViewLimit {
		last := stored[databaseStarterSourceViewLimit-1]
		after.ContainerID, after.SourceID, after.Position, after.ID = containerID, sourceID, last.Position, last.ID
		raw, _ := json.Marshal(after)
		response.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return event.JSON(http.StatusOK, response)
}
