package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// My tasks' sources (Notion's "작업 소스 추가", seen 2026-11-02): the task
// databases, at most ten, a person picks for the My tasks page of a
// workspace and Home's My tasks. Until they pick there is none: the page says
// where tasks come from and Home asks for sources, and the window to pick
// them opens with none picked (Notion's, seen 2026-11-24, two tabs).

func registerDatabaseMyTaskSourceRoutes(event *core.ServeEvent) {
	event.Router.Route(databaseMyTaskSourcesReadMethod, apiRoot+databaseMyTaskSourcesReadPath, myTaskSourcesRoute)
	event.Router.Route(databaseMyTaskSourcesSaveMethod, apiRoot+databaseMyTaskSourcesSavePath, myTaskSourcesRoute)
}

// myTaskWorkspace is the person asking and the workspace they read.
func myTaskWorkspace(event *core.RequestEvent) (*core.Record, workspaceAuthorization, bool, error) {
	user, ok, err := databaseToolbarConfigActor(event)
	if !ok {
		return nil, workspaceAuthorization{}, false, err
	}
	authorization, err := authorizeWorkspaceReader(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")))
	if isPermissionDenied(err) {
		return nil, workspaceAuthorization{}, false, apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "Workspace not found.")
	}
	if err != nil {
		return nil, workspaceAuthorization{}, false, writeDatabaseToolbarConfigError(event, err)
	}
	return user, authorization, true, nil
}

