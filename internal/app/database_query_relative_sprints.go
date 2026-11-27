package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseQueryRelativeSprintKind is a sprint relation filter's relative
// value, {"kind": …, "sprint": "current" | "next" | "last"}: the Sprint
// board's views follow the sprints with it rather than naming them, its
// filters reading "현재", "다음" and "마지막" as Notion's (seen 2026-11-04).
const databaseQueryRelativeSprintKind = "leafen.sprint"

// databaseRelativeSprints reads each sprint database's current, next and
// last sprints once a query.
type databaseRelativeSprints struct {
	app  core.App
	rows map[string]map[string][]string
}

func newDatabaseRelativeSprints(app core.App) *databaseRelativeSprints {
	return &databaseRelativeSprints{app: app, rows: map[string]map[string][]string{}}
}

// ids are the sprints of the sprint database in that place; none for a
// database holding no sprints.
func (sprints *databaseRelativeSprints) ids(dataSourceID, which string) []string {
	if sprints == nil || dataSourceID == "" {
		return nil
	}
	if byPlace, ok := sprints.rows[dataSourceID]; ok {
		return byPlace[which]
	}
	byPlace := map[string][]string{}
	var rows []struct {
		ID     string `db:"id"`
		Status string `db:"status"`
	}
	// Its status names each sprint's place; only three places are relative.
	_ = sprints.app.DB().NewQuery(`
		SELECT [[values]].[[row_id]] AS [[id]], json_extract([[values]].[[value]], '$') AS [[status]]
		FROM {{leafen_database_sprints}} AS [[sprints]]
		JOIN {{leafen_database_row_values}} AS [[values]]
			ON [[values]].[[property_id]] = [[sprints]].[[sprint_status_property_id]]
		JOIN {{leafen_database_rows}} AS [[rows]]
			ON [[rows]].[[id]] = [[values]].[[row_id]] AND [[rows]].[[data_source_id]] = [[sprints]].[[sprints_data_source_id]]
		WHERE [[sprints]].[[sprints_data_source_id]] = {:dataSourceId}
			AND json_extract([[values]].[[value]], '$') IN ({:current}, {:next}, {:last})
		ORDER BY [[rows]].[[position]], [[rows]].[[id]]
	`).Bind(dbx.Params{
		"dataSourceId": dataSourceID,
		"current":      databaseSprintStatusPrefix + databaseSprintStatusCurrent,
		"next":         databaseSprintStatusPrefix + databaseSprintStatusNext,
		"last":         databaseSprintStatusPrefix + databaseSprintStatusLast,
	}).All(&rows)
	for _, row := range rows {
		place := strings.TrimPrefix(row.Status, databaseSprintStatusPrefix)
		byPlace[place] = append(byPlace[place], row.ID)
	}
	sprints.rows[dataSourceID] = byPlace
	return byPlace[which]
}

// relativeSprints reads a relation's relative sprints; nil for any other
// property.
func (context databaseQueryContext) relativeSprints(property storedDatabaseQueryProperty) func(string) []string {
	if property.Type != graphValueTypeRelation || context.Sprints == nil {
		return nil
	}
	target := databaseQueryParsedPropertyConfig(property).RelationDatabaseID
	return func(which string) []string { return context.Sprints.ids(target, which) }
}
