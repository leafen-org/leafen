package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The My tasks page makes a task in one of its sources, assigned to the
// person with its status in the first To-do option, and checks one done or
// not (Notion's "새 작업" and a task's checkbox, seen 2026-11-02). Each goes
// through the database page's graph as the person, who must be able to
// write it; the request id makes asking again the same change.

func registerDatabaseMyTaskActionRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.Route(databaseMyTaskCreateMethod, apiRoot+databaseMyTaskCreatePath, func(event *core.RequestEvent) error {
		return createMyTask(event, ygo)
	})
	event.Router.Route(databaseMyTaskDoneMethod, apiRoot+databaseMyTaskDonePath, func(event *core.RequestEvent) error {
		return markMyTaskDone(event, ygo)
	})
}

// myTaskSource is one of the person's sources, with its task roles: one they
// picked, or, until they pick (readMyTaskSourceIDs nil), any task database for
// their own tasks, as an app or agent reads them without ?scope=sources — the
// page shows none until then. whose is whose tasks it gives: everyone's once
// the person picked their sources (the page reads them with ?scope=sources
// and filters by its views), their own until then.
type myTaskSource struct {
	database storedMyTaskDatabase
	roles    databaseTaskRoles
	whose    myTasksWhose
}

func loadMyTaskSource(app core.App, principalID string, workspace workspaceAuthorization, dataSourceID string) (myTaskSource, bool, error) {
	picked, err := readMyTaskSourceIDs(app, principalID, workspace.WorkspaceID)
	if err != nil {
		return myTaskSource{}, false, err
	}
	databases, err := readMyTaskDatabases(app, workspace)
	if err != nil {
		return myTaskSource{}, false, err
	}
	if picked != nil && !slices.Contains(picked, dataSourceID) {
		return myTaskSource{}, false, nil
	}
	whose := myTasksOwn
	if picked != nil {
		whose = myTasksEveryone
	}
	for _, database := range databases {
		roles := databaseTaskRoles{}
		if database.DataSourceID == dataSourceID && json.Unmarshal([]byte(database.TasksJSON), &roles) == nil &&
			roles.Enabled && roles.AssigneePropertyID != "" {
			return myTaskSource{database: database, roles: roles, whose: whose}, true, nil
		}
	}
	return myTaskSource{}, false, nil
}

func myTaskRequestHash(parts ...string) string {
	digest := sha256.Sum256([]byte("leafen:my-task\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func createMyTask(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, workspace, ok, err := myTaskWorkspace(event)
	if !ok {
		return err
	}
	input := databaseMyTaskCreateInput{}
	if err := event.BindBody(&input); err != nil || input.RequestID == "" || len(input.RequestID) > maxDurableJobRequestIDRunes ||
		utf8.RuneCountInString(input.Title) > maxGraphTitleRunes || len(input.TimeZone) > 64 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "Check the task.")
	}
	// The task database a task goes in is named, as the screen's 새 작업
	// names the source it is in: one left out is the request's to fix, not a
	// source missing (2026-11-26, an agent with one source set was told
	// its source wasn't one).
	if strings.TrimSpace(input.DataSourceID) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid,
			"Check the task: dataSourceId names the task database it goes in, one of My tasks' sources (GET "+databaseMyTaskSourcesReadPath+").")
	}
	source, found, err := loadMyTaskSource(event.App, user.Id, workspace, input.DataSourceID)
	if err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	if !found {
		return apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "That task database isn't one of My tasks' sources.")
	}
	hash := myTaskRequestHash(user.Id, input.RequestID, input.DataSourceID, input.Title)
	rowID := databaseMyTaskRowID(user.Id, input.RequestID)
	assignee, _ := json.Marshal([]string{user.Id})
	made, err := applyMyTaskChange(event, ygo, user.Id, source, "my-task-create:"+hash[:32], hash,
		func(txApp core.App, titleID string, status myTaskStatus) ([]graphOperation, error) {
			position, err := databaseRowPositionAtEnd(txApp, workspace.OwnerID, source.database.PageID, source.database.DataSourceID)
			if err != nil {
				return nil, err
			}
			title, _ := json.Marshal(input.Title)
			values := map[string]graphRowValue{
				titleID:                         {Type: graphValueTypeTitle, Value: title},
				source.roles.AssigneePropertyID: {Type: graphValueTypePerson, Value: assignee},
			}
			if value, ok := myTaskStatusValue(status, false); ok && source.roles.StatusPropertyID != "" {
				values[source.roles.StatusPropertyID] = value
			}
			return []graphOperation{{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
				ID: rowID, DataSourceID: source.database.DataSourceID, Position: position, Values: values,
			}}}, nil
		})
	if err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	// A task starts from its database's default template, as a row made in
	// the database's views does (useDefaultTemplateRows.ts): the template
	// fills what the task leaves empty — its title when none was typed, its
	// icon, its body — and the task keeps its assignee and status. Asked
	// again, the task made the first time keeps the template it took then.
	if made {
		templateID, err := databaseDefaultTemplateID(event.App, source.database.DataSourceID)
		if err == nil && templateID != "" {
			err = ygo.fillDatabaseRowFromTemplate(event.Request.Context(), event.App, user.Id, rowID, templateID, input.TimeZone, false)
		}
		if err != nil {
			// The task is made all the same; it starts empty.
			event.App.Logger().Warn("leafen my task default template not applied", "error", err)
		}
	}
	return answerMyTask(event, user.Id, workspace, source, rowID)
}