func myTaskSourcesRoute(event *core.RequestEvent) error {
	user, authorization, ok, err := myTaskWorkspace(event)
	if !ok {
		return err
	}
	candidates, err := readMyTaskCandidates(event.App, user.Id, authorization)
	if err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	if event.Request.Method == http.MethodPut {
		input := databaseMyTaskSourcesInput{}
		if err := event.BindBody(&input); err != nil || len(input.DataSourceIDs) > databaseMyTaskMaxSources {
			return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "Pick at most ten task databases.")
		}
		known := map[string]bool{}
		for _, candidate := range candidates {
			known[candidate.DataSourceID] = true
		}
		picked, seen := []string{}, map[string]bool{}
		for _, id := range input.DataSourceIDs {
			if !known[id] {
				return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "That isn't a task database you can read here.")
			}
			if !seen[id] {
				picked, seen[id] = append(picked, id), true
			}
		}
		encoded, _ := json.Marshal(picked)
		// No list at all (dataSourceIds null or left out) takes the pick back
		// to never having picked (configured false; 2026-11-16: an agent that
		// set sources and then emptied them couldn't undo the pick). It reads
		// as an empty list does, a pick of none: the page's "출처 없음", as
		// Notion's.
		save := `
			INSERT INTO {{leafen_my_task_sources}} ([[owner_id]], [[workspace_id]], [[sources]], [[updated_at]])
			VALUES ({:owner}, {:workspace}, {:sources}, {:now})
			ON CONFLICT ([[owner_id]], [[workspace_id]]) DO UPDATE SET [[sources]] = excluded.[[sources]], [[updated_at]] = excluded.[[updated_at]]`
		if input.DataSourceIDs == nil {
			save = `DELETE FROM {{leafen_my_task_sources}} WHERE [[owner_id]] = {:owner} AND [[workspace_id]] = {:workspace}`
		}
		if _, err := event.App.DB().NewQuery(save).Bind(dbx.Params{
			"owner": user.Id, "workspace": authorization.WorkspaceID, "sources": string(encoded), "now": time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
	}
	stored, err := readMyTaskSourceIDs(event.App, user.Id, authorization.WorkspaceID)
	if err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	response := databaseMyTaskSourcesResponse{Configured: stored != nil, Sources: []databaseMyTaskSourceWire{}, Candidates: candidates}
	byID := map[string]databaseMyTaskSourceWire{}
	for _, candidate := range candidates {
		byID[candidate.DataSourceID] = candidate
	}
	for _, id := range stored {
		if source, ok := byID[id]; ok {
			response.Sources = append(response.Sources, source)
		}
	}
	if err := withMyTaskSourceMaking(event.Request.Context(), event.App, user.Id, response.Sources); err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

// readMyTaskSourceIDs reads the sources a person picked in a workspace, or
// nil when they never picked.
func readMyTaskSourceIDs(app core.App, ownerID, workspaceID string) ([]string, error) {
	stored := ""
	err := app.DB().NewQuery(`
		SELECT [[sources]] FROM {{leafen_my_task_sources}} WHERE [[owner_id]] = {:owner} AND [[workspace_id]] = {:workspace}
	`).Bind(dbx.Params{"owner": ownerID, "workspace": workspaceID}).Row(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	if err := json.Unmarshal([]byte(stored), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// readMyTaskCandidates reads the task databases a person can read in the
// workspace, out of the trash (myTaskDatabaseListed), each with the path of
// pages it sits under, as Notion lists them to pick from.
func readMyTaskCandidates(app core.App, principalID string, workspace workspaceAuthorization) ([]databaseMyTaskSourceWire, error) {
	databases, err := readMyTaskDatabases(app, workspace)
	if err != nil {
		return nil, err
	}
	candidates := []databaseMyTaskSourceWire{}
	for _, database := range databases {
		roles := databaseTaskRoles{}
		if json.Unmarshal([]byte(database.TasksJSON), &roles) != nil || !roles.Enabled || roles.AssigneePropertyID == "" {
			continue
		}
		if listed, err := myTaskDatabaseListed(app, principalID, database.PageID); err != nil {
			return nil, err
		} else if !listed {
			continue
		}
		path, err := myTaskSourcePath(app, workspace.WorkspaceID, database.PageID)
		if err != nil {
			return nil, err
		}
		canEdit, err := myTaskDatabaseWritable(app, principalID, database.PageID)
		if err != nil {
			return nil, err
		}
		// What a task made in it takes, so the page can make one on its device
		// first (myTaskMadeHere.ts): its title, the person as its assignee, the
		// first To-do option of a status (a checkbox starts unchecked), and where
		// its due date is written.
		titleID, status, err := loadMyTaskProperties(app, database.DataSourceID, roles.StatusPropertyID)
		if err != nil {
			return nil, err
		}
		firstToDo := ""
		if !status.checkbox {
			firstToDo = status.first[0]
		}
		candidates = append(candidates, databaseMyTaskSourceWire{
			DataSourceID: database.DataSourceID, PageID: database.PageID, Title: database.Title, Icon: database.Icon, IconType: database.IconType,
			CanEdit: canEdit, Path: path, TitlePropertyID: titleID, AssigneePropertyID: roles.AssigneePropertyID,
			StatusPropertyID: roles.StatusPropertyID, FirstToDoOptionID: firstToDo, DueDatePropertyID: roles.DueDatePropertyID,
			StatusOptions: status.wire(), CreatedAt: database.Created,
		})
	}
	return candidates, nil
}

// wire is a status's options as My tasks gathers them into its own status:
// each group's in its database's order, the groups in Notion's (none for a
// checkbox). The page puts its sources' together, the oldest database's
// first and an option named alike once (MyTasksDatabaseView statusConfig),
// as Notion's My tasks does whatever order its sources were picked in
// (seen 2026-11-24, its board and grouping by option, two ways).
func (status myTaskStatus) wire() []databaseMyTaskStatusOptionWire {
	options := []databaseMyTaskStatusOptionWire{}
	for group, ids := range status.inGroup {
		for _, id := range ids {
			if option, ok := status.options[id]; ok {
				options = append(options, databaseMyTaskStatusOptionWire{Name: option[0], Color: option[1], Group: myTaskStatusGroups[group]})
			}
		}
	}
	return options
}

// myTaskSourcePath names the pages a database sits under, top first ("" for
// an untitled one, which the screen names): its own page too when it is a
// database inside one.
func myTaskSourcePath(app core.App, workspaceID, pageID string) ([]string, error) {
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if isPermissionDenied(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	pages := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
		Kind  string `db:"kind"`
	}{}
	encoded, _ := json.Marshal(ids)
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[title]], COALESCE([[kind]], '') AS [[kind]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&pages); err != nil {
		return nil, err
	}
	byID := map[string]int{}
	for index, page := range pages {
		byID[page.ID] = index
	}
	names := []string{}
	for index := len(ids) - 1; index >= 0; index-- {
		at, ok := byID[ids[index]]
		if !ok || (ids[index] == pageID && pages[at].Kind == "database") {
			continue
		}
		names = append(names, strings.TrimSpace(pages[at].Title))
	}
	return names, nil
}
