package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A sprint's page holds its tasks and notes, as Notion's (seen 2026-11-04):
// a linked board "By status" of the tasks of that sprint, grouped by status,
// with a table "By assignee" beside it (seen 2026-11-05), and a toggled
// "Planning notes" heading. A page anyone has written in stays
// as it is.

func databaseSprintPageViewConfig(
	app core.App, stored storedDatabaseSprints, roles databaseTaskRoles, rowID, which string,
) (map[string]any, error) {
	configs, err := databaseSprintBoardViewConfigs(app, stored, roles)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(configs[which], &fields); err != nil {
		return nil, err
	}
	delete(fields, "icon")
	fields["showDataSourceTitle"] = false
	fields["quickFilters"] = []any{
		map[string]any{"propertyId": stored.TaskSprintPropertyID, "operator": "contains", "value": []any{rowID}},
	}
	return fields, nil
}

// seedDatabaseSprintPageBodies writes the sprint pages' first body. They are
// the new rows of a Sprints database, named as the sprints themselves are,
// in the language of the person whose request makes them
// (generatedNameLanguage).
func seedDatabaseSprintPageBodies(ctx context.Context, app core.App, stored storedDatabaseSprints, actorID string, rowIDs []string) error {
	if len(rowIDs) == 0 {
		return nil
	}
	roles, err := loadDatabaseTaskRoles(app, stored.DataSourceID)
	if err != nil {
		return err
	}
	taskContainerID := ""
	err = app.DB().NewQuery(`
		SELECT [[container_id]] FROM {{leafen_database_container_sources}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[data_source_id]] = {:dataSourceId} AND [[source_container_id]] = '' LIMIT 1
	`).Bind(dbx.Params{
		"owner": stored.Owner, "workspaceId": stored.WorkspaceID, "dataSourceId": stored.DataSourceID,
	}).Row(&taskContainerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	names := generatedNamesFor(ctx, app, actorID).Sprints
	now := time.Now().UTC().UnixMilli()
	for _, rowID := range rowIDs {
		written := 0
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]] = {:id}`).
			Bind(dbx.Params{"id": rowID}).Row(&written); err != nil {
			return err
		}
		if written > 0 {
			continue
		}
		config, err := databaseSprintPageViewConfig(app, stored, roles, rowID, "current")
		if err != nil {
			return err
		}
		byAssignee, err := databaseSprintPageViewConfig(app, stored, roles, rowID, "byAssignee")
		if err != nil {
			return err
		}
		boardID := databaseSprintDeterministicID("sprint-page-board", rowID)
		viewID := derivedDatabaseEntityID("sprint-page-view", rowID)
		body := []newRowPageBlock{{
			ID: boardID, Position: "p0000000000", Type: graphBlockTypeInlineDatabase,
			Content: map[string]any{
				"dataSourceId": stored.DataSourceID, "viewId": viewID,
				"linkedDatabaseSource": true, "sourceContainerId": taskContainerID,
			},
		}, {
			// Closed, as Notion's "▶ 플래닝 노트" (seen 2026-11-04).
			ID:       databaseSprintDeterministicID("sprint-page-notes", rowID),
			Position: "p0000000001", Type: graphBlockTypeToggleHeading2,
			Content: map[string]any{"collapsed": true}, Spans: []graphTextSpan{{Text: names.PlanningNotes}},
		}}
		if err := writeNewRowPageBody(app, rowID, actorID, body, now); err != nil {
			return err
		}
		views := map[string][]graphDatabaseView{boardID: {{
			ID: viewID, DataSourceID: stored.DataSourceID, ContainerID: boardID, Type: graphViewTypeBoard,
			Name: names.ByStatus, Position: "v0", Config: databaseSprintJSON(config),
		}, {
			ID: derivedDatabaseEntityID("sprint-page-assignee-view", rowID), DataSourceID: stored.DataSourceID,
			ContainerID: boardID, Type: graphViewTypeTable, Name: names.ByAssignee, Position: "v1",
			Config: databaseSprintJSON(byAssignee),
		}}}
		if err := writeCopiedLinkedDatabaseViews(app, stored.Owner, stored.WorkspaceID, rowID, body, views); err != nil {
			return err
		}
	}
	return nil
}

// seedDatabaseSprintRowsMade writes the bodies of rows a change added to a
// Sprints database.
func seedDatabaseSprintRowsMade(ctx context.Context, app core.App, sprintsPageID, actorID string, rowIDs []string) error {
	if len(rowIDs) == 0 {
		return nil
	}
	sourceID := ""
	err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_sprints}} WHERE [[sprints_page_id]] = {:pageId} LIMIT 1`).
		Bind(dbx.Params{"pageId": sprintsPageID}).Row(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	stored, found, err := loadDatabaseSprints(app, sourceID)
	if err != nil || !found {
		return err
	}
	return seedDatabaseSprintPageBodies(ctx, app, stored, actorID, rowIDs)
}