func markMyTaskDone(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, workspace, ok, err := myTaskWorkspace(event)
	if !ok {
		return err
	}
	input := databaseMyTaskDoneInput{}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	if err := event.BindBody(&input); err != nil || input.RequestID == "" || len(input.RequestID) > maxDurableJobRequestIDRunes || rowID == "" ||
		utf8.RuneCountInString(input.StatusName) > maxGraphTitleRunes {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseToolbarConfigInvalid, "Check the task.")
	}
	dataSourceID := ""
	if err := event.App.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&dataSourceID); err != nil {
		return apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "Task not found.")
	}
	source, found, err := loadMyTaskSource(event.App, user.Id, workspace, dataSourceID)
	if err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	if !found || source.roles.StatusPropertyID == "" {
		return apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "Task not found.")
	}
	// Only a task the page shows is checked, so "not found" never follows a change made.
	if _, shown, err := findMyTask(event.Request.Context(), event.App, user.Id, workspace, source, rowID); err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	} else if !shown {
		return apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "Task not found.")
	}
	done := "0"
	if input.Done {
		done = "1"
	}
	parts := []string{user.Id, input.RequestID, rowID, done, input.Group}
	if input.StatusName != "" {
		parts = append(parts, input.StatusName)
	}
	hash := myTaskRequestHash(parts...)
	if _, err := applyMyTaskChange(event, ygo, user.Id, source, "my-task-done:"+hash[:32], hash,
		func(txApp core.App, _ string, status myTaskStatus) ([]graphOperation, error) {
			value, ok := myTaskStatusValue(status, input.Done)
			// A group chosen in the status column, a card moved to it or a step
			// of ⌘Z sets the option named in it, the one the task had or was
			// given (2026-11-24: ⌘Z put a task on its group's first option, not
			// the second it had) — its first when none is named or its database
			// has none so named; a checkbox only has done or not.
			for index, group := range myTaskStatusGroups {
				if input.Group != group || status.checkbox {
					continue
				}
				if option := status.optionIn(index, input.StatusName); option != "" {
					encoded, _ := json.Marshal(option)
					value, ok = graphRowValue{Type: graphValueTypeStatus, Value: encoded}, true
				}
			}
			if !ok {
				return nil, errDatabaseToolbarConfigInvalid
			}
			position := ""
			if err := txApp.DB().NewQuery(`SELECT [[position]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": rowID}).Row(&position); err != nil {
				return nil, err
			}
			return []graphOperation{{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
				ID: rowID, DataSourceID: dataSourceID, Position: position, SubitemParentID: storedRowParent(txApp, rowID),
				Values: map[string]graphRowValue{source.roles.StatusPropertyID: value},
			}}}, nil
		}); err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	return answerMyTask(event, user.Id, workspace, source, rowID)
}

// databaseMyTaskRowID is the row a request to make a task makes, the same
// each time it is asked.
func databaseMyTaskRowID(principalID, requestID string) string {
	return derivedPageID("my-task", principalID, requestID)
}

// myTaskStatusValue is the status a task done, or not, takes: its
// checkbox, or the first option of the Complete or To-do group.
func myTaskStatusValue(status myTaskStatus, done bool) (graphRowValue, bool) {
	if status.checkbox {
		value, _ := json.Marshal(done)
		return graphRowValue{Type: graphValueTypeCheckbox, Value: value}, true
	}
	option := status.first[0]
	if done {
		option = status.first[2]
	}
	if option == "" {
		return graphRowValue{}, false
	}
	value, _ := json.Marshal(option)
	return graphRowValue{Type: graphValueTypeStatus, Value: value}, true
}

// applyMyTaskChange makes a task's change, and says whether it made it now
// rather than answering a request it made before.
func applyMyTaskChange(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, principalID string, source myTaskSource, mutationID, hash string,
	operations func(txApp core.App, titleID string, status myTaskStatus) ([]graphOperation, error),
) (bool, error) {
	ctx, cancel := context.WithTimeout(event.Request.Context(), time.Minute)
	defer cancel()
	receipt, err := ygo.applyContentGraphMutationContext(ctx, principalID, source.database.PageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			PersonChange:       true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) { return hash, nil },
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				titleID, status, err := loadMyTaskProperties(txApp, source.database.DataSourceID, source.roles.StatusPropertyID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				planned, err := operations(txApp, titleID, status)
				return contentGraphMutationPreparation{Operations: planned}, err
			},
		})
	return err == nil && !receipt.Duplicate, err
}

// databaseDefaultTemplateID is a data source's live default template, or "".
func databaseDefaultTemplateID(app core.App, dataSourceID string) (string, error) {
	id := ""
	err := app.DB().NewQuery(`
		SELECT [[template]].[[id]] FROM {{leafen_database_templates}} AS [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} AS [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[data_source_id]] = {:id} AND [[template]].[[is_default]] = 1
			AND COALESCE([[state]].[[deleted_at]], '') = ''
		ORDER BY [[template]].[[position]], [[template]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// answerMyTask answers with the task as My tasks shows it: one of the
// tasks the page shows from its source (source.whose — everyone's once the
// person picked their sources; 2026-11-24: a task of someone else's checked
// there was written and answered as not found), wherever it is among them.
func answerMyTask(event *core.RequestEvent, principalID string, workspace workspaceAuthorization, source myTaskSource, rowID string) error {
	task, found, err := findMyTask(event.Request.Context(), event.App, principalID, workspace, source, rowID)
	if err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	if !found {
		// Made, but no longer assigned to the person or hidden from them.
		return apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "Task not found.")
	}
	if task.CanEdit, err = myTaskDatabaseWritable(event.App, principalID, source.database.PageID); err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	return event.JSON(http.StatusOK, databaseMyTaskResponse{Task: task})
}

// findMyTask reads a row as one of the tasks My tasks shows from its source,
// read from the row itself on, so a task far down a large database costs one
// row; false when it is not one of them.
func findMyTask(ctx context.Context, app core.App, principalID string, workspace workspaceAuthorization, source myTaskSource, rowID string,
) (databaseMyTaskWire, bool, error) {
	position := ""
	err := app.DB().NewQuery(`SELECT [[position]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id} AND [[data_source_id]] = {:source}`).
		Bind(dbx.Params{"id": rowID, "source": source.database.DataSourceID}).Row(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseMyTaskWire{}, false, nil
	}
	if err != nil {
		return databaseMyTaskWire{}, false, err
	}
	from := myTasksCursor{Page: source.database.PageID, Source: source.database.DataSourceID, Position: position, ID: rowID, itself: true}
	tasks, _, _, err := loadMyTasksInDatabase(ctx, app, principalID, workspace.OwnerID, source.database, source.roles, source.whose, from, 1)
	if err != nil || len(tasks) == 0 || tasks[0].RowID != rowID {
		return databaseMyTaskWire{}, false, err
	}
	return tasks[0], true, nil
}
