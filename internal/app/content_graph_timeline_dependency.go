package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Timeline views follow their source's dependency setting. This read-only
// projection is shared by normal, linked and public graph reads. Its bounded
// source set comes from the already authorized graph window, including windows
// with no rows; it adds neither another settings owner nor one query per view.
func projectTimelineDependencies(app core.App, owner string, items []graphReadItem) error {
	sourceIDs := map[string]struct{}{}
	for _, item := range items {
		if item.Kind != graphReadKindView {
			continue
		}
		delete(item.Value, "timelineDependency")
		if item.Value["type"] == graphViewTypeTimeline {
			if id, ok := item.Value["dataSourceId"].(string); ok {
				sourceIDs[id] = struct{}{}
			}
		}
	}
	if len(sourceIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(sourceIDs))
	for id := range sourceIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	params := dbx.Params{"owner": owner}
	placeholders := make([]string, len(ids))
	for index, id := range ids {
		key := fmt.Sprintf("timelineSource%d", index)
		params[key] = id
		placeholders[index] = "{:" + key + "}"
	}
	rows := []struct {
		DataSourceID          string `db:"data_source_id"`
		Enabled               bool   `db:"enabled"`
		PredecessorPropertyID string `db:"predecessor_property_id"`
		Revision              int    `db:"revision"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]] AS [[data_source_id]],
			CASE WHEN json_type([[features]].[[dependencies_json]], '$.enabled') IS NOT NULL
				THEN COALESCE(json_extract([[features]].[[dependencies_json]], '$.enabled'), 0) = 1
				ELSE [[binding]].[[revision]] IS NOT NULL END AS [[enabled]],
			COALESCE([[binding]].[[predecessor_property_id]], '') AS [[predecessor_property_id]],
			COALESCE([[binding]].[[revision]], 0) AS [[revision]]
		FROM {{leafen_data_sources}} AS [[source]]
		LEFT JOIN {{leafen_database_dependency_bindings}} AS [[binding]]
			ON [[binding]].[[data_source_id]] = [[source]].[[id]]
			AND [[binding]].[[owner]] = [[source]].[[owner]] AND [[binding]].[[page_id]] = [[source]].[[page_id]]
		LEFT JOIN {{leafen_database_task_features}} AS [[features]]
			ON [[features]].[[data_source_id]] = [[source]].[[id]]
			AND [[features]].[[owner]] = [[source]].[[owner]] AND [[features]].[[page_id]] = [[source]].[[page_id]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[id]] IN (` + strings.Join(placeholders, ",") + `)
			AND ([[binding]].[[revision]] IS NOT NULL OR json_type([[features]].[[dependencies_json]], '$.enabled') IS NOT NULL)
	`).Bind(params).All(&rows)
	if err != nil {
		return err
	}
	bySource := map[string]graphTimelineDependency{}
	for _, row := range rows {
		bySource[row.DataSourceID] = graphTimelineDependency{
			Enabled: row.Enabled, PredecessorPropertyID: row.PredecessorPropertyID, Revision: row.Revision,
		}
	}
	for _, item := range items {
		if item.Kind == graphReadKindView && item.Value["type"] == graphViewTypeTimeline {
			id, _ := item.Value["dataSourceId"].(string)
			if dependency, exists := bySource[id]; exists {
				item.Value["timelineDependency"] = dependency
			}
		}
	}
	return nil
}
