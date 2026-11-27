package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The Sprints database's properties and its table, as Notion's (seen
// 2026-11-04): a sprint's name, dates, number and tasks, how many tasks it
// has and how many of them are done, and its status. The two rollups are the
// sprint's own: their relation, property and calculation stay as they are
// made (a person sets only how their numbers show), and the table shows the
// status, dates and those two by status, the number and the tasks hidden.

// databaseSprintPropertyIDs are the Sprints database's properties.
type databaseSprintPropertyIDs struct {
	Name, Dates, Number, Tasks, Total, Completed, Status string
}

// databaseSprintDatabaseIcon is the database's icon, Notion's light gray database.
const databaseSprintDatabaseIcon = "database:lightgray"

// databaseSprintIcon is Notion's light gray runner, on the Sprint board, each
// sprint and the tasks' Sprint relation.
const databaseSprintIcon = "run:lightgray"

// The properties' places, in Notion's order.
const (
	databaseSprintNamePosition      = "p0"
	databaseSprintDatesPosition     = "p1"
	databaseSprintNumberPosition    = "p2"
	databaseSprintTasksPosition     = "p3"
	databaseSprintTotalPosition     = "p4"
	databaseSprintCompletedPosition = "p5"
	databaseSprintStatusPosition    = "p6"
)

// databaseSprintRollupConfigs are the settings of the rollups counting a
// sprint's tasks and the share of them done. Done is a status in the last
// group, the same done the task views read, or a checked box.
func databaseSprintRollupConfigs(
	app core.App, taskDataSourceID, statusPropertyID, tasksPropertyID string,
) (total, completed map[string]any, err error) {
	var status struct {
		Type   string `db:"type"`
		Config string `db:"config"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"id": statusPropertyID, "dataSourceId": taskDataSourceID}).One(&status); err != nil {
		return nil, nil, err
	}
	rollup := func(function string) map[string]any {
		return map[string]any{
			"rollupRelationPropertyId": tasksPropertyID, "rollupTargetPropertyId": statusPropertyID,
			"rollupFunction": function, "rollupLocked": true,
		}
	}
	if status.Type == graphValueTypeCheckbox {
		total, completed = rollup("count"), rollup("percent_checked")
	} else {
		total, completed = rollup("count_values"), rollup("percent_per_group")
		groups := databaseQueryParsedPropertyConfig(storedDatabaseQueryProperty{Type: status.Type, Config: status.Config}).StatusGroups
		if len(groups) > 0 {
			completed["rollupStatusGroup"] = groups[len(groups)-1].ID
		}
	}
	completed["numberDisplay"], completed["numberDisplayColor"] = "ring", "blue"
	total["hideInPagePanel"] = true
	// Each has an icon of its own, not the rollup's: a list and a checklist, as
	// Notion's Sprints database names them (seen 2026-11-06; a rollup a person
	// makes keeps the rollup's icon whatever it works out).
	total["icon"], completed["icon"] = "list:lightgray", "checklist:lightgray"
	return total, completed, nil
}

// databaseSprintAllViewConfig is the table of all sprints.
func databaseSprintAllViewConfig(ids databaseSprintPropertyIDs) map[string]any {
	return map[string]any{
		"propertyOrder":    []string{ids.Name, ids.Status, ids.Dates, ids.Total, ids.Completed, ids.Number, ids.Tasks},
		"hiddenProperties": []string{ids.Number, ids.Tasks},
		"sorts":            []any{map[string]any{"propertyId": ids.Status, "direction": "asc"}},
		"filterBarHidden":  true,
	}
}

// databaseSprintTimelineHidden are what the timeline's bars leave out: the
// number and the counts belong to the table.
func databaseSprintTimelineHidden(ids databaseSprintPropertyIDs) []string {
	return []string{ids.Number, ids.Total, ids.Completed}
}

// Stored rollups marked locked keep their relation, property and calculation:
// a change from any surface may set how their numbers show, not what they
// work out.
const databaseLockedRollupMessage = "This rollup's type, relation, property and calculation stay as they are."

var errDatabaseRollupLocked = graphPreconditionError(errorCodeGraphReferenceInvalid, databaseLockedRollupMessage)

type databaseLockedRollupFields struct {
	Locked   bool   `json:"rollupLocked"`
	Relation string `json:"rollupRelationPropertyId"`
	Target   string `json:"rollupTargetPropertyId"`
	Function string `json:"rollupFunction"`
	Group    string `json:"rollupStatusGroup"`
}

// guardLockedDatabaseRollups refuses changing what a locked rollup works out,
// or its type.
func guardLockedDatabaseRollups(app core.App, pageID string, operations []graphOperation) error {
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil {
			continue
		}
		raw := ""
		err := app.DB().NewQuery(`
			SELECT [[config]] FROM {{leafen_database_properties}}
			WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[type]] = {:type}
				AND json_valid([[config]]) AND json_extract([[config]], '$.rollupLocked') = 1
		`).Bind(dbx.Params{"id": operation.Property.ID, "pageId": pageID, "type": graphValueTypeRollup}).Row(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		stored, changed := databaseLockedRollupFields{}, databaseLockedRollupFields{}
		if operation.Property.Type != graphValueTypeRollup || json.Unmarshal([]byte(raw), &stored) != nil ||
			json.Unmarshal(operation.Property.Config, &changed) != nil || stored != changed {
			return errDatabaseRollupLocked
		}
	}
	return nil
}

// A row made in a Sprints database by any means takes the runner. Only the
// app's "+ 새 스프린트" also names it and makes it a future sprint, as
// Notion's (seen 2026-11-04): one made through the API or MCP has no status
// rather than the status's default, which would be a second next sprint.

// prepareDatabaseSprintRows lists the rows a change adds to the Sprints
// database on the page, leaving empty a status they are not given.
func prepareDatabaseSprintRows(app core.App, pageID string, operations []graphOperation) ([]string, error) {
	var sprints struct {
		Source string `db:"sprints_data_source_id"`
		Status string `db:"sprint_status_property_id"`
	}
	looked, made := false, []string(nil)
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		if !looked {
			looked = true
			err := app.DB().NewQuery(`
				SELECT [[sprints_data_source_id]], [[sprint_status_property_id]] FROM {{leafen_database_sprints}}
				WHERE [[sprints_page_id]] = {:pageId} LIMIT 1
			`).Bind(dbx.Params{"pageId": pageID}).One(&sprints)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
		}
		row := operation.Row
		if row.DataSourceID != sprints.Source {
			continue
		}
		if stored, err := databaseRowRecordSource(app, pageID, row.ID); err != nil {
			return nil, err
		} else if stored == sprints.Source {
			continue
		}
		made = append(made, row.ID)
		if _, given := row.Values[sprints.Status]; !given {
			if row.Values == nil {
				row.Values = map[string]graphRowValue{}
			}
			row.Values[sprints.Status] = graphRowValue{Type: graphValueTypeStatus, Value: json.RawMessage("null")}
		}
	}
	return made, nil
}

// giveDatabaseSprintRowsIcon gives the rows the runner, unless they came with
// an icon of their own.
func giveDatabaseSprintRowsIcon(app core.App, rowIDs []string) error {
	for _, rowID := range rowIDs {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_pages}} SET [[icon]] = {:icon}, [[icon_type]] = {:iconType}
			WHERE [[id]] = {:id} AND [[data_source_id]] != '' AND [[icon]] = ''
		`).Bind(dbx.Params{"icon": databaseSprintIcon, "iconType": nativeIconType, "id": rowID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// markSprintDatabases tells a read which of its data sources are Sprints
// databases, and what the app's new rows there start as, and which of its
// properties the sprints manage ("sprintManaged"), whose type and two-way
// relation every property menu keeps as the server does
// (databaseSprintManagedProperty).
func markSprintDatabases(app core.App, items []graphReadItem) error {
	sources := []string{}
	for _, item := range items {
		if item.Kind == graphReadKindProperty {
			if source, _ := item.Value["dataSourceId"].(string); source != "" {
				sources = append(sources, source)
			}
		}
	}
	managed, err := databaseSprintManagedProperties(app, sources)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Kind == graphReadKindProperty {
			if id, _ := item.Value["id"].(string); managed[id] != "" {
				item.Value["sprintManaged"] = managed[id]
			}
			continue
		}
		if item.Kind != graphReadKindDataSource {
			continue
		}
		source, _ := item.Value["id"].(string)
		status := ""
		err := app.DB().NewQuery(`
			SELECT [[sprint_status_property_id]] FROM {{leafen_database_sprints}} WHERE [[sprints_data_source_id]] = {:id}
		`).Bind(dbx.Params{"id": source}).Row(&status)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		item.Value["sprintStatusPropertyId"] = status
		item.Value["sprintFutureOptionId"] = databaseSprintStatusPrefix + databaseSprintStatusFuture
	}
	return nil
}

// A Sprints database's pages lay out as Notion's (seen 2026-11-05) until
// someone lays them out otherwise: its status, dates and completed tasks under
// the title, its number and task count after them, and its tasks in a
// relations group ("관계형") of their own. It is the layout such a database has with
// none stored, so one turned on before this, or one set back, reads it too.
func databaseSprintPageLayout(properties databaseSprintPropertyIDs) databasePageLayout {
	property := func(id string) databasePageLayoutGroupItem {
		return databasePageLayoutGroupItem{Kind: "property", PropertyID: id}
	}
	group := []databasePageLayoutGroupItem{}
	for _, id := range []string{properties.Status, properties.Dates, properties.Completed, properties.Number, properties.Total} {
		if id != "" {
			group = append(group, property(id))
		}
	}
	page := []databasePageLayoutModule{{ID: "properties", Kind: "properties"}}
	if properties.Tasks != "" {
		// Its tasks in a relations group of their own after the properties, one line, as Notion's (seen 2026-11-06).
		page = append(page, databasePageLayoutModule{ID: "relations", Kind: "relations",
			Relations: []databasePageLayoutRelation{{PropertyID: properties.Tasks, Display: "minimized"}}})
	}
	page = append(page, databasePageLayoutModule{ID: "comments", Kind: "comments"}, databasePageLayoutModule{ID: "content", Kind: "content"})
	return databasePageLayout{
		Heading:       databasePageLayoutHeading{Pinned: []string{}, Labels: true},
		Page:          page,
		Panel:         []databasePageLayoutModule{},
		PropertyGroup: group,
		Structure:     "simple",
		Tabs:          []databasePageLayoutTab{},
		Options: databasePageLayoutOptions{
			InlineComments: "default", PageComments: true, PropertyIcons: true, Templates: true,
		},
	}
}

// databaseSprintDefaultPageLayout is a Sprints database's layout when none is
// stored; nil for any other data source.
func databaseSprintDefaultPageLayout(app core.App, dataSourceID string) (*databasePageLayout, error) {
	stored := storedDatabaseSprints{}
	err := app.DB().NewQuery(`SELECT [[sprint_tasks_property_id]], [[sprint_status_property_id]], [[sprint_dates_property_id]]
		FROM {{leafen_database_sprints}} WHERE [[sprints_data_source_id]] = {:source} LIMIT 1`).
		Bind(dbx.Params{"source": dataSourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Name   string `db:"name"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]], [[name]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} ORDER BY [[position]], [[id]]`).Bind(dbx.Params{"source": dataSourceID}).All(&rows); err != nil {
		return nil, err
	}
	properties := databaseSprintPropertyIDs{Status: stored.StatusPropertyID, Dates: stored.DatesPropertyID}
	for _, row := range rows {
		switch {
		case row.ID == stored.SprintTasksPropertyID:
			properties.Tasks = row.ID
		case row.Type == graphValueTypeUniqueID && properties.Number == "":
			properties.Number = row.ID
		case row.Type == graphValueTypeRollup:
			config := map[string]any{}
			_ = json.Unmarshal([]byte(row.Config), &config)
			if config["numberDisplay"] == "ring" && properties.Completed == "" {
				properties.Completed = row.ID
			} else if properties.Total == "" {
				properties.Total = row.ID
			}
		}
	}
	layout := databaseSprintPageLayout(properties)
	return &layout, nil
}
