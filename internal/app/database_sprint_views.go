package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// applyDatabaseSprintViewConfigs points the sprint views at the sprints of
// the moment: each view keeps its own settings and takes the sprint filter
// (and grouping) the sprint owner gives it, on whichever page it sits and
// while that page is not in the trash.
func applyDatabaseSprintViewConfigs(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	stored storedDatabaseSprints,
	views map[string]string,
	configs map[string]json.RawMessage,
	mutationID func(pageID string) string,
) error {
	pages := map[string]map[string]string{}
	for key, viewID := range views {
		if viewID == "" {
			continue
		}
		pageID := ""
		err := app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_database_views}} WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId}
		`).Bind(dbx.Params{"id": viewID, "dataSourceId": stored.DataSourceID}).Row(&pageID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if pages[pageID] == nil {
			pages[pageID] = map[string]string{}
		}
		pages[pageID][key] = viewID
	}
	pageIDs := make([]string, 0, len(pages))
	for pageID := range pages {
		pageIDs = append(pageIDs, pageID)
	}
	sort.Strings(pageIDs)
	for _, pageID := range pageIDs {
		if page, err := app.FindRecordById(pagesCollection, pageID); err != nil || page.GetBool("in_trash") {
			continue
		}
		pageViews := pages[pageID]
		err := applyDatabaseSprintMutation(ctx, ygo, actorID, pageID, mutationID(pageID), false,
			func(records databasePageRecords) ([]graphOperation, error) {
				operations := []graphOperation{}
				keys := make([]string, 0, len(pageViews))
				for key := range pageViews {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					viewID := pageViews[key]
					entity, exists, err := loadDatabaseViewRecord(records.app, records.pageID, viewID)
					if err != nil {
						return nil, err
					}
					if !exists {
						continue
					}
					config := entity.Config
					if config == nil {
						config = map[string]any{}
					}
					fresh := map[string]any{}
					_ = json.Unmarshal(configs[key], &fresh)
					delete(config, "filters")
					delete(config, "filterGroup")
					delete(config, "quickFilters")
					for field, value := range fresh {
						config[field] = value
					}
					operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
						ID: viewID, DataSourceID: stored.DataSourceID, ContainerID: entity.ContainerID, Type: entity.Type,
						Name: entity.Name, Position: entity.Position, Config: databaseSprintJSON(config),
					}})
				}
				return operations, nil
			})
		if err != nil {
			return err
		}
	}
	return nil
}

// manageDatabaseSprintsInView makes a view of the task database the
// current-sprint view, as Notion's "Manage sprints in this view" does after
// the original one was deleted: the view shows the current sprint's tasks
// grouped by status and follows each new sprint from then on.
func manageDatabaseSprintsInView(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	stored storedDatabaseSprints,
	input databaseSprintActionInput,
) error {
	viewID := strings.TrimSpace(input.ViewID)
	pageID := ""
	err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_database_views}}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"id": viewID, "dataSourceId": stored.DataSourceID, "owner": stored.Owner}).Row(&pageID)
	if viewID == "" || errors.Is(err, sql.ErrNoRows) {
		return databaseToolbarRefused("viewId names no view of the sprints database")
	}
	if err != nil {
		return err
	}
	if _, err := authorizePageBody(app, actorID, pageID, permissionWorkspaceWrite); err != nil {
		return err
	}
	summaries, err := loadDatabaseSprintSummaries(app, stored)
	if err != nil {
		return err
	}
	byStatus := map[string]string{}
	for _, summary := range summaries {
		if _, seen := byStatus[summary.Status]; !seen {
			byStatus[summary.Status] = summary.ID
		}
	}
	if byStatus[databaseSprintStatusCurrent] == "" {
		return databaseToolbarStateRefused("there is no current sprint for the view to show")
	}
	roles, err := loadDatabaseTaskRoles(app, stored.DataSourceID)
	if err != nil {
		return err
	}
	// The view shows the current sprint's tasks by status, as the Sprint
	// board's current sprint does.
	configs := map[string]json.RawMessage{"current": databaseSprintJSON(map[string]any{
		"groupBy": roles.StatusPropertyID, "quickFilters": databaseSprintViewFilters(stored, roles, nil)["current"]["quickFilters"],
	})}
	err = applyDatabaseSprintViewConfigs(ctx, app, ygo, actorID, stored, map[string]string{"current": viewID}, configs,
		func(pageID string) string {
			return "sprint-view-" + databaseSprintDeterministicID("sprint-view", stored.DataSourceID, input.RequestID, pageID)
		})
	if err != nil {
		return err
	}
	views := stored.views()
	views.Current = viewID
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sprints}} SET [[views_json]] = {:views}, [[revision]] = [[revision]] + 1,
			[[last_request_id]] = {:requestId}, [[updated_at]] = {:now}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[revision]] = {:revision}
	`).Bind(dbx.Params{
		"views": string(databaseSprintJSON(views)), "requestId": input.RequestID, "now": time.Now().UnixMilli(),
		"dataSourceId": stored.DataSourceID, "revision": stored.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errDatabaseSprintConflict
	}
	return nil
}
