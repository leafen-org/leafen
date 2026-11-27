package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerMyTaskViewRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+databaseMyTaskViewsReadPath, readMyTaskViewsRoute)
	event.Router.PUT(apiRoot+databaseMyTaskViewsSavePath, saveMyTaskViewsRoute).Bind(apis.BodyLimit(databaseMyTaskViewsMaxBytes))
}

// "내 작업"'s views are its person's own, as Notion's page is theirs.
func readMyTaskViewsRoute(event *core.RequestEvent) error {
	user, workspace, ok, err := myTaskWorkspace(event)
	if !ok {
		return err
	}
	stored := "[]"
	err = event.App.DB().NewQuery(`SELECT [[views]] FROM {{leafen_my_task_views}} WHERE [[owner_id]] = {:owner} AND [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"owner": user.Id, "workspace": workspace.WorkspaceID}).Row(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the views.", err)
	}
	return event.JSON(http.StatusOK, map[string]any{"views": json.RawMessage(stored)})
}

func saveMyTaskViewsRoute(event *core.RequestEvent) error {
	user, workspace, ok, err := myTaskWorkspace(event)
	if !ok {
		return err
	}
	input := struct {
		Views []json.RawMessage `json:"views"`
	}{}
	if err := event.BindBody(&input); err != nil || len(input.Views) == 0 || len(input.Views) > databaseMyTaskMaxViews {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "Check the views.")
	}
	for _, view := range input.Views {
		if !validMyTaskView(view) {
			return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "Check the views.")
		}
	}
	encoded, _ := json.Marshal(input.Views)
	if _, err := event.App.DB().NewQuery(`INSERT INTO {{leafen_my_task_views}} ([[owner_id]], [[workspace_id]], [[views]], [[updated_at]])
		VALUES ({:owner}, {:workspace}, {:views}, {:now})
		ON CONFLICT ([[owner_id]], [[workspace_id]]) DO UPDATE SET [[views]] = excluded.[[views]], [[updated_at]] = excluded.[[updated_at]]`).
		Bind(dbx.Params{"owner": user.Id, "workspace": workspace.WorkspaceID, "views": string(encoded), "now": time.Now().UnixMilli()}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't save the views.", err)
	}
	return event.NoContent(http.StatusNoContent)
}

// validMyTaskView takes a view that says which view it is, its settings of a
// bounded size. Its order of tasks (rowPlaces, or rowOrder as kept before)
// is apart from them, bounded with every view's by the request: an order
// counted among the settings cut a view's order past about 900 tasks
// (2026-11-24).
func validMyTaskView(raw json.RawMessage) bool {
	view := struct {
		ID     string                     `json:"id"`
		Type   string                     `json:"type"`
		Config map[string]json.RawMessage `json:"config"`
	}{}
	if json.Unmarshal(raw, &view) != nil || view.ID == "" || view.Type == "" {
		return false
	}
	return len(raw)-len(view.Config["rowPlaces"])-len(view.Config["rowOrder"]) <= databaseMyTaskViewMaxBytes
}
